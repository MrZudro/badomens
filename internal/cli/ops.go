package cli

import (
	"github.com/manuelserver/badomens/internal/result"
	"github.com/manuelserver/badomens/internal/tailscale"
	"github.com/spf13/cobra"
)

func newLogsCmd(e *Env) *cobra.Command {
	var lines int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Últimas líneas de los logs de una instancia",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("logs", err), "")
			}
			rt, err := e.runtime()
			if err != nil {
				return e.finish(result.FromError("logs", err), "")
			}
			out, err := rt.Logs(cmd.Context(), spec.Slug, lines)
			if err != nil {
				return e.finish(result.FromError("logs", err), "")
			}
			return e.finish(result.OK("logs", "logs de "+spec.Slug, map[string]any{
				"instance": spec.Slug, "lines": out,
			}), out)
		},
	}
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "número de líneas")
	return cmd
}

func newConsoleCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "console <comando>",
		Short: "Envía un comando a la consola de la instancia (sin barra)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("console", err), "")
			}
			rt, err := e.runtime()
			if err != nil {
				return e.finish(result.FromError("console", err), "")
			}
			if err := rt.Console(cmd.Context(), spec.Slug, args[0]); err != nil {
				return e.finish(result.FromError("console", err), "")
			}
			human := "comando enviado a " + spec.Slug + ": " + args[0]
			return e.finish(result.OK("console", human, map[string]any{"instance": spec.Slug, "command": args[0]}), human)
		},
	}
}

func newResolveIPCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "resolve-ip",
		Short: "Resuelve la IP efectiva de publicación del host",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := e.requireConfig()
			if err != nil {
				return e.finish(result.FromError("resolve-ip", err), "")
			}
			if !cfg.Tailscale.Enabled {
				return e.finish(result.OK("resolve-ip", "127.0.0.1", map[string]any{"ip": "127.0.0.1", "source": "local"}), "127.0.0.1 (local)")
			}
			ip, err := tailscale.ResolveIPv4(cmd.Context())
			if err != nil {
				return e.finish(result.FromError("resolve-ip", err), "")
			}
			source := "tailnet"
			if !tailscale.InRange(ip) {
				source = "other"
			}
			return e.finish(result.OK("resolve-ip", ip, map[string]any{"ip": ip, "source": source}), ip+" ("+source+")")
		},
	}
}
