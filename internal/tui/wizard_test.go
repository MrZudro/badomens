package tui

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/ports"
	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

func redStepIndex(w *wizardScreen) int {
	for i, st := range w.steps {
		if st.title == "Red" {
			return i
		}
	}
	return -1
}

// TestAsistentePuertoLibre comprueba la validación en vivo del puerto: libre.
func TestAsistentePuertoLibre(t *testing.T) {
	w := newWizardScreen(svc.NewFake(nil), theme.For(theme.Dark), false)
	w.start(1)
	if redStepIndex(w) < 0 {
		t.Fatal("el asistente avanzado debe tener el paso Red con puerto")
	}
	w.setPortField("25565")
	cmd := w.checkPortCmd(0)
	if cmd == nil {
		t.Fatal("checkPortCmd debía comprobar el puerto")
	}
	msg := cmd()
	w.applyPortResult(msg.(wizardPortMsg))
	if w.portStatus != "free" {
		t.Fatalf("estado del puerto = %q, quiero free", w.portStatus)
	}
}

// TestAsistentePuertoOcupadoSugiere: un puerto ocupado muestra el sugerido y la
// acción para usarlo (RF-3).
func TestAsistentePuertoOcupadoSugiere(t *testing.T) {
	res := ports.Result{
		Port: 25565, Free: false, Suggested: 25566,
		Conflicts: []ports.Conflict{{
			Port: 25565, Severity: ports.SeverityBlock,
			Owner: ports.Owner{Kind: ports.OwnerDocker, Detail: "contenedor minecraft-creativo"},
		}},
	}
	w := newWizardScreen(svc.NewFakeWith(nil, svc.FakeOptions{PortResult: &res}), theme.For(theme.Dark), false)
	w.start(1)
	w.setPortField("25565")
	w.applyPortResult(w.checkPortCmd(0)().(wizardPortMsg))
	if w.portStatus != "busy" {
		t.Fatalf("estado del puerto = %q, quiero busy", w.portStatus)
	}
	if w.portSuggest != 25566 {
		t.Fatalf("sugerido = %d, quiero 25566", w.portSuggest)
	}
	// La acción de usar el sugerido debe existir y funcionar.
	found := false
	for _, a := range w.Actions() {
		if a.ID == "wizard.useport" {
			found = true
		}
	}
	if !found {
		t.Fatal("debía ofrecerse la acción de usar el puerto sugerido")
	}
	if cmd := w.Invoke("wizard.useport"); cmd == nil {
		t.Fatal("usar el sugerido debía revalidar")
	}
	if got := w.portFieldValue(); got != "25566" {
		t.Fatalf("puerto tras usar el sugerido = %q, quiero 25566", got)
	}
}

// TestAsistentePuertoInvalido: un valor no numérico se marca inválido sin sondeo.
func TestAsistentePuertoInvalido(t *testing.T) {
	w := newWizardScreen(svc.NewFake(nil), theme.For(theme.Dark), false)
	w.start(1)
	w.setPortField("no-es-un-puerto")
	if cmd := w.checkPortCmd(0); cmd != nil {
		t.Fatal("un puerto inválido no debe lanzar sondeo")
	}
	if w.portStatus != "invalid" {
		t.Fatalf("estado = %q, quiero invalid", w.portStatus)
	}
}

// TestAsistentePuertoEnPeticion: el puerto elegido viaja en la petición de creación.
func TestAsistentePuertoEnPeticion(t *testing.T) {
	w := newWizardScreen(svc.NewFake(nil), theme.For(theme.Dark), false)
	w.start(1)
	w.setPortField("25570")
	req := w.buildRequest()
	if req.Port != 25570 {
		t.Fatalf("req.Port = %d, quiero 25570", req.Port)
	}
}

