// Package engines modela los motores de servidor Minecraft soportados y sus
// catálogos de versiones (consultados en línea), igual que el proyecto Python
// original: Paper, Fabric, Forge, NeoForge y Vanilla.
package engines

import (
	"context"
	"fmt"
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

// RequiredPins devuelve las claves de pin OBLIGATORIAS de un motor (las que
// identifican de forma inequívoca la resolución). Vanilla no necesita ninguna.
func RequiredPins(e Engine) []string {
	switch e.Name {
	case Paper.Name:
		return []string{Paper.DepVar}
	case Fabric.Name:
		return []string{Fabric.DepVar}
	case Forge.Name:
		return []string{Forge.DepVar}
	case NeoForge.Name:
		return []string{NeoForge.DepVar}
	}
	return nil
}

// Version es una versión candidata del catálogo normalizado. Todos los campos
// salvo `compatible` (derivado) proceden de una fuente verificable.
type Version struct {
	// Version es el identificador de la versión ("1.21.4"), siempre la versión
	// REAL de Minecraft, nunca la del loader.
	Version string `json:"version"`
	// Stable indica que es una versión estable (release).
	Stable bool `json:"stable"`
	// Detail es una nota legible adicional (build, loader).
	Detail string `json:"detail,omitempty"`
	// Channel es el canal de publicación (release, snapshot, beta, alpha).
	Channel Channel `json:"channel,omitempty"`
	// ReleasedAt es la fecha de publicación (para ordenar).
	ReleasedAt time.Time `json:"released_at,omitempty"`
	// Recommended marca la última estable que el motor soporta. Se calcula
	// DESPUÉS de normalizar y ordenar.
	Recommended bool `json:"recommended,omitempty"`
	// JavaMajor es el Java requerido (25, 21, 17…); 0 = desconocido, nunca 8 por
	// defecto (AC-05).
	JavaMajor int `json:"java_major,omitempty"`
	// JavaStatus es el ORIGEN del dato de Java: verified (lo declara el
	// proveedor), inferred (regla versionada) o unknown.
	JavaStatus string `json:"java_status,omitempty"`
	// Build es el build/loader concreto (Paper build, Forge versión…).
	Build string `json:"build,omitempty"`
	// LoaderVersion es la versión exacta del loader, cuando corresponda.
	LoaderVersion string `json:"loader_version,omitempty"`
	// Compatibility es el estado TRIVALENTE respecto al motor.
	Compatibility Compatibility `json:"compatibility,omitempty"`
	// Compatible se DERIVA de Compatibility (compatible ⇔ verificado). Se
	// conserva por compatibilidad del contrato JSON.
	Compatible bool `json:"compatible,omitempty"`
	// IncompatibleWhy explica por qué no se puede seleccionar.
	IncompatibleWhy string `json:"incompatible_why,omitempty"`
	// Source identifica al proveedor del que procede el dato.
	Source string `json:"source,omitempty"`
	// FetchedAt es el momento de obtención del catálogo.
	FetchedAt time.Time `json:"fetched_at,omitempty"`
	// Stale indica que la fila viene de caché vencida o del catálogo embebido.
	Stale bool `json:"stale,omitempty"`
	// Warning describe una restricción o incertidumbre que afecta a la fila.
	Warning string `json:"warning,omitempty"`
}

// ID devuelve el identificador estable de la versión.
func (v Version) ID() string { return v.Version }

// Resolved es el resultado de resolver un motor+versión a pines finales. Todos
// los campos de la parte normalizada son ADITIVOS.
type Resolved struct {
	// Version es la versión de Minecraft resuelta.
	Version string `json:"version"`
	// Pins son los pines extra por motor (build/loader/forge…), claves de .env.
	Pins map[string]string `json:"pins"`
	// Detail es una nota legible adicional (build, loader).
	Detail string `json:"detail,omitempty"`

	// LoaderVersion es la versión exacta del loader, cuando corresponda.
	LoaderVersion string `json:"loader_version,omitempty"`
	// Build es el build exacto resuelto (nunca "latest").
	Build string `json:"build,omitempty"`
	// JavaMajor y JavaStatus son el requisito de Java y su origen.
	JavaMajor  int    `json:"java_major,omitempty"`
	JavaStatus string `json:"java_status,omitempty"`
	// Image es la imagen Docker coherente con el Java requerido ("" si es
	// desconocido: en ese caso NO se degrada la imagen).
	Image string `json:"image,omitempty"`
	// Compatibility es el estado trivalente del conjunto completo.
	Compatibility Compatibility `json:"compatibility,omitempty"`
	// Source es el proveedor del que procede la resolución.
	Source string `json:"source,omitempty"`
	// Warning describe la incertidumbre de la resolución, cuando exista.
	Warning string `json:"warning,omitempty"`
}

// Validate comprueba el invariante de T-02/AC-06: la resolución identifica
// inequívocamente la versión de Minecraft y contiene TODOS los pines
// obligatorios del motor. No existe un Resolved parcialmente válido.
func (r Resolved) Validate(e Engine) error {
	if strings.TrimSpace(r.Version) == "" {
		return catalogContentError(e.Name, CodeInvalidResponse, "resolve", "", Meta{},
			fmt.Errorf("la resolución no identifica la versión de Minecraft"))
	}
	for _, dep := range RequiredPins(e) {
		pin := strings.TrimSpace(r.Pins[dep])
		if pin == "" || strings.EqualFold(pin, "latest") {
			return catalogContentError(e.Name, CodeInvalidResponse, "resolve", "", Meta{},
				fmt.Errorf("la resolución de %s no fija el pin obligatorio %s", e.Name, dep))
		}
	}
	return nil
}

// normalizeResolved completa la parte normalizada de una resolución: build y
// loader a partir de los pines, requisito de Java (con su origen), imagen Docker
// coherente y compatibilidad trivalente del conjunto completo.
func normalizeResolved(e Engine, r Resolved) Resolved {
	if strings.TrimSpace(r.Source) == "" {
		r.Source = e.Name
	}
	if r.Build == "" {
		switch e.Name {
		case Paper.Name, Forge.Name, NeoForge.Name:
			r.Build = r.Pins[e.DepVar]
		}
	}
	if r.LoaderVersion == "" && e.Name == Fabric.Name {
		r.LoaderVersion = r.Pins[Fabric.DepVar]
	}
	if r.JavaMajor <= 0 {
		r.JavaMajor, r.JavaStatus = JavaRequirementFor(r.Version)
	} else if r.JavaStatus == "" {
		r.JavaStatus = JavaStatusVerified
	}
	if r.Image == "" {
		r.Image = JavaImageFor(r.Version, "")
	}
	state, why := CheckCompatibility(CompatibilityInput{
		Engine:           e,
		MinecraftVersion: r.Version,
		LoaderVersion:    r.LoaderVersion,
		Build:            r.Build,
		Pins:             r.Pins,
		JavaMajor:        r.JavaMajor,
		JavaStatus:       r.JavaStatus,
		DockerImage:      r.Image,
	})
	r.Compatibility = state
	if why != "" {
		if r.Warning != "" {
			r.Warning += " · " + why
		} else {
			r.Warning = why
		}
	}
	return r
}

// Provider consulta el catálogo de un motor.
type Provider interface {
	// Engine devuelve el motor que sirve el proveedor.
	Engine() Engine
	// ListVersions devuelve las versiones disponibles.
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
	return NewRegistryFrom(
		&paperProvider{engine: Paper, client: client},
		&fabricProvider{engine: Fabric, client: client},
		&forgeProvider{engine: Forge, client: client},
		&neoforgeProvider{engine: NeoForge, client: client},
		&vanillaProvider{engine: Vanilla, client: client},
	)
}

// NewRegistryFrom construye el registro con los proveedores dados. Lo usan las
// pruebas de integración y las fachadas que sustituyen el catálogo en línea por
// uno controlado; un motor sin proveedor falla al resolver, nunca en silencio.
func NewRegistryFrom(providers ...Provider) *Registry {
	reg := &Registry{providers: make(map[string]Provider, len(providers))}
	for _, p := range providers {
		if p == nil {
			continue
		}
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

// Resolve es la ruta ÚNICA de resolución: delega en el proveedor del motor y
// valida el invariante de pins obligatorios antes de devolver el resultado. Es
// el punto por el que deben pasar CLI, TUI y MCP.
func (r *Registry) Resolve(ctx context.Context, engine, version string) (Resolved, error) {
	p, err := r.Provider(engine)
	if err != nil {
		return Resolved{}, err
	}
	res, err := p.Resolve(ctx, version)
	if err != nil {
		return Resolved{}, err
	}
	if err := res.Validate(p.Engine()); err != nil {
		return Resolved{}, err
	}
	return normalizeResolved(p.Engine(), res), nil
}
