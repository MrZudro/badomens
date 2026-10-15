// Package marketplace define el contrato común de los marketplaces de plugins y
// mods: los tipos de dominio (proyecto, versión, artefacto, dependencia), la
// compatibilidad DECLARADA frente a un entorno concreto y el registro de
// fuentes.
//
// Este paquete no habla con la red, no descarga y no instala: solo describe. Los
// adaptadores (subpaquetes modrinth y hangar) traducen el esquema propio de cada
// proveedor a estos tipos con su PROPIO parser; el resolvedor (subpaquete
// resolve) decide qué versión exacta instalar y el instalador (subpaquete
// install) descarga, verifica y registra.
//
// Regla que no se rompe nunca: un resultado de búsqueda NO demuestra
// compatibilidad ni seguridad. Solo se marca Compatible cuando el proveedor
// DECLARA el loader y la versión de Minecraft y ambos coinciden con el entorno
// pedido; cualquier dato ausente es Unknown (desconocido). Lo no verificado
// nunca se convierte en compatible.
package marketplace

import (
	"context"
	"sort"
	"strings"
)

// SourceID identifica un marketplace.
type SourceID string

// Marketplaces soportados y vías avanzadas.
const (
	// SourceModrinth es la API pública de Modrinth (https://docs.modrinth.com/api).
	SourceModrinth SourceID = "modrinth"
	// SourceHangar es la API pública de Hangar (https://hangar.papermc.io/api-docs).
	SourceHangar SourceID = "hangar"
	// SourceURLDirecta y SourceLocal NO son marketplaces: no publican catálogo,
	// no pasan por este contrato y se instalan con internal/plugins/download
	// (vía avanzada, con la misma política de seguridad). Se nombran aquí solo
	// para que el registro de procedencia use un vocabulario estable.
	SourceURLDirecta SourceID = "url"
	SourceLocal      SourceID = "local"
	// SourceDesconocido es el origen de un registro antiguo: el archivo está
	// instalado, pero no se sabe de dónde salió.
	SourceDesconocido SourceID = "desconocido"
)

// Kind es el tipo de proyecto publicado.
type Kind string

// Tipos de proyecto conocidos.
const (
	KindMod          Kind = "mod"
	KindPlugin       Kind = "plugin"
	KindModpack      Kind = "modpack"
	KindDatapack     Kind = "datapack"
	KindResourcepack Kind = "resourcepack"
	KindShader       Kind = "shader"
	// KindUnknown es un tipo no reconocido: no se puede afirmar que sea
	// instalable ni lo contrario.
	KindUnknown Kind = "desconocido"
)

// Accepts dice si un proyecto de este tipo puede instalarse en una instancia con
// ese motor. Es una comprobación del TIPO de proyecto, no del artefacto: que el
// tipo encaje no implica que la versión concreta sea compatible.
func (k Kind) Accepts(engine string) bool {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "paper":
		return k == KindPlugin
	case "fabric", "forge", "neoforge":
		return k == KindMod
	default:
		return false
	}
}

// Compatibility es el veredicto de compatibilidad declarada frente a un entorno.
type Compatibility string

// Veredictos posibles. Nunca se colapsa Unknown a Compatible.
const (
	// Compatible: el proveedor declara loader Y versión de Minecraft y ambos
	// coinciden con el entorno. Es una comprobación sobre METADATOS declarados
	// por el proveedor: no demuestra que el artefacto funcione ni que sea seguro.
	Compatible Compatibility = "compatible"
	// Incompatible: el proveedor declara datos que NO coinciden con el entorno.
	Incompatible Compatibility = "incompatible"
	// Unknown: falta algún dato (no se indicó el entorno, o el proveedor no
	// declara loaders/versiones) para afirmar o negar nada.
	Unknown Compatibility = "desconocido"
)

// Environment describe el servidor destino.
type Environment struct {
	// Engine es el motor de badomens (paper, fabric, forge, neoforge).
	Engine string
	// Loader es el nombre que el proveedor declara para ese motor (paper,
	// fabric, PAPER...). Si está vacío se usa Engine tal cual.
	Loader string
	// GameVersion es la versión de Minecraft de la instancia (p. ej. 1.21.1).
	GameVersion string
}

