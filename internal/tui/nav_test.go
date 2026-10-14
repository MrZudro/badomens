package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// --- WS-2: navegación de primer nivel (Tab/Shift+Tab) y ayuda ---

// pressTab es Tab tal como lo entrega bubbletea.
func pressTab() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyTab} }

// pressShiftTab es Shift+Tab (pantalla anterior).
func pressShiftTab() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyShiftTab} }

// navConInstancia deja el Inicio con una fila seleccionada por la ruta de
// producción (App → Router → homeScreen).
func navConInstancia(a *App, slug string) {
	a.Update(pollRowsMsg{rows: []svc.Row{{
		Slug: slug, Name: "Survival", Engine: "paper", Version: "1.21.4",
		Port: 25565, State: "running", MaxPlayers: 20,
	}}})
}

// navAbreDetalle abre con Tab el detalle de la instancia seleccionada en Inicio.
func navAbreDetalle(t *testing.T, a *App, slug string) *instanceScreen {
	t.Helper()
	navConInstancia(a, slug)
	a.Update(pressTab())
	inst, ok := a.router.Top().(*instanceScreen)
	if !ok {
		t.Fatalf("Tab desde Inicio debía abrir el detalle, tengo %s", a.router.Top().ID())
	}
	if inst.slug != slug {
		t.Fatalf("el detalle debía ser «%s», tengo «%s»", slug, inst.slug)
	}
	return inst
}

// TestNavTabCiclaSecciones: Tab recorre inicio → instancias → salud → ayuda →
// inicio, y la ayuda es un overlay sobre la pantalla que la abrió.
func TestNavTabCiclaSecciones(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)

	// Inicio → Instancias: abre el detalle de la instancia seleccionada.
	navAbreDetalle(t, a, "survival")
	if a.router.Depth() != 2 {
		t.Fatalf("el detalle debe apilarse sobre Inicio, profundidad %d", a.router.Depth())
	}
	// Instancias → Salud.
	a.Update(pressTab())
	if got := a.router.Top().ID(); got != "health" {
		t.Fatalf("Tab desde el detalle debía ir a Salud, tengo %s", got)
	}
	// Salud → Ayuda (overlay: Salud sigue debajo).
	a.Update(pressTab())
	if ov := a.router.Overlay(); ov == nil || ov.ID() != "help" {
		t.Fatal("Tab desde Salud debía abrir la ayuda")
	}
	if got := a.router.Top().ID(); got != "health" {
		t.Fatalf("la ayuda es un overlay: Salud debe seguir debajo, tengo %s", got)
	}
	// Ayuda → Inicio: cierra el overlay y vuelve a la raíz.
	a.Update(pressTab())
	if ov := a.router.Overlay(); ov != nil {
		t.Fatalf("Tab desde la ayuda debía cerrar el overlay, tengo %s", ov.ID())
	}
	if got := a.router.Top().ID(); got != "home" {
		t.Fatalf("Tab desde la ayuda debía volver a Inicio, tengo %s", got)
	}
	if a.router.Depth() != 1 {
		t.Fatalf("Inicio debe ser la raíz de la pila, profundidad %d", a.router.Depth())
	}
}

// TestNavTabSinInstanciasCaeASalud: sin ninguna instancia, la sección
// «instancias» cae a Salud (decisión del contrato).
func TestNavTabSinInstanciasCaeASalud(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	a.Update(pressTab())
	if got := a.router.Top().ID(); got != "health" {
		t.Fatalf("sin instancias, Tab desde Inicio debía caer a Salud, tengo %s", got)
	}
}

