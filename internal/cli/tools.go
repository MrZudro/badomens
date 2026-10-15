package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/doctor"
	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/host"
	"github.com/MrZudro/badomens/internal/infra"
	"github.com/MrZudro/badomens/internal/tailscale"
	"github.com/MrZudro/badomens/internal/tools"
	"github.com/MrZudro/badomens/internal/update"
	"github.com/MrZudro/badomens/internal/version"
)

// buildRegistry construye el registro único de herramientas expuesto por MCP y,
// a través de él, al agente de Emiria/Jamcord.
func buildRegistry(e *Env) (*tools.Registry, error) {
	cfg := e.cfg
	registry := tools.New()

	readOnly := []*tools.Tool{
		{
			Name:        "badomens_version",
			Description: "Versión del binario badomens.",
			ReadOnly:    true,
			Handler: func(_ context.Context, _ tools.Args) (*tools.Result, error) {
				return &tools.Result{
					Text: "badomens " + version.Version,
					Data: map[string]any{"version": version.Version, "commit": version.Commit, "date": version.Date},
				}, nil
			},
		},
		{
			Name:        "badomens_capacity",
			Description: "Capacidad del equipo (CPU y RAM) y plan de instancias (máx. registradas y en paralelo).",
			ReadOnly:    true,
			Handler: func(ctx context.Context, _ tools.Args) (*tools.Result, error) {
				cap := host.Detect(ctx)
				data := map[string]any{
					"ncpu":            cap.NCPU,
					"mem_total_bytes": cap.MemTotalBytes,
					"mem_total_gib":   cap.MemTotalGiB(),
					"source":          cap.Source,
					"max_instances":   cfg.MaxInstances,
					"max_parallel":    cfg.MaxParallel,
					"default_memory":  cfg.DefaultMemory,
				}
				return &tools.Result{Text: "capacidad del equipo detectada", Data: data}, nil
			},
		},
		{
			Name:        "badomens_doctor",
			Description: "Diagnóstico del entorno: Docker, Docker Compose, Tailscale y coherencia de la configuración.",
			ReadOnly:    true,
			Handler: func(ctx context.Context, _ tools.Args) (*tools.Result, error) {
				r := doctor.Run(ctx, cfg)
				checks := make([]map[string]any, 0, len(r.Checks))
				for _, c := range r.Checks {
					checks = append(checks, map[string]any{"name": c.Name, "status": c.Status, "message": c.Message})
				}
				text := "doctor: " + itoa(r.OK) + " ok, " + itoa(r.Warns) + " aviso(s), " + itoa(r.Errors) + " error(es)"
				return &tools.Result{Text: text, Data: map[string]any{
					"checks":  checks,
					"summary": map[string]int{"ok": r.OK, "warns": r.Warns, "errors": r.Errors},
				}}, nil
			},
		},
		{
			Name:        "badomens_update_check",
			Description: "Comprueba si hay una versión nueva de badomens publicada en GitHub.",
			ReadOnly:    true,
			Handler: func(ctx context.Context, _ tools.Args) (*tools.Result, error) {
				info, err := update.Check(ctx, cfg.Repo)
				if err != nil {
					return nil, err
				}
				return &tools.Result{Text: updateMessage(info), Data: map[string]any{
					"current":          info.Current,
					"latest":           info.Latest,
					"update_available": info.UpdateAvailable,
					"url":              info.URL,
				}}, nil
			},
		},
		{
			Name:        "badomens_connection",
			Description: "Información de conexión de una instancia: dirección, MagicDNS, versión, loader y pasos para el jugador (RF-2).",
			ReadOnly:    true,
			Params:      []tools.Param{{Name: "instance", Type: tools.TypeString, Description: "slug de la instancia", Required: true}},
			Handler: func(ctx context.Context, args tools.Args) (*tools.Result, error) {
				slug, err := args.String("instance")
				if err != nil {
					return nil, err
				}
				spec, serr := e.specFor(slug)
				if serr != nil {
					return nil, serr
				}
				info := buildConnection(ctx, e, spec)
				data := map[string]any{"connection": connectionData(info), "player_steps": info.PlayerSteps, "invitation": info.Invitation()}
				return &tools.Result{Text: "conexión de " + slug + ": " + info.Preferred(), Data: data}, nil
			},
		},
		{
			Name:        "badomens_ports_check",
			Description: "Comprueba si un puerto está libre (registro, docker, host y tailnet) y sugiere uno alternativo (RF-3).",
			ReadOnly:    true,
			Params:      []tools.Param{{Name: "port", Type: tools.TypeInt, Description: "puerto a comprobar", Required: true}},
			Handler: func(ctx context.Context, args tools.Args) (*tools.Result, error) {
				port, verr := args.Int("port")
				if verr != nil {
					return nil, verr
				}
				alloc, aerr := e.portsAllocator(ctx)
				if aerr != nil {
					return nil, aerr
				}
				res, cerr := alloc.Check(ctx, netip.Addr{}, port)
				if cerr != nil {
					return nil, cerr
				}
				data := map[string]any{
					"port":      res.Port,
					"free":      res.Free,
					"suggested": res.Suggested,
					"conflicts": conflictsData(res.Conflicts),
				}
				text := "puerto " + strconv.Itoa(port) + ": " + boolFree(res.Free)
				if res.Suggested != 0 {
					text += " (sugerido " + strconv.Itoa(res.Suggested) + ")"
				}
				return &tools.Result{Text: text, Data: data}, nil
			},
		},
		{
			Name:        "badomens_engine_versions",
			Description: "Versiones disponibles y compatibles de un motor (RF-5), con los campos normalizados (compatibility, java_major, java_status, loader_version, build, source, fetched_at, stale, warning) y el estado del catálogo. Misma ruta que 'engine versions'.",
			ReadOnly:    true,
			Params: []tools.Param{
				{Name: "engine", Type: tools.TypeString, Description: "paper|fabric|forge|neoforge|vanilla", Required: true, Enum: catalogEngineEnum()},
				{Name: "all", Type: tools.TypeBool, Description: "incluir snapshots/betas"},
			},
			Handler: func(ctx context.Context, args tools.Args) (*tools.Result, error) {
				engine, err := args.String("engine")
				if err != nil {
					return nil, err
				}
				all, _ := args.Bool("all")
				service, serr := e.CatalogService()
				if serr != nil {
					return nil, serr
				}
				// Misma ruta que `engine versions` (CLI) y que el asistente (TUI): T-01.
				res, lerr := service.ListVersions(ctx, engine, engines.Filters{IncludeUnstable: all})
				if lerr != nil {
					return nil, lerr
				}
				engineLabel := res.Engine
				if en, ok := engines.ByName(res.Engine); ok {
					engineLabel = en.Label
				}
				out := make([]map[string]any, 0, len(res.Versions))
				for _, v := range res.Versions {
					out = append(out, engineVersionData(v))
				}
				data := map[string]any{
					"engine":   res.Engine,
					"stale":    res.Stale,
					"versions": out,
					"catalog":  catalogStatusData(res.Status),
				}
				if res.Warning != "" {
					data["warning"] = res.Warning
				}
				return &tools.Result{
					Text: itoa(len(res.Versions)) + " versiones de " + engineLabel,
					Data: data,
				}, nil
			},
		},
		{
			Name:        "badomens_engine_resolve",
			Description: "Resuelve una versión de Minecraft a los pins finales del motor (misma ruta que 'engine use'): version, pins, build, loader_version, java_major, java_status, compatibility, source y el estado del catálogo. Una versión inexistente es un error tipado (not_found).",
			ReadOnly:    true,
			Params: []tools.Param{
				{Name: "engine", Type: tools.TypeString, Description: "paper|fabric|forge|neoforge|vanilla", Required: true, Enum: catalogEngineEnum()},
				{Name: "version", Type: tools.TypeString, Description: "versión de Minecraft (vacío = la última estable)"},
			},
			Handler: func(ctx context.Context, args tools.Args) (*tools.Result, error) {
				engine, err := args.String("engine")
				if err != nil {
					return nil, err
				}
				version, err := args.OptionalString("version")
				if err != nil {
					return nil, err
				}
				service, serr := e.CatalogService()
				if serr != nil {
					return nil, serr
				}
				resolved, rerr := service.ResolveVersion(ctx, engine, version)
				if rerr != nil {
					return nil, rerr
				}
				status, _ := service.GetCatalogStatus(ctx, engine)
				name := engine
				if en, ok := engines.ByName(engine); ok {
					name = en.Name
				}
				data := catalogResolvedData(name, resolved, status)
				text := name + " " + resolved.Version + " · pins " + catalogPinsText(resolved.Pins)
				if resolved.Warning != "" {
					text += " · aviso: " + resolved.Warning
				}
				return &tools.Result{Text: text, Data: data}, nil
			},
		},
		{
			Name:        "badomens_catalog_status",
			Description: "Estado del catálogo de versiones de cada motor (fresh, stale, invalid, unavailable) SIN tocar la red: fuente, antigüedad, versión recomendada y aviso.",
			ReadOnly:    true,
			Params: []tools.Param{
				{Name: "engine", Type: tools.TypeString, Description: "paper|fabric|forge|neoforge|vanilla (vacío = todos)", Enum: catalogEngineEnum()},
			},
			Handler: func(ctx context.Context, args tools.Args) (*tools.Result, error) {
				name, err := args.OptionalString("engine")
				if err != nil {
					return nil, err
				}
				service, serr := e.CatalogService()
				if serr != nil {
					return nil, serr
				}
				names := engineNames()
				if strings.TrimSpace(name) != "" {
					names = []string{name}
				}
				out := make([]map[string]any, 0, len(names))
				for _, n := range names {
					st, serr := service.GetCatalogStatus(ctx, n)
					if serr != nil {
						return nil, serr
					}
					out = append(out, catalogStatusData(st))
				}
				return &tools.Result{
					Text: itoa(len(out)) + " catálogo(s) consultado(s)",
					Data: map[string]any{"catalogs": out},
				}, nil
			},
		},
		{
			Name:        "badomens_engine_compat",
			Description: "Cruza Minecraft + motor + loader + Java + imagen Docker + plugins y devuelve la compatibilidad TRIVALENTE (compatible|incompatible|unknown). Un dato desconocido NUNCA se convierte en compatible.",
			ReadOnly:    true,
			Params: []tools.Param{
				{Name: "engine", Type: tools.TypeString, Description: "paper|fabric|forge|neoforge|vanilla", Required: true, Enum: catalogEngineEnum()},
				{Name: "version", Type: tools.TypeString, Description: "versión de Minecraft"},
				{Name: "loader_version", Type: tools.TypeString, Description: "versión exacta del loader (Fabric)"},
				{Name: "build", Type: tools.TypeString, Description: "build exacto del motor"},
				{Name: "java_major", Type: tools.TypeInt, Description: "Java requerido (0 = desconocido)"},
				{Name: "image", Type: tools.TypeString, Description: "imagen Docker que se va a usar (itzg/minecraft-server:javaN)"},
				{Name: "fill", Type: tools.TypeBool, Description: "completa con el catálogo los datos que falten (consulta la red)"},
				{Name: "plugins", Type: tools.TypeArray, Description: "plugins declarados", Items: &tools.Param{
					Type: tools.TypeObject,
					Properties: []tools.Param{
						{Name: "name", Type: tools.TypeString, Description: "nombre del plugin", Required: true},
						{Name: "loaders", Type: tools.TypeArray, Description: "ecosistemas declarados (paper, bukkit, spigot, fabric, forge, neoforge)"},
						{Name: "minecraft", Type: tools.TypeArray, Description: "versiones de Minecraft declaradas como compatibles"},
					},
				}},
			},
			Handler: func(ctx context.Context, args tools.Args) (*tools.Result, error) {
				engine, err := args.String("engine")
				if err != nil {
					return nil, err
				}
				version, err := args.OptionalString("version")
				if err != nil {
					return nil, err
				}
				loader, err := args.OptionalString("loader_version")
				if err != nil {
					return nil, err
				}
				build, err := args.OptionalString("build")
				if err != nil {
					return nil, err
				}
				image, err := args.OptionalString("image")
				if err != nil {
					return nil, err
				}
				javaMajor, err := args.OptionalInt("java_major", 0)
				if err != nil {
					return nil, err
				}
				fill, _ := args.Bool("fill")
				plugins, perr := catalogPluginRequirements(args)
				if perr != nil {
					return nil, perr
				}
				service, serr := e.CatalogService()
				if serr != nil {
					return nil, serr
				}
				res, cerr := service.CheckCompatibility(ctx, engines.CompatibilityRequest{
					Engine:           engine,
					MinecraftVersion: version,
					LoaderVersion:    loader,
					Build:            build,
					JavaMajor:        javaMajor,
					DockerImage:      image,
					Plugins:          plugins,
					Fill:             fill,
				})
				if cerr != nil {
					return nil, cerr
				}
				text := string(res.Compatibility) + " · " + res.Engine + " " + res.MinecraftVersion
				if res.Reason != "" {
					text += " (" + res.Reason + ")"
				}
				return &tools.Result{Text: text, Data: catalogCompatibilityData(res)}, nil
			},
		},
		{
			Name:        "badomens_docker_status",
			Description: "Estado de Docker: instalado, daemon, compose y contexto; con el motivo estable en reason (RF-6).",
			ReadOnly:    true,
			Handler: func(ctx context.Context, _ tools.Args) (*tools.Result, error) {
				diag := dockerx.Diagnose(ctx, dockerx.DefaultEnv())
				data := diagnosisData(diag)
				return &tools.Result{Text: string(diag.State) + ": " + diag.Message, Data: data}, nil
			},
		},
		{
			Name:        "badomens_tailscale_status",
			Description: "Estado de la red privada Tailscale: IP, MagicDNS y peers, con motivo tipado (RF-6).",
			ReadOnly:    true,
			Handler: func(ctx context.Context, _ tools.Args) (*tools.Result, error) {
				st, err := tailscale.Snapshot(ctx)
				if err != nil {
					return nil, err
				}
				ip, _ := tailscale.ResolveIPv4(ctx)
				magic, _ := tailscale.MagicDNS(ctx)
				data := tailscaleData(st, ip, magic)
				data["status"] = infraStateData(infra.NewTailscale(tailscale.DefaultEnv()).Detect(ctx))
				return &tools.Result{Text: "tailscale ip=" + orDash(ip), Data: data}, nil
			},
		},
		{
			Name:        "badomens_infra_status",
			Description: "Estado tipado de los servicios externos (Docker, Tailscale y el daemon propio): ready/down/not_installed/needs_login… con motivo estable (Fase 8).",
			ReadOnly:    true,
			Handler: func(ctx context.Context, _ tools.Args) (*tools.Result, error) {
				dockerSt := infra.NewDocker(dockerx.DefaultEnv()).Detect(ctx)
				tsSt := infra.NewTailscale(tailscale.DefaultEnv()).Detect(ctx)
				daemonSt := daemonComponent(e).Detect(ctx)
				data := map[string]any{
					"docker":    infraStateData(dockerSt),
					"tailscale": infraStateData(tsSt),
					"daemon":    infraStateData(daemonSt),
				}
				text := "docker=" + string(dockerSt.Status) + " tailscale=" + string(tsSt.Status) + " daemon=" + string(daemonSt.Status)
				return &tools.Result{Text: text, Data: data}, nil
			},
		},
		{
			Name:        "badomens_idle_status",
			Description: "Estado de auto-apagado por inactividad de una instancia (RF-8).",
			ReadOnly:    true,
			Params:      []tools.Param{{Name: "instance", Type: tools.TypeString, Description: "slug de la instancia", Required: true}},
			Handler:     idleStatusHandler(e),
		},
		{
			Name:        "badomens_idle",
			Description: "Estado de auto-apagado por inactividad de una instancia (alias de badomens_idle_status, RF-8).",
			ReadOnly:    true,
			Params:      []tools.Param{{Name: "instance", Type: tools.TypeString, Description: "slug de la instancia", Required: true}},
			Handler:     idleStatusHandler(e),
		},
	}

	for _, t := range readOnly {
		if err := registry.Register(t); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// idleStatusHandler construye el manejador compartido del estado de auto-apagado.
func idleStatusHandler(e *Env) tools.Handler {
	return func(ctx context.Context, args tools.Args) (*tools.Result, error) {
		slug, err := args.String("instance")
		if err != nil {
			return nil, err
		}
		m, merr := e.manager()
		if merr != nil {
			return nil, merr
		}
		spec, lerr := m.Load(slug)
		if lerr != nil {
			return nil, lerr
		}
		rt, rerr := e.runtime()
		if rerr != nil {
			return nil, rerr
		}
		running := false
		if st, serr := rt.State(ctx, slug); serr == nil {
			running = st == "running"
		}
		data := idleData(spec.Idle)
		data["running"] = running
		return &tools.Result{Text: "idle de " + slug + ": " + enabledText(spec.Idle), Data: data}, nil
	}
}

// catalogEngineEnum es la lista de motores admitidos por las herramientas MCP.
func catalogEngineEnum() []string { return engines.Names() }

// catalogResolvedData serializa una resolución normalizada. MCP y la CLI publican
// SIEMPRE los mismos campos estructurados, sin depender de mensajes visuales.
func catalogResolvedData(engine string, res engines.Resolved, status engines.CatalogStatus) map[string]any {
	pins := res.Pins
	if pins == nil {
		pins = map[string]string{}
	}
	data := map[string]any{
		"engine":        engine,
		"version":       res.Version,
		"pins":          pins,
		"compatibility": string(res.Compatibility),
		"java_major":    res.JavaMajor,
		"java_status":   res.JavaStatus,
		"source":        res.Source,
		"stale":         status.State != engines.CatalogFresh && status.State != engines.CatalogStale,
		"catalog":       catalogStatusData(status),
	}
	if res.Build != "" {
		data["build"] = res.Build
	}
	if res.LoaderVersion != "" {
		data["loader_version"] = res.LoaderVersion
	}
	if res.Image != "" {
		data["image"] = res.Image
	}
	if !status.FetchedAt.IsZero() {
		data["fetched_at"] = status.FetchedAt
	}
	if res.Warning != "" {
		data["warning"] = res.Warning
	}
	return data
}

// catalogPinsText redacta los pins de forma legible (clave=valor).
func catalogPinsText(pins map[string]string) string {
	if len(pins) == 0 {
		return "(ninguno)"
	}
	keys := make([]string, 0, len(pins))
	for k := range pins {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+pins[k])
	}
	return strings.Join(parts, " ")
}

// catalogPluginRequirements interpreta el array de plugins declarados de una llamada MCP.
func catalogPluginRequirements(args tools.Args) ([]engines.PluginRequirement, error) {
	raw, ok := args["plugins"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var list []struct {
		Name      string   `json:"name"`
		Loaders   []string `json:"loaders"`
		Minecraft []string `json:"minecraft"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("el argumento %q debe ser una lista de plugins: %w", "plugins", err)
	}
	out := make([]engines.PluginRequirement, 0, len(list))
	for _, p := range list {
		if strings.TrimSpace(p.Name) == "" {
			return nil, fmt.Errorf("hay un plugin sin nombre en %q", "plugins")
		}
		out = append(out, engines.PluginRequirement{
			Name: p.Name, Loaders: p.Loaders, MinecraftVersions: p.Minecraft,
		})
	}
	return out, nil
}