// TestAsistenteDebounceRetrasa: al teclear en el puerto se agenda un tick de
// 300 ms en lugar de sondear en cada pulsación.
func TestAsistenteDebounceRetrasa(t *testing.T) {
	w := newWizardScreen(svc.NewFake(nil), theme.For(theme.Dark), false)
	w.start(1)
	w.step = redStepIndex(w)
	w.cur = 0
	w.enterField() // entra en edición del puerto
	if !w.editingPort() {
		t.Fatal("el puerto debía estar en edición")
	}
	before := w.portSeq
	_, cmd := w.handleStepKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if cmd == nil {
		t.Fatal("teclear en el puerto debía agendar el debounce")
	}
	if w.portSeq != before+1 || w.portStatus != "checking" {
		t.Fatalf("debounce no agendado: seq=%d status=%q", w.portSeq, w.portStatus)
	}
}

// --- Fase F: composición en paneles, F1 y caché de versiones (§5.12) ---

// countingCatalog cuenta las peticiones de versiones por motor + snapshots.
type countingCatalog struct {
	mu    sync.Mutex
	calls map[string]int
	items map[string][]engines.Version
}

// catKey es la clave de conteo del catálogo falso (motor + snapshots), definida
// aquí para no depender del formato interno de la caché del asistente.
func catKey(engine string, snaps bool) string {
	return engine + "|" + strconv.FormatBool(snaps)
}

func newCountingCatalog() *countingCatalog {
	ver := func() []engines.Version {
		return []engines.Version{{
			Version: "1.21.4", Channel: engines.ChannelRelease,
			Recommended: true, Compatible: true, JavaMajor: 21,
		}}
	}
	return &countingCatalog{
		calls: map[string]int{},
		items: map[string][]engines.Version{
			"paper|false":  ver(),
			"fabric|false": ver(),
			"forge|false":  ver(),
			"paper|true":   ver(),
		},
	}
}

func (c *countingCatalog) Versions(_ context.Context, engine string, snaps bool) ([]engines.Version, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls[catKey(engine, snaps)]++
	return c.items[catKey(engine, snaps)], nil
}

func (c *countingCatalog) count(engine string, snaps bool) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[catKey(engine, snaps)]
}

// §5.12: la caché de versiones por motor evita repetir la petición al volver a un
// motor ya consultado, y separa releases de snapshots.
func TestAsistenteCacheDeVersionesPorMotor(t *testing.T) {
	cat := newCountingCatalog()
	s := svc.NewFake(nil)
	s.Catalog = cat
	w := newWizardScreen(s, theme.For(theme.Dark), false)

	// Primer arranque: se pide el catálogo del motor inicial (paper).
	cmd := w.start(1)
	if cmd == nil {
		t.Fatal("el primer arranque debe pedir las versiones")
	}
	w.Update(cmd())
	if got := cat.count("paper", false); got != 1 {
		t.Fatalf("peticiones de paper = %d, quiero 1", got)
	}
	if w.verLoading {
		t.Fatal("tras recibir el catálogo no debe seguir cargando")
	}
	if got := w.versionValue(); got != "1.21.4" {
		t.Fatalf("debe quedar elegida la versión recomendada, tengo %q", got)
	}

	// Cambiar de motor pide el catálogo del nuevo motor una sola vez.
	w.cur = 1 // campo Motor
	if cmd := w.cycle(1); cmd == nil {
		t.Fatal("cambiar de motor debe pedir versiones si no están en caché")
	} else {
		w.Update(cmd())
	}
	if w.engine.Name != "fabric" {
		t.Fatalf("el motor debe ser fabric, tengo %q", w.engine.Name)
	}
	if got := cat.count("fabric", false); got != 1 {
		t.Fatalf("peticiones de fabric = %d, quiero 1", got)
	}

	// Volver a un motor ya consultado se sirve de la caché: ni Cmd ni petición.
	if cmd := w.cycle(-1); cmd != nil {
		t.Fatal("volver a un motor cacheado no debe pedir versiones de nuevo")
	}
	if w.engine.Name != "paper" {
		t.Fatalf("el motor debe volver a paper, tengo %q", w.engine.Name)
	}
	if got := cat.count("paper", false); got != 1 {
		t.Fatalf("peticiones de paper tras volver = %d, quiero 1", got)
	}
	if len(w.versions) != 1 || w.verLoading {
		t.Fatalf("la caché debe dejar las versiones listas (n=%d, loading=%v)", len(w.versions), w.verLoading)
	}
	if cmd := w.cycle(1); cmd != nil {
		t.Fatal("fabric también debe servirse de la caché")
	}
	if got := cat.count("fabric", false); got != 1 {
		t.Fatalf("peticiones de fabric tras volver = %d, quiero 1", got)
	}

	// Cada variante de snapshots tiene su propia entrada de caché (se vuelve a
	// paper, que ya está en caché, para no depender del motor anterior).
	if cmd := w.cycle(-1); cmd != nil {
		t.Fatal("volver a paper debe servirse de la caché")
	}
	if w.engine.Name != "paper" {
		t.Fatalf("el motor debe ser paper, tengo %q", w.engine.Name)
	}
	w.cur = 2 // campo Versión
	w.pickingVer = true
	_, cmd = w.handleVersionPicker(press("s"))
	if cmd == nil {
		t.Fatal("activar snapshots debe pedir esa lista (no está en caché)")
	}
	w.Update(cmd())
	if got := cat.count("paper", true); got != 1 {
		t.Fatalf("peticiones de paper con snapshots = %d, quiero 1", got)
	}
	if !w.showSNAPS {
		t.Fatal("los snapshots deben quedar activados")
	}
	// Volver a releases usa la caché de antes.
	if _, cmd = w.handleVersionPicker(press("s")); cmd != nil {
		t.Fatal("volver a releases no debe repetir la petición")
	}
	if w.showSNAPS {
		t.Fatal("los snapshots deben quedar desactivados")
	}
	if got := cat.count("paper", false); got != 1 {
		t.Fatalf("peticiones de paper sin snapshots = %d, quiero 1", got)
	}
}

