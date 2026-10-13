package cli

import (
	"github.com/manuelserver/badomens/internal/host"
	"github.com/manuelserver/badomens/internal/result"
	"github.com/spf13/cobra"
)

func newCapacityCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "capacity",
		Short: "Muestra la capacidad del equipo (CPU y RAM) para planificar instancias",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cap := host.Detect(cmd.Context())
			cfg, err := e.requireConfig()
			if err != nil {
				return e.finish(result.FromError("capacity", err), "")
			}
			data := map[string]any{
				"ncpu":            cap.NCPU,
				"mem_total_bytes": cap.MemTotalBytes,
				"mem_total_gib":   cap.MemTotalGiB(),
				"source":          cap.Source,
				"max_instances":   cfg.MaxInstances,
				"max_parallel":    cfg.MaxParallel,
			}
			human := "Equipo: " + itoa(cap.NCPU) + " CPU · " +
				trimFloat(cap.MemTotalGiB()) + " GiB RAM (" + cap.Source + ")\n" +
				"Instancias: máx " + itoa(cfg.MaxInstances) + ", en paralelo máx " + itoa(cfg.MaxParallel) + "\n" +
				"RAM por defecto por instancia: " + cfg.DefaultMemory
			return e.finish(result.OK("capacity", "capacidad detectada", data), human)
		},
	}
}

func trimFloat(f float64) string {
	whole := int64(f)
	frac := int64((f - float64(whole)) * 10)
	if frac == 0 {
		return itoa(int(whole))
	}
	return itoa(int(whole)) + "." + itoa(int(frac))
}