// LoaderName devuelve el nombre de loader que se compara con lo declarado por
// el proveedor: el loader explícito o, si no lo hay, el motor.
func (e Environment) LoaderName() string {
	if l := strings.TrimSpace(e.Loader); l != "" {
		return l
	}
	return strings.TrimSpace(e.Engine)
}

// Project es un proyecto de un marketplace tal y como lo describe el proveedor.
type Project struct {
	// Source identifica el marketplace del que procede.
	Source SourceID
	// ID es el identificador estable en el marketplace.
	ID string
	// Slug es el identificador legible (modrinth.com/mod/<slug>).
	Slug string
	// Title, Description, Author y URL son para mostrar.
	Title       string
	Description string
	Author      string
	URL         string
	// Kind es el tipo de proyecto.
	Kind Kind
	// Loaders y GameVersions son los DECLARADOS por el proveedor para el
	// proyecto entero. Vacío significa "no declarado": no se infiere.
	Loaders      []string
	GameVersions []string
	// Categories son las etiquetas del proveedor. Pueden solaparse con los
	// loaders (Modrinth pone "paper" o "fabric" entre las categorías de una
	// búsqueda), pero NUNCA se usan como prueba de compatibilidad.
	Categories []string
	Downloads  int64
	IconURL    string
	License    string
	// Status es el estado de publicación tal cual lo describe el proveedor
	// (approved, archived, draft, public, unlisted...).
	Status string
	// Published indica que el proveedor lo considera publicado. Un proyecto no
	// publicado no se debe instalar.
	Published bool
	// Compat y CompatReason son la compatibilidad declarada frente al entorno de
	// la consulta. En una búsqueda es Unknown salvo que el proveedor publique
	// loaders y versiones del proyecto.
	Compat       Compatibility
	CompatReason string
}

// Version es una versión publicada de un proyecto.
type Version struct {
	// Source identifica el marketplace del que procede.
	Source SourceID
	// ProjectID es el identificador del proyecto en el marketplace.
	ProjectID string
	// ID es el identificador estable de la versión.
	ID string
	// Name y Number son el nombre y el número de versión.
	Name   string
	Number string
	// Channel es el canal del proveedor: release, beta, alpha (Modrinth) o el
	// nombre del canal (Hangar: Release, Snapshot...).
	Channel string
	// Loaders y GameVersions son los DECLARADOS para ESTA versión.
	Loaders      []string
	GameVersions []string
	// Files son los artefactos descargables de esta versión.
	Files []File
	// Dependencies son las dependencias declaradas.
	Dependencies []Dependency
	// Status es el estado de publicación declarado (listed, archived, public...).
	Status string
	// Published indica que el proveedor la considera publicada.
	Published bool
	// Downloads y DatePublished son informativos.
	Downloads     int64
	DatePublished string
	// Compat y CompatReason son la compatibilidad declarada frente al entorno.
	Compat       Compatibility
	CompatReason string
}

// Prerelease indica si el canal de la versión no es un lanzamiento estable.
func (v Version) Prerelease() bool {
	switch strings.ToLower(strings.TrimSpace(v.Channel)) {
	case "", "release":
		return false
	default:
		return true
	}
}

// PrimaryFile devuelve el artefacto principal de la versión: el marcado como
// primary si lo hay; si no, el primero con URL. Devuelve false si la versión no
// publica ningún artefacto.
func (v Version) PrimaryFile() (File, bool) {
	var first *File
	for i := range v.Files {
		f := v.Files[i]
		if strings.TrimSpace(f.URL) == "" {
			continue
		}
		if f.Primary {
			return f, true
		}
		if first == nil {
			first = &v.Files[i]
		}
	}
	if first == nil {
		return File{}, false
	}
	return *first, true
}