// §5.12: F1 abre y cierra la ayuda larga del campo; «?» sigue funcionando y con
// la ayuda abierta las letras no se cuelan en el formulario.
func TestAsistenteAyudaF1YInterrogacion(t *testing.T) {
	w := newWizardScreen(svc.NewFake(nil), theme.For(theme.Dark), false)
	w.start(1)
	if w.helpOpen {
		t.Fatal("la ayuda debe empezar cerrada")
	}
	w.Update(tea.KeyMsg{Type: tea.KeyF1})
	if !w.helpOpen {
		t.Fatal("F1 debe abrir la ayuda larga del campo")
	}
	out := stripANSI(w.View(80, 19))
	if !strings.Contains(out, "AYUDA") || !strings.Contains(out, "Pulsa Esc o F1") {
		t.Fatalf("la ayuda larga debe verse en su panel:\n%s", out)
	}
	w.Update(tea.KeyMsg{Type: tea.KeyF1})
	if w.helpOpen {
		t.Fatal("F1 debe cerrar la ayuda")
	}
	w.Update(press("?"))
	if !w.helpOpen {
		t.Fatal("«?» debe seguir abriendo la ayuda")
	}
	w.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if w.helpOpen {
		t.Fatal("Esc debe cerrar la ayuda")
	}

	// Con un campo en edición, F1 abre la ayuda sin perder lo escrito y sin
	// dejar pasar las letras al formulario.
	w.cur = 0
	if _, cmd := w.enterField(); cmd == nil {
		t.Fatal("el nombre debe entrar en edición")
	}
	w.input.SetValue("mi-servidor")
	w.Update(tea.KeyMsg{Type: tea.KeyF1})
	if !w.helpOpen {
		t.Fatal("F1 debe abrir la ayuda también mientras se escribe")
	}
	if w.edit {
		t.Fatal("con la ayuda abierta el campo no debe seguir en edición")
	}
	if got := w.steps[0].fields[0].value; got != "mi-servidor" {
		t.Fatalf("F1 no debe perder lo escrito, tengo %q", got)
	}
	w.Update(press("x"))
	if got := w.steps[0].fields[0].value; got != "mi-servidor" {
		t.Fatalf("con la ayuda abierta las letras no deben ir al campo, tengo %q", got)
	}
	w.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if w.helpOpen {
		t.Fatal("Esc debe cerrar la ayuda y volver al formulario")
	}
}

