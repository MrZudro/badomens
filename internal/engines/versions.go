package engines

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Channel es el canal de publicación de una versión.
type Channel string

// Canales posibles.
const (
	ChannelRelease  Channel = "release"
	ChannelSnapshot Channel = "snapshot"
	ChannelBeta     Channel = "beta"
	ChannelAlpha    Channel = "alpha"
)

// String devuelve el identificador estable del canal.
func (c Channel) String() string { return string(c) }

// VersionOpts controla la consulta del catálogo.
type VersionOpts struct {
	// IncludeUnstable incluye snapshots/betas (ocultos por defecto).
	IncludeUnstable bool
}

// Catalog consulta las versiones disponibles y compatibles de un motor.
type Catalog interface {
	Versions(ctx context.Context, engine string, opts VersionOpts) ([]Version, error)
}

// Cache guarda en disco las versiones de cada motor (TTL por defecto 6 h) y
// sirve datos vencidos si no hay red (stale-while-revalidate).
type Cache struct {
	// Dir es la raíz donde se guarda <dir>/versions/<motor>.json.
	Dir string
	// TTL es el tiempo de vida de la caché.
	TTL time.Duration
	// Registry resuelve el proveedor de cada motor.
	Registry *Registry
	// Now es el reloj inyectable.
	Now func() time.Time
}

// NewCache construye una caché sobre el registro.
func NewCache(dir string, reg *Registry) *Cache {
	c := &Cache{Dir: filepath.Join(dir, "versions"), Registry: reg, TTL: 6 * time.Hour}
	if c.Registry == nil {
		c.Registry = NewRegistry(DefaultClient())
	}
	return c
}

func (c *Cache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// cachedDoc es el documento en disco.
type cachedDoc struct {
	Engine   string    `json:"engine"`
	Fetched  time.Time `json:"fetched"`
	Versions []Version `json:"versions"`
}

// allVersionsProvider devuelve también los canales inestables. Lo implementan
// los proveedores que pueden listar snapshots/betas.
type allVersionsProvider interface {
	ListAllVersions(ctx context.Context) ([]Version, error)
}

// Versions devuelve las versiones de un motor. stale=true indica que se usó
// caché vencida (el proveedor falló y no había caché fresca).
func (c *Cache) Versions(ctx context.Context, engine string, opts VersionOpts) (ver []Version, stale bool, err error) {
	now := c.now()
	doc, rerr := c.readCache(c.path(engine))
	if doc != nil && rerr == nil && doc.Fetched.After(now.Add(-c.TTL)) {
		return filter(doc.Versions, opts), false, nil
	}
	provider, perr := c.Registry.Provider(engine)
	if perr != nil {
		if doc != nil {
			return filter(doc.Versions, opts), true, nil
		}
		return nil, false, perr
	}
	var list []Version
	if all, ok := provider.(allVersionsProvider); ok {
		list, err = all.ListAllVersions(ctx)
	} else {
		list, err = provider.ListVersions(ctx)
	}
	if err != nil {
		if doc != nil {
			return filter(doc.Versions, opts), true, nil
		}
		return nil, false, err
	}
	enriched := Enrich(provider.Engine().Name, list)
	_ = c.writeCache(c.path(engine), engine, enriched)
	return filter(enriched, opts), false, nil
}

func (c *Cache) path(engine string) string {
	return filepath.Join(c.Dir, engine+".json")
}

func (c *Cache) readCache(path string) (*cachedDoc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var doc cachedDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func (c *Cache) writeCache(path, engine string, versions []Version) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(cachedDoc{Engine: engine, Fetched: c.now(), Versions: versions})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// filter aplica el filtro de canales estables.
func filter(list []Version, opts VersionOpts) []Version {
	if opts.IncludeUnstable {
		return list
	}
	out := make([]Version, 0, len(list))
	for _, v := range list {
		if v.Channel == ChannelRelease || v.Channel == "" {
			out = append(out, v)
		}
	}
	return out
}

// Enrich completa los metadatos de una lista de versiones: canal, Java
// requerido, marca de recomendada y compatibilidad. No cambia el orden.
func Enrich(engine string, list []Version) []Version {
	out := make([]Version, len(list))
	copy(out, list)
	for i := range out {
		if out[i].Channel == "" {
			if !out[i].Stable {
				out[i].Channel = ChannelSnapshot
			} else {
				out[i].Channel = ChannelRelease
			}
		}
		if !out[i].Compatible {
			out[i].Compatible = true
		}
		out[i].JavaMajor = JavaMajorFor(out[i].ID())
	}
	// La recomendada es la última estable del canal release.
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Channel == ChannelRelease {
			out[i].Recommended = true
			break
		}
	}
	// Forge/NeoForge: se normalizan los pares "mc-build" a su versión de
	// Minecraft como ID y se conserva el build en Detalle.
	if engine == "forge" || engine == "neoforge" {
		out = normalizeMaven(out)
	}
	return out
}

// normalizeMaven agrupa versiones "mc-build" por versión de Minecraft y deja la
// más reciente, descartando pares con build de pre-release.
func normalizeMaven(list []Version) []Version {
	byMC := map[string]Version{}
	for _, v := range list {
		raw := v.Version
		mc, build := splitMCBuild(raw)
		if mc == "" {
			mc = raw
		}
		prev, ok := byMC[mc]
		if !ok || build > prev.Build {
			v.Version = mc
			if build != "" {
				v.Build = build
				v.Detail = raw
			}
			byMC[mc] = v
		}
	}
	out := make([]Version, 0, len(byMC))
	for _, v := range byMC {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ReleasedAt.IsZero() || out[j].ReleasedAt.IsZero() {
			return out[i].Version < out[j].Version
		}
		return out[i].ReleasedAt.Before(out[j].ReleasedAt)
	})
	return out
}

func splitMCBuild(raw string) (mc, build string) {
	i := strings.LastIndex(raw, "-")
	if i <= 0 {
		return raw, ""
	}
	return raw[:i], raw[i+1:]
}
