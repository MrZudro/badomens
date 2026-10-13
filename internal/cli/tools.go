package cli

import (
	"context"

	"github.com/manuelserver/badomens/internal/config"
	"github.com/manuelserver/badomens/internal/doctor"
	"github.com/manuelserver/badomens/internal/host"
	"github.com/manuelserver/badomens/internal/tools"
	"github.com/manuelserver/badomens/internal/update"
	"github.com/manuelserver/badomens/internal/version"
)

// buildRegistry construye el registro único de herramientas expuesto por MCP y,
// a través de él, al agente de Emiria/Jamcord. En F0 son de solo lectura; las de
// gestión de instancias llegan en F1.
func buildRegistry(cfg *config.Config) (*tools.Registry, error) {
	reg := tools.New()

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
	}

	for _, t := range readOnly {
		if err := reg.Register(t); err != nil {
			return nil, err
		}
	}
	return reg, nil
}
