package modrinth

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/plugins/marketplace"
)

// Search implementa marketplace.Source: busca proyectos por texto y, si la
// consulta lo pide, filtra por entorno.
//
// La compatibilidad de cada resultado se calcula SIEMPRE con los metadatos que
// devuelve el proveedor. Como la búsqueda de Modrinth no publica loaders, el
// resultado de una búsqueda suele quedar en Unknown: es lo correcto, porque un
// resultado no demuestra compatibilidad.
func (c *Client) Search(ctx context.Context, q marketplace.SearchQuery) (marketplace.SearchResult, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	facets, err := searchFacets(q)
	if err != nil {
		return marketplace.SearchResult{}, err
	}
	endpoint, err := c.endpoint("/search", map[string]string{
		"query":  strings.TrimSpace(q.Text),
		"facets": facets,
		"limit":  strconv.Itoa(limit),
		"offset": strconv.Itoa(offset),
	})
	if err != nil {
		return marketplace.SearchResult{}, err
	}

	var doc searchDoc
	if _, err := c.HTTP.GetJSONWithMeta(ctx, endpoint, &doc, c.opts("search_projects")); err != nil {
		return marketplace.SearchResult{}, marketplace.WrapTransport(ID, "search_projects", err)
	}
	if doc.Hits == nil || doc.TotalHits == nil {
		return marketplace.SearchResult{}, marketplace.InvalidResponse(ID, "search_projects",
			"faltan las claves hits/total_hits")
	}
	out := marketplace.SearchResult{Total: *doc.TotalHits, Offset: doc.Offset, Limit: doc.Limit}
	if out.Limit == 0 {
		out.Limit = limit
	}
	for _, h := range *doc.Hits {
		p := marketplace.Project{
			Source:      ID,
			ID:          h.ProjectID,
			Slug:        h.Slug,
			Title:       h.Title,
			Description: h.Description,
			Author:      h.Author,
			URL:         projectURL(h.Slug),
			Kind:        kind(h.ProjectType),
			// Deliberadamente NO se copian las categories a Loaders: Modrinth
			// mezcla loaders y etiquetas en ese campo, y usarlo como prueba de
			// compatibilidad sería afirmar algo que el proveedor no dice.
			GameVersions: marketplace.NormalizeDeclared(h.Versions),
			Categories:   marketplace.NormalizeDeclared(h.Categories),
			Downloads:    h.Downloads,
			IconURL:      h.IconURL,
			License:      h.License,
		}
		p.Compat, p.CompatReason = p.Compatibility(q.Env)
		out.Projects = append(out.Projects, p)
	}
	return out, nil
}

// Project implementa marketplace.Source: obtiene un proyecto por slug o id.
func (c *Client) Project(ctx context.Context, ref string) (*marketplace.Project, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errx.New(errx.CodeUsage, "falta el proyecto de Modrinth",
			"indica el slug o el identificador del proyecto").WithDetail("reason", "proyecto_vacio")
	}
	endpoint, err := c.endpoint("/project/"+url.PathEscape(ref), nil)
	if err != nil {
		return nil, err
	}
	var doc projectDoc
	if _, err := c.HTTP.GetJSONWithMeta(ctx, endpoint, &doc, c.opts("get_project")); err != nil {
		return nil, marketplace.WrapTransport(ID, "get_project", err)
	}
	if doc.ID == nil || doc.Slug == nil || doc.ProjectType == nil {
		return nil, marketplace.InvalidResponse(ID, "get_project",
			"faltan las claves id/slug/project_type")
	}
	p := &marketplace.Project{
		Source:       ID,
		ID:           *doc.ID,
		Slug:         *doc.Slug,
		Title:        doc.Title,
		Description:  doc.Description,
		URL:          projectURL(*doc.Slug),
		Kind:         kind(*doc.ProjectType),
		Loaders:      marketplace.NormalizeDeclared(doc.Loaders),
		GameVersions: marketplace.NormalizeDeclared(doc.GameVersions),
		Downloads:    doc.Downloads,
		IconURL:      doc.IconURL,
		License:      licenseLabel(doc),
		Status:       doc.Status,
		Published:    projectStatus(doc.Status),
	}
	return p, nil
}

