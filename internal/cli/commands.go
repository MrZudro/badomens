package cli

import (
	"github.com/manuelserver/badomens/internal/result"
	"github.com/spf13/cobra"
)

// commandEntry describe un comando en el manifiesto estable.
type commandEntry struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

func newCommandsCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "commands",
		Short: "Lista el manifiesto de comandos (JSON estable)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries := []commandEntry{
				{Name: "setup", Summary: "Asistente de configuración de una instancia"},
				{Name: "instance", Summary: "Crear/listar/inspeccionar/gestionar instancias"},
				{Name: "engine", Summary: "Catálogo y selección de motor/versión"},
				{Name: "world", Summary: "Biblioteca de mundos (listar/crear/usar/exportar/importar)"},
				{Name: "backup", Summary: "Copias de seguridad (crear/listar/restaurar/podar)"},
				{Name: "access", Summary: "Whitelist y operadores"},
				{Name: "plugin", Summary: "Plugins/mods por motor"},
				{Name: "doctor", Summary: "Diagnóstico del entorno y de rendimiento (TPS)"},
				{Name: "update", Summary: "Comprobar si hay una versión nueva"},
				{Name: "capacity", Summary: "Capacidad del equipo"},
				{Name: "config", Summary: "Configuración global"},
				{Name: "mcp", Summary: "Servidor MCP por stdio"},
				{Name: "version", Summary: "Versión del binario"},
			}
			data := map[string]any{"commands": entries, "count": len(entries)}
			human := "Comandos disponibles:\n"
			for _, en := range entries {
				human += "  " + en.Name + " — " + en.Summary + "\n"
			}
			return e.finish(result.OK("commands", "manifiesto de comandos", data), human)
		},
	}
}