// File es un artefacto descargable de una versión.
type File struct {
	// Name es el nombre del archivo.
	Name string
	// URL es la dirección de descarga publicada por el proveedor.
	URL string
	// Size es el tamaño declarado; 0 si no se conoce.
	Size int64
	// Primary marca el artefacto principal de la versión.
	Primary bool
	// External indica que el marketplace NO aloja el artefacto (lo publica un
	// tercero): suele venir sin tamaño ni hash.
	External bool
	// Hashes son los checksums publicados.
	Hashes []Hash
}

// Hash es un checksum publicado por el proveedor.
type Hash struct {
	// Algorithm es el nombre normalizado: sha256, sha512 o sha1.
	Algorithm string
	// Value es el checksum en hexadecimal.
	Value string
}

// Algoritmos de checksum reconocidos.
const (
	HashSHA256 = "sha256"
	HashSHA512 = "sha512"
	HashSHA1   = "sha1"
)

// Hash devuelve el checksum del algoritmo pedido.
func (f File) Hash(algorithm string) (string, bool) {
	want := strings.ToLower(strings.TrimSpace(algorithm))
	for _, h := range f.Hashes {
		if strings.ToLower(strings.TrimSpace(h.Algorithm)) == want && strings.TrimSpace(h.Value) != "" {
			return strings.ToLower(strings.TrimSpace(h.Value)), true
		}
	}
	return "", false
}

// StrongestHash devuelve el checksum utilizable más fuerte publicado: SHA-256
// (lo verifica download.StageURL durante la copia) o, si no lo hay, SHA-512 (se
// verifica tras la descarga). SHA-1 se ignora deliberadamente: es débil y no se
// usa como prueba de integridad.
func (f File) StrongestHash() (Hash, bool) {
	for _, algo := range []string{HashSHA256, HashSHA512} {
		if v, ok := f.Hash(algo); ok {
			return Hash{Algorithm: algo, Value: v}, true
		}
	}
	return Hash{}, false
}

// Verifiable indica si hay un checksum con un algoritmo aceptable.
func (f File) Verifiable() bool {
	_, ok := f.StrongestHash()
	return ok
}

// DependencyKind clasifica una dependencia declarada.
type DependencyKind string

// Tipos de dependencia.
const (
	// DepRequired es obligatoria: sin ella el plugin/mod puede no cargar.
	DepRequired DependencyKind = "required"
	// DepOptional es opcional.
	DepOptional DependencyKind = "optional"
	// DepIncompatible declara una incompatibilidad explícita.
	DepIncompatible DependencyKind = "incompatible"
	// DepEmbedded ya está incluida en el artefacto.
	DepEmbedded DependencyKind = "embedded"
	// DepUnknown es un tipo no reconocido.
	DepUnknown DependencyKind = "desconocido"
)

// Dependency es una dependencia declarada por el proveedor.
type Dependency struct {
	// ProjectID identifica el proyecto del que depende (puede faltar).
	ProjectID string
	// VersionID fija una versión concreta (puede faltar).
	VersionID string
	// Name es el nombre legible cuando el proveedor no da identificadores
	// (Hangar publica nombres de plugin).
	Name string
	// Kind clasifica la dependencia.
	Kind DependencyKind
}

// Key devuelve una clave estable para comparar una dependencia con lo instalado.
func (d Dependency) Key() string {
	for _, candidate := range []string{d.ProjectID, d.Name} {
		if k := strings.ToLower(strings.TrimSpace(candidate)); k != "" {
			return k
		}
	}
	return ""
}

// SearchQuery describe una búsqueda de proyectos.
type SearchQuery struct {
	// Text es el texto libre; vacío permite filtrar solo por entorno.
	Text string
	// Kind filtra por tipo de proyecto (vacío = sin filtro).
	Kind Kind
	// Env describe el servidor destino. Se usa para CALCULAR la compatibilidad
	// declarada de cada resultado y, si FilterEnv es true, para pedir al
	// proveedor que filtre.
	Env Environment
	// FilterEnv pide al proveedor que restrinja los resultados al entorno
	// (Modrinth: facets de versiones y categorías; Hangar: plataforma). Es solo
	// una ayuda de relevancia: un resultado filtrado por el servidor sigue sin
	// ser prueba de compatibilidad, así que la compatibilidad se calcula
	// siempre con los metadatos declarados.
	FilterEnv bool
	// Limit y Offset paginan (0 = valor por defecto del adaptador).
	Limit  int
	Offset int
}