// §5.12: el modo rápido tiene 3 pasos y el avanzado 7, con sus títulos, y el
// panel del paso lleva el título y el contador «n/N» (Revisar incluido).
func TestAsistentePasosPorModo(t *testing.T) {
	casos := []struct {
		mode    int
		titulos []string
	}{
		{0, []string{"Básico", "Recursos", "Revisar"}},
		{1, []string{"Básico", "Mundo", "Jugadores", "Recursos", "Red", "Automatización", "Revisar"}},
	}
	for _, c := range casos {
		w := newWizardScreen(svc.NewFake(nil), theme.For(theme.Dark), false)
		w.start(c.mode)
		if len(w.steps) != len(c.titulos) {
			t.Fatalf("modo %d: %d pasos, quiero %d", c.mode, len(w.steps), len(c.titulos))
		}
		for i, want := range c.titulos {
			if w.steps[i].title != want {
				t.Fatalf("modo %d: paso %d = %q, quiero %q", c.mode, i, w.steps[i].title, want)
			}
		}
		n := itoa(len(c.titulos))
		out := stripANSI(w.View(106, 25))
		if !strings.Contains(out, "Básico") || !strings.Contains(out, "1/"+n) {
			t.Fatalf("modo %d: el panel del paso debe llevar título y contador 1/%s:\n%s", c.mode, n, out)
		}
		w.step = len(w.steps) - 1
		w.cur = 0
		w.syncInput()
		out = stripANSI(w.View(106, 25))
		if !strings.Contains(out, "REVISAR") || !strings.Contains(out, n+"/"+n) {
			t.Fatalf("modo %d: el paso Revisar debe ir en su panel con %s/%s:\n%s", c.mode, n, n, out)
		}
	}
}

// §5.12: en ancho hay panel AYUDA lateral y el formulario, el selector de
// versiones y el resumen van cada uno en su panel; en estrecho, una sola columna.
func TestAsistentePanelesYAyudaLateral(t *testing.T) {
	w := newWizardScreen(svc.NewFake(nil), theme.For(theme.Dark), false)
	w.start(1)
	w.versions = goldenVersions()
	w.verLoading = false
	w.versionsSeen = true

	ancho := stripANSI(w.View(106, 25))
	for _, want := range []string{"Básico", "1/7", "AYUDA", "Ejemplo:"} {
		if !strings.Contains(ancho, want) {
			t.Fatalf("en ancho debe verse %q:\n%s", want, ancho)
		}
	}
	// Formulario + ayuda suman exactamente el ancho interior (misma altura).
	lines := strings.Split(ancho, "\n")
	for i, ln := range lines[1:] {
		if layout.Width(ln) != 106 {
			t.Fatalf("línea %d mide %d celdas, quiero 106: %q", i+2, layout.Width(ln), ln)
		}
	}

	w.pickingVer = true
	sel := stripANSI(w.View(106, 25))
	if !strings.Contains(sel, "VERSIONES") || !strings.Contains(sel, "[S] mostrar snapshots") {
		t.Fatalf("el selector de versiones debe ir en su panel:\n%s", sel)
	}
	if !strings.Contains(sel, "1.21.4") {
		t.Fatalf("el panel VERSIONES debe listar las versiones:\n%s", sel)
	}

	w.pickingVer = false
	w.step = len(w.steps) - 1
	w.cur = 0
	w.syncInput()
	rev := stripANSI(w.View(106, 25))
	if !strings.Contains(rev, "REVISAR") || !strings.Contains(rev, "[Enter] Crear y arrancar") {
		t.Fatalf("el paso Revisar debe ir en su panel con la tecla de creación:\n%s", rev)
	}

	// En estrecho no hay panel lateral: la ayuda del campo va bajo la etiqueta.
	w.step, w.cur = 0, 0
	w.syncInput()
	estrecho := stripANSI(w.View(66, 12))
	if strings.Contains(estrecho, "AYUDA") {
		t.Fatalf("en estrecho no debe haber panel AYUDA:\n%s", estrecho)
	}
	if !strings.Contains(estrecho, "Nombre visible") {
		t.Fatalf("en estrecho la ayuda corta debe seguir bajo el campo:\n%s", estrecho)
	}
}

