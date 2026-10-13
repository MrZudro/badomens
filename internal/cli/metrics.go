package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/manuelserver/badomens/internal/backup"
	"github.com/manuelserver/badomens/internal/result"
	"github.com/manuelserver/badomens/internal/tps"
	"github.com/spf13/cobra"
)

func newMetricsCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "metrics",
		Short: "Métricas de una instancia: contenedor, mundo y backups",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("metrics", err), "")
			}
			cfg, _ := e.requireConfig()
			rt, err := e.runtime()
			if err != nil {
				return e.finish(result.FromError("metrics", err), "")
			}
			state, _ := rt.State(cmd.Context(), spec.Slug)

			data := map[string]any{"instance": spec.Slug, "state": state}
			human := fmt.Sprintf("Instancia %s · estado %s", spec.Slug, state)

			if state == "running" {
				cpu, used, limit, err := tps.ContainerStats(cmd.Context(), rt.ContainerName(spec.Slug))
				if err == nil {
					data["cpu_percent"] = cpu
					data["mem_used_mb"] = used
					data["mem_limit_mb"] = limit
					human += fmt.Sprintf("\nCPU %.1f%% · RAM %.0f / %.0f MB", cpu, used, limit)
				}
			}

			worldSize := dirSizeMB(filepath.Join(cfg.DataDir, "instances", spec.Slug, "server", "world"))
			data["world_size_mb"] = worldSize
			human += fmt.Sprintf("\nMundo: %.1f MB", worldSize)

			b := &backup.Backend{DataDir: cfg.DataDir}
			backups, err := b.List(spec.Slug)
			if err == nil {
				last := ""
				if len(backups) > 0 {
					last = backups[0].Name
				}
				data["backups"] = len(backups)
				data["last_backup"] = last
				human += fmt.Sprintf("\nBackups: %d (último: %s)", len(backups), last)
			}

			return e.finish(result.OK("metrics", "métricas de "+spec.Slug, data), human)
		},
	}
}

func dirSizeMB(dir string) float64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	mb := float64(total) / (1024 * 1024)
	return round1(mb)
}

func round1(f float64) float64 {
	return float64(int64(f*10)) / 10
}
