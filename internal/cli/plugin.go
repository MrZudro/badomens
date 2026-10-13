package cli

import (
	"fmt"

	"github.com/manuelserver/badomens/internal/errx"
	"github.com/manuelserver/badomens/internal/instance"
	"github.com/manuelserver/badomens/internal/plugins"
	"github.com/manuelserver/badomens/internal/result"
	"github.com/spf13/cobra"
)

func newPluginCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Plugins/mods por instancia (según motor)",
	}
	cmd.AddCommand(newPluginAddCmd(e), newPluginListCmd(e), newPluginRemoveCmd(e))
	return cmd
}

func (e *Env) pluginInstaller() (*plugins.Installer, error) {
	cfg, err := e.requireConfig()
	if err != nil {
		return nil, err
	}
	return &plugins.Installer{DataDir: cfg.DataDir}, nil
}

func newPluginAddCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "add <archivo-o-url>",
		Short: "Instala un plugin/mod en la instancia (carpeta destino según motor)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("plugin add", err), "")
			}
			inst, err := e.pluginInstaller()
			if err != nil {
				return e.finish(result.FromError("plugin add", err), "")
			}
			added, err := inst.Add(cmd.Context(), spec, args[0])
			if err != nil {
				return e.finish(result.FromError("plugin add", err), "")
			}
			names := []string{added}
			spec.Plugins = appendUnique(spec.Plugins, added)
			_ = specSaved(e, spec)

			human := fmt.Sprintf("%s instalado en %s/%s", added, spec.Slug, inst.Dir(spec))
			if w := plugins.WarnFor(names); w != "" {
				human += "\n⚠️ " + w
				res := result.Warn("plugin add", human, map[string]any{"instance": spec.Slug, "added": added, "warning": w})
				return e.finish(res, human)
			}
			return e.finish(result.OK("plugin add", human, map[string]any{"instance": spec.Slug, "added": added}), human)
		},
	}
}

func newPluginListCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Lista los plugins/mods instalados",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("plugin list", err), "")
			}
			inst, err := e.pluginInstaller()
			if err != nil {
				return e.finish(result.FromError("plugin list", err), "")
			}
			list, err := inst.List(spec)
			if err != nil {
				return e.finish(result.FromError("plugin list", err), "")
			}
			human := fmt.Sprintf("Plugins/mods de %s (%d):", spec.Slug, len(list))
			for _, p := range list {
				human += "\n  " + p
			}
			return e.finish(result.OK("plugin list", "plugins", map[string]any{"instance": spec.Slug, "plugins": list}), human)
		},
	}
}

func newPluginRemoveCmd(e *Env) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "remove <nombre>",
		Short: "Elimina un plugin/mod instalado",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("plugin remove", err), "")
			}
			ok, err := e.confirm("eliminar el plugin", args[0], yes)
			if err != nil || !ok {
				return e.finish(result.FromError("plugin remove", errx.Usage("operación cancelada", "")), "operación cancelada")
			}
			inst, err := e.pluginInstaller()
			if err != nil {
				return e.finish(result.FromError("plugin remove", err), "")
			}
			if err := inst.Remove(spec, args[0]); err != nil {
				return e.finish(result.FromError("plugin remove", err), "")
			}
			spec.Plugins = removeString(spec.Plugins, args[0])
			_ = specSaved(e, spec)
			human := "plugin " + args[0] + " eliminado de " + spec.Slug
			return e.finish(result.OK("plugin remove", human, map[string]any{"instance": spec.Slug, "removed": args[0]}), human)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirmar sin preguntar")
	return cmd
}

func specSaved(e *Env, spec *instance.Spec) error {
	m, err := e.manager()
	if err != nil {
		return err
	}
	return m.Save(spec)
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func removeString(list []string, v string) []string {
	out := list[:0]
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
