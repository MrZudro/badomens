package components

import (
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// TestPanelMideExacto comprueba que el panel ocupa exactamente w×h celdas y
// encierra el cuerpo: ni desborda ni deja bordes abiertos, en Unicode y ASCII.
func TestPanelMideExacto(t *testing.T) {
	pal := theme.For(theme.Dark)
	for _, ascii := range []bool{false, true} {
		for _, sz := range [][2]int{{28, 8}, {40, 12}, {60, 20}, {34, 30}} {
			w, h := sz[0], sz[1]
			out := Panel("INSTANCIAS", "2/5 · 1/3 en marcha", "línea uno\nlínea dos con acentos áéí 🚀", w, h, true, pal, ascii)
			lines := strings.Split(out, "\n")
			if len(lines) != h {
				t.Fatalf("ascii=%v %dx%d: %d líneas", ascii, w, h, len(lines))
			}
			for i, l := range lines {
				if got := layout.Width(l); got != w {
					t.Fatalf("ascii=%v %dx%d: línea %d mide %d celdas: %q", ascii, w, h, i, got, l)
				}
			}
		}
	}
}

// TestPanelTituloLargoNoDesborda: un título/contador que no cabe se recorta sin
// romper el ancho.
func TestPanelTituloLargoNoDesborda(t *testing.T) {
	pal := theme.For(theme.Dark)
	out := Panel(strings.Repeat("MUY LARGO ", 10), "9/9 · 9/9 en marcha", "", 24, 4, false, pal, false)
	for _, l := range strings.Split(out, "\n") {
		if layout.Width(l) != 24 {
			t.Fatalf("el panel debe medir 24 celdas: %d en %q", layout.Width(l), l)
		}
	}
}