// SearchResult es el resultado de una búsqueda.
type SearchResult struct {
	// Projects son los proyectos encontrados, ya traducidos al dominio.
	Projects []Project
	// Total es el total de coincidencias según el proveedor.
	Total int
	// Offset y Limit son la paginación efectiva.
	Offset int
	Limit  int
}

// VersionFilter filtra las versiones de un proyecto.
type VersionFilter struct {
	// Loaders y GameVersions acotan por entorno. El proveedor puede aplicarlos
	// en el servidor (Modrinth) o en el cliente (Hangar): en ambos casos el
	// adaptador devuelve SOLO lo que declara cumplirlos.
	Loaders      []string
	GameVersions []string
	// Limit acota el número de versiones pedidas (0 = el del adaptador).
	Limit int
}

// Source es el contrato común de un marketplace. Cada adaptador implementa su
// propio parser y su propia lógica: aquí solo se fija la forma de la respuesta.
// Todos los métodos devuelven errores tipados de badomens (errx), nunca errores
// HTTP en crudo.
type Source interface {
	// ID identifica el marketplace.
	ID() SourceID
	// Label es el nombre legible para humanos.
	Label() string
	// Search busca proyectos.
	Search(ctx context.Context, q SearchQuery) (SearchResult, error)
	// Project obtiene un proyecto por slug o por identificador.
	Project(ctx context.Context, ref string) (*Project, error)
	// Versions lista las versiones de un proyecto. projectID es el ID del
	// marketplace (el adaptador acepta también el slug).
	Versions(ctx context.Context, projectID string, f VersionFilter) ([]Version, error)
}

// Catalog es el registro de fuentes disponibles. El llamador (CLI, TUI, MCP)
// construye las fuentes y las registra una sola vez: no se crean clientes ni
// parsers por pantalla.
type Catalog struct {
	sources map[SourceID]Source
	order   []SourceID
}

// NewCatalog crea un catálogo vacío.
func NewCatalog() *Catalog {
	return &Catalog{sources: map[SourceID]Source{}}
}

// Register añade una fuente. Una fuente con ID repetido se reemplaza.
func (c *Catalog) Register(s Source) {
	if c.sources == nil {
		c.sources = map[SourceID]Source{}
	}
	id := s.ID()
	if _, seen := c.sources[id]; !seen {
		c.order = append(c.order, id)
	}
	c.sources[id] = s
}

// Get devuelve la fuente con ese identificador.
func (c *Catalog) Get(id SourceID) (Source, bool) {
	s, ok := c.sources[id]
	return s, ok
}

// IDs devuelve los identificadores registrados, en orden de alta.
func (c *Catalog) IDs() []SourceID {
	out := make([]SourceID, 0, len(c.order))
	out = append(out, c.order...)
	return out
}

// Labels devuelve los identificadores legibles registrados.
func (c *Catalog) Labels() []string {
	out := make([]string, 0, len(c.order))
	for _, id := range c.order {
		if s, ok := c.sources[id]; ok {
			out = append(out, string(id)+" ("+s.Label()+")")
		}
	}
	return out
}

// KnownLoader informa si el motor de badomens tiene traducción conocida a un
// loader de marketplace. Vanilla no admite plugins ni mods.
func KnownLoader(engine string) bool {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "paper", "fabric", "forge", "neoforge":
		return true
	default:
		return false
	}
}

// NormalizeDeclared ordena y deduplica sin distinguir mayúsculas, conservando el
// primer valor visto. Se usa al normalizar listas declaradas por el proveedor.
func NormalizeDeclared(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		k := strings.ToLower(v)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}
