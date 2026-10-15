package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// ===========================================================================
// Fase 12.1 · Accesibilidad (A11Y-*)
// ===========================================================================

// A11Y-01 (§6.1): NINGÚN estado depende solo del color. El semáforo de la
// instancia muestra siempre su TEXTO, también en el estado desconocido.
func TestA11Y01SemaforoConTextoSegunEstado(t *testing.T) {
	cases := []struct {
		raw   string
		state components.ServerState
	}{
		{"running", components.ServerOnline},
		{"restarting", components.ServerStarting},
		{"exited", components.ServerStopped},
		{"paused", components.ServerUnknown},
	}
	for _, c := range cases {
		a := testApp()
		resize(a, 100, 30)
		inst := openInstance(t, a, "survival")
		inst.detail.State = c.raw
		out := stripANSI(a.View())
		label := c.state.Label(false)
		if !strings.Contains(out, label) {
			t.Fatalf("estado %q: la vista debe mostrar el texto %q (nunca solo color):\n%s", c.raw, label, out)
		}
	}
}

// A11Y-02 (§6.1/§6.2, P-06): con un input enfocado, las letras n/a/q/d/b se
// escriben LITERALMENTE; no cambian de pantalla, no abren overlays, no borran ni
// hacen backup ni salen.
func TestA11Y02LetrasLiteralesConInputEnfocado(t *testing.T) {
	const letters = "naqdb"

	// Consola: se enfoca con «/».
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	a.Update(press("/"))
	for _, r := range letters {
		a.Update(press(string(r)))
	}
	if got := inst.input.Value(); got != letters {
		t.Fatalf("la consola debe escribir %q literalmente, tengo %q", letters, got)
	}
	if a.router.Top().ID() != "instance" || a.router.Overlay() != nil || a.quitting {
		t.Fatalf("escribir no debe navegar/salir: top=%s overlay=%v quitting=%v", a.router.Top().ID(), a.router.Overlay(), a.quitting)
	}

	// Prompt de whitelist: se abre con «A» y captura el texto.
	b := testApp()
	resize(b, 120, 40)
	inst2 := openInstance(t, b, "survival")
	inst2.tab = 2
	b.Update(press("a"))
	if !inst2.prompt || inst2.promptAct != "acl.add" {
		t.Fatalf("«a» debe abrir el prompt de alta; prompt=%v act=%q", inst2.prompt, inst2.promptAct)
	}
	for _, r := range letters {
		b.Update(press(string(r)))
	}
	if got := inst2.input.Value(); got != letters {
		t.Fatalf("el prompt debe escribir %q literalmente, tengo %q", letters, got)
	}
	if b.router.Top().ID() != "instance" || b.router.Depth() != 2 {
		t.Fatalf("escribir no debe apilar pantallas: top=%s depth=%d", b.router.Top().ID(), b.router.Depth())
	}
}

// A11Y-03: la compatibilidad es TRIVALENTE y siempre lleva texto: compatible,
// incompatible y desconocido (unknown) se distinguen sin depender del color.
func TestA11Y03CompatibilidadTrivalenteConTexto(t *testing.T) {
	w := newWizardScreen(svc.NewFake(nil), theme.For(theme.Dark), true)
	cases := []struct {
		name string
		c    engines.Compatibility
		want string
	}{
		{"compatible", engines.CompatibilityCompatible, "compatible"},
		{"incompatible", engines.CompatibilityIncompatible, "incompatible"},
		{"desconocido", engines.CompatibilityUnknown, "sin verificar"},
		{"vacío", "", "sin verificar"},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		got := w.compatibilityText(tc.c)
		if !strings.Contains(got, tc.want) {
			t.Fatalf("%s: el texto debe contener %q, tengo %q", tc.name, tc.want, got)
		}
		seen[got] = true
	}
	if len(seen) < 3 {
		t.Fatalf("los estados de compatibilidad deben ser distinguibles por texto, tengo %d: %v", len(seen), seen)
	}
	if t1 := w.compatibilityText(engines.CompatibilityCompatible); strings.Contains(t1, "incompatible") || strings.Contains(t1, "sin verificar") {
		t.Fatalf("«compatible» no puede confundirse con otros estados: %q", t1)
	}
	if tUnknown := w.compatibilityText(engines.CompatibilityUnknown); !strings.Contains(tUnknown, "unknown") {
		t.Fatalf("el estado desconocido debe nombrarse explícitamente: %q", tUnknown)
	}
}

