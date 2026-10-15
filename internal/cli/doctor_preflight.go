package cli

import (
	"fmt"
	"strings"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/plugins"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

// newDoctorPreflightCmd implementa `badomens doctor preflight [instancia]` (plan
// 12.5): informa de las incompatibilidades CONOCIDAS entre Minecraft, motor,
// loader/build, Java, imagen Docker y los plugins/mods instalados ANTES de
// descargar o iniciar nada.
//
// Consume la abstracción existente (`CatalogService.CheckCompatibility`) con los
// pines que la instancia YA tiene: no hay una segunda ruta de comprobación. Un
// dato desconocido nunca se convierte en "compatible" (T-03): el comando sale 0
// solo si el veredicto es `compatible`; `unknown` es aviso (1) e `incompatible`
// es error (2).
func newDoctorPreflightCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preflight [instancia]",
		Short: "Compatibilidad conocida de una instancia antes de descargar o iniciar",
		Long: `Cruza la versión de Minecraft, el motor, el loader/build, el Java requerido, la
imagen Docker y los plugins/mods instalados de una instancia y publica el
veredicto trivalente: compatible (0), unknown (1) o incompatible (2).

Los pines se toman de la configuración de la instancia (nunca se resuelve de
nuevo); para confirmar que el motor publica esa versión se consulta el catálogo
(caché primero).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug, err := e.instanceArg(args)
			if err != nil {
				return e.finish(result.FromError("doctor preflight", err), "")
			}
			cfg, err := e.requireConfig()
			if err != nil {
				return e.finish(result.FromError("doctor preflight", err), "")
			}
			spec, err := e.specFor(slug)
			if err != nil {
				return e.finish(result.FromError("doctor preflight", err), "")
			}
			service, err := e.CatalogService()
			if err != nil {
				return e.finish(result.FromError("doctor preflight", err), "")
			}
			status, _ := service.GetCatalogStatus(cmd.Context(), spec.Engine)
			req := preflightRequest(cfg, spec)
			res, cerr := service.CheckCompatibility(cmd.Context(), req)
			if cerr != nil {
				return e.finish(result.FromError("doctor preflight", cerr), "")
			}
			data := catalogCompatibilityData(res)
			data["instance"] = spec.Slug
			data["pins"] = spec.Pins
			data["catalog"] = catalogStatusData(status)
			data["plugins_checked"] = len(res.Plugins)
			human := fmt.Sprintf("%s (%s %s): %s", spec.Slug, res.Engine, res.MinecraftVersion, res.Compatibility)
			if res.Reason != "" {
				human += "\n  ↳ " + res.Reason
			}
			switch res.Compatibility {
			case engines.CompatibilityIncompatible:
				verdictErr := errx.New(errx.CodeGeneral,
					"la instancia tiene incompatibilidades conocidas: "+res.Reason,
					"cambia de versión/motor/loader o retira el plugin afectado").
					WithDetail("diagnostic", engines.DiagnoseIncompatible(spec.Engine, "preflight", res.Reason, status))
				r := result.FromError("doctor preflight", verdictErr)
				r.Data = data
				return e.finish(r, human)
			case engines.CompatibilityUnknown:
				return e.finish(result.Warn("doctor preflight",
					"compatibilidad no confirmada: "+res.Reason, data), human)
			}
			return e.finish(result.OK("doctor preflight",
				"sin incompatibilidades conocidas", data), human)
		},
	}
	return cmd
}

// preflightRequest construye la petición de compatibilidad de una instancia a
// partir de su configuración GUARDADA (no se resuelve nada de nuevo).
func preflightRequest(cfg *config.Config, spec *instance.Spec) engines.CompatibilityRequest {
	eng, _ := engines.ByName(spec.Engine)
	major, javaStatus := engines.JavaRequirementFor(spec.Version)
	req := engines.CompatibilityRequest{
		Engine:           spec.Engine,
		MinecraftVersion: spec.Version,
		Pins:             spec.Pins,
		JavaMajor:        major,
		JavaStatus:       javaStatus,
		DockerImage:      engines.JavaImageFor(spec.Version, cfg.Image),
		Plugins:          preflightPlugins(cfg, spec),
	}
	if eng.Name == engines.Fabric.Name {
		req.LoaderVersion = strings.TrimSpace(spec.Pins[eng.DepVar])
	} else {
		req.Build = strings.TrimSpace(spec.Pins[eng.DepVar])
	}
	return req
}

// preflightPlugins traduce la procedencia registrada de los plugins/mods de la
// instancia a los requisitos que evalúa el servicio. Un plugin sin metadatos
// viaja SIN declararlos: el veredicto será `unknown`, nunca "compatible".
func preflightPlugins(cfg *config.Config, spec *instance.Spec) []engines.PluginRequirement {
	eng, ok := engines.ByName(spec.Engine)
	if !ok || !eng.SupportsPlugins() {
		return nil
	}
	register, err := plugins.NewProvenance(cfg.DataDir).Load(spec)
	if err != nil || register == nil {
		return nil
	}
	out := make([]engines.PluginRequirement, 0, len(register.Records))
	for _, rec := range register.Records {
		req := engines.PluginRequirement{Name: rec.Name}
		if strings.TrimSpace(rec.Loader) != "" {
			req.Loaders = []string{rec.Loader}
		}
		if strings.TrimSpace(rec.GameVersion) != "" {
			req.MinecraftVersions = []string{rec.GameVersion}
		}
		out = append(out, req)
	}
	return out
}
