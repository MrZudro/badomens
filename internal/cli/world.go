package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/MrZudro/badomens/internal/worlds"
	"github.com/spf13/cobra"
)

func newWorldCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "world",
		Short: "Biblioteca de mundos compartida",
	}
	cmd.AddCommand(
		newWorldListCmd(e),
		newWorldSaveCmd(e),
		newWorldUseCmd(e),
		newWorldExportCmd(e),
		newWorldImportCmd(e),
	)
	return cmd
}

func (e *Env) worldsManager() (*worlds.Manager, error) {
	cfg, err := e.requireConfig()
	if err != nil {
		return nil, err
	}
	return &worlds.Manager{DataDir: cfg.DataDir}, nil
}

func newWorldListCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Lista los mundos de la biblioteca (y el activo de la instancia)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := e.worldsManager()
			if err != nil {
				return e.finish(result.FromError("world list", err), "")
			}
			list, err := m.List()
			if err != nil {
				return e.finish(result.FromError("world list", err), "")
			}
			data := map[string]any{"library": list}
			human := fmt.Sprintf("Mundos en la biblioteca (%d):", len(list))
			if len(list) == 0 {
				human += "\n  (vacía)"
			}
			for _, w := range list {
				human += "\n  " + w
			}
			if e.instanceFlag != "" {
				spec, err := e.specFor(e.instanceFlag)
				if err != nil {
					return e.finish(result.FromError("world list", err), "")
				}
				cfg, _ := e.requireConfig()
				active := isDir(filepath.Join(cfg.DataDir, "instances", spec.Slug, "server", "world"))
				data["instance"] = spec.Slug
				data["active_world"] = active
				human += fmt.Sprintf("\nMundo activo de %s: %v", spec.Slug, active)
			}
			return e.finish(result.OK("world list", "mundos", data), human)
		},
	}
}

func newWorldSaveCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "save <nombre>",
		Short: "Guarda el mundo activo de la instancia en la biblioteca",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("world save", err), "")
			}
			m, err := e.worldsManager()
			if err != nil {
				return e.finish(result.FromError("world save", err), "")
			}
			res := mutatingResult("world save", spec.Slug, func() error {
				return m.SaveWorld(instDirFrom(e, spec.Slug), args[0])
			}, e, cmd)
			return e.finish(res, humanFor(res))
		},
	}
}

func newWorldUseCmd(e *Env) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "use <nombre>",
		Short: "Despliega un mundo de la biblioteca como activo (sustituye el actual)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("world use", err), "")
			}
			ok, err := e.confirm("sustituir el mundo activo por", args[0], yes)
			if err != nil || !ok {
				return e.finish(result.FromError("world use", errx.Usage("operación cancelada", "")), "operación cancelada")
			}
			m, err := e.worldsManager()
			if err != nil {
				return e.finish(result.FromError("world use", err), "")
			}
			res := mutatingResult("world use", spec.Slug, func() error {
				return m.UseWorld(instDirFrom(e, spec.Slug), args[0])
			}, e, cmd)
			return e.finish(res, humanFor(res))
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirmar sin preguntar")
	return cmd
}

func newWorldExportCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "export [nombre]",
		Short: "Exporta el mundo activo a un zip verificable",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("world export", err), "")
			}
			name := spec.Slug
			if len(args) == 1 && args[0] != "" {
				name = args[0]
			}
			m, err := e.worldsManager()
			if err != nil {
				return e.finish(result.FromError("world export", err), "")
			}
			var outPath, outSha string
			phases, err := e.withStopped(cmd, spec.Slug, func() error {
				var eerr error
				outPath, outSha, eerr = m.Export(instDirFrom(e, spec.Slug), name)
				return eerr
			})
			if err != nil {
				res := result.FromError("world export", err)
				res.WithPhases(phases)
				return e.finish(res, "")
			}
			res := result.OK("world export", "mundo exportado: "+outPath, map[string]any{
				"path": outPath, "sha256": outSha,
			}).WithPhases(phases)
			return e.finish(res, "Exportado: "+outPath+"\nsha256: "+outSha)
		},
	}
}

func newWorldImportCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "import <zip> <nombre>",
		Short: "Importa un zip de mundo a la biblioteca",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := e.worldsManager()
			if err != nil {
				return e.finish(result.FromError("world import", err), "")
			}
			if err := m.ImportFromZip(args[0], args[1]); err != nil {
				return e.finish(result.FromError("world import", err), "")
			}
			human := fmt.Sprintf("Mundo %q importado desde %s", args[1], args[0])
			return e.finish(result.OK("world import", human, map[string]any{"name": args[1]}), human)
		},
	}
}

// mutatingResult ejecuta una operación con la instancia detenida (relanzándola
// si estaba corriendo).
func mutatingResult(action, slug string, mutate func() error, e *Env, cmd *cobra.Command) *result.Result {
	phases, err := e.withStopped(cmd, slug, mutate)
	if err != nil {
		res := result.FromError(action, err)
		res.WithPhases(phases)
		return res
	}
	return result.OK(action, action+" en "+slug+" completado", map[string]any{"instance": slug}).WithPhases(phases)
}

func instDirFrom(e *Env, slug string) string {
	cfg, _ := e.requireConfig()
	return filepath.Join(cfg.DataDir, "instances", slug)
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