// TestNavShiftTabInvierteElCiclo: Shift+Tab recorre el orden al revés.
func TestNavShiftTabInvierteElCiclo(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	navAbreDetalle(t, a, "survival") // pila: Inicio, Detalle

	// Instancias → Inicio.
	a.Update(pressShiftTab())
	if got := a.router.Top().ID(); got != "home" {
		t.Fatalf("Shift+Tab desde el detalle debía volver a Inicio, tengo %s", got)
	}
	if a.router.Depth() != 1 {
		t.Fatalf("Inicio debe quedar como raíz, profundidad %d", a.router.Depth())
	}
	// Inicio → Ayuda.
	a.Update(pressShiftTab())
	if ov := a.router.Overlay(); ov == nil || ov.ID() != "help" {
		t.Fatal("Shift+Tab desde Inicio debía abrir la ayuda")
	}
	// Ayuda → Salud.
	a.Update(pressShiftTab())
	if ov := a.router.Overlay(); ov != nil {
		t.Fatalf("Shift+Tab desde la ayuda debía cerrar el overlay, tengo %s", ov.ID())
	}
	if got := a.router.Top().ID(); got != "health" {
		t.Fatalf("Shift+Tab desde la ayuda debía ir a Salud, tengo %s", got)
	}
	// Salud → Instancias: abre el detalle de la selección de Inicio.
	a.Update(pressShiftTab())
	if got := a.router.Top().ID(); got != "instance" {
		t.Fatalf("Shift+Tab desde Salud debía abrir el detalle, tengo %s", got)
	}
}

// TestNavInstanciasSubeAlDetalleYaApilado: con un detalle ya apilado, Tab sube a
// él en vez de abrir otro.
func TestNavInstanciasSubeAlDetalleYaApilado(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	navAbreDetalle(t, a, "survival")
	a.Update(pressTab()) // detalle → Salud (pila: Inicio, Detalle, Salud)
	if got := a.router.Top().ID(); got != "health" {
		t.Fatalf("esperaba Salud, tengo %s", got)
	}
	a.Update(pressShiftTab()) // Salud → Instancias (vuelve al detalle apilado)
	if got := a.router.Top().ID(); got != "instance" {
		t.Fatalf("debía volver al detalle ya apilado, tengo %s", got)
	}
	if a.router.Depth() != 2 {
		t.Fatalf("no debe abrirse un segundo detalle, profundidad %d", a.router.Depth())
	}
}

// TestNavTabNoPisaFormularios: Tab solo navega cuando la pantalla activa no
// captura el teclado (asistente, consola, paleta).
func TestNavTabNoPisaFormularios(t *testing.T) {
	t.Run("asistente sin modo", func(t *testing.T) {
		a := testApp()
		resize(a, 120, 40)
		a.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
		if a.router.Top().ID() != "wizard" {
			t.Fatalf("Ctrl+N debe abrir el asistente, tengo %s", a.router.Top().ID())
		}
		a.Update(pressTab())
		a.Update(pressShiftTab())
		if got := a.router.Top().ID(); got != "wizard" {
			t.Fatalf("Tab no debe salir del asistente, tengo %s", got)
		}
		if a.router.Overlay() != nil {
			t.Fatalf("Tab no debe abrir overlays con el asistente abierto, tengo %s", a.router.Overlay().ID())
		}
	})

	t.Run("asistente capturando", func(t *testing.T) {
		a := testApp()
		resize(a, 120, 40)
		a.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
		a.Update(press("r")) // configuración rápida: el asistente captura el teclado
		wz, _ := a.router.Top().(*wizardScreen)
		if wz == nil || !wz.CapturesInput() {
			t.Fatal("el asistente debía capturar el teclado")
		}
		step, cur := wz.step, wz.cur
		a.Update(pressTab())
		if got := a.router.Top().ID(); got != "wizard" {
			t.Fatalf("Tab con el asistente activo no debe cambiar de pantalla, tengo %s", got)
		}
		if wz.step == step && wz.cur == cur {
			t.Fatal("Tab debía seguir siendo del asistente: avanza de campo o de paso")
		}
		if a.router.Overlay() != nil {
			t.Fatal("Tab no debe abrir overlays con el asistente activo")
		}
	})

	t.Run("consola", func(t *testing.T) {
		a := testApp()
		resize(a, 120, 40)
		inst := navAbreDetalle(t, a, "survival")
		inst.Invoke("instance.console") // pestaña Consola con el campo enfocado
		if !inst.CapturesInput() {
			t.Fatal("la consola enfocada debía capturar el teclado")
		}
		a.Update(pressTab())
		if got := a.router.Top().ID(); got != "instance" {
			t.Fatalf("Tab con la consola enfocada no debe cambiar de pantalla, tengo %s", got)
		}
	})

	t.Run("paleta", func(t *testing.T) {
		a := testApp()
		resize(a, 120, 40)
		a.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
		if ov := a.router.Overlay(); ov == nil || ov.ID() != "palette" {
			t.Fatal("Ctrl+P debe abrir la paleta")
		}
		a.Update(pressTab())
		if ov := a.router.Overlay(); ov == nil || ov.ID() != "palette" {
			t.Fatal("Tab no debe cerrar la paleta")
		}
		if got := a.router.Top().ID(); got != "home" {
			t.Fatalf("Tab no debe navegar con la paleta abierta, tengo %s", got)
		}
	})
}

