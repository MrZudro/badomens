package cli

import (
	"runtime"
	"time"

	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/infra"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

func newDockerCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docker",
		Short: "Diagnóstico y reparación de Docker (RF-6)",
	}
	cmd.AddCommand(newDockerStatusCmd(e), newDockerEnsureCmd(e))
	return cmd
}

func newDockerStatusCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Estado de Docker (instalado, daemon, compose y contexto)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			diag := dockerx.Diagnose(cmd.Context(), dockerx.DefaultEnv())
			data := diagnosisData(diag)
			// Estado tipado del mecanismo común (aditivo): misma fuente de verdad.
			data["infra_status"] = infraStateData(infra.NewDocker(dockerx.DefaultEnv()).Detect(cmd.Context()))
			if diag.State.Ready() {
				return e.finish(result.OK("docker status", "Docker está listo", data), dockerStatusHuman(diag))
			}
			return e.finish(result.FromError("docker status", dockerx.ErrFor(diag)), dockerStatusHuman(diag))
		},
	}
}

func newDockerEnsureCmd(e *Env) *cobra.Command {
	var wait int
	var printScript bool
	cmd := &cobra.Command{
		Use:   "ensure",
		Short: "Intenta dejar Docker listo (arranca el daemon/escritorio y espera)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if printScript {
				script, err := dockerx.Script(runtime.GOOS)
				if err != nil {
					return e.finish(result.FromError("docker ensure", err), "")
				}
				return e.finish(result.OK("docker ensure", "script de respaldo", map[string]any{"script": script}), script)
			}
			opts := dockerx.Options{Timeout: 120 * time.Second}
			if wait > 0 {
				opts.Timeout = time.Duration(wait) * time.Second
			}
			res := dockerx.Ensure(cmd.Context(), dockerx.DefaultEnv(), opts)
			data := map[string]any{
				"ready":        res.Ready,
				"state":        string(res.Diagnosis.State),
				"reason":       res.Diagnosis.Reason,
				"attempts":     attemptsData(res.Attempts),
				"pending":      stepsData(res.Pending),
				"infra_status": infraStateData(infra.NewDocker(dockerx.DefaultEnv()).Detect(cmd.Context())),
			}
			if res.Ready {
				return e.finish(result.OK("docker ensure", "Docker listo", data), "Docker listo")
			}
			return e.finish(result.FromError("docker ensure", dockerx.ErrFor(res.Diagnosis)), dockerEnsureHuman(res))
		},
	}
	cmd.Flags().IntVar(&wait, "wait", 0, "segundos máximos de espera (por defecto 120)")
	cmd.Flags().BoolVar(&printScript, "print-script", false, "imprimir el script de respaldo para tu SO")
	return cmd
}

func diagnosisData(d dockerx.Diagnosis) map[string]any {
	return map[string]any{
		"state":           string(d.State),
		"reason":          d.Reason,
		"message":         d.Message,
		"detail":          d.Detail,
		"platform":        d.Platform,
		"server_version":  d.ServerVersion,
		"compose_version": d.ComposeVersion,
		"context":         d.Context,
	}
}

func dockerStatusHuman(d dockerx.Diagnosis) string {
	out := string(d.State) + ": " + d.Message
	if d.ServerVersion != "" {
		out += "\n  server " + d.ServerVersion
	}
	if d.ComposeVersion != "" {
		out += "\n  compose " + d.ComposeVersion
	}
	if d.Context != "" {
		out += "\n  contexto " + d.Context
	}
	return out
}

func attemptsData(as []dockerx.Attempt) []map[string]any {
	out := make([]map[string]any, 0, len(as))
	for _, a := range as {
		out = append(out, map[string]any{
			"description": a.Step.Description,
			"ok":          a.OK,
			"error":       a.Err,
		})
	}
	return out
}

func stepsData(ss []dockerx.Step) []map[string]any {
	out := make([]map[string]any, 0, len(ss))
	for _, s := range ss {
		out = append(out, map[string]any{
			"description": s.Description,
			"command":     s.Command,
			"instruction": s.Instruction,
			"interactive": s.Interactive,
		})
	}
	return out
}

func dockerEnsureHuman(res dockerx.EnsureResult) string {
	out := res.Diagnosis.Message
	for _, a := range res.Attempts {
		if a.OK {
			out += "\n  ✓ " + a.Step.Description
		} else {
			out += "\n  ✖ " + a.Step.Description
			if a.Err != "" {
				out += " — " + a.Err
			}
		}
	}
	for _, s := range res.Pending {
		if s.Instruction != "" {
			out += "\n  manual: " + s.Instruction
		} else if len(s.Command) > 0 && s.Interactive {
			out += "\n  manual: " + joinArgs(s.Command)
		}
	}
	return out
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}
