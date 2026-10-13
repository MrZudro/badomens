// Package provision agrupa el núcleo de creación de instancias: a partir de una
// petición, resuelve motor y versión, valida, guarda el registro, instala
// plugins/mods y (si se pide) arranca. No lee de stdin: los prompts viven en la
// capa CLI, y la TUI lo usa directamente desde su backend.
package provision

import (
	"context"
	"strings"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/host"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/plugins"
	"github.com/MrZudro/badomens/internal/runtime"
	"github.com/MrZudro/badomens/internal/setup"
)

// Request describe lo que el operador quiere crear. Los campos vacíos toman los
// valores por defecto (motor paper, última versión, memoria de la config…).
type Request struct {
	Name             string
	Engine           string
	Version          string
	Memory           string
	CPU              int
	Difficulty       string
	Gamemode         string
	MaxPlayers       int
	Seed             string
	Online           bool
	WhitelistEnabled bool
	Whitelist        []string
	Ops              []string
	Motd             string
	Icon             string
	Plugins          []string
	Start            bool
}

// Resolver resuelve un motor a su proveedor de catálogo. engines.Registry lo
// cumple; en pruebas se puede inyectar un fake sin red.
type Resolver interface {
	Provider(name string) (engines.Provider, error)
}

// Deps son las dependencias necesarias para crear una instancia.
type Deps struct {
	Cfg      *config.Config
	Manager  *instance.Manager
	Resolver Resolver
	Runtime  *runtime.Runtime // necesario solo si Start=true
}

// Result resume lo creado.
type Result struct {
	Spec     *instance.Spec
	Warnings []string
	Started  bool
}

// Create valida la petición, crea la instancia, instala los plugins y, si se
// pide, la arranca. Devuelve el spec guardado y avisos (por ejemplo, de TPS).
// En caso de fallo al arrancar devuelve igualmente el spec ya guardado para que
// el llamador pueda describir el estado parcial.
func Create(ctx context.Context, d Deps, req Request) (*Result, error) {
	if d.Cfg == nil || d.Manager == nil || d.Resolver == nil {
		return nil, errx.New(errx.CodeGeneral, "dependencias incompletas para crear la instancia", "")
	}

	// 1) Nombre → slug.
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errx.Usage("falta el nombre de la instancia", "usa --name")
	}
	slug, err := instance.NormalizeSlug(name)
	if err != nil {
		return nil, err
	}
	if _, err := d.Manager.Load(slug); err == nil {
		return nil, errx.Usage("ya existe una instancia "+slug, "usa otro nombre")
	}

	// 2) Motor.
	engineName := strings.ToLower(strings.TrimSpace(req.Engine))
	if engineName == "" {
		engineName = "paper"
	}
	provider, err := d.Resolver.Provider(engineName)
	if err != nil {
		return nil, err
	}

	// 3) Versión (resolución en línea).
	resolved, err := provider.Resolve(ctx, req.Version)
	if err != nil {
		return nil, err
	}

	// 4) Recursos.
	memory := req.Memory
	if memory == "" {
		memory = d.Cfg.DefaultMemory
	}
	if memory == "" {
		memory = "4G"
	}
	if host.ParseMemory(memory) == 0 {
		return nil, errx.Usage("memoria inválida "+quote(memory), "ej. 4G")
	}

	// 5) Máximo de jugadores (recomendado).
	modded := engineName != "paper" && engineName != "vanilla"
	recommended := setup.RecommendMaxPlayers(float64(host.ParseMemory(memory))/float64(1024*1024*1024), modded)
	maxPlayers := req.MaxPlayers
	if maxPlayers <= 0 {
		maxPlayers = recommended
	}

	// 6) Dificultad y modo.
	difficulty := req.Difficulty
	if difficulty == "" {
		difficulty = "normal"
	}
	gamemode := req.Gamemode
	if gamemode == "" {
		gamemode = "survival"
	}

	// 7) Puerto libre.
	port, err := d.Manager.NextPort(25565)
	if err != nil {
		return nil, err
	}

	spec := &instance.Spec{
		Slug:             slug,
		Name:             name,
		Engine:           provider.Engine().Name,
		Version:          resolved.Version,
		Pins:             resolved.Pins,
		Memory:           memory,
		CPU:              req.CPU,
		Port:             port,
		Difficulty:       difficulty,
		Gamemode:         gamemode,
		MaxPlayers:       maxPlayers,
		OnlineMode:       req.Online,
		Motd:             req.Motd,
		Icon:             req.Icon,
		WhitelistEnabled: req.WhitelistEnabled,
		Whitelist:        req.Whitelist,
		Ops:              req.Ops,
		Seed:             req.Seed,
		CreatedAt:        instance.Stamp(),
	}
	if err := spec.Validate(d.Manager.EngineNames()); err != nil {
		return nil, errx.Wrap(errx.CodeGeneral, err, "configuración inválida", "")
	}
	if err := d.Manager.Save(spec); err != nil {
		return nil, err
	}

	// 8) Plugins/mods.
	warnings := []string{}
	if len(req.Plugins) > 0 {
		inst := &plugins.Installer{DataDir: d.Cfg.DataDir}
		names := make([]string, 0, len(req.Plugins))
		for _, src := range req.Plugins {
			added, aerr := inst.Add(ctx, spec, src)
			if aerr != nil {
				return &Result{Spec: spec, Warnings: warnings}, aerr
			}
			names = append(names, added)
		}
		spec.Plugins = append(spec.Plugins, names...)
		_ = d.Manager.Save(spec)
		if w := plugins.WarnFor(names); w != "" {
			warnings = append(warnings, w)
		}
	}

	res := &Result{Spec: spec, Warnings: warnings}

	// 9) Arranque opcional.
	if req.Start {
		if d.Runtime == nil {
			return res, errx.New(errx.CodeGeneral, "no se puede arrancar: falta el runtime", "")
		}
		if err := d.Runtime.Start(ctx, spec); err != nil {
			return res, err
		}
		res.Started = true
	}
	return res, nil
}

func quote(s string) string { return "\"" + s + "\"" }
