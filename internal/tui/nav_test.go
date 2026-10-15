package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// --- Navegación por teclado sin la fila de pestañas superior (SPEC 2 §15) ---

// pressTab es Tab tal como lo entrega bubbletea.
func pressTab() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyTab} }

// pressShiftTab es Shift+Tab (zona o panel anterior).
func pressShiftTab() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyShiftTab} }

// pressEsc entrega Esc y, si la pantalla responde con un mensaje (p. ej.
// popScreenMsg), lo devuelve al App como haría el bucle de bubbletea.
func pressEsc(a *App) {
	if _, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		if msg := cmd(); msg != nil {
			a.Update(msg)
		}
	}
}

// navConInstancia deja el Inicio con una fila seleccionada por la ruta de
// producción (App → Router → homeScreen).
func navConInstancia(a *App, slug string) {
	a.Update(pollRowsMsg{rows: []svc.Row{{
		Slug: slug, Name: "Survival", Engine: "paper", Version: "1.21.4",
		Port: 25565, State: "running", MaxPlayers: 20,
	}}})
}

// UI-TABS-01 (§15/UX-17): la fila de pestañas de primer nivel ya no existe; el
// detalle se sigue abriendo desde Inicio con Enter y la navegación no depende de
// ninguna fila de pestañas, tampoco en terminal compacto.
func TestUITabs01SinFilaDePestanasSuperior(t *testing.T) {
	for _, sz := range [][2]int{{40, 12}, {80, 24}, {110, 30}, {132, 40}} {
		w, h := sz[0], sz[1]
		a := testApp()
		resize(a, w, h)
		navConInstancia(a, "survival")
		out := stripANSI(a.View())
		for _, ln := range strings.Split(out, "\n") {
			// La fila retirada pintaba las secciones de primer nivel juntas y
			// marcaba la activa con corchetes.
			if strings.Contains(ln, "Inicio") && strings.Contains(ln, "Salud") {
				t.Fatalf("%dx%d: sigue habiendo una fila de pestañas de primer nivel: %q", w, h, ln)
			}
			if strings.Contains(ln, "[Inicio]") || strings.Contains(ln, "[Instancias]") {
				t.Fatalf("%dx%d: la sección no debe marcarse con corchetes (la fila se retiró): %q", w, h, ln)
			}
		}
		// El acceso al detalle desde Inicio sigue funcionando (Enter).
		openInstance(t, a, "survival")
		if got := a.router.Top().ID(); got != "instance" {
			t.Fatalf("%dx%d: Enter desde Inicio debe abrir el detalle, tengo %s", w, h, got)
		}
		if a.router.Depth() != 2 {
			t.Fatalf("%dx%d: el detalle se apila sobre Inicio, profundidad %d", w, h, a.router.Depth())
		}
		// Y se vuelve con Esc, sin ayuda de ninguna pestaña.
		pressEsc(a)
		if got := a.router.Top().ID(); got != "home" {
			t.Fatalf("%dx%d: Esc desde el detalle debe volver a Inicio, tengo %s", w, h, got)
		}
	}
}

