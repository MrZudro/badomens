// Package i18n es el catálogo de textos de la interfaz (TUI v2). Los textos
// visibles no se escriben en línea dentro de los modelos: van aquí. El catálogo
// es aditivo; añadir inglés después solo requiere una nueva entrada.
package i18n

import (
	"sort"
	"strings"
)

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

// Tf devuelve el texto de una clave sustituyendo las variables {{clave}} por su
// valor. Es la forma de componer textos con datos sin concatenar en la pantalla.
func (c *Catalog) Tf(key string, vars map[string]string) string {
	s := c.T(key)
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
	}
	return s
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

	// Carcasa de la app (cabecera, banners, toasts, salida).
	"app.too_small":               "Agranda la terminal (mínimo 40×12). Ahora: {{w}}×{{h}}",
	"app.header.docker_ok":        "● Docker OK",
	"app.header.docker_unchecked": "Docker sin comprobar",
	"app.header.docker_bad":       "✖ Docker {{state}}",
	"app.header.ascii_ok":         "[OK] Docker",
	"app.header.ascii_unchecked":  "[..] Docker sin comprobar",
	"app.header.ascii_bad":        "[XX] Docker {{state}}",
	"app.header.counters":         "{{inst}}/{{maxInst}} inst · {{run}}/{{maxRun}} en marcha",
	"app.header.ts_ok":            "Tailscale ●",
	"app.header.ts_bad":           "✖ Tailscale",
	"app.header.ascii_ts_ok":      "[OK] Tailscale",
	"app.header.ascii_ts_bad":     "[XX] Tailscale",

	// Navegación de primer nivel (§2): Inicio · Instancias · Salud · Ayuda.
	"nav.home":      "Inicio",
	"nav.instances": "Instancias",
	"nav.health":    "Salud",
	"nav.help":      "Ayuda",

	// Barra de estado (§3): mensaje de dominio más reciente + indicador busy.
	"app.status.busy":              "⟳ trabajando…",
	"app.status.busy_ascii":        "[..] trabajando...",
	"app.banner.docker_down":       "✖ Docker no está disponible — tus servidores no responden. [Ctrl+R] Reparar Docker",
	"app.banner.docker_starting":   "◐ Docker está arrancando… [Ctrl+R] Reintentar",
	"app.banner.ascii_down":        "[XX] Docker no esta disponible - tus servidores no responden. [Ctrl+R] Reparar Docker",
	"app.banner.ascii_starting":    "[..] Docker esta arrancando... [Ctrl+R] Reintentar",
	"app.footer.hint":              "Pista: {{hint}}",
	"app.toast.docker_down":        "✖ Docker se detuvo — tus servidores no responden. [H] Reparar",
	"app.toast.docker_up":          "✓ Docker volvió",
	"app.toast.docker_ready":       "✓ Docker está listo",
	"app.toast.docker_repair_fail": "✖ No se pudo reparar Docker: {{cause}}",
	"app.toast.docker_still":       "⚠ Docker sigue sin responder: {{cause}}",
	"app.toast.repairing":          "⟳ Intentando iniciar Docker…",
	"app.toast.create_fail":        "✖ No se pudo crear: {{cause}}",
	"app.toast.created":            "✓ Instancia «{{name}}» creada",
	"app.toast.idle_no_watcher":    "⚠ El auto-apagado no está activo: nadie lo vigila. Actívalo con 'badomens idle daemon'.",
	"app.toast.idle_countdown":     "⟳ «{{name}}» se apagará por inactividad en {{remaining}}",
	"app.toast.idle_cancelled":     "✓ «{{name}}»: hay jugadores de nuevo",
	"app.toast.idle_stopped":       "☾ «{{name}}» se apagó por inactividad",
	"app.quit.title":               "Hay una operación en curso",
	"app.quit.body":                "Si sales ahora se cancelará la operación pendiente.",
	"app.quit.wait":                "No, esperar",
	"app.quit.wait_obj":            "a que termine",
	"app.quit.wait_hint":           "Cancela la salida y deja terminar la operación.",
	"app.quit.now":                 "Sí, salir",
	"app.quit.now_obj":             "y cancelar la operación",
	"app.quit.now_hint":            "Cierra la TUI cancelando lo pendiente.",
	"app.disabled.docker":          "Docker no está activo — pulsa H para repararlo",
	"global.health.obj":            "Salud del sistema",
	"global.health.hint":           "Muestra Docker, Tailscale, disco y RAM.",
	"global.new.obj":               "una instancia nueva",
	"global.new.hint":              "Abre el asistente de nueva instancia.",
	"global.help.obj":              "la ayuda de esta pantalla",
	"global.help.hint":             "Muestra todos los atajos.",
}
