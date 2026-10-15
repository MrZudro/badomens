package hangar

import (
	"context"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/plugins/marketplace"
)

// platformPreference es el orden con el que se elige el artefacto principal
// cuando la consulta no pide una plataforma concreta. Paper primero: es la
// plataforma que badomens arranca.
var platformPreference = []string{PlatformPaper, PlatformFolia, PlatformWaterfall, PlatformVelocity, PlatformBungee}

// Search implementa marketplace.Source. Hangar busca con `q` y, si la consulta lo
// pide, acota por plataforma con `platform`. El filtro por versión de Minecraft
// NO existe en la búsqueda de Hangar: por eso los resultados declaran
// plataformas pero no versiones de Minecraft, y su compatibilidad queda en
// Unknown hasta consultar las versiones.
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
	params := map[string]string{
		"q":      strings.TrimSpace(q.Text),
		"limit":  strconv.Itoa(limit),
		"offset": strconv.Itoa(offset),
	}
	if q.FilterEnv {
		if p := platformFor(envName(q.Env)); p != "" {
			params["platform"] = p
		}
	}
	endpoint, err := c.endpoint("/projects", params)
	if err != nil {
		return marketplace.SearchResult{}, err
	}
	var page projectsPage
	if _, err := c.HTTP.GetJSONWithMeta(ctx, endpoint, &page, c.opts("search_projects")); err != nil {
		return marketplace.SearchResult{}, marketplace.WrapTransport(ID, "search_projects", err)
	}
	if page.Pagination == nil || page.Result == nil {
		return marketplace.SearchResult{}, marketplace.InvalidResponse(ID, "search_projects",
			"faltan las claves pagination/result")
	}
	out := marketplace.SearchResult{
		Total:  page.Pagination.Count,
		Offset: page.Pagination.Offset,
		Limit:  page.Pagination.Limit,
	}
	if out.Limit == 0 {
		out.Limit = limit
	}
	for i := range *page.Result {
		p, perr := projectFromDoc(&(*page.Result)[i])
		if perr != nil {
			return marketplace.SearchResult{}, perr
		}
		p.Compat, p.CompatReason = p.Compatibility(q.Env)
		out.Projects = append(out.Projects, *p)
	}
	return out, nil
}

// Project implementa marketplace.Source. Hangar identifica el proyecto por su
// slug exacto (distingue mayúsculas: EssentialsX, WorldEdit).
func (c *Client) Project(ctx context.Context, ref string) (*marketplace.Project, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errx.New(errx.CodeUsage, "falta el proyecto de Hangar",
			"indica el slug del proyecto").WithDetail("reason", "proyecto_vacio")
	}
	endpoint, err := c.endpoint("/projects/"+url.PathEscape(ref), nil)
	if err != nil {
		return nil, err
	}
	var doc projectDoc
	if _, err := c.HTTP.GetJSONWithMeta(ctx, endpoint, &doc, c.opts("get_project")); err != nil {
		return nil, marketplace.WrapTransport(ID, "get_project", err)
	}
	return projectFromDoc(&doc)
}

// Versions implementa marketplace.Source. La API de Hangar NO filtra por
// plataforma ni por versión de Minecraft en este punto: se pagina y se filtra
// aquí, sobre los datos que cada versión declara.
func (c *Client) Versions(ctx context.Context, projectID string, f marketplace.VersionFilter) ([]marketplace.Version, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errx.New(errx.CodeUsage, "falta el identificador del proyecto",
			"indica el slug del proyecto").WithDetail("reason", "proyecto_vacio")
	}
	platforms := platformsFor(f.Loaders)
	game := marketplace.NormalizeDeclared(f.GameVersions)
	want := f.Limit
	if want <= 0 {
		want = PageLimit
	}

	var out []marketplace.Version
	offset := 0
	for page := 0; page < MaxPages; page++ {
		endpoint, err := c.endpoint("/projects/"+url.PathEscape(projectID)+"/versions", map[string]string{
			"limit":  strconv.Itoa(PageLimit),
			"offset": strconv.Itoa(offset),
		})
		if err != nil {
			return nil, err
		}
		var doc versionsPage
		if _, err := c.HTTP.GetJSONWithMeta(ctx, endpoint, &doc, c.opts("list_project_versions")); err != nil {
			return nil, marketplace.WrapTransport(ID, "list_project_versions", err)
		}
		if doc.Pagination == nil || doc.Result == nil {
			return nil, marketplace.InvalidResponse(ID, "list_project_versions",
				"faltan las claves pagination/result")
		}
		for i := range *doc.Result {
			v, ok, verr := versionFromDoc(&(*doc.Result)[i], platforms, game)
			if verr != nil {
				return nil, verr
			}
			if !ok {
				continue
			}
			out = append(out, v)
			if len(out) >= want {
				return out, nil
			}
		}
		offset += PageLimit
		if len(*doc.Result) == 0 || offset >= doc.Pagination.Count {
			break
		}
	}
	return out, nil
}

