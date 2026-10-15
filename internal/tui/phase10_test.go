package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// --- Fase 10 · Pulido de UI (SPEC 2 §13/§14 y §15) ---

// helpGlobalKeys son las teclas globales que la hoja de Ayuda debe documentar
// (SPEC 2 §13.2/§13.3), incluidas las retiradas de la barra inferior.
var helpGlobalKeys = []string{"[F1]", "[F2]", "[TAB]", "[Shift+Tab]", "[Ctrl+P]", "[Ctrl+Q]", "[Esc]", "[Enter]", "[Ctrl+N]"}

// HELP-01 (§13.2/UX-14): la hoja de Ayuda es un grid de columnas alineadas que
// se adapta al ancho del terminal —se prueba en 60×18, 80×24, 100×30 y 132×40—
// sin desbordar el marco ni mover el pie, y contiene todos los atajos globales
// (incluidos los que ya no están en la barra).
func TestHELP01GridSeAdaptaAlAncho(t *testing.T) {
	for _, sz := range [][2]int{{60, 18}, {80, 24}, {100, 30}, {132, 40}} {
		w, h := sz[0], sz[1]
		a := testApp()
		resize(a, w, h)
		navConInstancia(a, "survival")
		a.Update(tea.KeyMsg{Type: tea.KeyF1})
		if ov := a.router.Overlay(); ov == nil || ov.ID() != "help" {
			t.Fatalf("%dx%d: F1 debe abrir la hoja de Ayuda", w, h)
		}
		out := stripANSI(a.View())
		assertWithinSize(t, out, w, h)
		if lines := strings.Split(out, "\n"); len(lines) != h {
			t.Fatalf("%dx%d: la vista debe ocupar exactamente %d líneas, tengo %d", w, h, h, len(lines))
		}
		if !strings.Contains(out, "Globales (cualquier pantalla)") {
			t.Fatalf("%dx%d: la hoja debe titular el bloque de globales:\n%s", w, h, out)
		}
		for _, key := range helpGlobalKeys {
			if !strings.Contains(out, key) {
				t.Fatalf("%dx%d: la hoja debe documentar %q:\n%s", w, h, key, out)
			}
		}
		// Las etiquetas definitivas de los comandos retirados de la barra.
		for _, want := range []string{"[Ctrl+P] paleta", "[F2] salud", "[Ctrl+Q] salir"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%dx%d: la hoja debe etiquetar %q:\n%s", w, h, want, out)
			}
		}
	}
}

// HELP-02 (§13.2): el grid se reparte en columnas alineadas —todas las celdas
// arrancan en la misma columna en todas las filas— y el número de columnas se
// ajusta al ancho disponible sin romper la alineación.
func TestHELP02GridAlineadoYColumnas(t *testing.T) {
	entries := helpGlobals(DefaultKeys, map[string]bool{})
	if len(entries) < 6 {
		t.Fatalf("la hoja debe ordenar los comandos globales, tengo %d", len(entries))
	}
	cellW := entries[0].cell()
	for _, e := range entries {
		if layout.Width(e.cell()) > layout.Width(cellW) {
			cellW = e.cell()
		}
	}
	natural := layout.Width(cellW)
	for _, tc := range []struct {
		inner, cols int
	}{{24, 1}, {50, 2}, {82, 3}, {200, 3}} {
		rows := helpGrid(entries, tc.inner, lipgloss.NewStyle())
		want := (len(entries) + tc.cols - 1) / tc.cols
		if len(rows) != want {
			t.Fatalf("ancho %d: se esperaban %d filas en %d columnas, tengo %d", tc.inner, want, tc.cols, len(rows))
		}
		// El ancho de celda queda acotado para que quepan al menos dos columnas.
		colW := natural
		if tc.inner >= 40 && colW > (tc.inner-helpGap)/2 {
			colW = (tc.inner - helpGap) / 2
		}
		if colW > tc.inner {
			colW = tc.inner
		}
		for _, r := range rows {
			if layout.Width(r) > tc.inner {
				t.Fatalf("ancho %d: la fila mide %d celdas: %q", tc.inner, layout.Width(r), r)
			}
			for c := 0; c < tc.cols; c++ {
				start := c * (colW + helpGap)
				if start >= layout.Width(r) {
					break // la última fila puede llevar menos celdas
				}
				if r[start] != '[' {
					t.Fatalf("ancho %d: la columna %d no arranca alineada (pos %d) en %q", tc.inner, c, start, r)
				}
			}
		}
	}
}

// HELP-03 (§13.2): el orden es una cascada piramidal por longitud de tecla
// ascendente: F1, F2, Tab → Shift+Tab → Ctrl+P, Ctrl+Q → resto (Esc, Enter,
// Ctrl+N) de menor a mayor longitud.
func TestHELP03OrdenEnCascadaPiramidal(t *testing.T) {
	out := stripANSI(newHelpScreen(DefaultKeys, nil, theme.For(theme.Dark), false).View(110, 30))
	order := []string{"[F1]", "[F2]", "[TAB]", "[Shift+Tab]", "[Ctrl+P]", "[Ctrl+Q]", "[Esc]", "[Enter]", "[Ctrl+N]"}
	prev := -1
	for _, key := range order {
		i := strings.Index(out, key)
		if i < 0 {
			t.Fatalf("la hoja debe incluir %q:\n%s", key, out)
		}
		if i < prev {
			t.Fatalf("el orden en cascada piramidal está roto: %q aparece antes de lo esperado:\n%s", key, out)
		}
		prev = i
	}
}

