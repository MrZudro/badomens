package cli

import (
	"github.com/MrZudro/badomens/internal/mcp"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

func newMCPCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Servidor MCP sobre el registro único de herramientas",
	}
	cmd.AddCommand(newMCPServeCmd(e))
	return cmd
}

func newMCPServeCmd(e *Env) *cobra.Command {
	var readOnly bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Sirve MCP por stdio (para clientes como Claude Desktop, Cursor o el agente de Emiria)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := e.Registry()
			if err != nil {
				return e.finish(result.FromError("mcp serve", err), "")
			}
			server := mcp.New(reg, readOnly)
			return server.Serve(cmd.Context(), e.stdin, e.stdout)
		},
	}
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "oculta las herramientas que modifican estado")
	return cmd
}
