package cli

import (
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

// commandEntry describe un comando en el manifiesto estable.
type commandEntry struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

// commandManifest es el manifiesto estable de comandos.
func commandManifest() []commandEntry {
	return []commandEntry{
		{Name: "setup", Summary: "Asistente interactivo/desatendido de configuración"},
		{Name: "instance list", Summary: "Lista las instancias y su estado"},
		{Name: "instance inspect", Summary: "Configuración completa de una instancia"},
		{Name: "instance create", Summary: "Crea una instancia desatendida"},
		{Name: "instance start|stop|restart", Summary: "Ciclo de vida (límite de paralelos en start)"},
		{Name: "instance close", Summary: "Apagado con cuidado via consola"},
		{Name: "instance remove", Summary: "Elimina instancia (contenedor y carpeta)"},
		{Name: "connect", Summary: "Información de conexión de una instancia (RF-2)"},
		{Name: "ports check|list", Summary: "Estado y reserva de puertos (RF-3)"},
		{Name: "engine list|current|use|versions", Summary: "Catálogo, selección y versiones de motor (RF-5)"},
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
		{Name: "docker status|ensure", Summary: "Diagnóstico y reparación de Docker (RF-6)"},
		{Name: "tailscale status|ensure", Summary: "Estado y reparación de la red privada (RF-6)"},
		{Name: "idle set|status|disable|daemon", Summary: "Auto-apagado por inactividad (RF-8)"},
		{Name: "update", Summary: "¿Hay una versión nueva? (con --apply la descarga y reemplaza)"},
		{Name: "uninstall", Summary: "Desinstala el binario y la configuración (--purge borra los datos)"},
		{Name: "capacity", Summary: "Capacidad del equipo"},
		{Name: "config", Summary: "Configuración global"},
		{Name: "mcp", Summary: "Servidor MCP por stdio"},
		{Name: "tui", Summary: "Panel interactivo multi-instancia en el terminal"},
		{Name: "version", Summary: "Versión del binario"},
	}
}

func newCommandsCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "commands",
		Short: "Lista el manifiesto de comandos (JSON estable)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries := commandManifest()
			human := "Comandos disponibles:\n"
			for _, en := range entries {
				human += "  " + en.Name + " — " + en.Summary + "\n"
			}
			return e.finish(result.OK("commands", "manifiesto de comandos", map[string]any{"count": len(entries), "commands": entries}), human)
		},
	}
}
