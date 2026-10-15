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

// engineVersionData serializa una versión con el modelo normalizado de la Fase 3. Los
// campos nuevos son ADITIVOS: se conservan `id`, `channel`, `released_at`,
// `recommended`, `java_major`, `build`, `detail`, `compatible` e
// `incompatible_why` del contrato anterior.
func engineVersionData(v engines.Version) map[string]any {
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
				out = append(out, engineVersionData(v))
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
	cmd.AddCommand(newEngineListCmd(e), newEngineCurrentCmd(e), newEngineUseCmd(e), newEngineVersionsCmd(e), newEngineCompatCmd(e))
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

// newEngineUseCmd cambia el motor y la versión de una instancia. La resolución
// pasa por el SERVICIO de catálogo (T-01) y el documento informa de si la
// operación terminó, falló o quedó parcialmente aplicada (AC-10).
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
			service, err := e.CatalogService()
			if err != nil {
				return e.finish(result.FromError("engine use", err), "")
			}
			// Ruta ÚNICA de resolución (T-01): delega en el servicio y valida el
			// invariante de pins obligatorios antes de tocar la configuración
			// (AC-06: no existe una resolución parcialmente válida).
			resolved, err := service.ResolveVersion(cmd.Context(), eng.Name, version)
			if err != nil {
				return e.finish(result.FromError("engine use", err), "")
			}
			status, _ := service.GetCatalogStatus(cmd.Context(), eng.Name)

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

			detail := map[string]any{
				"instance":       spec.Slug,
				"engine":         eng.Name,
				"version":        resolved.Version,
				"pins":           resolved.Pins,
				"build":          resolved.Build,
				"loader_version": resolved.LoaderVersion,
				"java_major":     resolved.JavaMajor,
				"java_status":    resolved.JavaStatus,
				"compatibility":  string(resolved.Compatibility),
				"source":         resolved.Source,
				"catalog":        catalogStatusData(status),
				"outcome":        string(result.OutcomeApplied),
			}
			phases := []result.Phase{result.PhaseFor("resolve", result.OutcomeApplied)}
			human := fmt.Sprintf("%s → %s %s (%s)", spec.Slug, eng.Name, resolved.Version, strings.TrimSpace(resolved.Detail))
			if resolved.Warning != "" {
				detail["warning"] = resolved.Warning
				human += " · aviso: " + resolved.Warning
			}

			// Nada se ha aplicado todavía: un fallo aquí no deja estado a medias.
			notApplied := func(err error) error {
				detail["outcome"] = string(result.OutcomeFailed)
				res := result.FromError("engine use", err)
				res.Data = detail
				res.WithPhases(phases)
				return e.finish(res, "")
			}

			spec.Engine = eng.Name
			spec.Version = resolved.Version
			spec.Pins = resolved.Pins
			if err := spec.Validate(m.EngineNames()); err != nil {
				return notApplied(errx.Wrap(errx.CodeGeneral, err, "configuración inválida", ""))
			}
			if err := m.Save(spec); err != nil {
				return notApplied(err)
			}
			// La configuración YA quedó escrita: cualquier fallo posterior deja
			// un estado PARCIAL recuperable (AC-10).
			phases = append(phases, result.PhaseFor("config", result.OutcomeApplied))
			if noApply {
				return e.finish(result.OK("engine use", human+" (sin aplicar)", detail).WithPhases(phases), human+" (sin aplicar)")
			}
			partial := func(err error) error {
				detail["outcome"] = string(result.OutcomePartial)
				detail["follow_up"] = "reintenta 'badomens -i " + spec.Slug + " start' cuando el entorno esté listo"
				res := result.FromError("engine use", err)
				res.Data = detail
				res.WithPhases(append(phases, result.PhaseFor("runtime", result.OutcomePartial)))
				return e.finish(res, "")
			}

			rt, err := e.runtime()
			if err != nil {
				return partial(err)
			}
			state, err := rt.State(cmd.Context(), slug)
			if err != nil {
				return partial(err)
			}
			if state == "running" {
				if err := rt.Restart(cmd.Context(), slug); err != nil {
					return partial(err)
				}
				human += " · reiniciado"
			} else if err := rt.Start(cmd.Context(), spec); err != nil {
				return partial(err)
			}
			phases = append(phases, result.PhaseFor("runtime", result.OutcomeApplied))
			return e.finish(result.OK("engine use", human, detail).WithPhases(phases), human)
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "versión de Minecraft (vacía = última)")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "solo escribir la configuración, no aplicar")
	return cmd
}

