package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/MrZudro/badomens/internal/uninstall"
	"github.com/spf13/cobra"
)

func newUninstallCmd(e *Env) *cobra.Command {
	var yes, purge, dryRun bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Desinstala badomens: binario y configuración (los datos se conservan salvo --purge)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := e.requireConfig()
			if err != nil {
				return e.finish(result.FromError("uninstall", err), "")
			}
			if dryRun {
				paths, perr := uninstall.DefaultPaths(cfg)
				if perr != nil {
					return e.finish(result.FromError("uninstall", perr), "")
				}
				plan := uninstall.Plan(paths, uninstall.Options{Purge: purge})
				human := "Qué haría 'badomens uninstall' (--dry-run):\n"
				for _, p := range plan {
					human += "  - " + p + "\n"
				}
				human += "  - confirmación previa obligatoria"
				return e.finish(result.OK("uninstall", "previsualización de la desinstalación", map[string]any{"steps": plan}), strings.TrimRight(human, "\n"))
			}

			action := "desinstalar badomens"
			if purge {
				action += " y borrar TODOS los datos (instancias, mundos, backups)"
			}
			ok, cerr := e.confirm(action, "badomens", yes)
			if cerr != nil || !ok {
				return e.finish(result.FromError("uninstall", errx.Usage("operación cancelada", "")), "desinstalación cancelada")
			}

			paths, perr := uninstall.DefaultPaths(cfg)
			if perr != nil {
				return e.finish(result.FromError("uninstall", perr), "")
			}
			exe, _ := os.Executable()
			res, err := uninstall.Run(cmd.Context(), paths, exe, stopAllForUninstall(e), uninstall.Options{Purge: purge})
			if err != nil {
				return e.finish(result.FromError("uninstall", err), "")
			}
			human := renderUninstall(res, purge)
			return e.finish(result.OK("uninstall", human, map[string]any{
				"removed":  res.Removed,
				"kept":     res.Kept,
				"warnings": res.Warnings,
			}), human)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirmar sin preguntar")
	cmd.Flags().BoolVar(&purge, "purge", false, "borrar también TODOS los datos (instancias, mundos, backups)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "describir qué se hará sin borrar nada")
	return cmd
}

// stopAllForUninstall devuelve la función que detiene las instancias corriendo.
func stopAllForUninstall(e *Env) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		m, err := e.manager()
		if err != nil {
			return err
		}
		specs, err := m.List()
		if err != nil {
			return err
		}
		rt, err := e.runtime()
		if err != nil {
			return err
		}
		var first error
		for _, s := range specs {
			st, err := rt.State(ctx, s.Slug)
			if err != nil {
				continue
			}
			if st == "running" {
				if serr := rt.Stop(ctx, s.Slug); serr != nil && first == nil {
					first = serr
				}
			}
		}
		return first
	}
}

func renderUninstall(res *uninstall.Result, purge bool) string {
	var b strings.Builder
	b.WriteString("Desinstalación completada\n")
	for _, r := range res.Removed {
		fmt.Fprintf(&b, "  ✓ %s\n", r)
	}
	for _, k := range res.Kept {
		fmt.Fprintf(&b, "  ▶ %s\n", k)
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(&b, "  ⚠ %s\n", w)
	}
	if !purge {
		b.WriteString("  ▶ Para borrar también los datos: 'badomens uninstall --purge' (¡los mundos y backups se pierden!)\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
