package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

// versionData serializa una versión con el modelo normalizado de la Fase 3. Los
// campos nuevos son ADITIVOS: se conservan `id`, `channel`, `released_at`,
// `recommended`, `java_major`, `build`, `detail`, `compatible` e
// `incompatible_why` del contrato anterior.
func versionData(v engines.Version) map[string]any {
	return map[string]any{
		"id":               v.ID(),
		"channel":          string(v.Channel),
		"released_at":      v.ReleasedAt,
		"recommended":      v.Recommended,
		"java_major":       v.JavaMajor,
		"java_status":      v.JavaStatus,
		"build":            v.Build,
		"loader_version":   v.LoaderVersion,
		"detail":           v.Detail,
		"compatibility":    string(v.Compatibility),
		"compatible":       v.Compatible,
		"incompatible_why": v.IncompatibleWhy,
		"source":           v.Source,
		"fetched_at":       v.FetchedAt,
		"stale":            v.Stale,
		"warning":          v.Warning,
	}
}

// versionLine redacta una línea legible de una versión.
func versionLine(v engines.Version) string {
	marker := "  "
	if v.Recommended {
		marker = " ★"
	}
	if v.IncompatibleWhy != "" {
		return fmt.Sprintf("%s %s ✖ (%s)", marker, v.ID(), v.IncompatibleWhy)
	}
	extra := ""
	if v.JavaMajor > 0 {
		extra += fmt.Sprintf("  Java %d", v.JavaMajor)
	}
	if v.Build != "" {
		extra += "  build " + v.Build
	}
	if v.LoaderVersion != "" {
		extra += "  loader " + v.LoaderVersion
	}
	if !v.ReleasedAt.IsZero() {
		extra += "  " + timeSince(v.ReleasedAt)
	}
	return fmt.Sprintf("%s %s%s", marker, v.ID(), extra)
}

func newEngineVersionsCmd(e *Env) *cobra.Command {
	var all bool
	var refresh bool
	cmd := &cobra.Command{
		Use:   "versions <motor>",
		Short: "Versiones disponibles y compatibles del motor (RF-5)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			versions, stale, status, verr := e.loadCatalog(cmd.Context(), args[0],
				engines.VersionOpts{IncludeUnstable: all}, refresh)
			if verr != nil {
				return e.finish(result.FromError("engine versions", verr), "")
			}
			engine, _ := engines.ByName(args[0])
			label := engine.Label
			if label == "" {
				label = args[0]
			}
			out := make([]map[string]any, 0, len(versions))
			human := fmt.Sprintf("Versiones de %s (%d):", label, len(versions))
			for _, v := range versions {
				out = append(out, versionData(v))
				human += "\n" + versionLine(v)
			}
			if stale {
				human += "\n(lista usada de caché o del catálogo embebido; sin conexión con el catálogo)"
			}
			data := map[string]any{
				"engine":   args[0],
				"stale":    stale,
				"catalog":  catalogStatusData(status),
				"versions": out,
			}
			if stale {
				data["warnings"] = []string{"lista usada de caché o del catálogo embebido; puede no estar al día"}
			}
			return e.finish(result.OK("engine versions", fmt.Sprintf("%d versiones de %s", len(versions), label), data), human)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "incluir snapshots/betas")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "refrescar el catálogo antes de listar")
	return cmd
}

// timeSince redacta el tiempo transcurrido desde una fecha.
func timeSince(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < 0:
		return ""
	case d < 24*time.Hour:
		return "hoy"
	case d < 48*time.Hour:
		return "ayer"
	case d < 30*24*time.Hour:
		return fmt.Sprintf("hace %d días", int(d/(24*time.Hour)))
	default:
		return fmt.Sprintf("hace %d meses", int(d/(30*24*time.Hour)))
	}
}

func newEngineCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "engine",
		Short: "Catálogo y selección de motor/versión",
	}
	cmd.AddCommand(newEngineListCmd(e), newEngineCurrentCmd(e), newEngineUseCmd(e), newEngineVersionsCmd(e))
	return cmd
}

func newEngineListCmd(e *Env) *cobra.Command {
	var limit int
	var all bool
	var refresh bool
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

			// MISMA ruta de caché, validación y normalización que `engine
			// versions`: se acabó el listado directo del proveedor sin caché.
			versions, stale, _, err := e.loadCatalog(cmd.Context(), args[0],
				engines.VersionOpts{IncludeUnstable: all}, refresh)
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
			label := args[0]
			if en, ok := engines.ByName(args[0]); ok {
				label = en.Label
			}
			human := fmt.Sprintf("Versiones de %s (%d):", label, len(list))
			for _, v := range list {
				human += "\n  " + v
			}
			if stale {
				human += "\n(lista usada de caché o del catálogo embebido; sin conexión con el catálogo)"
			}
			return e.finish(result.OK("engine list", fmt.Sprintf("%d versiones de %s", len(list), args[0]), list), human)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "mostrar solo las N últimas versiones")
	cmd.Flags().BoolVar(&all, "all", false, "incluir snapshots/betas")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "refrescar el catálogo antes de listar")
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
			eng, ok := engines.ByName(engineName)
			if !ok {
				return e.finish(result.FromError("engine use",
					errx.Usage("motor desconocido "+engineName, "usa uno de: "+strings.Join(engineNames(), ", "))), "")
			}
			// Ruta ÚNICA de resolución: delega en el proveedor y valida el
			// invariante de pins obligatorios antes de tocar la configuración
			// (AC-06: no existe una resolución parcialmente válida).
			resolved, err := reg().Resolve(cmd.Context(), eng.Name, version)
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
			spec.Engine = eng.Name
			spec.Version = resolved.Version
			spec.Pins = resolved.Pins
			if err := spec.Validate(m.EngineNames()); err != nil {
				return e.finish(result.FromError("engine use", errx.Wrap(errx.CodeGeneral, err, "configuración inválida", "")), "")
			}
			if err := m.Save(spec); err != nil {
				return e.finish(result.FromError("engine use", err), "")
			}

			human := fmt.Sprintf("%s → %s %s (%s)", spec.Slug, spec.Engine, spec.Version, strings.TrimSpace(resolved.Detail))
			detail := map[string]any{
				"instance":      spec.Slug,
				"engine":        spec.Engine,
				"version":       spec.Version,
				"pins":          spec.Pins,
				"build":         resolved.Build,
				"java_major":    resolved.JavaMajor,
				"java_status":   resolved.JavaStatus,
				"compatibility": string(resolved.Compatibility),
			}
			if resolved.Warning != "" {
				detail["warning"] = resolved.Warning
				human += " · aviso: " + resolved.Warning
			}
			if noApply {
				return e.finish(result.OK("engine use", human+" (sin aplicar)", detail), human+" (sin aplicar)")
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
			return e.finish(result.OK("engine use", human, detail), human)
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "versión de Minecraft (vacía = última)")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "solo escribir la configuración, no aplicar")
	return cmd
}
