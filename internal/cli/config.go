package cli

import (
	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

func newConfigCmd(e *Env) *cobra.Command {
	var initFile bool
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Muestra la configuración resuelta (o crea el archivo con --init)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if initFile {
				cfg, err := e.requireConfig()
				if err != nil {
					return e.finish(result.FromError("config", err), "")
				}
				if err := cfg.Save(); err != nil {
					return e.finish(result.FromError("config", err), "")
				}
				path, _ := config.Path()
				res := result.OK("config", "configuración escrita en "+path, map[string]any{"path": path})
				return e.finish(res, "Configuración escrita en "+path)
			}

			cfg, err := e.requireConfig()
			if err != nil {
				return e.finish(result.FromError("config", err), "")
			}
			path, _ := config.Path()
			res := result.OK("config", "configuración resuelta", map[string]any{
				"path":           path,
				"data_dir":       cfg.DataDir,
				"image":          cfg.Image,
				"max_instances":  cfg.MaxInstances,
				"max_parallel":   cfg.MaxParallel,
				"default_memory": cfg.DefaultMemory,
				"tailscale":      cfg.Tailscale,
				"repo":           cfg.Repo,
				"update_check":   cfg.UpdateCheck,
			})
			human := "Configuración (" + path + ")\n" +
				"  data_dir: " + cfg.DataDir + "\n" +
				"  image: " + cfg.Image + "\n" +
				"  max_instances: " + itoa(cfg.MaxInstances) + "\n" +
				"  max_parallel: " + itoa(cfg.MaxParallel) + "\n" +
				"  default_memory: " + cfg.DefaultMemory + "\n" +
				"  tailscale: " + boolText(cfg.Tailscale.Enabled) + "\n" +
				"  repo: " + cfg.Repo + "\n" +
				"  update_check: " + boolText(cfg.UpdateCheck)
			return e.finish(res, human)
		},
	}
	cmd.Flags().BoolVar(&initFile, "init", false, "escribe el archivo de configuración por defecto")
	return cmd
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func boolText(b bool) string {
	if b {
		return "activado"
	}
	return "desactivado"
}
