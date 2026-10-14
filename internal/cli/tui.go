package cli

import (
	"context"
	"fmt"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/MrZudro/badomens/internal/tui"
	"github.com/spf13/cobra"
)

func newTUICmd(e *Env) *cobra.Command {
	var force, snapshot, noPreflight bool
	var view, themeName string
	var noAnimations, ascii, plain bool
	var snapW, snapH int
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Panel interactivo multi-instancia en el terminal",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := e.requireConfig()
			if err != nil {
				return e.finish(result.FromError("tui", err), "")
			}
			if plain {
				out, perr := tuiPlain(cmd.Context(), e)
				if perr != nil {
					return e.finish(result.FromError("tui", perr), "")
				}
				return e.finish(result.OK("tui", "resumen lineal", map[string]any{"plain": out}), out)
			}
			if themeName != "" {
				cfg.UI.Theme = themeName
			}
			if noAnimations {
				cfg.UI.ReducedMotion = true
			}
			if ascii {
				cfg.UI.ASCII = true
			}
			if snapshot {
				if view == "" {
					view = "list"
				}
				out, serr := tui.RenderSnapshot(cfg, e.instanceFlag, view, snapW, snapH)
				if serr != nil {
					return e.finish(result.FromError("tui", serr), "")
				}
				return e.finish(result.OK("tui", "snapshot del panel", map[string]any{"frame": out}), out)
			}
			if !force && (!isTTY(e.stdin) || !isTTYWriter(e.stdout)) {
				return e.finish(result.FromError("tui", errx.Usage(
					"la TUI necesita una terminal interactiva",
					"ejecuta 'badomens tui' en tu terminal, o usa 'badomens tui --plain' para un resumen lineal. Requisitos: Docker + Compose y, si está activo, Tailscale ('badomens doctor' los comprueba). Usa --force solo para salidas capturadas",
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
	cmd.Flags().StringVar(&themeName, "theme", "", "tema: dark|light|high-contrast|colorblind")
	cmd.Flags().BoolVar(&noAnimations, "no-animations", false, "movimiento reducido: sin pulso, transiciones ni spinners animados")
	cmd.Flags().BoolVar(&ascii, "ascii", false, "iconos y bordes solo ASCII")
	cmd.Flags().BoolVar(&plain, "plain", false, "salida lineal sin cajas ni animación (accesibilidad/lector de pantalla)")
	cmd.Flags().IntVar(&snapW, "width", 110, "ancho del snapshot (con --snapshot)")
	cmd.Flags().IntVar(&snapH, "height", 30, "alto del snapshot (con --snapshot)")
	return cmd
}

// tuiPlain imprime un resumen lineal de las instancias, sin cajas ni color, para
// lectores de pantalla y entornos no interactivos (P-16).
func tuiPlain(ctx context.Context, e *Env) (string, error) {
	m, err := e.manager()
	if err != nil {
		return "", err
	}
	rt, err := e.runtime()
	if err != nil {
		return "", err
	}
	specs, err := m.List()
	if err != nil {
		return "", err
	}
	if len(specs) == 0 {
		return "No hay instancias. Usa 'badomens setup --name <nombre>' para crear la primera.", nil
	}
	out := fmt.Sprintf("Instancias (%d):", len(specs))
	for _, s := range specs {
		state, _ := rt.State(ctx, s.Slug)
		if state == "" {
			state = "sin contenedor"
		}
		out += fmt.Sprintf("\n- %s: %s %s, puerto %d, estado %s", s.Slug, s.Engine, s.Version, s.Port, state)
	}
	return out, nil
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