// §5.12/§8: un fallo de creación se queda visible en el panel del paso Revisar,
// tanto si lo entrega el App como si llega directo a la pantalla.
func TestAsistenteErrorDeCreacionVisible(t *testing.T) {
	w := newWizardScreen(svc.NewFake(nil), theme.For(theme.Dark), false)
	w.start(0)
	w.step = len(w.steps) - 1
	w.cur = 0
	w.syncInput()
	w.Update(wizardCreatedMsg{err: errors.New("no hay cupo de servidores")})
	if w.step != len(w.steps)-1 {
		t.Fatalf("el error debe quedar en el paso Revisar, tengo %d", w.step)
	}
	out := stripANSI(w.View(106, 25))
	for _, want := range []string{"REVISAR", "no se pudo crear", "no hay cupo de servidores"} {
		if !strings.Contains(out, want) {
			t.Fatalf("el panel debe mostrar %q:\n%s", want, out)
		}
	}

	// Ruta de producción: el App reenvía el error a la pantalla (no solo un toast).
	a := NewApp(svc.NewFake(nil), darkOpts())
	resize(a, 110, 30)
	a.router.Push(newWizardScreen(a.svc, a.pal, a.ascii))
	wz, ok := a.router.Top().(*wizardScreen)
	if !ok {
		t.Fatal("se esperaba el asistente en la cima")
	}
	wz.start(0)
	wz.step = len(wz.steps) - 1
	wz.cur = 0
	wz.syncInput()
	a.Update(wizardCreatedMsg{err: errors.New("sin cupo disponible")})
	if wz.err == "" {
		t.Fatal("el App debe reenviar el error de creación al asistente")
	}
	if got := stripANSI(a.View()); !strings.Contains(got, "sin cupo disponible") {
		t.Fatalf("el error debe verse en la pantalla:\n%s", got)
	}
	if a.router.Top().ID() != "wizard" {
		t.Fatalf("tras fallar la creación el asistente debe seguir abierto, tengo %s", a.router.Top().ID())
	}
}

// §5.12: el resto del teclado del paso se mantiene: Tab avanza, Shift+Tab vuelve,
// ←/→ ciclan el valor, Espacio alterna los booleanos y Esc retrocede o sale.
func TestAsistenteTeclasDelPaso(t *testing.T) {
	w := newWizardScreen(svc.NewFake(nil), theme.For(theme.Dark), false)
	w.start(1)

	// Tab en el último campo del paso avanza al siguiente paso.
	w.cur, w.edit = len(w.steps[0].fields)-1, false
	w.Update(tea.KeyMsg{Type: tea.KeyTab})
	if w.step != 1 {
		t.Fatalf("Tab debe avanzar de paso, tengo %d", w.step)
	}
	// Shift+Tab vuelve al paso anterior.
	w.edit = false
	w.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if w.step != 0 {
		t.Fatalf("Shift+Tab debe volver al paso anterior, tengo %d", w.step)
	}

	// ←/→ ciclan el motor (y su lista de versiones).
	w.cur, w.edit = 1, false
	w.Update(press("right"))
	if w.engine.Name != "fabric" {
		t.Fatalf("→ debe ciclar el motor, tengo %q", w.engine.Name)
	}
	w.Update(press("left"))
	if w.engine.Name != "paper" {
		t.Fatalf("← debe volver al motor anterior, tengo %q", w.engine.Name)
	}

	// Espacio alterna los booleanos del paso de jugadores.
	w.step, w.cur, w.edit = 2, 4, false // campo Lista blanca
	antes := w.steps[2].fields[4].boolVal
	w.Update(press(" "))
	if w.steps[2].fields[4].boolVal == antes {
		t.Fatal("Espacio debe alternar el booleano")
	}

	// Esc retrocede de paso y, en el primero, sale del asistente.
	w.step = 3
	if _, cmd := w.Update(tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatal("Esc en un paso intermedio no debe salir del asistente")
	}
	if w.step != 2 {
		t.Fatalf("Esc debe volver al paso anterior, tengo %d", w.step)
	}
	w.step, w.edit = 0, false
	_, cmd := w.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc en el primer paso debe pedir volver")
	}
	if _, ok := cmd().(popScreenMsg); !ok {
		t.Fatalf("se esperaba popScreenMsg, tengo %T", cmd())
	}
}

