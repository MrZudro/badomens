package cli

import (
	"fmt"

	"github.com/manuelserver/badomens/internal/result"
	"github.com/spf13/cobra"
)

// newAccessCmd gestiona whitelist/operadores enviando los comandos de consola
// adecuados a la instancia activa.
func newAccessCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "access",
		Short: "Whitelist y operadores",
	}
	cmd.AddCommand(
		sendConsoleCmd(e, "whitelist-add", "whitelist add"),
		sendConsoleCmd(e, "whitelist-remove", "whitelist remove"),
		sendConsoleCmd(e, "ops-add", "op"),
		sendConsoleCmd(e, "ops-remove", "deop"),
	)
	return cmd
}

func sendConsoleCmd(e *Env, use, consolePrefix string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <jugador>",
		Short: consolePrefix + " <jugador> en la consola de la instancia",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError(use, err), "")
			}
			rt, err := e.runtime()
			if err != nil {
				return e.finish(result.FromError(use, err), "")
			}
			command := consolePrefix + " " + args[0]
			if err := rt.Console(cmd.Context(), spec.Slug, command); err != nil {
				return e.finish(result.FromError(use, err), "")
			}
			human := fmt.Sprintf("Comando '%s' enviado a %s", command, spec.Slug)
			return e.finish(result.OK(use, human, map[string]any{"instance": spec.Slug, "command": command}), human)
		},
	}
}
