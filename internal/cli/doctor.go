package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/doctor"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/MrZudro/badomens/internal/tps"
	"github.com/spf13/cobra"
)

func newDoctorCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnóstico del entorno y de rendimiento (TPS)",
	}
	cmd.AddCommand(newDoctorTpsCmd(e), newDoctorAPICmd(e), newDoctorPreflightCmd(e))
	cmd.RunE = func(cobraCmd *cobra.Command, args []string) error {
		return runDoctorEnv(e, cobraCmd)
	}
	return cmd
}

func runDoctorEnv(e *Env, cmd *cobra.Command) error {
	cfg, err := e.requireConfig()
	if err != nil {
		return e.finish(result.FromError("doctor", err), "")
	}
	report := doctor.Run(cmd.Context(), cfg)
	data := map[string]any{
		"checks":  report.Checks,
		"summary": map[string]int{"ok": report.OK, "warns": report.Warns, "errors": report.Errors},
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
}

func newDoctorTpsCmd(e *Env) *cobra.Command {
	var samples int
	var intervalMS int
	cmd := &cobra.Command{
		Use:   "tps",
		Short: "Diagnóstico de TPS/MSPT de una instancia (o de todas si no usas --instance)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := e.runtime()
			if err != nil {
				return e.finish(result.FromError("doctor tps", err), "")
			}
			opts := tps.Options{Samples: samples, Interval: time.Duration(intervalMS) * time.Millisecond}

			var targets []*instance.Spec
			if e.instanceFlag != "" {
				s, err := e.specFor(e.instanceFlag)
				if err != nil {
					return e.finish(result.FromError("doctor tps", err), "")
				}
				targets = []*instance.Spec{s}
			} else {
				m, err := e.manager()
				if err != nil {
					return e.finish(result.FromError("doctor tps", err), "")
				}
				if targets, err = m.List(); err != nil {
					return e.finish(result.FromError("doctor tps", err), "")
				}
			}

			reports := []map[string]any{}
			human := ""
			worst := "good"
			critical, degraded := 0, 0
			for _, s := range targets {
				state, _ := rt.State(cmd.Context(), s.Slug)
				entry := map[string]any{"instance": s.Slug, "engine": s.Engine, "version": s.Version, "state": state}
				if state != "running" {
					entry["score"] = "not-running"
					human += fmt.Sprintf("\n%s: no medida (instancia detenida)", s.Slug)
					reports = append(reports, entry)
					continue
				}
				rep, err := tps.Collect(cmd.Context(), rt, s.Slug, s.Engine, opts)
				if err != nil {
					entry["score"] = "error"
					entry["error"] = err.Error()
					reports = append(reports, entry)
					human += fmt.Sprintf("\n%s: error midiendo TPS: %v", s.Slug, err)
					critical++
					continue
				}
				entry["report"] = rep
				reports = append(reports, entry)
				human += renderTPSReport(s.Slug, rep)
				switch rep.Score {
				case "critical":
					critical++
					worst = "critical"
				case "degraded":
					degraded++
					if worst != "critical" {
						worst = "degraded"
					}
				}
			}

			msg := fmt.Sprintf("doctor tps terminó: %d crítica(s), %d degradada(s)", critical, degraded)
			data := map[string]any{"reports": reports, "summary": map[string]int{"critical": critical, "degraded": degraded}}
			switch worst {
			case "critical":
				res := result.FromError("doctor tps", errx.New(errx.CodeGeneral, msg, "corrige las instancias afectadas y vuelve a medir"))
				res.Data = data
				return e.finish(res, human)
			case "degraded":
				res := result.Warn("doctor tps", msg, data)
				return e.finish(res, human)
			default:
				res := result.OK("doctor tps", msg, data)
				return e.finish(res, human)
			}
		},
	}
	cmd.Flags().IntVar(&samples, "samples", 3, "número de muestras")
	cmd.Flags().IntVar(&intervalMS, "interval-ms", 2000, "intervalo entre muestras (ms)")
	return cmd
}

func renderTPSReport(slug string, r *tps.Report) string {
	var b strings.Builder
	tpsStr := "-"
	if r.AvgTPS >= 0 {
		tpsStr = fmt.Sprintf("%.1f", r.AvgTPS)
	}
	msptStr := "-"
	if r.MSPT >= 0 {
		msptStr = fmt.Sprintf("%.1f", r.MSPT)
	}
	fmt.Fprintf(&b, "\n%s: TPS %s · MSPT %s · CPU %.1f%% · RAM %.0f MB · lag %d",
		slug, tpsStr, msptStr, r.CPUAvg, r.MemUsedMB, r.LagEvents)
	for _, w := range r.Suggestions {
		b.WriteString("\n  ↳ " + w)
	}
	return b.String()
}

func renderChecks(r doctor.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Diagnóstico: %d ok · %d aviso(s) · %d error(es)\n", r.OK, r.Warns, r.Errors)
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "  [%s] %s: %s\n", c.Status, c.Name, c.Message)
	}
	return strings.TrimRight(b.String(), "\n")
}
