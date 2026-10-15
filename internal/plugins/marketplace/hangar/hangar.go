// Package hangar implementa el adaptador de la API pública v1 de Hangar
// (https://hangar.papermc.io/api-docs), el marketplace de plugins de PaperMC.
//
// Tiene su PROPIO parser y sus propios tipos de transporte (projectDoc,
// versionsDoc, versionDoc, platformDownload...): no comparte decodificación con
// Modrinth ni con ningún otro proveedor (T-06). Solo comparte los tipos de
// dominio de internal/plugins/marketplace y el transporte HTTP de
// internal/engines.
//
// Particularidades de Hangar que condicionan el adaptador:
//
//   - Los plugins se publican por PLATAFORMA (PAPER, WATERFALL, VELOCITY,
//     BUNGEECORD, FOLIA) y cada versión declara qué versiones de Minecraft
//     soporta en cada una. El filtro por plataforma y el filtrado de versiones
//     de Minecraft se aplican AQUÍ, en el cliente, porque la API de versiones no
//     los acepta.
//   - Solo algunas versiones alojan el archivo (downloadUrl + fileInfo con
//     sha256). Otras apuntan a un tercero (externalUrl) sin tamaño ni
//     checksum: en ese caso el artefacto queda SIN verificación disponible y
//     así se informa, nunca se finge.
package hangar

import (
	"sort"
	"strconv"
	"strings"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/plugins/marketplace"
)

// ID identifica la fuente en el catálogo de marketplaces.
const ID marketplace.SourceID = marketplace.SourceHangar

// DefaultBaseURL es el prefijo de la API pública v1.
const DefaultBaseURL = "https://hangar.papermc.io/api/v1"

// Límites del adaptador.
const (
	// MaxSearchLimit es el máximo de resultados por página aceptado por Hangar.
	MaxSearchLimit = 25
	// DefaultSearchLimit es el tamaño de página por defecto de una búsqueda.
	DefaultSearchLimit = 20
	// PageLimit es el tamaño de página que se pide al listar versiones.
	PageLimit = 25
	// MaxPages es el tope de páginas que se recorren al listar versiones; acota
	// el trabajo aunque haya cientos.
	MaxPages = 4
)

// Plataformas que publica Hangar.
const (
	PlatformPaper     = "PAPER"
	PlatformWaterfall = "WATERFALL"
	PlatformVelocity  = "VELOCITY"
	PlatformBungee    = "BUNGEECORD"
	PlatformFolia     = "FOLIA"
)

// Client es el adaptador de Hangar. Es seguro para uso concurrente.
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
func (c *Client) Label() string { return "Hangar (PaperMC)" }

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

// --- Esquema de Hangar (parser propio) -------------------------------------

// namespace es el propietario y el slug de un proyecto.
type namespace struct {
	Owner string `json:"owner"`
	Slug  string `json:"slug"`
}