// newEngineCompatCmd cruza Minecraft + motor + loader + Java + imagen Docker +
// plugins con la compatibilidad TRIVALENTE del servicio (nunca convierte
// "desconocido" en "compatible"):
//
//	compatible   → exit 0
//	unknown      → exit 1 (aviso)
//	incompatible → exit 2 (error)
func newEngineCompatCmd(e *Env) *cobra.Command {
	var (
		version string
		loader  string
		build   string
		image   string
		javaMaj int
		fill    bool
		plugins []string
	)
	cmd := &cobra.Command{
		Use:   "compat <motor>",
		Short: "Comprueba la compatibilidad de Minecraft + motor + loader + Java + imagen + plugins",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := e.CatalogService()
			if err != nil {
				return e.finish(result.FromError("engine compat", err), "")
			}
			requirements := make([]engines.PluginRequirement, 0, len(plugins))
			for _, raw := range plugins {
				p, perr := catalogParsePluginSpec(raw)
				if perr != nil {
					return e.finish(result.FromError("engine compat", perr), "")
				}
				requirements = append(requirements, p)
			}
			res, err := service.CheckCompatibility(cmd.Context(), engines.CompatibilityRequest{
				Engine:           args[0],
				MinecraftVersion: version,
				LoaderVersion:    loader,
				Build:            build,
				JavaMajor:        javaMaj,
				DockerImage:      image,
				Plugins:          requirements,
				Fill:             fill,
			})
			if err != nil {
				return e.finish(result.FromError("engine compat", err), "")
			}
			data := catalogCompatibilityData(res)
			human := fmt.Sprintf("%s %s: %s", res.Engine, res.MinecraftVersion, res.Compatibility)
			if res.Reason != "" {
				human += " · " + res.Reason
			}
			if res.Warning != "" {
				human += " · aviso: " + res.Warning
			}
			switch res.Compatibility {
			case engines.CompatibilityIncompatible:
				// Un conflicto comprobado es un fallo, no un aviso.
				r := result.FromError("engine compat", errx.New(errx.CodeGeneral,
					"combinación incompatible: "+res.Reason, "cambia de versión, motor o loader"))
				r.Data = data
				return e.finish(r, "")
			case engines.CompatibilityUnknown:
				// Desconocido NUNCA es compatible: el documento avisa.
				return e.finish(result.Warn("engine compat",
					"compatibilidad desconocida: "+res.Reason, data), human)
			}
			return e.finish(result.OK("engine compat", "combinación compatible y verificada", data), human)
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "versión de Minecraft (vacía = la última del motor con --fill)")
	cmd.Flags().StringVar(&loader, "loader", "", "versión exacta del loader (Fabric)")
	cmd.Flags().StringVar(&build, "build", "", "build exacto del motor (Paper/Forge/NeoForge)")
	cmd.Flags().StringVar(&image, "image", "", "imagen Docker que se va a usar (itzg/minecraft-server:javaN)")
	cmd.Flags().IntVar(&javaMaj, "java", 0, "Java requerido (0 = desconocido; con --fill se infiere)")
	cmd.Flags().BoolVar(&fill, "fill", false, "completa con el servicio los datos que falten (consulta el catálogo)")
	cmd.Flags().StringArrayVar(&plugins, "plugin", nil, "plugin declarado: 'nombre@loaders@versiones' (repetible), ej. 'ViaVersion@paper,bukkit@1.20,1.21'")
	return cmd
}

// catalogCompatibilityData serializa el veredicto trivalente del servicio.
func catalogCompatibilityData(res engines.CompatibilityResult) map[string]any {
	data := map[string]any{
		"engine":        res.Engine,
		"compatibility": string(res.Compatibility),
		"java_major":    res.JavaMajor,
		"java_status":   res.JavaStatus,
	}
	if res.MinecraftVersion != "" {
		data["version"] = res.MinecraftVersion
	}
	if res.LoaderVersion != "" {
		data["loader_version"] = res.LoaderVersion
	}
	if res.Build != "" {
		data["build"] = res.Build
	}
	if res.DockerImage != "" {
		data["image"] = res.DockerImage
	}
	if len(res.Pins) > 0 {
		data["pins"] = res.Pins
	}
	if res.Reason != "" {
		data["reason"] = res.Reason
	}
	if res.Warning != "" {
		data["warning"] = res.Warning
	}
	if len(res.Plugins) > 0 {
		list := make([]map[string]any, 0, len(res.Plugins))
		for _, p := range res.Plugins {
			entry := map[string]any{"name": p.Name, "compatibility": string(p.Compatibility)}
			if p.Reason != "" {
				entry["reason"] = p.Reason
			}
			list = append(list, entry)
		}
		data["plugins"] = list
	}
	return data
}

// catalogParsePluginSpec interpreta 'nombre@loaders@versiones' (loaders y versiones
// separados por comas; cualquiera de las dos partes puede ir vacía).
func catalogParsePluginSpec(raw string) (engines.PluginRequirement, error) {
	parts := strings.Split(strings.TrimSpace(raw), "@")
	name := strings.TrimSpace(parts[0])
	if name == "" {
		return engines.PluginRequirement{}, errx.Usage("plugin sin nombre en --plugin", "usa 'nombre@loaders@versiones'")
	}
	out := engines.PluginRequirement{Name: name}
	if len(parts) > 1 {
		out.Loaders = catalogSplitCSV(parts[1])
	}
	if len(parts) > 2 {
		out.MinecraftVersions = catalogSplitCSV(parts[2])
	}
	return out, nil
}

// catalogSplitCSV separa una lista por comas descartando vacíos.
func catalogSplitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}
