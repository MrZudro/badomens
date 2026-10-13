package engines

import (
	"context"
	"fmt"
	"strings"
)

// Caso base común: APIs de formato {versions:[...]}.
type apiVersions struct {
	Versions []string `json:"versions"`
}

// paperProvider consulta la API v2 de PaperMC.
type paperProvider struct {
	engine Engine
	client *Client
	base   string
}

func (p *paperProvider) Engine() Engine { return p.engine }

func (p *paperProvider) Base() string {
	if p.base != "" {
		return p.base
	}
	return "https://api.papermc.io/v2/projects/paper"
}

func (p *paperProvider) ListVersions(ctx context.Context) ([]Version, error) {
	var out apiVersions
	if err := p.client.GetJSON(ctx, p.Base()+"/versions", &out); err != nil {
		return nil, err
	}
	versions := make([]Version, 0, len(out.Versions))
	for _, v := range out.Versions {
		versions = append(versions, Version{Version: v, Stable: true})
	}
	return versions, nil
}

type paperBuilds struct {
	Builds []struct {
		Build   int    `json:"build"`
		Channel string `json:"channel"`
	} `json:"builds"`
}

func (p *paperProvider) Resolve(ctx context.Context, version string) (Resolved, error) {
	list, err := p.ListVersions(ctx)
	if err != nil {
		return Resolved{}, err
	}
	v, err := pickVersion(list, version)
	if err != nil {
		return Resolved{}, err
	}
	res := Resolved{Version: v.Version, Pins: map[string]string{}}

	var builds paperBuilds
	if err := p.client.GetJSON(ctx, p.Base()+"/versions/"+v.Version+"/builds", &builds); err == nil && len(builds.Builds) > 0 {
		// Última build estable (canal por defecto del proyecto Paper).
		b := builds.Builds[len(builds.Builds)-1]
		res.Pins[Paper.DepVar] = fmt.Sprintf("%d", b.Build)
		res.Detail = fmt.Sprintf("build %d (%s)", b.Build, strings.TrimSpace(b.Channel))
	}
	return res, nil
}

// vanillaProvider consulta el manifiesto de versiones de Mojang.
type vanillaProvider struct {
	engine Engine
	client *Client
	base   string
}

func (p *vanillaProvider) Engine() Engine { return p.engine }

func (p *vanillaProvider) Base() string {
	if p.base != "" {
		return p.base
	}
	return "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
}

type manifest struct {
	Versions []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"versions"`
}

func (p *vanillaProvider) ListVersions(ctx context.Context) ([]Version, error) {
	var m manifest
	if err := p.client.GetJSON(ctx, p.Base(), &m); err != nil {
		return nil, err
	}
	// El manifiesto viene descendente (nuevo primero). Se conservan los releases y
	// la lista se invierte para que quede ascendente (antiguo → nuevo), que es el
	// contrato que asume pickVersion ("última = última estable").
	var newest []Version
	for _, v := range m.Versions {
		if v.Type != "release" {
			continue
		}
		newest = append(newest, Version{Version: v.ID, Stable: true})
	}
	versions := make([]Version, 0, len(newest))
	for i := len(newest) - 1; i >= 0; i-- {
		versions = append(versions, newest[i])
	}
	return versions, nil
}

func (p *vanillaProvider) Resolve(ctx context.Context, version string) (Resolved, error) {
	list, err := p.ListVersions(ctx)
	if err != nil {
		return Resolved{}, err
	}
	// Para vanilla solo interesan releases.
	releases := make([]Version, 0, len(list))
	for _, v := range list {
		if v.Stable {
			releases = append(releases, v)
		}
	}
	v, err := pickVersion(releases, version)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{Version: v.Version, Pins: map[string]string{}}, nil
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
