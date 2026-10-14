// Package engines modela los motores de servidor Minecraft soportados y sus
// catálogos de versiones (consultados en línea), igual que el proyecto Python
// original: Paper, Fabric, Forge, NeoForge y Vanilla.
package engines

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Engine describe un motor.
type Engine struct {
	// Name es el identificador corto (paper, fabric…).
	Name string `json:"name"`
	// Label es el nombre legible.
	Label string `json:"label"`
	// TypeValue es el valor TYPE de la imagen itzg/minecraft-server.
	TypeValue string `json:"type_value"`
	// PluginDir es la carpeta de plugins/mods dentro del volumen.
	PluginDir string `json:"plugin_dir"` // "plugins", "mods" o "" (vanilla)
	// DepVar es la variable de pin de la imagen itzg (PAPER_BUILD…).
	DepVar string `json:"dep_var"`
}

// Relativo a Vanilla: no tiene carpeta de plugins.
var (
	Paper    = Engine{Name: "paper", Label: "Paper", TypeValue: "PAPER", PluginDir: "plugins", DepVar: "PAPER_BUILD"}
	Fabric   = Engine{Name: "fabric", Label: "Fabric", TypeValue: "FABRIC", PluginDir: "mods", DepVar: "FABRIC_LOADER_VERSION"}
	Forge    = Engine{Name: "forge", Label: "Forge", TypeValue: "FORGE", PluginDir: "mods", DepVar: "FORGE_VERSION"}
	NeoForge = Engine{Name: "neoforge", Label: "NeoForge", TypeValue: "NEOFORGE", PluginDir: "mods", DepVar: "NEOFORGE_VERSION"}
	Vanilla  = Engine{Name: "vanilla", Label: "Vanilla", TypeValue: "VANILLA", PluginDir: ""}
)

// Order es el orden estable de exposición.
var Order = []Engine{Paper, Fabric, Forge, NeoForge, Vanilla}

// ByName devuelve el motor por su nombre corto.
func ByName(name string) (Engine, bool) {
	for _, e := range Order {
		if e.Name == strings.ToLower(strings.TrimSpace(name)) {
			return e, true
		}
	}
	return Engine{}, false
}

// SupportsPlugins indica si el motor admite plugins/mods.
func (e Engine) SupportsPlugins() bool { return e.PluginDir != "" }

// Version es una versión candidata del catálogo.
type Version struct {
	// Version es el identificador de la versión ("1.21.4").
	Version string `json:"version"`
	// Stable indica que es una versión estable (release).
	Stable bool `json:"stable"`
	// Detail es una nota legible adicional (build, loader).
	Detail string `json:"detail,omitempty"`
	// Channel es el canal de publicación (release, snapshot, beta, alpha).
	Channel Channel `json:"channel,omitempty"`
	// ReleasedAt es la fecha de publicación (para ordenar).
	ReleasedAt time.Time `json:"released_at,omitempty"`
	// Recommended marca la última estable que el motor soporta.
	Recommended bool `json:"recommended,omitempty"`
	// JavaMajor es el Java requerido (8/16/17/21…).
	JavaMajor int `json:"java_major,omitempty"`
	// Build es el build/loader concreto (Paper build, Forge versión…).
	Build string `json:"build,omitempty"`
	// Compatible indica si se puede seleccionar.
	Compatible bool `json:"compatible,omitempty"`
	// IncompatibleWhy explica por qué no se puede seleccionar.
	IncompatibleWhy string `json:"incompatible_why,omitempty"`
}

// ID devuelve el identificador estable de la versión.
func (v Version) ID() string { return v.Version }

// JavaMajorFor calcula (de forma aproximada) el Java que necesita una versión
// de Minecraft. Es orientativo para la etiqueta de imagen de itzg.
func JavaMajorFor(mc string) int {
	if major, ok := javaMajorKnown(mc); ok {
		return major
	}
	return 8
}

