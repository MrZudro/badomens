package engines

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Este archivo contiene los proveedores Paper y Vanilla. Paper usa **Fill API
// v3** (la API v2 fue retirada y hoy devuelve 404; ver `docs/REGRESSION_MATRIX.md`
// API-03) y Vanilla usa el manifiesto oficial de Mojang.
//
// Reglas que aplican a ambos:
//   - Ningún fallo se silencia: si el dato es obligatorio (el build de Paper, el
//     manifiesto de Mojang), el error se propaga.
//   - Toda petición lleva RequestOpts{Provider, Operation} para que el
//     diagnóstico del transporte salga etiquetado.
//   - `base` es inyectable (pruebas con httptest); vacío = endpoint oficial.

// Operaciones lógicas de cada proveedor (etiqueta del diagnóstico).
const (
	paperOpListVersions     = "list_versions"
	paperOpResolveBuild     = "resolve_build"
	paperOpResolveVersion   = "resolve_version"
	vanillaOpListReleases   = "list_releases"
	vanillaOpListAll        = "list_all_versions"
	vanillaOpResolveVersion = "resolve_version"
)

// catalogContentError construye un error tipado del transporte para los fallos
// que detecta un proveedor al validar el CONTENIDO de una respuesta que, a
// nivel de transporte, fue correcta (esquema distinto, catálogo vacío, ausencia
// de un artefacto utilizable…). Usa el código estable, de forma que el
// consumidor decide con `CodeOf(err)`/`errors.Is(err, Err…)` y nunca mirando el
// texto. `url` puede ir vacío si el fallo es local (no hubo petición).
func catalogContentError(provider, code, operation, url string, meta Meta, cause error) *TransportError {
	return &TransportError{
		Code:       code,
		Provider:   provider,
		Operation:  operation,
		URL:        url,
		HTTPStatus: meta.Status,
		Attempts:   meta.Attempts,
		Err:        cause,
	}
}

// ---------------------------------------------------------------------------
// Paper (Fill API v3)
// ---------------------------------------------------------------------------

// paperFillBase es la raíz de Fill v3 para el proyecto Paper. Sustituye a
// `https://api.papermc.io/v2/projects/paper`, retirada.
const paperFillBase = "https://fill.papermc.io/v3/projects/paper"

const (
	// paperServerDownloadKey es la clave de descarga del artefacto de servidor
	// en Fill v3.
	paperServerDownloadKey = "server:default"
	// paperServerDownloadPrefix: cualquier otra clave de artefacto de servidor,
	// por si Fill añade variantes (se usa como respaldo documentado).
	paperServerDownloadPrefix = "server:"
	// paperBuildChannelStable es el canal de los builds publicados como estables.
	paperBuildChannelStable = "STABLE"
)

// paperProvider consulta el catálogo y los builds de PaperMC (Fill v3).
type paperProvider struct {
	engine Engine
	client *Client
	base   string
}

func (p *paperProvider) Engine() Engine { return p.engine }

// Base devuelve la raíz de Fill v3 (inyectable en las pruebas con httptest).
func (p *paperProvider) Base() string {
	if p.base != "" {
		return p.base
	}
	return paperFillBase
}

// --- Esquema de Fill v3 ---

// paperVersionsDoc es la respuesta de GET /v3/projects/paper/versions. A
// diferencia de la v2, `versions` es un array de OBJETOS (no de cadenas): la
// transformación a Version es explícita (ver ListVersions). Cada entrada trae
// además un array `builds` con los números publicados; no se usa para elegir
// build porque la fuente canónica es GET .../versions/{version}/builds.
type paperVersionsDoc struct {
	Versions []paperVersionEntry `json:"versions"`
}

// paperVersionEntry es una entrada del catálogo: la versión y sus builds.
type paperVersionEntry struct {
	Version paperVersionInfo `json:"version"`
}

// paperVersionInfo describe una versión de Paper.
type paperVersionInfo struct {
	// ID es el identificador de la versión ("1.21.4", "26.3").
	ID string `json:"id"`
	// Support es la ventana de soporte declarada por PaperMC. NO indica
	// estabilidad: una release antigua es UNSUPPORTED y sigue siendo estable.
	Support struct {
		// Status es SUPPORTED / UNSUPPORTED.
		Status string `json:"status"`
		// End es la fecha de fin de soporte ("" si el campo viene null).
		End string `json:"end"`
	} `json:"support"`
	// Java declara el Java mínimo que exige la versión (25 en 26.x).
	Java struct {
		Version struct {
			Minimum int `json:"minimum"`
		} `json:"version"`
	} `json:"java"`
}

