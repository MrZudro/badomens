// Package i18n es el catálogo de textos de la interfaz (TUI v2). Los textos
// visibles no se escriben en línea dentro de los modelos: van aquí. El catálogo
// es aditivo; añadir inglés después solo requiere una nueva entrada.
package i18n

import "sort"

// Catalog es un catálogo de textos por idioma.
type Catalog struct {
	Lang string
	data map[string]string
}

// Es devuelve el catálogo en español (el idioma por defecto del proyecto).
func Es() *Catalog {
	return &Catalog{Lang: "es", data: es}
}

// T devuelve el texto de una clave. Si la clave no existe, devuelve la clave
// entre corchetes para que el fallo sea visible en la UI.
func (c *Catalog) T(key string) string {
	if s, ok := c.data[key]; ok {
		return s
	}
	return "[" + key + "]"
}

// Has indica si la clave existe.
func (c *Catalog) Has(key string) bool {
	_, ok := c.data[key]
	return ok
}

// Keys devuelve las claves ordenadas.
func (c *Catalog) Keys() []string {
	out := make([]string, 0, len(c.data))
	for k := range c.data {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// es es el catálogo español. Las claves con "{{...}}" se formatean luego.
var es = map[string]string{
	// Barra de ayuda global.
	"help.bar":            "? Ayuda · Ctrl+P Comandos · Tab Cambiar panel · Q Salir",
	"help.full":           "Atajos de esta pantalla",
	"palette.title":       "Comandos",
	"palette.empty":       "Sin coincidencias",
	"status.running":      "En línea",
	"status.started":      "Iniciando…",
	"status.stopping":     "Deteniendo…",
	"status.stopped":      "Detenido",
	"status.problems":     "Con problemas",
	"status.error":        "Error",
	"status.idle":         "Dormido por inactividad",
	"home.title":          "INSTANCIAS",
	"home.empty.title":    "Todavía no hay instancias",
	"home.empty.cta":      "Crear mi primer servidor",
	"home.count":          "{{actual}}/{{max}} · {{running}}/{{maxRun}} en marcha",
	"connect.title":       "Cómo conectarse",
	"connect.address":     "Dirección",
	"connect.address_dns": "Nombre (MagicDNS)",
	"connect.copy":        "Copiar dirección",
	"connect.copy_invite": "Copiar invitación",
	"wizard.title":        "Nueva instancia",
	"wizard.step":         "Paso {{current}} de {{total}}",
	"wizard.review":       "Revisar y crear",
	"action.start":        "Iniciar «{{name}}»",
	"action.stop":         "Detener «{{name}}» (guarda el mundo)",
	"action.restart":      "Reiniciar «{{name}}»",
	"action.backup":       "Crear backup ahora",
	"action.delete":       "Sí, borrar la instancia para siempre",
	"action.cancel":       "No, volver",
	"toast.started":       "✓ «{{name}}» iniciado en {{secs}} s",
	"toast.stopped":       "«{{name}}» detenido",
	"toast.error":         "✖ No se pudo {{verb}}: {{cause}} — {{fix}}",
	"override.text":       "Escribe el nombre de la instancia para confirmar:",
	"idle.countdown":      "Apagado en {{remaining}}",
	"idle.off":            "Auto-apagado desactivado",
	"health.title":        "Salud del sistema",
	"health.docker":       "Docker",
	"health.tailscale":    "Tailscale",
	"health.disk":         "Disco",
	"health.ram":          "RAM",
	"health.update":       "Actualización",
}