// TestNavRouterAuxiliares: Root, Contains y PopTo sostienen la navegación.
func TestNavRouterAuxiliares(t *testing.T) {
	root := &fakeScreen{id: "home"}
	r := NewRouter(root)
	if r.Root() != root {
		t.Fatal("Root debe devolver la primera pantalla apilada")
	}
	inst := &fakeScreen{id: "instance"}
	health := &fakeScreen{id: "health"}
	r.Push(inst)
	r.Push(health)

	if !r.Contains("instance") || !r.Contains("home") {
		t.Fatal("Contains debe encontrar las pantallas apiladas")
	}
	if r.Contains("wizard") {
		t.Fatal("Contains no debe inventar pantallas ausentes")
	}
	if !r.PopTo("instance") || r.Top() != inst || r.Depth() != 2 {
		t.Fatalf("PopTo debe dejar la cima pedida: ok=%v depth=%d", r.Top() != inst, r.Depth())
	}
	if r.PopTo("wizard") {
		t.Fatal("PopTo debe devolver false si el ID no está en la pila")
	}
	if r.Depth() != 2 || r.Top() != inst {
		t.Fatalf("PopTo sin destino no debe tocar la pila: depth=%d", r.Depth())
	}
	if !r.PopTo("home") || r.Top() != root || r.Depth() != 1 {
		t.Fatalf("PopTo(\"home\") debe dejar la raíz: depth=%d", r.Depth())
	}
}

// TestNavTeclasTabEnLaBarra: Tab/Shift+Tab se documentan como navegación de
// pantallas y aparecen en las barras de ayuda.
func TestNavTeclasTabEnLaBarra(t *testing.T) {
	if h := DefaultKeys.NextPane.Help(); h.Key != "Tab" || !strings.Contains(h.Desc, "pantalla") {
		t.Fatalf("NextPane debe documentarse como «Tab · cambiar pantalla», tengo %q/%q", h.Key, h.Desc)
	}
	if h := DefaultKeys.PrevPane.Help(); h.Key != "Shift+Tab" || !strings.Contains(h.Desc, "pantalla") {
		t.Fatalf("PrevPane debe documentarse como «Shift+Tab · pantalla anterior», tengo %q/%q", h.Key, h.Desc)
	}
	bars := map[string]string{
		"ShortHelp":       DefaultKeys.ShortHelp(),
		"ShortHelpASCII":  DefaultKeys.ShortHelpASCII(),
		"GlobalHelp":      DefaultKeys.GlobalHelp(),
		"GlobalHelpASCII": DefaultKeys.GlobalHelpASCII(),
	}
	for name, bar := range bars {
		if !strings.Contains(bar, "Tab") {
			t.Errorf("%s debe incluir Tab: %q", name, bar)
		}
	}
	// Las variantes tiny se quedan sin Tab para no perder «Salir» al truncar.
	if !strings.Contains(DefaultKeys.GlobalHelpTiny(), "Salir") {
		t.Fatal("GlobalHelpTiny debe conservar «Salir»")
	}
	if !strings.Contains(DefaultKeys.GlobalHelpTinyASCII(), "Salir") {
		t.Fatal("GlobalHelpTinyASCII debe conservar «Salir»")
	}
}