// Versions implementa marketplace.Source: lista las versiones de un proyecto
// filtrando en el servidor por loader y versión de Minecraft, que es el filtro
// que Modrinth sí soporta de forma nativa.
func (c *Client) Versions(ctx context.Context, projectID string, f marketplace.VersionFilter) ([]marketplace.Version, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errx.New(errx.CodeUsage, "falta el identificador del proyecto",
			"indica el id o el slug del proyecto").WithDetail("reason", "proyecto_vacio")
	}
	params := map[string]string{}
	if l := marketplace.NormalizeDeclared(f.Loaders); len(l) > 0 {
		raw, err := jsonArray(l)
		if err != nil {
			return nil, err
		}
		params["loaders"] = raw
	}
	if g := marketplace.NormalizeDeclared(f.GameVersions); len(g) > 0 {
		raw, err := jsonArray(g)
		if err != nil {
			return nil, err
		}
		params["game_versions"] = raw
	}
	endpoint, err := c.endpoint("/project/"+url.PathEscape(projectID)+"/version", params)
	if err != nil {
		return nil, err
	}
	var docs []versionDoc
	if _, err := c.HTTP.GetJSONWithMeta(ctx, endpoint, &docs, c.opts("list_project_versions")); err != nil {
		return nil, marketplace.WrapTransport(ID, "list_project_versions", err)
	}
	limit := f.Limit
	if limit <= 0 || limit > MaxVersionResults {
		limit = MaxVersionResults
	}
	out := make([]marketplace.Version, 0, len(docs))
	for i := range docs {
		v, verr := versionFromDoc(&docs[i])
		if verr != nil {
			return nil, verr
		}
		out = append(out, v)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// endpoint arma una URL absoluta con los parámetros no vacíos.
func (c *Client) endpoint(path string, params map[string]string) (string, error) {
	u, err := url.Parse(c.baseURL() + path)
	if err != nil {
		return "", errx.New(errx.CodeUsage, "la URL base de Modrinth no es válida", "revisa la configuración").
			WithDetail("reason", "url_invalida")
	}
	if len(params) > 0 {
		values := u.Query()
		for k, v := range params {
			if strings.TrimSpace(v) != "" {
				values.Set(k, v)
			}
		}
		u.RawQuery = values.Encode()
	}
	return u.String(), nil
}

// searchFacets construye el parámetro `facets`. El orden recomendado por la API
// es tipo de proyecto, versiones de Minecraft y categorías.
func searchFacets(q marketplace.SearchQuery) (string, error) {
	var groups [][]string
	if k := strings.TrimSpace(string(q.Kind)); k != "" && q.Kind != marketplace.KindUnknown {
		groups = append(groups, []string{"project_type:" + k})
	}
	if q.FilterEnv {
		if mc := strings.TrimSpace(q.Env.GameVersion); mc != "" {
			groups = append(groups, []string{"versions:" + mc})
		}
		if l := strings.TrimSpace(q.Env.Loader); l != "" {
			groups = append(groups, []string{"categories:" + l})
		}
	}
	if len(groups) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(groups)
	if err != nil {
		return "", errx.New(errx.CodeUsage, "no se pudo construir el filtro de búsqueda", "").
			WithDetail("reason", "filtro_invalido")
	}
	return string(raw), nil
}

// jsonArray serializa una lista como array JSON, que es el formato que Modrinth
// exige para loaders y game_versions.
func jsonArray(values []string) (string, error) {
	raw, err := json.Marshal(values)
	if err != nil {
		return "", errx.New(errx.CodeUsage, "no se pudo construir el filtro de versiones", "").
			WithDetail("reason", "filtro_invalido")
	}
	return string(raw), nil
}

// projectURL devuelve la página pública del proyecto.
func projectURL(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	return "https://modrinth.com/project/" + slug
}

// licenseLabel compone la licencia declarada.
func licenseLabel(doc projectDoc) string {
	if doc.License.ID != "" {
		return doc.License.ID
	}
	return doc.License.Name
}

// versionFromDoc traduce una versión del esquema de Modrinth al dominio.
func versionFromDoc(d *versionDoc) (marketplace.Version, error) {
	if d.ID == nil || d.ProjectID == nil || d.VersionNumber == nil ||
		d.Loaders == nil || d.GameVersions == nil || d.Files == nil {
		return marketplace.Version{}, marketplace.InvalidResponse(ID, "list_project_versions",
			"falta alguna de las claves id/project_id/version_number/loaders/game_versions/files")
	}
	v := marketplace.Version{
		Source:        ID,
		ProjectID:     *d.ProjectID,
		ID:            *d.ID,
		Name:          d.Name,
		Number:        *d.VersionNumber,
		Channel:       channel(d.VersionType),
		Loaders:       marketplace.NormalizeDeclared(*d.Loaders),
		GameVersions:  marketplace.NormalizeDeclared(*d.GameVersions),
		Status:        d.Status,
		Published:     versionStatus(d.Status),
		Downloads:     d.Downloads,
		DatePublished: d.DatePublished,
	}
	for _, f := range *d.Files {
		v.Files = append(v.Files, marketplace.File{
			Name:    f.Filename,
			URL:     f.URL,
			Size:    f.Size,
			Primary: f.Primary,
			Hashes:  hashes(f.Hashes),
		})
	}
	for _, dep := range d.Dependencies {
		v.Dependencies = append(v.Dependencies, marketplace.Dependency{
			ProjectID: deref(dep.ProjectID),
			VersionID: deref(dep.VersionID),
			Name:      deref(dep.FileName),
			Kind:      dependencyKind(dep.DependencyType),
		})
	}
	return v, nil
}

// hashes traduce el mapa de checksums publicado, conservando solo los
// algoritmos conocidos. Modrinth publica sha512 y sha1 (nunca sha256).
func hashes(raw map[string]string) []marketplace.Hash {
	if len(raw) == 0 {
		return nil
	}
	var out []marketplace.Hash
	for _, algo := range []string{marketplace.HashSHA256, marketplace.HashSHA512, marketplace.HashSHA1} {
		if v := strings.TrimSpace(raw[algo]); v != "" {
			out = append(out, marketplace.Hash{Algorithm: algo, Value: strings.ToLower(v)})
		}
	}
	return out
}

// channel traduce el version_type de Modrinth. Un tipo desconocido NO se trata
// como estable: se marca como "desconocido" para que el resolvedor lo excluya
// salvo que se pidan versiones preliminares.
func channel(versionType string) string {
	switch strings.ToLower(strings.TrimSpace(versionType)) {
	case "release", "beta", "alpha":
		return strings.ToLower(strings.TrimSpace(versionType))
	default:
		return "desconocido"
	}
}