// paperBuild es un elemento de GET /v3/projects/paper/versions/{version}/builds.
type paperBuild struct {
	// ID es el número de build (creciente con el tiempo dentro de la versión).
	ID int `json:"id"`
	// Time es la fecha de publicación del build (RFC3339).
	Time string `json:"time"`
	// Channel es el canal del build: ALPHA, BETA o STABLE.
	Channel string `json:"channel"`
	// Downloads son los artefactos del build por clave ("server:default"…).
	Downloads map[string]paperArtifact `json:"downloads"`
}

// paperArtifact es un artefacto descargable de un build.
type paperArtifact struct {
	Name      string            `json:"name"`
	Checksums map[string]string `json:"checksums"`
	Size      int64             `json:"size"`
	URL       string            `json:"url"`
}

// ListVersions devuelve el catálogo de Paper. Fill v3 lo entrega de la versión
// más nueva a la más antigua; aquí se invierte para respetar el contrato interno
// (ascendente, "la última es la última estable") que asume pickVersion.
//
// Las pre-releases (-pre, -rc) se devuelven con canal beta y Stable=false: el
// catálogo público (filter) las descarta salvo que se pidan inestables.
//
// Limitación conocida: /versions no expone fecha de publicación, así que
// ReleasedAt queda a cero; la fecha solo existe por build (paperBuild.Time).
func (p *paperProvider) ListVersions(ctx context.Context) ([]Version, error) {
	endpoint := p.Base() + "/versions"
	var doc paperVersionsDoc
	meta, err := p.client.GetJSONWithMeta(ctx, endpoint, &doc,
		RequestOpts{Provider: p.engine.Name, Operation: paperOpListVersions})
	if err != nil {
		return nil, err
	}
	if len(doc.Versions) == 0 {
		return nil, catalogContentError(p.engine.Name, CodeSchemaMismatch, paperOpListVersions, endpoint, meta,
			fmt.Errorf("el catálogo no contiene entradas de versión"))
	}
	versions := make([]Version, 0, len(doc.Versions))
	for i := len(doc.Versions) - 1; i >= 0; i-- {
		info := doc.Versions[i].Version
		id := strings.TrimSpace(info.ID)
		if id == "" {
			return nil, catalogContentError(p.engine.Name, CodeSchemaMismatch, paperOpListVersions, endpoint, meta,
				fmt.Errorf("hay una entrada de versión sin id"))
		}
		channel, stable := paperVersionChannel(id)
		versions = append(versions, Version{
			Version: id,
			Stable:  stable,
			Channel: channel,
			Detail:  paperSupportDetail(info.Support.Status, info.Support.End),
		})
	}
	return versions, nil
}

// Resolve resuelve una versión (vacía = última estable) a los pines que usará
// el runtime. Una versión de Paper NO queda resuelta hasta tener el pin exacto
// del build (`PAPER_BUILD`): si no hay un build estable con artefacto de
// servidor descargable, se devuelve error y nunca una resolución parcial.
func (p *paperProvider) Resolve(ctx context.Context, version string) (Resolved, error) {
	list, err := p.ListVersions(ctx)
	if err != nil {
		return Resolved{}, err
	}
	v, err := pickVersion(list, version)
	if err != nil {
		if version == "" {
			return Resolved{}, err
		}
		// La versión pedida no existe: fallo local (sin petición asociada, por
		// eso no se rellena URL), tipado para que el consumidor lo clasifique.
		return Resolved{}, catalogContentError(p.engine.Name, CodeNotFound, paperOpResolveVersion, "", Meta{}, err)
	}
	build, err := p.stableBuild(ctx, v.Version)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{
		Version: v.Version,
		Pins:    map[string]string{p.engine.DepVar: strconv.Itoa(build.ID)},
		Detail:  fmt.Sprintf("build %d (%s)", build.ID, build.Channel),
	}, nil
}

// stableBuild elige, para una versión concreta, el build ESTABLE más reciente
// que exponga un artefacto de servidor descargable. Se descartan los builds
// experimentales (ALPHA/BETA) y los que no exponen descarga. El error se
// propaga siempre: sin build verificable no hay resolución.
func (p *paperProvider) stableBuild(ctx context.Context, version string) (paperBuild, error) {
	endpoint := p.buildsURL(version)
	var builds []paperBuild
	meta, err := p.client.GetJSONWithMeta(ctx, endpoint, &builds,
		RequestOpts{Provider: p.engine.Name, Operation: paperOpResolveBuild})
	if err != nil {
		return paperBuild{}, err
	}
	if len(builds) == 0 {
		return paperBuild{}, catalogContentError(p.engine.Name, CodeNotFound, paperOpResolveBuild, endpoint, meta,
			fmt.Errorf("la versión %s no tiene builds publicados", version))
	}
	build, ok := pickStableBuild(builds)
	if !ok {
		return paperBuild{}, catalogContentError(p.engine.Name, CodeNotFound, paperOpResolveBuild, endpoint, meta,
			fmt.Errorf("la versión %s no tiene ningún build estable con artefacto de servidor descargable", version))
	}
	return build, nil
}

