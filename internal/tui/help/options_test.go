package help

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestCatalogCompleto verifica la regla dura de RF-4: toda opción tiene
// descripción corta (≤ 90 caracteres) y descripción larga.
func TestCatalogCompleto(t *testing.T) {
	if len(Entries) < 20 {
		t.Fatalf("quiero el catálogo completo de opciones, tengo %d", len(Entries))
	}
	for _, h := range Entries {
		if h.Key == "" || h.Title == "" {
			t.Fatalf("entrada sin key/título: %+v", h)
		}
		if strings.TrimSpace(h.Short) == "" {
			t.Fatalf("%s: falta la descripción corta", h.Key)
		}
		if n := utf8.RuneCountInString(h.Short); n > 90 {
			t.Fatalf("%s: descripción corta de %d caracteres (máx 90): %q", h.Key, n, h.Short)
		}
		if strings.TrimSpace(h.Long) == "" {
			t.Fatalf("%s: falta la descripción larga", h.Key)
		}
		if utf8.RuneCountInString(h.Long) < 80 {
			t.Fatalf("%s: la descripción larga es demasiado corta: %d", h.Key, utf8.RuneCountInString(h.Long))
		}
	}
}

func TestGetYKeys(t *testing.T) {
	h, ok := Get("memory")
	if !ok || h.Title != "Memoria (RAM)" {
		t.Fatalf("Get(memory): %+v ok=%v", h, ok)
	}
	if _, ok := Get("no-existe"); ok {
		t.Fatal("una clave desconocida no debe existir")
	}
	keys := Keys()
	if len(keys) == 0 {
		t.Fatal("Keys vacío")
	}
	for i := 1; i < len(keys); i++ {
		if keys[i] < keys[i-1] {
			t.Fatalf("Keys debe ir ordenado: %v", keys)
		}
	}
	if !Has("port") || Has("nope") {
		t.Fatal("Has debe reflejar el catálogo")
	}
}

// TestEtiquetasDeImpactoSanas: RequiresRestart y OnlyAtCreation no deben
// convivir en la misma opción (no tiene sentido).
func TestEtiquetasDeImpactoSanas(t *testing.T) {
	for _, h := range Entries {
		hasRR := has(h.Impact, TagRequiresRestart)
		hasOC := has(h.Impact, TagOnlyAtCreation)
		if hasRR && hasOC {
			t.Fatalf("%s mezcla RequiresRestart y OnlyAtCreation", h.Key)
		}
	}
}

func has(tags []Tag, want Tag) bool {
	for _, tg := range tags {
		if tg == want {
			return true
		}
	}
	return false
}
