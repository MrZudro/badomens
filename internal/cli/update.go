package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/MrZudro/badomens/internal/update"
	"github.com/spf13/cobra"
)

func newUpdateCmd(e *Env) *cobra.Command {
	var repo string
	var apply bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Comprueba si hay una versión nueva publicada",
		Long: `Consulta la última release del repositorio y la compara con la versión
del binario. Con --apply descarga la nueva versión, verifica su checksum y
reemplaza el binario (pidiendo confirmación).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := e.requireConfig()
			if err != nil {
				return e.finish(result.FromError("update", err), "")
			}
			if repo == "" {
				repo = cfg.Repo
			}
			info, err := update.Check(cmd.Context(), repo)
			if err != nil {
				return e.finish(result.FromError("update", err), "")
			}
			if apply && info.UpdateAvailable {
				ok, cerr := e.confirm("actualizar a la versión", info.Latest, false)
				if cerr != nil || !ok {
					return e.finish(result.FromError("update", errx.Usage("actualización cancelada", "")), "actualización cancelada")
				}
				res, aerr := update.Apply(cmd.Context(), repo)
				if aerr != nil {
					return e.finish(result.FromError("update", aerr), "")
				}
				human := fmt.Sprintf("Actualizado de %s a %s", res.From, res.To)
				if res.RestartRequired {
					human += "\n" + res.Notes + " (el binario nuevo quedó instalado al lado)"
				}
				return e.finish(result.OK("update", human, map[string]any{
					"from": res.From, "to": res.To, "restart_required": res.RestartRequired, "notes": res.Notes,
				}), human)
			}
			human := buildUpdateText(info)
			if info.UpdateAvailable {
				human += fmt.Sprintf("\nActualiza a %s: %s", info.Latest, info.URL)
			}
			res := result.OK("update", updateMessage(info), info)
			return e.finish(res, human)
		},
	}
	cmd.Flags().StringVar(&repo, "repo", "", "repositorio owner/nombre a consultar (por defecto, el de config)")
	cmd.Flags().BoolVar(&apply, "apply", false, "descarga, verifica y reemplaza el binario (pide confirmación)")
	return cmd
}

func updateMessage(info update.Info) string {
	if info.UpdateAvailable {
		return fmt.Sprintf("hay una versión nueva: %s (actual %s)", info.Latest, info.Current)
	}
	return "estás en la última versión (" + info.Current + ")"
}

func buildUpdateText(info update.Info) string {
	return fmt.Sprintf("Actual: %s\nÚltima: %s", info.Current, info.Latest)
}

// maybeUpdateInteractive comprueba (con throttle) si hay una versión nueva y,
// si la hay, pregunta al operador si quiere actualizar. Solo debe llamarse en
// una terminal interactiva. Devuelve true si el binario se reemplazó: el
// llamador no debe seguir con el proceso viejo.
func maybeUpdateInteractive(ctx context.Context, e *Env, repo string) (bool, error) {
	if strings.TrimSpace(repo) == "" {
		return false, nil
	}
	dir, err := config.Dir()
	if err != nil {
		return false, err
	}
	cache := update.LoadCache(dir)
	if !cache.Due(time.Now(), update.CheckTTL) {
		return false, nil
	}

	info, cerr := update.Check(ctx, repo)
	cache.LastCheck = time.Now()
	_ = update.SaveCache(dir, cache)
	if cerr != nil {
		return false, cerr
	}
	if !info.UpdateAvailable {
		return false, nil
	}
	if cache.Skipped == info.Latest {
		return false, nil
	}

	msg := fmt.Sprintf("Nueva versión %s disponible (actual %s). ¿Actualizar ahora? (s/N)", info.Latest, info.Current)
	ans := strings.ToLower(strings.TrimSpace(e.prompt(msg, "N")))
	switch ans {
	case "s", "si", "sí", "y", "yes":
	default:
		cache.Skipped = info.Latest
		_ = update.SaveCache(dir, cache)
		return false, nil
	}

	if _, aerr := update.Apply(ctx, repo); aerr != nil {
		return false, aerr
	}
	cache.Latest = info.Latest
	_ = update.SaveCache(dir, cache)
	return true, nil
}
