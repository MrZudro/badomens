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
	"github.com/MrZudro/badomens/internal/ports"
	"github.com/MrZudro/badomens/internal/result"
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
	Port             int
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

// Resolver resuelve motor+versión a los pines finales por la RUTA ÚNICA del
// servicio de catálogo (engines.CatalogService la cumple). provision NUNCA llama
// a provider.Resolve directamente: así hereda la validación del invariante de
// pins obligatorios (AC-06) y los errores normalizados (T-01).
type Resolver interface {
	ResolveVersion(ctx context.Context, engine, version string) (engines.Resolved, error)
}

// Deps son las dependencias necesarias para crear una instancia.
type Deps struct {
	Cfg      *config.Config
	Manager  *instance.Manager
	Resolver Resolver
	Runtime  *runtime.Runtime // necesario solo si Start=true
	// Ports asigna y reserva puertos libremente (RF-3); si es nil se usa la
	// heurística antigua del registro.
	Ports *ports.Allocator
}

// Result resume lo creado y, sobre todo, CÓMO terminó (AC-10): un fallo al
// instalar plugins o al arrancar no oculta que la instancia ya quedó escrita.
type Result struct {
	Spec     *instance.Spec
	Warnings []string
	Started  bool
	// Outcome clasifica el desenlace: applied, partial o failed.
	Outcome result.Outcome
	// Compatibility y JavaRequirement describen la resolución aplicada.
	Compatibility engines.Compatibility
	JavaMajor     int
	JavaStatus    string
}

// Create valida la petición, crea la instancia, instala los plugins y, si se
// pide, la arranca. Devuelve el spec guardado y avisos (por ejemplo, de TPS).
// En caso de fallo parcial (plugins o arranque) devuelve igualmente el resultado
// con Outcome=partial y el spec ya guardado, para que el llamador pueda
// describir con exactitud qué quedó aplicado.
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
	eng, ok := engines.ByName(engineName)
	if !ok {
		return nil, errx.Usage("motor desconocido "+engineName,
			"usa uno de: "+strings.Join(engines.Names(), ", "))
	}

	// 3) Versión: ruta ÚNICA de resolución (T-01). El servicio valida antes de
	// devolver que la resolución identifica la versión y trae TODOS los pines
	// obligatorios del motor (AC-06).
	resolved, err := d.Resolver.ResolveVersion(ctx, eng.Name, req.Version)
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

	// 7) Puerto libre (RF-3): se reserva de forma atómica y la reserva se libera
	// al terminar (la instancia registrada ya protege su puerto ante el checker).
	port := 0
	var releaseReservation func()
	if d.Ports != nil {
		preferred := req.Port
		if preferred <= 0 {
			preferred = 25565
		}
		res, perr := d.Ports.Reserve(ctx, slug, preferred)
		if perr != nil {
			return nil, perr
		}
		port = res.Port
		releaseReservation = func() { _ = d.Ports.Release(ctx, slug) }
		defer releaseReservation()
	} else {
		base := req.Port
		if base <= 0 {
			base = 25565
		}
		var nerr error
		port, nerr = d.Manager.NextPort(base)
		if nerr != nil {
			return nil, nerr
		}
	}

	spec := &instance.Spec{
		Slug:             slug,
		Name:             name,
		Engine:           eng.Name,
		Version:          resolved.Version,
		Pins:             resolved.Pins,
		Memory:           memory,
		CPU:              req.CPU,
		Port:             port,
		PortReservation:  port,
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

	// La instancia ya está escrita: a partir de aquí, cualquier fallo deja un
	// estado PARCIAL recuperable (la configuración no se pierde).
	warnings := []string{}
	res := &Result{
		Spec:          spec,
		Warnings:      warnings,
		Outcome:       result.OutcomeApplied,
		Compatibility: resolved.Compatibility,
		JavaMajor:     resolved.JavaMajor,
		JavaStatus:    resolved.JavaStatus,
	}

	// 8) Plugins/mods.
	if len(req.Plugins) > 0 {
		inst := &plugins.Installer{DataDir: d.Cfg.DataDir}
		names := make([]string, 0, len(req.Plugins))
		for _, src := range req.Plugins {
			added, aerr := inst.Add(ctx, spec, src)
			if aerr != nil {
				res.Outcome = result.OutcomePartial
				return res, aerr
			}
			names = append(names, added)
		}
		spec.Plugins = append(spec.Plugins, names...)
		_ = d.Manager.Save(spec)
		if w := plugins.WarnFor(names); w != "" {
			res.Warnings = append(res.Warnings, w)
		}
	}

	// 9) Arranque opcional.
	if req.Start {
		if d.Runtime == nil {
			res.Outcome = result.OutcomePartial
			return res, errx.New(errx.CodeGeneral, "no se puede arrancar: falta el runtime", "")
		}
		if err := d.Runtime.Start(ctx, spec); err != nil {
			res.Outcome = result.OutcomePartial
			return res, err
		}
		res.Started = true
	}
	return res, nil
}

func quote(s string) string { return "\"" + s + "\"" }