// buildsURL compone la URL de los builds de una versión escapando el segmento.
func (p *paperProvider) buildsURL(version string) string {
	return p.Base() + "/versions/" + url.PathEscape(version) + "/builds"
}

// pickStableBuild devuelve el build estable más reciente con descarga de
// servidor utilizable. El número de build crece con el tiempo dentro de una
// versión (Fill devuelve la lista descendente), así que el mayor ID estable es
// el más reciente.
func pickStableBuild(builds []paperBuild) (paperBuild, bool) {
	var best paperBuild
	found := false
	for _, b := range builds {
		if !strings.EqualFold(strings.TrimSpace(b.Channel), paperBuildChannelStable) {
			continue
		}
		if _, ok := serverArtifact(b.Downloads); !ok {
			continue
		}
		if !found || b.ID > best.ID {
			best, found = b, true
		}
	}
	return best, found
}

// serverArtifact devuelve el artefacto de servidor descargable de un build. Se
// prefiere la clave documentada `server:default`; si no está, se busca cualquier
// clave `server:*` en orden estable.
func serverArtifact(downloads map[string]paperArtifact) (paperArtifact, bool) {
	if artifact, ok := downloads[paperServerDownloadKey]; ok && artifact.URL != "" {
		return artifact, true
	}
	keys := make([]string, 0, len(downloads))
	for key := range downloads {
		if strings.HasPrefix(key, paperServerDownloadPrefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		if artifact := downloads[key]; artifact.URL != "" {
			return artifact, true
		}
	}
	return paperArtifact{}, false
}

// paperVersionChannel clasifica una versión de Paper por su identificador: los
// sufijos de pre-release (-pre, -rc) se marcan como canal beta y no estables,
// para que el catálogo público no ofrezca versiones sin liberar. Fill no expone
// un canal por versión en /versions, así que la convención de nombre es el único
// dato disponible (confirmado contra el catálogo real: solo hay -preN y -rcN).
func paperVersionChannel(id string) (Channel, bool) {
	lower := strings.ToLower(id)
	if strings.Contains(lower, "-pre") || strings.Contains(lower, "-rc") {
		return ChannelBeta, false
	}
	return ChannelRelease, true
}

// paperSupportDetail describe la ventana de soporte declarada por Paper.
func paperSupportDetail(status, end string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return ""
	}
	if end = strings.TrimSpace(end); end != "" {
		return status + " hasta " + end
	}
	return status
}

// ---------------------------------------------------------------------------
// Vanilla (manifiesto oficial de Mojang)
// ---------------------------------------------------------------------------

// vanillaManifestURL es el manifiesto oficial de versiones de Minecraft.
const vanillaManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

// vanillaProvider consulta el manifiesto de versiones de Mojang.
type vanillaProvider struct {
	engine Engine
	client *Client
	base   string
}

func (p *vanillaProvider) Engine() Engine { return p.engine }

// Base devuelve la URL del manifiesto (inyectable en las pruebas con httptest).
func (p *vanillaProvider) Base() string {
	if p.base != "" {
		return p.base
	}
	return vanillaManifestURL
}

// manifest es el manifiesto oficial. `latest` se conserva como parte del
// contrato y para diagnóstico; la resolución se apoya en el orden de `versions`.
type manifest struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []manifestEntry `json:"versions"`
}

// manifestEntry es una entrada del manifiesto.
type manifestEntry struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	ReleaseTime string `json:"releaseTime"`
}

// fetchManifest descarga y valida el manifiesto con trazabilidad. Devuelve
// además los metadatos de la respuesta para poder clasificar los fallos de
// contenido con el estado HTTP real.
func (p *vanillaProvider) fetchManifest(ctx context.Context, operation string) (manifest, Meta, error) {
	var m manifest
	meta, err := p.client.GetJSONWithMeta(ctx, p.Base(), &m,
		RequestOpts{Provider: p.engine.Name, Operation: operation})
	if err != nil {
		return manifest{}, meta, err
	}
	if len(m.Versions) == 0 {
		return manifest{}, meta, catalogContentError(p.engine.Name, CodeSchemaMismatch, operation, p.Base(), meta,
			fmt.Errorf("el manifiesto no contiene versiones"))
	}
	return m, meta, nil
}