// assertWizardFits comprueba que la vista del asistente ocupa exactamente el alto
// del cuerpo y no excede el ancho interior (medido en celdas).
func assertWizardFits(t *testing.T, w *wizardScreen, inner, bodyH int) {
	t.Helper()
	out := w.View(inner, bodyH)
	lines := strings.Split(out, "\n")
	if len(lines) != bodyH {
		t.Fatalf("la vista debe ocupar %d líneas, tengo %d", bodyH, len(lines))
	}
	for i, ln := range lines {
		if layout.Width(ln) > inner {
			t.Fatalf("línea %d mide %d celdas (máximo %d): %q", i+1, layout.Width(ln), inner, stripANSI(ln))
		}
	}
}

// §5.12/UI-05: el asistente no desborda en los tamaños de referencia: alto exacto
// y ninguna línea por encima del ancho, en modo, formulario, selector, revisión y
// ayuda larga, y también dentro del marco de la app.
func TestAsistenteNoDesborda(t *testing.T) {
	// Extremos: ni con una sola celda la vista desborda.
	extremo := newWizardScreen(svc.NewFake(nil), theme.For(theme.Dark), false)
	assertWizardFits(t, extremo, 1, 1)
	extremo.start(1)
	assertWizardFits(t, extremo, 2, 2)

	for _, sz := range [][2]int{{40, 12}, {70, 20}, {80, 24}, {110, 30}, {132, 40}, {200, 50}} {
		w, h := sz[0], sz[1]
		inner, bodyH := w-4, h-5

		// Pantalla de elección de modo.
		modo := newWizardScreen(svc.NewFake(nil), theme.For(theme.Dark), false)
		assertWizardFits(t, modo, inner, bodyH)

		for _, mode := range []int{0, 1} {
			wz := newWizardScreen(svc.NewFake(nil), theme.For(theme.Dark), false)
			wz.start(mode)
			wz.versions = goldenVersions()
			wz.verLoading = false
			wz.versionsSeen = true
			for step := 0; step < len(wz.steps); step++ {
				wz.step, wz.cur = step, 0
				wz.syncInput()
				assertWizardFits(t, wz, inner, bodyH) // formulario o revisión
				wz.pickingVer = true
				assertWizardFits(t, wz, inner, bodyH) // selector de versiones
				wz.pickingVer = false
				wz.helpOpen = true
				assertWizardFits(t, wz, inner, bodyH) // ayuda larga
				wz.helpOpen = false
			}
			// Con un campo en edición el ancho del input sigue al panel.
			wz.step, wz.cur = 0, 0
			wz.syncInput()
			assertWizardFits(t, wz, inner, bodyH)
		}

		// La app completa (marco, estado y barra de atajos) tampoco desborda.
		a := NewApp(svc.NewFake(nil), darkOpts())
		resize(a, w, h)
		a.router.Push(newWizardScreen(a.svc, a.pal, a.ascii))
		wz, ok := a.router.Top().(*wizardScreen)
		if !ok {
			t.Fatal("se esperaba el asistente en la cima")
		}
		wz.start(1)
		wz.versions = goldenVersions()
		wz.verLoading = false
		wz.versionsSeen = true
		for step := 0; step < len(wz.steps); step++ {
			wz.step, wz.cur = step, 0
			wz.syncInput()
			assertWithinSize(t, stripANSI(a.View()), w, h)
		}
	}
}
