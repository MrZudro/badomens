package cli

import (
	"fmt"

	"github.com/MrZudro/badomens/internal/result"
	"github.com/MrZudro/badomens/internal/update"
	"github.com/spf13/cobra"
)

func newUpdateCmd(e *Env) *cobra.Command {
	var repo string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Comprueba si hay una versión nueva publicada",
		Long: `Consulta la última release del repositorio y la compara con la versión
del binario. No descarga ni reemplaza nada.`,
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
			human := buildUpdateText(info)
			if info.UpdateAvailable {
				human += fmt.Sprintf("\nActualiza a %s: %s", info.Latest, info.URL)
			}
			res := result.OK("update", updateMessage(info), info)
			return e.finish(res, human)
		},
	}
	cmd.Flags().StringVar(&repo, "repo", "", "repositorio owner/nombre a consultar (por defecto, el de config)")
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
