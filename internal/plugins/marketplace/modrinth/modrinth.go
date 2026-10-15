// Package modrinth implementa el adaptador de la API pública v2 de Modrinth
// (https://docs.modrinth.com/api) para el catálogo de marketplaces.
//
// Tiene su PROPIO parser y sus propios tipos de transporte (searchDoc,
// projectDoc, versionDoc): no comparte decodificación con Hangar ni con ningún
// otro proveedor (T-06). Solo comparte los tipos de dominio de
// internal/plugins/marketplace y el transporte HTTP de internal/engines.
//
// Modrinth exige un User-Agent identificable con vía de contacto: el cliente
// debe construirse con engines.DefaultClient() (badomens/<versión>
// (+https://github.com/MrZudro/badomens)) o con uno equivalente.
package modrinth

import (
	"strings"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/plugins/marketplace"
)

// ID identifica la fuente en el catálogo de marketplaces.
const ID marketplace.SourceID = marketplace.SourceModrinth

// DefaultBaseURL es el prefijo de la API pública v2.
const DefaultBaseURL = "https://api.modrinth.com/v2"

// Límites del adaptador. La búsqueda los aplica en el servidor; el listado de
// versiones NO admite paginación en Modrinth (devuelve todas las que encajan),
// así que el tope se aplica aquí y se documenta como propio.
const (
	// DefaultSearchLimit es el tamaño de página por defecto de una búsqueda.
	DefaultSearchLimit = 20
	// MaxSearchLimit es el máximo aceptado por la API (100).
	MaxSearchLimit = 100
	// MaxVersionResults es el tope de versiones que este adaptador devuelve por
	// proyecto. Modrinth devuelve TODAS las que cumplen el filtro; el tope es
	// nuestro y las versiones llegan de la más reciente a la más antigua.
	MaxVersionResults = 200
)

// Client es el adaptador de Modrinth. Es seguro para uso concurrente.
type Client struct {
	// HTTP es el transporte común de catálogos. Si es nil se usa
	// engines.DefaultClient().
	HTTP *engines.Client
	// BaseURL permite apuntar a otro host (pruebas, espejos). Vacío ⇒
	// DefaultBaseURL.
	BaseURL string
}

// New construye el adaptador sobre el transporte dado.
func New(http *engines.Client) *Client {
	if http == nil {
		http = engines.DefaultClient()
	}
	return &Client{HTTP: http, BaseURL: DefaultBaseURL}
}

// ID implementa marketplace.Source.
func (c *Client) ID() marketplace.SourceID { return ID }

// Label implementa marketplace.Source.
func (c *Client) Label() string { return "Modrinth" }

// baseURL devuelve el prefijo normalizado, sin barra final.
func (c *Client) baseURL() string {
	b := strings.TrimSpace(c.BaseURL)
	if b == "" {
		b = DefaultBaseURL
	}
	return strings.TrimRight(b, "/")
}

// opts fija la trazabilidad de una operación lógica.
func (c *Client) opts(operation string) engines.RequestOpts {
	return engines.RequestOpts{
		Provider:  string(ID),
		Operation: operation,
		Expected:  engines.ContentJSON,
	}
}

// --- Esquema de Modrinth (parser propio) -----------------------------------

// searchDoc es la respuesta de GET /search.
type searchDoc struct {
	Hits      *[]searchHit `json:"hits"`
	TotalHits *int         `json:"total_hits"`
	Offset    int          `json:"offset"`
	Limit     int          `json:"limit"`
}

// searchHit es un proyecto dentro del resultado de búsqueda. Ojo: la búsqueda
// NO publica loaders; solo `categories` (que puede incluir nombres de loader) y
// `versions` (versiones de Minecraft). Por eso un resultado de búsqueda nunca se
// marca compatible sin más datos.
type searchHit struct {
	ProjectID     string   `json:"project_id"`
	ProjectType   string   `json:"project_type"`
	Slug          string   `json:"slug"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	Author        string   `json:"author"`
	Categories    []string `json:"categories"`
	Versions      []string `json:"versions"`
	Downloads     int64    `json:"downloads"`
	IconURL       string   `json:"icon_url"`
	License       string   `json:"license"`
	LatestVersion string   `json:"latest_version"`
}

// projectDoc es la respuesta de GET /project/{id|slug}.
type projectDoc struct {
	ID           *string  `json:"id"`
	Slug         *string  `json:"slug"`
	ProjectType  *string  `json:"project_type"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Status       string   `json:"status"`
	Loaders      []string `json:"loaders"`
	GameVersions []string `json:"game_versions"`
	Downloads    int64    `json:"downloads"`
	IconURL      string   `json:"icon_url"`
	Team         string   `json:"team"`
	License      struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"license"`
}

// versionDoc es una versión de GET /project/{id|slug}/version.
type versionDoc struct {
	ID            *string         `json:"id"`
	ProjectID     *string         `json:"project_id"`
	VersionNumber *string         `json:"version_number"`
	Name          string          `json:"name"`
	VersionType   string          `json:"version_type"`
	Status        string          `json:"status"`
	Loaders       *[]string       `json:"loaders"`
	GameVersions  *[]string       `json:"game_versions"`
	Downloads     int64           `json:"downloads"`
	DatePublished string          `json:"date_published"`
	Files         *[]fileDoc      `json:"files"`
	Dependencies  []dependencyDoc `json:"dependencies"`
}

// fileDoc es un artefacto publicado por Modrinth. Los hashes publicados son
// sha512 y sha1; NO publica sha256.
type fileDoc struct {
	Hashes   map[string]string `json:"hashes"`
	URL      string            `json:"url"`
	Filename string            `json:"filename"`
	Primary  bool              `json:"primary"`
	Size     int64             `json:"size"`
}

// dependencyDoc es una dependencia declarada.
type dependencyDoc struct {
	VersionID      *string `json:"version_id"`
	ProjectID      *string `json:"project_id"`
	FileName       *string `json:"file_name"`
	DependencyType string  `json:"dependency_type"`
}

// --- Traducción al dominio ---------------------------------------------------

// kind traduce el project_type de Modrinth.
func kind(projectType string) marketplace.Kind {
	switch strings.ToLower(strings.TrimSpace(projectType)) {
	case "mod":
		return marketplace.KindMod
	case "plugin":
		return marketplace.KindPlugin
	case "modpack":
		return marketplace.KindModpack
	case "datapack":
		return marketplace.KindDatapack
	case "resourcepack":
		return marketplace.KindResourcepack
	case "shader":
		return marketplace.KindShader
	default:
		return marketplace.KindUnknown
	}
}

// dependencyKind traduce el dependency_type de Modrinth.
func dependencyKind(raw string) marketplace.DependencyKind {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "required":
		return marketplace.DepRequired
	case "optional":
		return marketplace.DepOptional
	case "incompatible":
		return marketplace.DepIncompatible
	case "embedded":
		return marketplace.DepEmbedded
	default:
		return marketplace.DepUnknown
	}
}

// deref devuelve el valor de un puntero o "" si es nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// projectStatus decide si un proyecto está publicado a partir de su estado.
// `approved` es el estado normal; `unlisted` está publicado pero oculto.
func projectStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "approved", "unlisted":
		return true
	default:
		return false
	}
}

// versionStatus decide si una versión está publicada a partir de su estado.
func versionStatus(status string) bool {
	return strings.ToLower(strings.TrimSpace(status)) == "listed"
}
