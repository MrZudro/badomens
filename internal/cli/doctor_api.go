package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/doctor"
	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

// newDoctorAPICmd implementa `badomens doctor api` (plan 12.3 · AC-12 · spec
// 6.4): por proveedor de catálogo informa de la conectividad, el código HTTP, el
// tiempo de respuesta, el formato, la versión de contrato, el esquema de la
// respuesta, la edad de la caché y cuándo se consultó por última vez.
//
// La sonda usa la MISMA ruta que el catálogo (transporte único + adaptador del
// proveedor) y el estado consultable de la caché: no hay cliente ni parser
// paralelos. El documento es ADITIVO.
func newDoctorAPICmd(e *Env) *cobra.Command {
	var timeoutSeconds int
	var bases []string
	cmd := &cobra.Command{
		Use:   "api [motor]",
		Short: "Diagnóstico de los proveedores de catálogo (conectividad, HTTP, tiempos, formato, contrato y caché)",
		Long: `Comprueba cada proveedor de catálogo (paper, fabric, forge, neoforge, vanilla)
consultando su catálogo real por la misma ruta que usa badomens:
conectividad, código HTTP, tiempo, formato y esquema de la respuesta, versión del
contrato, número de versiones y estado de la caché (fuente y antigüedad).

Sin argumento sondea todos los motores; con uno, solo ese. Un proveedor que falla
en vivo pero conserva caché utilizable es un AVISO; sin caché utilizable es un
ERROR.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := e.CatalogService()
			if err != nil {
				return e.finish(result.FromError("doctor api", err), "")
			}
			names, err := catalogEngineNames(args)
			if err != nil {
				return e.finish(result.FromError("doctor api", err), "")
			}
			opts, berr := doctorProbeOptions(timeoutSeconds, bases)
			if berr != nil {
				return e.finish(result.FromError("doctor api", berr), "")
			}
			report := doctor.ProbeAPI(cmd.Context(), service, names, opts)
			data := map[string]any{
				"providers": report.Providers,
				"summary": map[string]int{
					"ok": report.OK, "warnings": report.Warnings, "errors": report.Errors,
				},
			}
			human := fmt.Sprintf("Proveedores de catálogo: %d ok · %d aviso(s) · %d error(es)\n%s",
				report.OK, report.Warnings, report.Errors, doctor.APIHuman(report))
			msg := fmt.Sprintf("doctor api: %d ok, %d aviso(s), %d error(es)", report.OK, report.Warnings, report.Errors)
			switch {
			case report.Errors > 0:
				res := result.FromError("doctor api", errx.New(errx.CodeUnavailable, msg,
					"revisa la conexión; 'catalog status' muestra si hay datos en caché utilizables"))
				res.Data = data
				return e.finish(res, human)
			case report.Warnings > 0:
				return e.finish(result.Warn("doctor api", msg, data), human)
			default:
				return e.finish(result.OK("doctor api", msg, data), human)
			}
		},
	}
	cmd.Flags().IntVar(&timeoutSeconds, "timeout", 20, "límite por proveedor en segundos (0 = el del transporte)")
	cmd.Flags().StringArrayVar(&bases, "base", nil, "raíz del endpoint de un motor: motor=URL (repetible; uso avanzado o con un espejo)")
	return cmd
}

// doctorProbeOptions traduce los flags a las opciones de la sonda. Un `--base`
// mal formado es un error de USO, no un fallo del diagnóstico.
func doctorProbeOptions(timeoutSeconds int, bases []string) (engines.ProbeOptions, error) {
	opts := engines.ProbeOptions{}
	if timeoutSeconds > 0 {
		opts.Timeout = time.Duration(timeoutSeconds) * time.Second
	}
	for _, raw := range bases {
		name, url, ok := strings.Cut(raw, "=")
		name = strings.ToLower(strings.TrimSpace(name))
		url = strings.TrimSpace(url)
		if !ok || name == "" || url == "" {
			return opts, errx.Usage("--base inválido: "+raw, "usa --base motor=URL, por ejemplo --base paper=https://fill.papermc.io/v3/projects/paper")
		}
		if _, valid := engines.ByName(name); !valid {
			return opts, errx.Usage("--base con motor desconocido: "+name,
				"usa uno de: "+strings.Join(engineNames(), ", "))
		}
		if opts.Bases == nil {
			opts.Bases = map[string]string{}
		}
		opts.Bases[name] = url
	}
	return opts, nil
}
