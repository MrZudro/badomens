package i18n

import (
	"strings"
	"testing"
)

// requiredKeys son claves que la interfaz siempre usa; si faltan, hay una
// cadena visible fuera del catálogo.
var requiredKeys = []string{
	"help.bar", "home.title", "home.empty.cta", "connect.title",
	"wizard.title", "action.start", "action.delete", "action.cancel",
	"toast.error", "idle.countdown", "health.title",
}

func TestEsCatalogoCompletoYNoVacio(t *testing.T) {
	c := Es()
	if c.Lang != "es" {
		t.Fatalf("idioma: %s", c.Lang)
	}
	keys := c.Keys()
	if len(keys) < 30 {
		t.Fatalf("catálogo demasiado pequeño: %d claves", len(keys))
	}
	for _, k := range requiredKeys {
		if !c.Has(k) || strings.TrimSpace(c.T(k)) == "" {
			t.Fatalf("falta o está vacía la clave requerida %q", k)
		}
	}
}

func TestTConClaveDesconocidaEsVisible(t *testing.T) {
	c := Es()
	got := c.T("nope.noexiste")
	if !strings.Contains(got, "nope.noexiste") {
		t.Fatalf("la clave desconocida debe ser visible: %q", got)
	}
}

// TestCadenasConFormato: las claves con {{...}} no se dejan sin formato en el
// catálogo (se sustituyen en la interfaz).
func TestCadenasConFormato(t *testing.T) {
	c := Es()
	for _, k := range c.Keys() {
		if strings.Contains(c.T(k), "{{") && strings.Contains(c.T(k), "}}") {
			// Se admite que la plantilla incluya marcadores (se formatean luego).
			continue
		}
		if strings.Contains(c.T(k), "{{") {
			t.Fatalf("clave %q: marcador sin cerrar", k)
		}
	}
}
