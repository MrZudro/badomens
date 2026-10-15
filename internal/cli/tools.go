package cli

import (
	"context"
	"net/netip"
	"strconv"
	"time"

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
			Description: "Versiones disponibles y compatibles de un motor (RF-5). Lista solo versiones que el motor publica.",
			ReadOnly:    true,
			Params: []tools.Param{
				{Name: "engine", Type: tools.TypeString, Description: "paper|fabric|forge|neoforge|vanilla", Required: true, Enum: []string{"paper", "fabric", "forge", "neoforge", "vanilla"}},
				{Name: "all", Type: tools.TypeBool, Description: "incluir snapshots/betas"},
			},
			Handler: func(ctx context.Context, args tools.Args) (*tools.Result, error) {
				engine, err := args.String("engine")
				if err != nil {
					return nil, err
				}
				all, _ := args.Bool("all")
				cache := engines.NewCache(cfg.DataDir, reg())
				if d, derr := time.ParseDuration(cfg.Versions.CacheTTL); derr == nil && d > 0 {
					cache.TTL = d
				}
				versions, stale, verr := cache.Versions(ctx, engine, engines.VersionOpts{IncludeUnstable: all})
				if verr != nil {
					return nil, verr
				}
				engineLabel := engine
				if en, ok := engines.ByName(engine); ok {
					engineLabel = en.Label
				}
				out := make([]map[string]any, 0, len(versions))
				for _, v := range versions {
					out = append(out, map[string]any{
						"id": v.ID(), "channel": string(v.Channel), "recommended": v.Recommended,
						"java_major": v.JavaMajor, "build": v.Build, "released_at": v.ReleasedAt,
						"compatible": v.Compatible, "incompatible_why": v.IncompatibleWhy,
					})
				}
				return &tools.Result{
					Text: itoa(len(versions)) + " versiones de " + engineLabel,
					Data: map[string]any{"engine": engine, "stale": stale, "versions": out},
				}, nil
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
