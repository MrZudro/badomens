package cli

import (
	"fmt"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/MrZudro/badomens/internal/tui"
	"github.com/spf13/cobra"
)

func newTUICmd(e *Env) *cobra.Command {
	var force, snapshot, noPreflight bool
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
					"ejecuta 'badomens tui' en tu terminal (Windows Terminal, PowerShell…). Requisitos: Docker + Compose y, si está activo, Tailscale ('badomens doctor' los comprueba). Usa --force solo para salidas capturadas",
				)), "")
			}
			// Comprobación de actualización al arrancar (solo interactivo).
			if !noPreflight {
				if updated, uerr := checkUpdateAtTUI(cmd, e, cfg); updated || uerr != nil {
					return updatedOrErr(e, updated, uerr)
				}
			}
			if err := tui.Run(cfg, tui.Options{Preflight: !noPreflight}); err != nil {
				return e.finish(result.FromError("tui", err), "")
			}
			return e.finish(result.OK("tui", "panel cerrado", nil), "panel cerrado")
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "arrancar aunque la salida no sea una terminal (pruebas/capturas)")
	cmd.Flags().BoolVar(&snapshot, "snapshot", false, "renderiza un frame y sale (pruebas/capturas)")
	cmd.Flags().BoolVar(&noPreflight, "no-preflight", false, "saltar la comprobación de requisitos y de actualización al abrir")
	cmd.Flags().StringVar(&view, "view", "", "pantalla del snapshot: list|detail|logs|backups|help (usa -i <slug> para detail/logs/backups)")
	return cmd
}

// checkUpdateAtTUI comprueba (con throttle) si hay una versión nueva y, si la
// hay, pregunta al operador si actualizar. Devuelve updated=true si el binario
// se actualizó (la TUI no debe lanzarse con el código viejo). Solo corre en
// terminales interactivas; en --json/no-TTY no hace nada.
func checkUpdateAtTUI(cmd *cobra.Command, e *Env, cfg *config.Config) (bool, error) {
	if !e.jsonFlag && isTTY(e.stdin) && isTTYWriter(e.stdout) && cfg.UpdateCheck {
		applied, err := maybeUpdateInteractive(cmd.Context(), e, cfg.Repo)
		if applied {
			return true, nil
		}
		if err != nil {
			_, _ = fmt.Fprintf(e.stderr, "aviso: no se pudo comprobar actualizaciones: %v\n", err)
		}
	}
	return false, nil
}

// updatedOrErr concluye el comando cuando la actualización se aplicó o falló.
func updatedOrErr(e *Env, updated bool, uerr error) error {
	if uerr != nil {
		res := result.FromError("tui", uerr)
		return e.finish(res, "")
	}
	return e.finish(result.OK("tui", "actualizado; vuelve a ejecutar 'badomens tui' para usar la nueva versión", nil), "actualizado; vuelve a ejecutar 'badomens tui'")
}