// TestNavAyudaSeparaPantallaYGlobales: la ayuda lista las acciones de la pantalla
// activa y, aparte, los globales del menú principal; una tecla usada por la
// pantalla no se repite en globales, y hay pista de cierre.
func TestNavAyudaSeparaPantallaYGlobales(t *testing.T) {
	acts := []components.Action{
		{ID: "global.help", Verb: "Ver", Object: "la ayuda", Key: "F1", Hint: "Muestra los atajos."},
		{ID: "wizard.next", Verb: "Siguiente", Object: "paso", Key: "tab", Hint: "Avanza al siguiente paso."},
		{ID: "instance.start", Verb: "Iniciar", Object: "«survival»", Key: "s", Hint: "Arranca el servidor."},
	}
	out := stripANSI(newHelpScreen(DefaultKeys, acts, theme.For(theme.Dark), false).View(100, 30))

	if !strings.Contains(out, "Globales (menú principal)") {
		t.Fatalf("la ayuda debe titular el bloque de globales:\n%s", out)
	}
	if !strings.Contains(out, "Esc o F1: cerrar") {
		t.Fatalf("la ayuda debe indicar cómo cerrarse:\n%s", out)
	}
	iPantalla := strings.Index(out, "[S]")
	iGlobales := strings.Index(out, "Globales (menú principal)")
	if iPantalla < 0 || iGlobales < 0 || iPantalla > iGlobales {
		t.Fatalf("el bloque de la pantalla debe ir antes que el de globales:\n%s", out)
	}
	if strings.Count(out, "Globales") != 1 {
		t.Fatalf("el bloque de globales debe aparecer una sola vez:\n%s", out)
	}
	globals := out[iGlobales:]
	// El asistente ya usa Tab: no debe repetirse como atajo global (Shift+Tab sí es otro).
	if strings.Contains(globals, "cambiar pantalla") {
		t.Fatalf("Tab ya es de la pantalla activa y no debe repetirse en globales:\n%s", out)
	}
	for _, want := range []string{"Ctrl+P", "F2", "Ctrl+N", "Ctrl+Q"} {
		if !strings.Contains(globals, want) {
			t.Fatalf("los globales deben incluir %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "[tab]") {
		t.Fatalf("la acción de pantalla que usa Tab debe mostrarse:\n%s", out)
	}
	if strings.Contains(out, "global.") {
		t.Fatalf("la ayuda no debe filtrar los IDs internos:\n%s", out)
	}
}

// TestNavAyudaSinAtajosPropios: si la pantalla no declara atajos, la ayuda lo dice
// en vez de dejar el bloque vacío.
func TestNavAyudaSinAtajosPropios(t *testing.T) {
	acts := []components.Action{
		{ID: "global.help", Verb: "Ver", Object: "la ayuda", Key: "F1", Hint: "Muestra los atajos."},
	}
	out := stripANSI(newHelpScreen(DefaultKeys, acts, theme.For(theme.Dark), true).View(100, 30))
	if !strings.Contains(out, "no tiene atajos propios") {
		t.Fatalf("la ayuda debe avisar de que la pantalla no tiene atajos:\n%s", out)
	}
	if !strings.Contains(out, "cambiar pantalla") {
		t.Fatalf("con la pantalla sin atajos, Tab debe seguir documentado:\n%s", out)
	}
}

// TestNavBarraContextualVisibleEn70 (v1.2.1): la barra global se acortó al añadir
// Tab, de modo que en 70 columnas siguen viéndose los atajos contextuales de la
// pantalla y la propia pista de Tab.
func TestNavBarraContextualVisibleEn70(t *testing.T) {
	a := testApp()
	resize(a, 70, 20)
	navConInstancia(a, "survival")
	out := stripANSI(a.View())
	if !strings.Contains(out, "[S] Iniciar") {
		t.Fatalf("en 70 columnas deben verse los atajos contextuales:\n%s", out)
	}
	if !strings.Contains(out, "Tab") {
		t.Fatalf("la barra global debe seguir mostrando Tab:\n%s", out)
	}
}