// normalize transforma las entradas del manifiesto en versiones del catálogo,
// conservando el orden del manifiesto (de la más nueva a la más antigua) y
// rellenando canal y fecha.
func (p *vanillaProvider) normalize(m manifest, operation string, meta Meta) ([]Version, error) {
	versions := make([]Version, 0, len(m.Versions))
	for _, entry := range m.Versions {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			return nil, catalogContentError(p.engine.Name, CodeSchemaMismatch, operation, p.Base(), meta,
				fmt.Errorf("hay una entrada del manifiesto sin id"))
		}
		releasedAt, err := parseReleaseTime(entry.ReleaseTime)
		if err != nil {
			return nil, catalogContentError(p.engine.Name, CodeSchemaMismatch, operation, p.Base(), meta,
				fmt.Errorf("la fecha %q de %s no es RFC3339: %w", entry.ReleaseTime, id, err))
		}
		channel, stable := vanillaChannel(entry.Type)
		versions = append(versions, Version{
			Version:    id,
			Stable:     stable,
			Channel:    channel,
			ReleasedAt: releasedAt,
		})
	}
	return versions, nil
}

// vanillaChannel traduce el tipo de entrada del manifiesto al canal del
// catálogo. Tipos oficiales: release, snapshot, old_beta y old_alpha.
func vanillaChannel(kind string) (Channel, bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "release":
		return ChannelRelease, true
	case "old_beta":
		return ChannelBeta, false
	case "old_alpha":
		return ChannelAlpha, false
	default:
		// snapshot y cualquier tipo nuevo desconocido: no estable.
		return ChannelSnapshot, false
	}
}

// parseReleaseTime interpreta `releaseTime` del manifiesto (RFC3339 con offset,
// p. ej. "2026-09-13T12:05:46+00:00"). Una fecha presente pero malformada es un
// fallo de esquema, no un cero silencioso; una fecha vacía se acepta como
// desconocida (las entradas de prueba antiguas pueden no traerla).
func parseReleaseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, s)
}

// ListVersions devuelve solo las releases de Minecraft (contrato histórico) en
// orden ascendente (antiguo → nuevo). El manifiesto llega al revés, así que se
// invierte para que `pickVersion` ("última = última estable") sea correcto.
func (p *vanillaProvider) ListVersions(ctx context.Context) ([]Version, error) {
	m, meta, err := p.fetchManifest(ctx, vanillaOpListReleases)
	if err != nil {
		return nil, err
	}
	all, err := p.normalize(m, vanillaOpListReleases, meta)
	if err != nil {
		return nil, err
	}
	newest := make([]Version, 0, len(all))
	for _, v := range all {
		if v.Stable {
			newest = append(newest, v)
		}
	}
	if len(newest) == 0 {
		return nil, catalogContentError(p.engine.Name, CodeNotFound, vanillaOpListReleases, p.Base(), meta,
			fmt.Errorf("el manifiesto no contiene releases"))
	}
	versions := make([]Version, 0, len(newest))
	for i := len(newest) - 1; i >= 0; i-- {
		versions = append(versions, newest[i])
	}
	return versions, nil
}

// Resolve resuelve una versión (vacía = última release) a los pines del motor.
// Vanilla no necesita pines extra (la versión identifica la imagen de itzg).
func (p *vanillaProvider) Resolve(ctx context.Context, version string) (Resolved, error) {
	list, err := p.ListVersions(ctx)
	if err != nil {
		return Resolved{}, err
	}
	v, err := pickVersion(list, version)
	if err != nil {
		if version == "" {
			return Resolved{}, err
		}
		// Versión pedida inexistente: fallo local tipado (sin petición asociada).
		return Resolved{}, catalogContentError(p.engine.Name, CodeNotFound, vanillaOpResolveVersion, "", Meta{}, err)
	}
	return Resolved{Version: v.Version, Pins: map[string]string{}}, nil
}

// ListAllVersions devuelve releases y snapshots con su canal y fecha, ordenados
// de antiguo a nuevo por fecha de publicación. Es la entrada del catálogo con
// canales (RF-5); ListVersions se mantiene intacta para no romper el contrato.
func (p *vanillaProvider) ListAllVersions(ctx context.Context) ([]Version, error) {
	m, meta, err := p.fetchManifest(ctx, vanillaOpListAll)
	if err != nil {
		return nil, err
	}
	versions, err := p.normalize(m, vanillaOpListAll, meta)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(versions, func(i, j int) bool {
		if versions[i].ReleasedAt.IsZero() || versions[j].ReleasedAt.IsZero() {
			return versions[i].Version < versions[j].Version
		}
		return versions[i].ReleasedAt.Before(versions[j].ReleasedAt)
	})
	return versions, nil
}

// pickVersion elige la versión pedida o la última estable. Devuelve error si la
// versión pedida no existe.
func pickVersion(list []Version, version string) (Version, error) {
	if version != "" {
		for i := len(list) - 1; i >= 0; i-- {
			if list[i].Version == version {
				return list[i], nil
			}
		}
		return Version{}, fmt.Errorf("la versión %q no existe para este motor", version)
	}
	// Última estable.
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].Stable {
			return list[i], nil
		}
	}
	if len(list) == 0 {
		return Version{}, fmt.Errorf("el catálogo no devolvió versiones")
	}
	return list[len(list)-1], nil
}
