package cli

import (
	"fmt"
	"strings"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

func newEngineCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "engine",
		Short: "Catálogo y selección de motor/versión",
	}
	cmd.AddCommand(newEngineListCmd(e), newEngineCurrentCmd(e), newEngineUseCmd(e))
	return cmd
}

func newEngineListCmd(e *Env) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "list [motor]",
		Short: "Lista los motores o las versiones de uno (paper, fabric, forge, neoforge, vanilla)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry := reg()
			if len(args) == 0 {
				names := registry.List()
				out := make([]map[string]any, 0, len(names))
				for _, en := range names {
					out = append(out, map[string]any{"name": en.Name, "label": en.Label, "type": en.TypeValue, "plugin_dir": en.PluginDir})
				}
				human := "Motores:\n"
				for _, en := range names {
					human += fmt.Sprintf("  %-10s %s (TYPE=%s)\n", en.Name, en.Label, en.TypeValue)
				}
				return e.finish(result.OK("engine list", "motores disponibles", out), strings.TrimRight(human, "\n"))
			}

			provider, err := registry.Provider(args[0])
			if err != nil {
				return e.finish(result.FromError("engine list", err), "")
			}
			versions, err := provider.ListVersions(cmd.Context())
			if err != nil {
				return e.finish(result.FromError("engine list", err), "")
			}
			if limit > 0 && len(versions) > limit {
				versions = versions[len(versions)-limit:]
			}
			list := make([]string, 0, len(versions))
			for _, v := range versions {
				list = append(list, v.Version)
			}
			human := fmt.Sprintf("Versiones de %s (%d):", provider.Engine().Label, len(list))
			for _, v := range list {
				human += "\n  " + v
			}
			return e.finish(result.OK("engine list", fmt.Sprintf("%d versiones de %s", len(list), provider.Engine().Name), list), human)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "mostrar solo las N últimas versiones")
	return cmd
}

func newEngineCurrentCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "current <instancia>",
		Short: "Motor y versión configurados de una instancia",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.specFor(args[0])
			if err != nil {
				return e.finish(result.FromError("engine current", err), "")
			}
			data := map[string]any{"instance": spec.Slug, "engine": spec.Engine, "version": spec.Version, "pins": spec.Pins}
			human := fmt.Sprintf("%s: %s %s", spec.Slug, spec.Engine, spec.Version)
			return e.finish(result.OK("engine current", human, data), human)
		},
	}
}

func newEngineUseCmd(e *Env) *cobra.Command {
	var version string
	var noApply bool
	cmd := &cobra.Command{
		Use:   "use <instancia> <motor>",
		Short: "Cambia motor/versión de una instancia (o la crea desde cero)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug, engineName := args[0], args[1]
			provider, err := reg().Provider(engineName)
			if err != nil {
				return e.finish(result.FromError("engine use", err), "")
			}
			resolved, err := provider.Resolve(cmd.Context(), version)
			if err != nil {
				return e.finish(result.FromError("engine use", err), "")
			}

			m, err := e.manager()
			if err != nil {
				return e.finish(result.FromError("engine use", err), "")
			}
			spec, err := m.Load(slug)
			if err != nil {
				// No existe: no se puede cambiar el motor de una instancia
				// inexistente.
				return e.finish(result.FromError("engine use", err), "")
			}
			spec.Engine = provider.Engine().Name
			spec.Version = resolved.Version
			spec.Pins = resolved.Pins
			if err := spec.Validate(m.EngineNames()); err != nil {
				return e.finish(result.FromError("engine use", errx.Wrap(errx.CodeGeneral, err, "configuración inválida", "")), "")
			}
			if err := m.Save(spec); err != nil {
				return e.finish(result.FromError("engine use", err), "")
			}

			human := fmt.Sprintf("%s → %s %s (%s)", spec.Slug, spec.Engine, spec.Version, strings.TrimSpace(resolved.Detail))
			if noApply {
				return e.finish(result.OK("engine use", human+" (sin aplicar)", map[string]any{"instance": spec.Slug}), human+" (sin aplicar)")
			}

			rt, err := e.runtime()
			if err != nil {
				return e.finish(result.FromError("engine use", err), "")
			}
			state, err := rt.State(cmd.Context(), slug)
			if err != nil {
				return e.finish(result.FromError("engine use", err), "")
			}
			if state == "running" {
				if err := rt.Restart(cmd.Context(), slug); err != nil {
					return e.finish(result.FromError("engine use", err), "")
				}
				human += " · reiniciado"
			} else if err := rt.Start(cmd.Context(), spec); err != nil {
				return e.finish(result.FromError("engine use", err), "")
			}
			data := map[string]any{"instance": spec.Slug, "engine": spec.Engine, "version": spec.Version, "pins": spec.Pins}
			return e.finish(result.OK("engine use", human, data), human)
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "versión de Minecraft (vacía = última)")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "solo escribir la configuración, no aplicar")
	return cmd
}