// HELP-04 (§13.2): la hoja explica de forma coherente Enter (abrir/confirmar),
// Esc (volver/cancelar sin guardar) y cómo se cierra la propia ayuda.
func TestHELP04InstruccionesDeEnterEscYCierre(t *testing.T) {
	out := stripANSI(newHelpScreen(DefaultKeys, nil, theme.For(theme.Dark), false).View(100, 30))
	for _, want := range []string{
		"Enter abre o confirma",
		"Esc vuelve o cancela sin guardar",
		"F1 o Esc cierra",
		"[Esc] volver o cancelar",
		"[Enter] abrir o confirmar",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("la hoja debe incluir la instrucción %q:\n%s", want, out)
		}
	}
}

// SOON-01 (§14/UX-16): la sección Plugins muestra la etiqueta (SOON) en la
// pestaña y en el panel, y no sugiere que la funcionalidad esté operativa.
func TestSOON01PluginsAvisaSOON(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 5

	chrome := stripANSI(inst.Chrome(112))
	if !strings.Contains(chrome, "Plugins/Mods (SOON)") {
		t.Fatalf("la pestaña de Plugins debe anunciar (SOON):\n%s", chrome)
	}
	panel := stripANSI(inst.pluginsView(106, 24))
	for _, want := range []string{"PLUGINS/MODS (SOON)", "Próximamente"} {
		if !strings.Contains(panel, want) {
			t.Fatalf("el panel de Plugins debe avisar (SOON) con %q:\n%s", want, panel)
		}
	}
	// En terminal compacto el aviso sigue visible (pestaña activa + cuerpo).
	resize(a, 40, 12)
	compacto := stripANSI(a.View())
	if !strings.Contains(compacto, "(SOON)") {
		t.Fatalf("en compacto el aviso (SOON) debe seguir visible:\n%s", compacto)
	}
}

// CON-04 (§10 Fase 10): la Consola mantiene buffer y viewport dentro de su
// región: `consoleView(w,h)` ocupa exactamente su alto (nunca desplaza el footer
// ni desborda el ancho), incluso con el buffer lleno (2.000 líneas) y con una
// línea de log muy larga.
func TestCON04ConsolaDentroDeSuRegion(t *testing.T) {
	for _, sz := range [][2]int{{60, 18}, {80, 24}, {100, 30}, {132, 40}} {
		w, h := sz[0], sz[1]
		a := testApp()
		resize(a, w, h)
		inst := openInstance(t, a, "survival")
		inst.tab = 1
		inst.logs = testLogLines(maxLogLines) + "\n" + strings.Repeat("Z", 500)

		inner := w - 4
		bodyH := h - 4 // reserva fija de la carcasa (cabecera, separador, estado, acciones)
		vista := stripANSI(inst.consoleView(inner, bodyH))
		lines := strings.Split(vista, "\n")
		if len(lines) != bodyH {
			t.Fatalf("%dx%d: la consola debe ocupar exactamente %d líneas, tengo %d", w, h, bodyH, len(lines))
		}
		for i, ln := range lines {
			if layout.Width(ln) > inner {
				t.Fatalf("%dx%d: la línea %d de la consola mide %d celdas: %q", w, h, i+1, layout.Width(ln), ln)
			}
		}
		// Y la vista completa mantiene el pie de atajos en la última línea.
		out := stripANSI(a.View())
		full := strings.Split(out, "\n")
		if len(full) != h {
			t.Fatalf("%dx%d: el marco debe ocupar %d líneas, tengo %d", w, h, h, len(full))
		}
		if last := full[h-1]; !strings.Contains(last, "Ayuda") {
			t.Fatalf("%dx%d: la consola no debe desplazar el pie de atajos: %q", w, h, last)
		}
	}
}

// ESC-02 (§6/UX-15): con el input de comandos enfocado, Esc devuelve el foco sin
// ejecutar nada: ni envía el comando pendiente, ni sale del detalle, ni guarda
// histórico.
func TestESC02EscNoEjecutaNada(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 1

	a.Update(press("/"))
	for _, r := range "stop" {
		a.Update(press(string(r)))
	}
	// «stop» es peligroso: el primer Enter lo deja pendiente de confirmar.
	if _, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatalf("un comando peligroso no debe ejecutarse en el primer Enter, tengo %T", cmd)
	}
	if inst.pending != "stop" {
		t.Fatalf("el aviso de comando peligroso debe quedar pendiente, tengo %q", inst.pending)
	}
	hist := len(inst.history)

	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("Esc no debe ejecutar nada, tengo %T", cmd)
	}
	if inst.InputFocused() || inst.input.Focused() {
		t.Fatal("Esc debe devolver el foco sin ejecutar nada")
	}
	if inst.pending != "" {
		t.Fatalf("Esc debe cancelar el comando pendiente, tengo %q", inst.pending)
	}
	if len(inst.history) != hist {
		t.Fatalf("Esc no debe tocar el histórico (%d → %d)", hist, len(inst.history))
	}
	if got := a.router.Top().ID(); got != "instance" {
		t.Fatalf("Esc no debe abandonar el detalle, tengo %s", got)
	}
}
