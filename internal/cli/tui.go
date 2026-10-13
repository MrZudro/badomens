package cli

import (
	"github.com/manuelserver/badomens/internal/errx"
	"github.com/manuelserver/badomens/internal/result"
	"github.com/manuelserver/badomens/internal/tui"
	"github.com/spf13/cobra"
)

func newTUICmd(e *Env) *cobra.Command {
	var force, snapshot bool
	var view string
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Panel interactivo multi-instancia en el terminal",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := e.requireConfig()
			if err != nil {
				return e.finish(result.FromError("tui", err), "")
			}
			if snapshot {
				if view == "" {
					view = "list"
				}
				out, serr := tui.RenderSnapshot(cfg, e.instanceFlag, view)
				if serr != nil {
					return e.finish(result.FromError("tui", serr), "")
				}
				return e.finish(result.OK("tui", "snapshot del panel", map[string]any{"frame": out}), out)
			}
			if !force && (!isTTY(e.stdin) || !isTTYWriter(e.stdout)) {
				return e.finish(result.FromError("tui", errx.Usage(
					"la TUI necesita una terminal interactiva",
					"ejecuta 'badomens tui' dentro de tu terminal (Windows Terminal, PowerShell…), o usa --force para salidas capturadas",
				)), "")
			}
			if err := tui.Run(cfg); err != nil {
				return e.finish(result.FromError("tui", err), "")
			}
			return e.finish(result.OK("tui", "panel cerrado", nil), "panel cerrado")
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "arrancar aunque la salida no sea una terminal (pruebas/capturas)")
	cmd.Flags().BoolVar(&snapshot, "snapshot", false, "renderiza un frame y sale (pruebas/capturas)")
	cmd.Flags().StringVar(&view, "view", "", "pantalla del snapshot: list|detail|logs|backups|help (usa -i <slug> para detail/logs/backups)")
	return cmd
}
