package components

import (
	"strings"
	"testing"
)

// UX-01: la barra inferior usa un formato ÚNICO «[Tecla] Etiqueta» y las
// etiquetas definitivas de la SPEC (§1.2).
func TestFooterFormatoUnicoYEtiquetas(t *testing.T) {
	items := []FooterKey{
		FooterKeyFor("b", "Crear backup ahora"),
		FooterKeyFor("f1", "Ver la ayuda"),
		FooterKeyFor("tab", "Cambiar de zona"),
		FooterKeyFor("d", "Eliminar para siempre"),
		FooterKeyFor("esc", "Volver a la lista"),
	}
	out := RenderFooter(items, 200, pal())
	for _, want := range []string{"[B] Backup", "[F1] Ayuda", "[TAB] Desplazarse", "[D] Borrar", "[Esc] Atrás"} {
		if !strings.Contains(out, want) {
			t.Fatalf("la barra debe contener %q:\n%s", want, out)
		}
	}
	// Ninguna etiqueta queda sin corchetes ni con el texto crudo del verbo.
	for _, bad := range []string{"Crear backup ahora", "Ver la ayuda", "Cambiar de zona"} {
		if strings.Contains(out, bad) {
			t.Fatalf("la barra no debe conservar la etiqueta cruda %q:\n%s", bad, out)
		}
	}
}

// Las teclas no imprimibles se muestran con su forma canónica.
func TestFooterDisplayKey(t *testing.T) {
	cases := map[string]string{"f1": "F1", "tab": "TAB", "esc": "Esc", "ctrl+p": "Ctrl+P", "s": "S", "up": "↑"}
	for in, want := range cases {
		if got := DisplayKey(in); got != want {
			t.Fatalf("DisplayKey(%q) = %q, quiero %q", in, got, want)
		}
	}
}

// Una tecla que no cabe se descarta ENTERA: nunca se pinta media tecla.
func TestFooterDescartaEntero(t *testing.T) {
	items := []FooterKey{
		FooterKeyFor("s", "Iniciar"),
		FooterKeyFor("f1", "Ayuda"),
	}
	out := RenderFooter(items, 14, pal())
	if !strings.Contains(out, "[S] Iniciar") {
		t.Fatalf("el primer atajo debe verse:\n%s", out)
	}
	if strings.Contains(out, "[F1") || strings.Contains(out, "Ayud") {
		t.Fatalf("un atajo que no cabe no debe pintarse a medias: %q", out)
	}
}

// UX-01/§1.2: un atajo que no cabe se descarta ENTERO también cuando es el
// PRIMERO de la barra: nunca se pinta media tecla («[S] Ini…»). Si ni el primero
// cabe, la barra queda vacía y el llamante reserva lo esencial.
func TestFooterPrimerAtajoQueNoCabeSeDescartaEntero(t *testing.T) {
	items := []FooterKey{
		FooterKeyFor("s", "Iniciar"),
		FooterKeyFor("b", "Backup"),
	}
	out := RenderFooter(items, 5, pal())
	if out != "" {
		t.Fatalf("si ni el primer atajo cabe, no debe pintarse nada, tengo %q", out)
	}
	// Con sitio para el primero y no para el segundo, se descarta el segundo.
	out = RenderFooter(items, len("[S] Iniciar"), pal())
	if out != "[S] Iniciar" {
		t.Fatalf("con 11 celdas solo cabe el primer atajo completo, tengo %q", out)
	}
	if strings.Contains(out, "…") || strings.Contains(out, "[B") {
		t.Fatalf("el segundo atajo no debe pintarse a medias: %q", out)
	}
}