// endpoint arma una URL absoluta con los parámetros no vacíos.
func (c *Client) endpoint(p string, params map[string]string) (string, error) {
	u, err := url.Parse(c.baseURL() + p)
	if err != nil {
		return "", errx.New(errx.CodeUsage, "la URL base de Hangar no es válida", "revisa la configuración").
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

// projectURL devuelve la página pública del proyecto.
func projectURL(owner, slug string) string {
	if strings.TrimSpace(slug) == "" {
		return ""
	}
	u := "https://hangar.papermc.io/"
	if o := strings.TrimSpace(owner); o != "" {
		u += o + "/"
	}
	return u + slug
}

// envName devuelve el motor o, si no lo hay, el loader del entorno.
func envName(env marketplace.Environment) string {
	if e := strings.TrimSpace(env.Engine); e != "" {
		return e
	}
	return strings.TrimSpace(env.Loader)
}

// projectFromDoc traduce un proyecto del esquema de Hangar al dominio.
func projectFromDoc(d *projectDoc) (*marketplace.Project, error) {
	if d.ID == nil || d.Name == nil || d.Namespace == nil {
		return nil, marketplace.InvalidResponse(ID, "get_project",
			"faltan las claves id/name/namespace")
	}
	loaders := sortedKeys(d.SupportedPlatforms)
	p := &marketplace.Project{
		Source:       ID,
		ID:           number(d.ID),
		Slug:         d.Namespace.Slug,
		Title:        *d.Name,
		Description:  d.Description,
		Author:       d.Namespace.Owner,
		URL:          projectURL(d.Namespace.Owner, d.Namespace.Slug),
		Kind:         marketplace.KindPlugin,
		Loaders:      loaders,
		GameVersions: unionPlatformVersions(d.SupportedPlatforms, loaders),
		Categories:   categories(d),
		IconURL:      d.AvatarURL,
		License:      licenseLabel(d),
		Status:       statusLabel(d.Visibility, ""),
		Published:    published(d.Visibility),
	}
	if d.Stats != nil {
		p.Downloads = d.Stats.Downloads
	}
	return p, nil
}

// categories devuelve la categoría y las palabras clave declaradas.
func categories(d *projectDoc) []string {
	out := marketplace.NormalizeDeclared([]string{d.Category})
	if d.Settings != nil {
		out = append(out, marketplace.NormalizeDeclared(d.Settings.Keywords)...)
	}
	return out
}

// licenseLabel compone la licencia declarada.
func licenseLabel(d *projectDoc) string {
	if d.Settings == nil {
		return ""
	}
	if d.Settings.License.Type != "" {
		return d.Settings.License.Type
	}
	return d.Settings.License.Name
}

// unionPlatformVersions devuelve la unión de las versiones de Minecraft
// declaradas por las plataformas indicadas.
func unionPlatformVersions(platforms map[string][]string, keys []string) []string {
	var all []string
	for _, k := range keys {
		all = append(all, platforms[k]...)
	}
	return marketplace.NormalizeDeclared(all)
}

// versionFromDoc traduce una versión del esquema de Hangar al dominio.
//
// Devuelve ok=false cuando la versión no declara ninguna de las plataformas
// pedidas o ninguna de las versiones de Minecraft pedidas: en ese caso NO se
// devuelve al llamador, porque no es una opción válida para ese entorno.
//
// Si la consulta pide una plataforma, `GameVersions` se acota a las versiones
// que esa versión declara PARA ESA plataforma (no a la unión de todas), para no
// afirmar que soporta una versión de Minecraft que solo declara en otra
// plataforma.
func versionFromDoc(d *versionDoc, platforms, game []string) (marketplace.Version, bool, error) {
	if d.ID == nil || d.Name == nil || d.PlatformDependencies == nil {
		return marketplace.Version{}, false, marketplace.InvalidResponse(ID, "list_project_versions",
			"faltan las claves id/name/platformDependencies")
	}
	declared := sortedKeys(d.PlatformDependencies)
	matched := intersect(platforms, declared)
	if len(platforms) > 0 && len(matched) == 0 {
		return marketplace.Version{}, false, nil
	}
	if len(matched) == 0 {
		matched = declared
	}

	versions := unionPlatformVersions(d.PlatformDependencies, matched)
	if len(game) > 0 && len(intersect(game, versions)) == 0 {
		return marketplace.Version{}, false, nil
	}

	v := marketplace.Version{
		Source:        ID,
		ProjectID:     number(d.ProjectID),
		ID:            number(d.ID),
		Name:          *d.Name,
		Number:        *d.Name,
		Channel:       channelName(d.Channel),
		Loaders:       declared,
		GameVersions:  versions,
		Status:        statusLabel(d.Visibility, d.ReviewState),
		Published:     published(d.Visibility),
		DatePublished: d.CreatedAt,
	}
	if d.Stats != nil {
		v.Downloads = d.Stats.TotalDownloads
	}

	primary := primaryPlatform(d.Downloads, platforms)
	for _, p := range sortedKeys(d.Downloads) {
		if len(platforms) > 0 && !contains(matched, p) {
			continue
		}
		dl := d.Downloads[p]
		f := marketplace.File{
			Name:     fileName(dl, p),
			URL:      downloadURL(dl),
			Primary:  p == primary,
			External: strings.TrimSpace(dl.DownloadURL) == "",
		}
		if dl.FileInfo != nil {
			f.Size = dl.FileInfo.SizeBytes
			if h := strings.TrimSpace(dl.FileInfo.SHA256Hash); h != "" {
				f.Hashes = []marketplace.Hash{{Algorithm: marketplace.HashSHA256, Value: strings.ToLower(h)}}
			}
		}
		if f.URL == "" {
			continue
		}
		v.Files = append(v.Files, f)
	}
	v.Dependencies = dependencies(d, matched)
	return v, true, nil
}

// dependencies traduce las dependencias declaradas para las plataformas
// indicadas, sin duplicados por nombre.
func dependencies(d *versionDoc, platforms []string) []marketplace.Dependency {
	seen := map[string]bool{}
	var out []marketplace.Dependency
	for _, p := range sortedKeys(d.PluginDependencies) {
		if len(platforms) > 0 && !contains(platforms, p) {
			continue
		}
		for _, dep := range d.PluginDependencies[p] {
			name := strings.TrimSpace(dep.Name)
			key := strings.ToLower(name) + "|" + number(dep.ProjectID)
			if seen[key] {
				continue
			}
			seen[key] = true
			kind := marketplace.DepOptional
			if dep.Required {
				kind = marketplace.DepRequired
			}
			out = append(out, marketplace.Dependency{
				ProjectID: number(dep.ProjectID),
				Name:      name,
				Kind:      kind,
			})
		}
	}
	return out
}

// primaryPlatform elige la plataforma del artefacto principal: la primera
// plataforma pedida que tenga archivo o, si no se pidió ninguna, la primera por
// orden de preferencia.
func primaryPlatform(downloads map[string]platformDownload, platforms []string) string {
	available := func(p string) bool { return strings.TrimSpace(downloadURL(downloads[p])) != "" }
	for _, p := range platforms {
		if available(p) {
			return p
		}
	}
	for _, p := range platformPreference {
		if available(p) {
			return p
		}
	}
	for _, p := range sortedKeys(downloads) {
		if available(p) {
			return p
		}
	}
	return ""
}

// downloadURL devuelve la URL preferida del artefacto: la alojada por Hangar si
// existe y, si no, la externa (que el usuario tendrá que confiar en descargar).
func downloadURL(dl platformDownload) string {
	if u := strings.TrimSpace(dl.DownloadURL); u != "" {
		return u
	}
	return strings.TrimSpace(dl.ExternalURL)
}

// fileName devuelve el nombre del archivo: el declarado por Hangar o, si no lo
// hay (descarga externa), el último segmento de la URL.
func fileName(dl platformDownload, platform string) string {
	if dl.FileInfo != nil {
		if n := strings.TrimSpace(dl.FileInfo.Name); n != "" {
			return n
		}
	}
	if u, err := url.Parse(downloadURL(dl)); err == nil {
		if base := path.Base(u.Path); base != "" && base != "." && base != "/" {
			return base
		}
	}
	return strings.ToLower(platform) + ".jar"
}

// intersect devuelve los elementos de list que están en want, sin distinguir
// mayúsculas: Hangar declara las plataformas en mayúsculas (PAPER) y badomens
// usa minúsculas (paper).
func intersect(want, list []string) []string {
	var out []string
	for _, w := range want {
		for _, l := range list {
			if strings.EqualFold(strings.TrimSpace(w), strings.TrimSpace(l)) {
				out = append(out, l)
				break
			}
		}
	}
	return out
}

// contains informa si la lista contiene la etiqueta, sin distinguir mayúsculas.
func contains(list []string, want string) bool {
	return len(intersect([]string{want}, list)) > 0
}
