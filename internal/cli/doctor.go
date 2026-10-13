package cli

import (
	"fmt"
	"strings"

	"github.com/manuelserver/badomens/internal/doctor"
	"github.com/manuelserver/badomens/internal/errx"
	"github.com/manuelserver/badomens/internal/result"
	"github.com/spf13/cobra"
)

func newDoctorCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnóstico del entorno (Docker, Tailscale, configuración)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := e.requireConfig()
			if err != nil {
				return e.finish(result.FromError("doctor", err), "")
			}
			report := doctor.Run(cmd.Context(), cfg)
			data := map[string]any{
				"checks": report.Checks,
				"summary": map[string]int{
					"ok": report.OK, "warns": report.Warns, "errors": report.Errors,
				},
			}
			human := renderChecks(report)
			switch {
			case report.Errors > 0:
				res := result.FromError("doctor", errx.New(
					errx.CodeGeneral,
					fmt.Sprintf("doctor terminó con %d error(es) y %d aviso(s)", report.Errors, report.Warns),
					"corrige lo indicado y vuelve a ejecutar 'badomens doctor'",
				))
				res.Data = data
				return e.finish(res, human)
			case report.Warns > 0:
				res := result.Warn("doctor", fmt.Sprintf("doctor terminó con %d aviso(s)", report.Warns), data)
				return e.finish(res, human)
			default:
				res := result.OK("doctor", "doctor terminó sin problemas", data)
				return e.finish(res, human)
			}
		},
	}
}

func renderChecks(r doctor.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Diagnóstico: %d ok · %d aviso(s) · %d error(es)\n", r.OK, r.Warns, r.Errors)
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "  [%s] %s: %s\n", c.Status, c.Name, c.Message)
	}
	return strings.TrimRight(b.String(), "\n")
}
