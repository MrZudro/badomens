package theme

import (
	"testing"
)

// TestContrasteWCAGAA verifica la regla de accesibilidad: todos los pares
// texto/fondo declarados cumplen ≥ 4.5:1 (WCAG AA para texto normal).
func TestContrasteWCAGAA(t *testing.T) {
	for _, name := range All() {
		p := For(name)
		if p.Name != name {
			t.Fatalf("For(%s) devolvió %s", name, p.Name)
		}
		if len(p.Contrasts) == 0 {
			t.Fatalf("%s: sin pares de contraste declarados", name)
		}
		for _, pair := range p.Contrasts {
			ratio := ContrastRatio(pair[0], pair[1])
			if ratio < 4.5 {
				t.Fatalf("%s: contraste %s sobre %s = %.2f (< 4.5)", name, pair[0], pair[1], ratio)
			}
		}
	}
}

func TestParseDevuelveValores(t *testing.T) {
	cases := map[string]Name{
		"": Dark, "dark": Dark, "LIGHT": Light, "light": Light,
		"high-contrast": HighContrast, "colorblind": Colorblind, "nada": Dark,
	}
	for in, want := range cases {
		if got := Parse(in); got != want {
			t.Fatalf("Parse(%q): quiero %s, tengo %s", in, want, got)
		}
	}
}

func TestRelLumFunciona(t *testing.T) {
	white := RelLum("#ffffff")
	black := RelLum("#000000")
	if white <= black {
		t.Fatalf("el blanco debe tener mayor luminancia: %f vs %f", white, black)
	}
	if r := ContrastRatio("#ffffff", "#000000"); r < 20 {
		t.Fatalf("blanco/negro debe superar 20:1, tengo %.1f", r)
	}
	// Short hex sin '#'
	if got := RelLum("fff"); got <= 0 {
		t.Fatalf("hex corto debe funcionar, tengo %f", got)
	}
	if got := RelLum("kkk"); got != 0 {
		t.Fatalf("hex inválido debe dar 0, tengo %f", got)
	}
}

func TestIconosASCIIYUnicode(t *testing.T) {
	if got := Icon("running", false); got != "● En línea" {
		t.Fatalf("unicode: %q", got)
	}
	if got := Icon("running", true); got != "[OK] En línea" {
		t.Fatalf("ascii: %q", got)
	}
	if got := Icon("desconocido", false); got != "desconocido" {
		t.Fatalf("estado desconocido debe pasarse tal cual: %q", got)
	}
}
