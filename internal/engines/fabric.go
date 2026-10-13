package engines

import (
	"context"
	"fmt"
	"strings"
)

// fabricProvider consulta la meta de FabricMC (juego y loader).
type fabricProvider struct {
	engine Engine
	client *Client
	base   string
}

func (p *fabricProvider) Engine() Engine { return p.engine }

func (p *fabricProvider) Base() string {
	if p.base != "" {
		return p.base
	}
	return "https://meta.fabricmc.net/v2"
}

type fabricGame struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

type fabricLoader struct {
	Version string `json:"version"`
}

func (p *fabricProvider) ListVersions(ctx context.Context) ([]Version, error) {
	var games []fabricGame
	if err := p.client.GetJSON(ctx, p.Base()+"/versions/game", &games); err != nil {
		return nil, err
	}
	versions := make([]Version, 0, len(games))
	for _, g := range games {
		versions = append(versions, Version{Version: g.Version, Stable: g.Stable})
	}
	return versions, nil
}

func (p *fabricProvider) Resolve(ctx context.Context, version string) (Resolved, error) {
	list, err := p.ListVersions(ctx)
	if err != nil {
		return Resolved{}, err
	}
	v, err := pickVersion(list, version)
	if err != nil {
		return Resolved{}, err
	}
	res := Resolved{Version: v.Version, Pins: map[string]string{}}
	var loaders []fabricLoader
	if err := p.client.GetJSON(ctx, p.Base()+"/versions/loader", &loaders); err == nil && len(loaders) > 0 {
		loader := strings.TrimPrefix(loaders[0].Version, "v")
		res.Pins[Fabric.DepVar] = loader
		res.Detail = "loader " + loader
	}
	return res, nil
}

// forgeProvider y neoforgeProvider usan metadatos Maven: la lista viene en
// versioning.versions con formato "<mc>-<build>" (Forge) o "<nc>-<build>"
// (NeoForge, donde <nc> ≈ versión de Minecraft sin el prefijo "1.").
type mavenRoot struct {
	Versioning struct {
		Versions []string `json:"versions"`
	} `json:"versioning"`
}

type forgeProvider struct {
	engine Engine
	client *Client
	base   string
}

func (p *forgeProvider) Engine() Engine { return p.engine }

func (p *forgeProvider) Base() string {
	if p.base != "" {
		return p.base
	}
	return "https://files.minecraftforge.net/net/minecraftforge/forge"
}

func (p *forgeProvider) ListVersions(ctx context.Context) ([]Version, error) {
	return mavenVersions(ctx, p.client, p.Base()+"/maven-metadata.json")
}

func (p *forgeProvider) Resolve(ctx context.Context, version string) (Resolved, error) {
	list, err := p.ListVersions(ctx)
	if err != nil {
		return Resolved{}, err
	}
	return resolveMaven(list, version, versionPrefix(version), Forge.DepVar)
}

type neoforgeProvider struct {
	engine Engine
	client *Client
	base   string
}

func (p *neoforgeProvider) Engine() Engine { return p.engine }

func (p *neoforgeProvider) Base() string {
	if p.base != "" {
		return p.base
	}
	return "https://maven.neoforged.net/releases/net/neoforged/neoforge"
}

func (p *neoforgeProvider) ListVersions(ctx context.Context) ([]Version, error) {
	return mavenVersions(ctx, p.client, p.Base()+"/maven-metadata.json")
}

func (p *neoforgeProvider) Resolve(ctx context.Context, version string) (Resolved, error) {
	list, err := p.ListVersions(ctx)
	if err != nil {
		return Resolved{}, err
	}
	return resolveMaven(list, version, versionPrefix(version), NeoForge.DepVar)
}

func mavenVersions(ctx context.Context, client *Client, url string) ([]Version, error) {
	var m mavenRoot
	if err := client.GetJSON(ctx, url, &m); err != nil {
		return nil, err
	}
	versions := make([]Version, 0, len(m.Versioning.Versions))
	for _, v := range m.Versioning.Versions {
		// Se marca estable salvo sufijos de pre-release conocidos.
		lower := strings.ToLower(v)
		stable := !strings.ContainsAny(lower, "beta") && !strings.Contains(lower, "rc") && !strings.Contains(lower, "\u03b1") && !strings.Contains(lower, "alpha")
		versions = append(versions, Version{Version: v, Stable: stable})
	}
	return versions, nil
}

// versionPrefix convierte una versión de Minecraft "1.21.1" en el prefijo de
// NeoForge "21.1" y la deja igual para Forge (que usa "1.21.1-").
func versionPrefix(mc string) string {
	if strings.HasPrefix(mc, "1.") {
		return strings.TrimPrefix(mc, "1.")
	}
	return mc
}

func resolveMaven(list []Version, requested, prefix, pinKey string) (Resolved, error) {
	if len(list) == 0 {
		return Resolved{}, fmt.Errorf("el catálogo no devolvió versiones")
	}
	if requested != "" {
		// Se acepta el formato completo "<mc>-<build>" o el prefijo "<mc>".
		for i := len(list) - 1; i >= 0; i-- {
			v := list[i].Version
			if v == requested || strings.HasPrefix(v, requested+"-") {
				return Resolved{Version: requested, Pins: map[string]string{pinKey: v}, Detail: v}, nil
			}
		}
		return Resolved{}, fmt.Errorf("no hay una versión cuya base coincida con %q", requested)
	}
	// Última estable con la base marcada (funciona mejor para NeoForge).
	best := list[len(list)-1]
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].Stable {
			best = list[i]
			break
		}
	}
	// Extraer el componente "mc": para Forge es la parte antes del último '-';
	// para NeoForge, quitar el último componente "-build" y dejar "21.1".
	mc := best.Version
	if i := strings.LastIndex(mc, "-"); i > 0 {
		mc = mc[:i]
	}
	return Resolved{Version: mc, Pins: map[string]string{pinKey: best.Version}, Detail: best.Version}, nil
}