// NAV-01 (revisado, §15): F2 abre Salud desde cualquier pantalla y Esc vuelve a
// Inicio; la Ayuda sigue siendo un overlay que se cierra con Esc o F1.
func TestNav01SaludYAyudaSinFilaDePestanas(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	navConInstancia(a, "survival")

	a.Update(tea.KeyMsg{Type: tea.KeyF2})
	if got := a.router.Top().ID(); got != "health" {
		t.Fatalf("F2 debe abrir Salud, tengo %s", got)
	}
	pressEsc(a)
	if got := a.router.Top().ID(); got != "home" {
		t.Fatalf("Esc en Salud debe volver a Inicio, tengo %s", got)
	}
	if a.router.Depth() != 1 {
		t.Fatalf("Inicio debe quedar como raíz, profundidad %d", a.router.Depth())
	}

	a.Update(tea.KeyMsg{Type: tea.KeyF1})
	if ov := a.router.Overlay(); ov == nil || ov.ID() != "help" {
		t.Fatal("F1 debe abrir la ayuda")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if ov := a.router.Overlay(); ov != nil {
		t.Fatalf("Esc debe cerrar la ayuda, tengo %s", ov.ID())
	}
	if got := a.router.Top().ID(); got != "home" {
		t.Fatalf("la ayuda es un overlay: Inicio debe seguir debajo, tengo %s", got)
	}
}

// NAV-03: con el input de la consola enfocado, Tab sigue siendo del campo
// (autocompleta) y no cambia de pestaña, de zona ni de pantalla.
func TestNav03TabConElInputEnfocadoNoNavega(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.Invoke("instance.console") // pestaña Consola con el campo enfocado
	if !inst.CapturesInput() {
		t.Fatal("la consola enfocada debía capturar el teclado")
	}
	tab := inst.tab
	a.Update(pressTab())
	if got := a.router.Top().ID(); got != "instance" {
		t.Fatalf("Tab con la consola enfocada no debe cambiar de pantalla, tengo %s", got)
	}
	if inst.tab != tab {
		t.Fatalf("Tab con la consola enfocada no debe cambiar de pestaña (%d → %d)", tab, inst.tab)
	}
	if inst.input.Value() != consoleCommands[0] {
		t.Fatalf("Tab debe autocompletar en la consola, tengo %q", inst.input.Value())
	}
}

// NAV-04: el asistente conserva Tab para avanzar de campo o de paso y nunca sale
// de él por navegación.
func TestNav04AsistenteConservaTab(t *testing.T) {
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
}

// --- ZONE: Tab/Shift+Tab = «Desplazarse» entre zonas/paneles (§1.2) ---

// ZONE-01: en Inicio, Tab desplaza el foco entre lista · detalle · actividad sin
// cambiar de pantalla, y el foco se marca por texto (no solo por color).
func TestZone01TabDesplazaZonasEnInicio(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	navConInstancia(a, "survival")
	home, ok := a.router.Top().(*homeScreen)
	if !ok {
		t.Fatalf("se esperaba Inicio, tengo %s", a.router.Top().ID())
	}
	a.View() // el número de zonas depende de lo que pintó el último View
	if !home.zone.active(0) {
		t.Fatal("la lista debe ser la zona con foco al abrir Inicio")
	}
	a.Update(pressTab())
	if !home.zone.active(1) {
		t.Fatal("Tab debe pasar el foco a la zona siguiente")
	}
	if got := a.router.Top().ID(); got != "home" {
		t.Fatalf("Tab no debe cambiar de pantalla, tengo %s", got)
	}
	if out := stripANSI(home.View(116, 24)); !strings.Contains(out, components.ZoneMarker(false)+" DETALLE") {
		t.Fatalf("la zona con foco debe marcarse en el título del panel:\n%s", out)
	}
	a.Update(pressShiftTab())
	if !home.zone.active(0) {
		t.Fatal("Shift+Tab debe volver a la zona anterior")
	}
	if out := stripANSI(home.View(116, 24)); !strings.Contains(out, components.ZoneMarker(false)+" INSTANCIAS") {
		t.Fatalf("al volver, la lista debe recuperar la marca de foco:\n%s", out)
	}
}

// ZONE-02: en el detalle, Tab desplaza el foco entre los paneles de la pestaña
// activa (Resumen: RESUMEN · CÓMO CONECTARSE) sin cambiar de pestaña ni de pantalla.
func TestZone02TabEntrePanelesDelResumen(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 0
	a.View()
	if !inst.zone.active(0) {
		t.Fatal("el panel RESUMEN debe ser la zona con foco al entrar")
	}
	a.Update(pressTab())
	if !inst.zone.active(1) {
		t.Fatal("Tab debe pasar el foco a CÓMO CONECTARSE")
	}
	if inst.tab != 0 {
		t.Fatal("Tab no debe cambiar de pestaña: eso es ←/→")
	}
	if got := a.router.Top().ID(); got != "instance" {
		t.Fatalf("Tab no debe salir del detalle, tengo %s", got)
	}
	vista := stripANSI(inst.summaryView(106, 24))
	if !strings.Contains(vista, components.ZoneMarker(false)+" CÓMO CONECTARSE") {
		t.Fatalf("el panel con foco debe marcarse por texto:\n%s", vista)
	}
	if strings.Contains(vista, components.ZoneMarker(false)+" RESUMEN") {
		t.Fatalf("solo una zona puede tener el foco:\n%s", vista)
	}
	a.Update(pressShiftTab())
	if !inst.zone.active(0) {
		t.Fatal("Shift+Tab debe volver a la zona anterior")
	}
}

// ZONE-03: en una pestaña con un solo panel no hay zonas que desplazar y Tab no
// hace nada (ni cambia de pestaña ni de pantalla).
func TestZone03PestanaSinZonasNoHaceNada(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 3 // Backups: un solo panel
	a.View()
	if inst.zone.move(1) {
		t.Fatal("con un solo panel no hay zonas que desplazar")
	}
	a.Update(pressTab())
	if inst.tab != 3 {
		t.Fatalf("Tab no debe cambiar de pestaña, tengo %d", inst.tab)
	}
	if got := a.router.Top().ID(); got != "instance" {
		t.Fatalf("Tab no debe cambiar de pantalla, tengo %s", got)
	}
}

// --- Hoja de Ayuda: bloques y deduplicación (§13) ---

// La ayuda lista las acciones de la pantalla activa y, aparte, los globales; una
// tecla usada por la pantalla no se repite en globales y hay pista de cierre.
func TestAyudaSeparaPantallaYGlobales(t *testing.T) {
	acts := []components.Action{
		{ID: "global.help", Verb: "Ver", Object: "la ayuda", Key: "F1", Hint: "Muestra los atajos."},
		{ID: "wizard.next", Verb: "Siguiente", Object: "paso", Key: "tab", Hint: "Avanza al siguiente paso."},
		{ID: "instance.start", Verb: "Iniciar", Object: "«survival»", Key: "s", Hint: "Arranca el servidor."},
	}
	out := stripANSI(newHelpScreen(DefaultKeys, acts, theme.For(theme.Dark), false).View(100, 30))

	if !strings.Contains(out, "Globales (cualquier pantalla)") {
		t.Fatalf("la ayuda debe titular el bloque de globales:\n%s", out)
	}
	iPantalla := strings.Index(out, "[S]")
	iGlobales := strings.Index(out, "Globales (cualquier pantalla)")
	if iPantalla < 0 || iGlobales < 0 || iPantalla > iGlobales {
		t.Fatalf("el bloque de la pantalla debe ir antes que el de globales:\n%s", out)
	}
	if strings.Count(out, "Globales") != 1 {
		t.Fatalf("el bloque de globales debe aparecer una sola vez:\n%s", out)
	}
	globals := out[iGlobales:]
	// El asistente ya usa Tab: no debe repetirse como atajo global.
	if strings.Contains(globals, "[TAB]") {
		t.Fatalf("Tab ya es de la pantalla activa y no debe repetirse en globales:\n%s", out)
	}
	for _, want := range []string{"Ctrl+P", "F2", "Ctrl+N", "Ctrl+Q"} {
		if !strings.Contains(globals, want) {
			t.Fatalf("los globales deben incluir %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "[TAB]") {
		t.Fatalf("la acción de pantalla que usa Tab debe mostrarse en el formato único:\n%s", out)
	}
	if strings.Contains(out, "global.") {
		t.Fatalf("la ayuda no debe filtrar los IDs internos:\n%s", out)
	}
}

// Si la pantalla no declara atajos, la ayuda lo dice en vez de dejar el bloque
// vacío, y los globales siguen documentados.
func TestAyudaSinAtajosPropios(t *testing.T) {
	acts := []components.Action{
		{ID: "global.help", Verb: "Ver", Object: "la ayuda", Key: "F1", Hint: "Muestra los atajos."},
	}
	out := stripANSI(newHelpScreen(DefaultKeys, acts, theme.For(theme.Dark), true).View(100, 30))
	if !strings.Contains(out, "no tiene atajos propios") {
		t.Fatalf("la ayuda debe avisar de que la pantalla no tiene atajos:\n%s", out)
	}
	if !strings.Contains(out, "[TAB]") {
		t.Fatalf("con la pantalla sin atajos, Tab debe seguir documentado:\n%s", out)
	}
}

// La barra contextual de Inicio sigue viéndose en 70 columnas con el formato
// único «[Tecla] Etiqueta» (SPEC 2 §1).
func TestNavBarraContextualVisibleEn70(t *testing.T) {
	a := testApp()
	resize(a, 70, 20)
	navConInstancia(a, "survival")
	out := stripANSI(a.View())
	if !strings.Contains(out, "[S] Iniciar") {
		t.Fatalf("en 70 columnas deben verse los atajos contextuales:\n%s", out)
	}
	if !strings.Contains(out, "[TAB] Desplazarse") {
		t.Fatalf("la barra debe mostrar [TAB] Desplazarse:\n%s", out)
	}
}
