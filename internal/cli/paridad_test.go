package cli

import (
	"strings"
	"testing"
)

// paridad es la tabla del RF-1: cada subcomando CLI debe tener una pantalla o
// acción de la TUI, o estar marcado como solo-cli. El valor describe el destino.
var paridad = map[string]string{
	"setup":                                      "wizard",
	"instance.list":                              "home",
	"instance.inspect":                           "instance",
	"instance.create":                            "wizard",
	"instance.start|stop|restart":                "home",
	"instance.close":                             "instance",
	"instance.remove":                            "instance",
	"connect":                                    "connectcard",
	"ports.check|list":                           "wizard+health",
	"engine.list|current|use|versions":           "wizard+ajustes",
	"world.list|save|use|export|import":          "instance/mundos",
	"backup.create|list|restore|prune":           "instance/backups",
	"access.whitelist-add|remove|ops-add|remove": "instance/jugadores",
	"plugin.add|list|remove":                     "instance/plugins",
	"logs":                                       "instance/consola",
	"console":                                    "instance/consola",
	"metrics":                                    "sparkline",
	"resolve-ip":                                 "connectcard+health",
	"doctor":                                     "health",
	"doctor.tps":                                 "health",
	"docker.status|ensure":                       "health+reparar-docker",
	"tailscale.status|ensure":                    "health+reparar-tailscale",
	"idle.set|status|disable|daemon|install-service|uninstall-service": "instance/auto-apagado",
	"update":    "health",
	"uninstall": "solo-cli",
	"capacity":  "health+wizard",
	"config":    "settings",
	"mcp":       "solo-cli",
	"tui":       "tui",
	"version":   "solo-cli",
}

// TestParidadCLITUI (RF-1): ningún subcomando del manifiesto puede quedar sin
// entrada en la tabla de paridad (docs/tui-paridad.md).
func TestParidadCLITUI(t *testing.T) {
	entries := commandManifest()
	if len(entries) < 20 {
		t.Fatalf("manifiesto sospechosamente corto: %d", len(entries))
	}
	for _, en := range entries {
		key := normalizeKey(en.Name)
		if _, ok := paridad[key]; !ok {
			t.Fatalf("el subcomando %q no tiene entrada en la tabla de paridad (docs/tui-paridad.md)", en.Name)
		}
	}
}

func normalizeKey(name string) string {
	return strings.ReplaceAll(strings.TrimSpace(name), " ", ".")
}