// projectDoc es la respuesta de GET /projects/{slug} y de cada elemento de
// GET /projects.
type projectDoc struct {
	ID          *int64     `json:"id"`
	Name        *string    `json:"name"`
	Namespace   *namespace `json:"namespace"`
	Category    string     `json:"category"`
	Description string     `json:"description"`
	LastUpdated string     `json:"lastUpdated"`
	Visibility  string     `json:"visibility"`
	AvatarURL   string     `json:"avatarUrl"`
	Stats       *struct {
		Downloads int64 `json:"downloads"`
	} `json:"stats"`
	SupportedPlatforms map[string][]string `json:"supportedPlatforms"`
	Settings           *struct {
		License struct {
			Name string `json:"name"`
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"license"`
		Keywords []string `json:"keywords"`
	} `json:"settings"`
}

// projectsPage es la respuesta paginada de GET /projects.
type projectsPage struct {
	Pagination *pagination   `json:"pagination"`
	Result     *[]projectDoc `json:"result"`
}

// versionsPage es la respuesta paginada de GET /projects/{slug}/versions.
type versionsPage struct {
	Pagination *pagination   `json:"pagination"`
	Result     *[]versionDoc `json:"result"`
}

// pagination describe el recuento total y la ventana devuelta.
type pagination struct {
	Count  int `json:"count"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// versionDoc es una versión del esquema de Hangar.
type versionDoc struct {
	ID           *int64   `json:"id"`
	ProjectID    *int64   `json:"projectId"`
	Name         *string  `json:"name"`
	Visibility   string   `json:"visibility"`
	Description  string   `json:"description"`
	Author       string   `json:"author"`
	CreatedAt    string   `json:"createdAt"`
	ReviewState  string   `json:"reviewState"`
	PinnedStatus string   `json:"pinnedStatus"`
	Channel      *channel `json:"channel"`
	Stats        *struct {
		TotalDownloads int64 `json:"totalDownloads"`
	} `json:"stats"`
	Downloads            map[string]platformDownload   `json:"downloads"`
	PluginDependencies   map[string][]pluginDependency `json:"pluginDependencies"`
	PlatformDependencies map[string][]string           `json:"platformDependencies"`
}

// channel es el canal de publicación de una versión (Release, Snapshot...).
type channel struct {
	Name  string   `json:"name"`
	Flags []string `json:"flags"`
}

// platformDownload describe el artefacto publicado para una plataforma. Si
// fileInfo y downloadUrl faltan, el archivo lo aloja un tercero (externalUrl).
type platformDownload struct {
	FileInfo    *fileInfo `json:"fileInfo"`
	ExternalURL string    `json:"externalUrl"`
	DownloadURL string    `json:"downloadUrl"`
}

// fileInfo son los datos del archivo alojado por Hangar.
type fileInfo struct {
	Name       string `json:"name"`
	SizeBytes  int64  `json:"sizeBytes"`
	SHA256Hash string `json:"sha256Hash"`
}

// pluginDependency es una dependencia declarada para una plataforma.
type pluginDependency struct {
	Name        string `json:"name"`
	ProjectID   *int64 `json:"projectId"`
	Required    bool   `json:"required"`
	ExternalURL string `json:"externalUrl"`
	Platform    string `json:"platform"`
}

// --- Traducción al dominio --------------------------------------------------

// platformFor traduce el motor de badomens a la plataforma de Hangar. Devuelve
// "" si el motor no tiene plataforma en Hangar (fabric, forge, neoforge...).
func platformFor(engine string) string {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "paper":
		return PlatformPaper
	default:
		return ""
	}
}

// platformsFor traduce la lista de loaders pedida a claves de plataforma.
// Acepta indistintamente "paper" y "PAPER".
func platformsFor(loaders []string) []string {
	var out []string
	for _, l := range marketplace.NormalizeDeclared(loaders) {
		up := strings.ToUpper(strings.TrimSpace(l))
		switch up {
		case PlatformPaper, PlatformWaterfall, PlatformVelocity, PlatformBungee, PlatformFolia:
			out = append(out, up)
		}
	}
	return out
}

// sortedKeys devuelve las claves de un mapa ordenadas, para que la traducción
// sea determinista (y las pruebas, también).
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// published decide si un proyecto está publicado: Hangar lo declara con
// `visibility`.
func published(visibility string) bool {
	return strings.EqualFold(strings.TrimSpace(visibility), "public")
}

// channelName normaliza el canal de publicación al vocabulario del dominio.
func channelName(c *channel) string {
	if c == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(c.Name)) {
	case "release":
		return "release"
	case "snapshot":
		return "snapshot"
	case "alpha":
		return "alpha"
	case "beta":
		return "beta"
	case "":
		return ""
	default:
		return strings.ToLower(strings.TrimSpace(c.Name))
	}
}

// statusLabel compone el estado de publicación de una versión.
func statusLabel(visibility, reviewState string) string {
	visibility = strings.TrimSpace(visibility)
	reviewState = strings.TrimSpace(reviewState)
	switch {
	case visibility == "" && reviewState == "":
		return ""
	case reviewState == "":
		return visibility
	default:
		return visibility + "/" + reviewState
	}
}

// number convierte un entero a texto (para ids numéricos de Hangar).
func number(n *int64) string {
	if n == nil {
		return ""
	}
	return strconv.FormatInt(*n, 10)
}
