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
				{Name: "setup", Summary: "Asistente interactivo/desatendido de configuración"},
				{Name: "instance list", Summary: "Lista las instancias y su estado"},
				{Name: "instance inspect", Summary: "Configuración completa de una instancia"},
				{Name: "instance create", Summary: "Crea una instancia desatendida"},
				{Name: "instance start|stop|restart", Summary: "Ciclo de vida (límite de paralelos en start)"},
				{Name: "instance close", Summary: "Apagado con cuidado via consola"},
				{Name: "instance remove", Summary: "Elimina instancia (contenedor y carpeta)"},
				{Name: "engine list|current|use", Summary: "Catálogo y selección de motor/versión"},
				{Name: "world list|save|use|export|import", Summary: "Biblioteca de mundos"},
				{Name: "backup create|list|restore|prune", Summary: "Copias de seguridad por instancia"},
				{Name: "access whitelist-add|remove|ops-add|remove", Summary: "Whitelist y operadores via consola"},
				{Name: "plugin add|list|remove", Summary: "Plugins/mods según motor"},
				{Name: "logs", Summary: "Logs de una instancia"},
				{Name: "console", Summary: "Comando de consola"},
				{Name: "metrics", Summary: "Métricas de una instancia (CPU/RAM/mundo/backups)"},
				{Name: "resolve-ip", Summary: "IP de publicación del host"},
				{Name: "doctor", Summary: "Diagnóstico del entorno"},
				{Name: "doctor tps", Summary: "Diagnóstico de TPS/MSPT de las instancias"},
				{Name: "update", Summary: "¿Hay una versión nueva?"},
				{Name: "capacity", Summary: "Capacidad del equipo"},
				{Name: "config", Summary: "Configuración global"},
				{Name: "mcp", Summary: "Servidor MCP por stdio"},
				{Name: "tui", Summary: "Panel interactivo multi-instancia en el terminal"},
				{Name: "version", Summary: "Versión del binario"},
			}
			human := "Comandos disponibles:\n"
			for _, en := range entries {
				human += "  " + en.Name + " — " + en.Summary + "\n"
			}
			return e.finish(result.OK("commands", "manifiesto de comandos", map[string]any{"count": len(entries), "commands": entries}), human)
		},
	}
}
