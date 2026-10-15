package cli

import (
	"fmt"
	"strings"

	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

// newReproCmd agrupa la reproducibilidad (plan 12.5 · spec 6.4): exportar la
// descripción DECLARATIVA de una instancia (versiones y pins exactos) y
// recrearla. Consume las abstracciones existentes: no resuelve versiones ni crea
// rutas de catálogo paralelas.
func newReproCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repro",
		Short: "Reproducibilidad: exporta o recrea la descripción exacta de una instancia",
	}
	cmd.AddCommand(newReproExportCmd(e), newReproImportCmd(e))
	return cmd
}

func newReproExportCmd(e *Env) *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "export [instancia]",
		Short: "Exporta la descripción declarativa reproducible (versiones y pins exactos)",
		Long: `Escribe un manifiesto JSON con todo lo necesario para recrear la instancia:
motor, versión real de Minecraft, PINS EXACTOS (nunca "latest"), Java, imagen,
recursos y ajustes. No incluye el mundo ni los datos generados.

Sin --output (o con -) el manifiesto se imprime tal cual.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug, err := e.instanceArg(args)
			if err != nil {
				return e.finish(result.FromError("repro export", err), "")
			}
			m, err := e.manager()
			if err != nil {
				return e.finish(result.FromError("repro export", err), "")
			}
			manifest, err := m.ExportManifest(slug)
			if err != nil {
				return e.finish(result.FromError("repro export", err), "")
			}
			raw, err := instance.ManifestJSON(manifest)
			if err != nil {
				return e.finish(result.FromError("repro export", err), "")
			}
			path := strings.TrimSpace(out)
			if path == "" || path == "-" {
				return e.finish(result.OK("repro export",
					"manifiesto reproducible de "+manifest.Slug,
					map[string]any{"instance": manifest.Slug, "manifest": manifest}),
					strings.TrimRight(string(raw), "\n"))
			}
			if err := instance.WriteManifest(path, manifest); err != nil {
				return e.finish(result.FromError("repro export", err), "")
			}
			return e.finish(result.OK("repro export",
				"manifiesto de "+manifest.Slug+" escrito en "+path,
				map[string]any{"instance": manifest.Slug, "path": path, "manifest": manifest}),
				"Manifiesto reproducible escrito en "+path)
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "archivo de destino (vacío o - = imprimir por stdout)")
	return cmd
}

func newReproImportCmd(e *Env) *cobra.Command {
	var slug string
	var replace bool
	cmd := &cobra.Command{
		Use:   "import <archivo>",
		Short: "Recrea la configuración de una instancia desde un manifiesto (sin resolver de nuevo)",
		Long: `Recrea la CONFIGURACIÓN de una instancia a partir de un manifiesto exportado.
Los pins ya vienen fijados, así que no se consulta la red ni se resuelve nada.

Si la instancia ya existe hay que pedirlo explícitamente con --replace. Después
de importar, arranca la instancia con 'badomens -i <slug> start'.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manifest, err := instance.ReadManifest(args[0])
			if err != nil {
				return e.finish(result.FromError("repro import", err), "")
			}
			if err := manifest.Validate(); err != nil {
				return e.finish(result.FromError("repro import", err), "")
			}
			m, err := e.manager()
			if err != nil {
				return e.finish(result.FromError("repro import", err), "")
			}
			target := strings.TrimSpace(slug)
			if target == "" {
				target = manifest.Slug
			}
			spec, err := m.ImportManifest(manifest, target, replace)
			if err != nil {
				return e.finish(result.FromError("repro import", err), "")
			}
			data := map[string]any{
				"instance":     spec.Slug,
				"engine":       spec.Engine,
				"version":      spec.Version,
				"pins":         spec.Pins,
				"source":       args[0],
				"follow_up":    "badomens -i " + spec.Slug + " start",
				"reproducible": "pins exactos del manifiesto; no se consultó el catálogo",
			}
			human := fmt.Sprintf("instancia %s recreada desde el manifiesto (%s %s)",
				spec.Slug, spec.Engine, spec.Version)
			return e.finish(result.OK("repro import", human, data), human)
		},
	}
	cmd.Flags().StringVar(&slug, "name", "", "slug de la instancia a crear (vacío = el del manifiesto)")
	cmd.Flags().BoolVar(&replace, "replace", false, "sobrescribir la configuración si la instancia ya existe")
	return cmd
}