// javaMajorKnown reconoce el Java mayor de una versión de Minecraft. El segundo
// valor es false cuando la versión no es reconocible (no se debe asumir Java).
func javaMajorKnown(mc string) (int, bool) {
	mc = strings.ToLower(strings.TrimSpace(mc))
	switch {
	case strings.HasPrefix(mc, "1.20.5"), strings.HasPrefix(mc, "1.21"), strings.HasPrefix(mc, "1.22"):
		return 21, true
	case strings.HasPrefix(mc, "1.17"):
		return 16, true
	case strings.HasPrefix(mc, "1.18"), strings.HasPrefix(mc, "1.19"), strings.HasPrefix(mc, "1.20"):
		return 17, true
	case strings.HasPrefix(mc, "1.12"), strings.HasPrefix(mc, "1.13"), strings.HasPrefix(mc, "1.14"),
		strings.HasPrefix(mc, "1.15"), strings.HasPrefix(mc, "1.16"):
		return 8, true
	}
	return 0, false
}

// JavaTag devuelve la etiqueta de imagen de itzg para un Java mayor ("java17"),
// o "" si no hay una etiqueta conocida.
func JavaTag(major int) string {
	switch major {
	case 8, 11, 16, 17, 21, 22:
		return "java" + strconv.Itoa(major)
	}
	return ""
}

// JavaImageFor adapta la imagen base al Java que requiere una versión de
// Minecraft (RF-5): conserva el repositorio de `fallback` y elige la etiqueta
// `javaN`. Si la versión no es reconocible, devuelve `fallback` sin cambios.
func JavaImageFor(mc, fallback string) string {
	major, ok := javaMajorKnown(mc)
	if !ok {
		return fallback
	}
	tag := JavaTag(major)
	if tag == "" {
		return fallback
	}
	repo := "itzg/minecraft-server"
	if fallback != "" {
		repo = fallback
		if i := strings.Index(repo, ":"); i >= 0 {
			repo = repo[:i]
		}
	}
	return repo + ":" + tag
}

// Resolved es el resultado de resolver un motor+versión a pines finales.
type Resolved struct {
	// Version es la versión de Minecraft resuelta.
	Version string `json:"version"`
	// Pins son los pines extra por motor (build/loader/forge…), claves de .env.
	Pins map[string]string `json:"pins"`
	// Detail es una nota legible adicional (build, loader).
	Detail string `json:"detail,omitempty"`
}

// Provider consulta el catálogo de un motor.
type Provider interface {
	// Engine devuelve el motor que sirve el proveedor.
	Engine() Engine
	// ListVersions devuelve las versiones disponibles (estables primero).
	ListVersions(ctx context.Context) ([]Version, error)
	// Resolve resuelve una versión pedida (vacía = última estable) a pines.
	Resolve(ctx context.Context, version string) (Resolved, error)
}

// Registry es el mapa de motores → proveedores.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry construye el registro con los proveedores por defecto.
func NewRegistry(client *Client) *Registry {
	reg := &Registry{providers: map[string]Provider{}}
	for _, p := range []Provider{
		&paperProvider{engine: Paper, client: client},
		&fabricProvider{engine: Fabric, client: client},
		&forgeProvider{engine: Forge, client: client},
		&neoforgeProvider{engine: NeoForge, client: client},
		&vanillaProvider{engine: Vanilla, client: client},
	} {
		reg.providers[p.Engine().Name] = p
	}
	return reg
}

// Provider devuelve el proveedor de un motor.
func (r *Registry) Provider(name string) (Provider, error) {
	p, ok := r.providers[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		names := make([]string, 0, len(r.providers))
		for n := range r.providers {
			names = append(names, n)
		}
		return nil, fmt.Errorf("motor desconocido %q (disponibles: %s)", name, strings.Join(names, ", "))
	}
	return p, nil
}

// List devuelve los motores en orden estable.
func (r *Registry) List() []Engine {
	return Order
}