// A11Y-04 (§6.1): la app es usable en terminales COMPACTOS. En 60×18, 80×24,
// 100×30 y 132×40 la vista no desborda ni el ancho ni el alto y el pie de atajos
// (con «[F1] Ayuda») sigue en la última línea en Inicio, Detalle y Ayuda; abrir y
// cerrar la Ayuda sigue funcionando (funciones esenciales accesibles sin fila de
// pestañas literal).
func TestA11Y04TerminalesCompactos(t *testing.T) {
	sizes := [][2]int{{60, 18}, {80, 24}, {100, 30}, {132, 40}}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		a := testApp()
		resize(a, w, h)
		checkFrame(t, a, w, h, "home")

		// Detalle (pestañas reales del detalle, sin fila de pestañas de primer nivel).
		inst := openInstance(t, a, "survival")
		checkFrame(t, a, w, h, "detail")
		// Cambiar de pestaña con ←/→ sigue funcionando en compacto.
		before := inst.tab
		a.Update(tea.KeyMsg{Type: tea.KeyRight})
		if inst.tab == before {
			t.Fatalf("%dx%d: ←/→ deben cambiar de pestaña del detalle", w, h)
		}

		// Ayuda como overlay: se abre con F1 y se cierra con Esc.
		a.Update(tea.KeyMsg{Type: tea.KeyF1})
		if a.router.Overlay() == nil {
			t.Fatalf("%dx%d: F1 debe abrir la Ayuda", w, h)
		}
		checkFrame(t, a, w, h, "help")
		a.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if a.router.Overlay() != nil {
			t.Fatalf("%dx%d: Esc debe cerrar la Ayuda", w, h)
		}
	}
}

// checkFrame valida el marco de una vista: alto exacto, ninguna línea desborda y
// el pie con «Ayuda» en la última línea.
func checkFrame(t *testing.T, a *App, w, h int, label string) {
	t.Helper()
	out := stripANSI(a.View())
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Fatalf("%s %dx%d: la vista debe ocupar %d líneas, tengo %d", label, w, h, h, len(lines))
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, "Ayuda") {
		t.Fatalf("%s %dx%d: el pie debe contener «Ayuda» en la última línea: %q", label, w, h, last)
	}
	for i, ln := range lines {
		if layout.Width(ln) > w {
			t.Fatalf("%s %dx%d: la línea %d mide %d celdas: %q", label, w, h, i+1, layout.Width(ln), ln)
		}
	}
}

// A11Y-05 (§6.1): los indicadores de carga NO bloquean la navegación ni son el
// único indicador de actividad: el estado ocupado lleva TEXTO y las teclas siguen
// procesándose.
func TestA11Y05CargaNoBloqueaNiEsElUnicoIndicador(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	a.Update(busyMsg{on: true})
	if !a.busy {
		t.Fatal("busyMsg debe marcar la app como ocupada")
	}
	// Indicador textual de actividad (no solo un spinner animado).
	if out := stripANSI(a.View()); !strings.Contains(out, a.cat.T("app.status.busy")) {
		t.Fatalf("con una operación en curso debe verse el texto de actividad:\n%s", out)
	}
	// La navegación sigue viva mientras hay una operación en curso.
	before := inst.tab
	a.Update(tea.KeyMsg{Type: tea.KeyRight})
	if inst.tab == before {
		t.Fatal("la navegación debe seguir funcionando durante una operación")
	}
	// El pie fijo no desaparece: la actividad se comunica, no bloquea.
	lines := strings.Split(stripANSI(a.View()), "\n")
	if !strings.Contains(lines[len(lines)-1], "Ayuda") {
		t.Fatalf("el pie debe seguir visible durante una operación: %q", lines[len(lines)-1])
	}
}
