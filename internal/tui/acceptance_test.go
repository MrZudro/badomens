package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/svc"
)

// openInstance abre el detalle de una instancia por la ruta de producción
// (App → Router → instanceScreen) y devuelve la pantalla.
func openInstance(t *testing.T, a *App, slug string) *instanceScreen {
	t.Helper()
	a.Update(pollRowsMsg{rows: []svc.Row{{
		Slug: slug, Name: "Survival", Engine: "paper", Version: "1.21.4",
		Port: 25565, State: "running", MaxPlayers: 20,
	}}})
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter debe pedir abrir el detalle")
	}
	a.Update(cmd())
	inst, ok := a.router.Top().(*instanceScreen)
	if !ok {
		t.Fatalf("se esperaba la pantalla de instancia, tengo %s", a.router.Top().ID())
	}
	a.Update(homeDetailMsg{d: &svc.Detail{Spec: goldenSpec(), State: "running", Host: "100.64.0.1"}, slug: slug})
	return inst
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func countStr(list []string, want string) int {
	n := 0
	for _, s := range list {
		if s == want {
			n++
		}
	}
	return n
}

// --- KEY: enrutamiento de teclas y foco ---

// KEY-01: en Jugadores, 'A' abre el prompt y le da el foco, sin operar aún.
func TestKEY01PromptAccesoNoOpera(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	a.Update(press("a"))
	if !inst.prompt || inst.promptAct != "acl.add" {
		t.Fatalf("A debe abrir el prompt de alta; prompt=%v act=%q", inst.prompt, inst.promptAct)
	}
	if !inst.input.Focused() {
		t.Fatal("el input del prompt debe tener el foco")
	}
	if len(inst.access.Whitelist) != 0 {
		t.Fatal("abrir el prompt no debe mutar la lista")
	}
}

// KEY-02: escribir 'N' y 'n' en el prompt los inserta como texto (no abre el
// asistente ni ningún diálogo global).
func TestKEY02NSeEscribeComoTexto(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	a.Update(press("a"))
	a.Update(press("N"))
	a.Update(press("n"))
	if inst.input.Value() != "Nn" {
		t.Fatalf("N y n deben escribirse en el input, tengo %q", inst.input.Value())
	}
	if a.router.Top().ID() != "instance" {
		t.Fatalf("no debe abrirse otra pantalla, tengo %s", a.router.Top().ID())
	}
	if a.router.Depth() != 2 {
		t.Fatalf("no debe apilarse ninguna pantalla, profundidad %d", a.router.Depth())
	}
}

// KEY-03: nombre válido + Enter invoca una operación con read-back y deja el
// jugador visible como estado verificado.
func TestKEY03AltaConReadBack(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	a.Update(press("a"))
	for _, r := range "maria" {
		a.Update(press(string(r)))
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := ejecutaCmd(cmd)
	am, ok := msg.(accessMsg)
	if !ok {
		t.Fatalf("se esperaba accessMsg, tengo %T", msg)
	}
	if am.err != nil {
		t.Fatalf("alta: %v", am.err)
	}
	a.Update(am)
	if !containsStr(inst.access.Whitelist, "maria") {
		t.Fatalf("«maria» debe estar en la lista tras verificar: %v", inst.access.Whitelist)
	}
	if inst.access.Source != svc.AccessLive {
		t.Fatalf("el alta verificada debe ser live, tengo %q", inst.access.Source)
	}
}

// KEY-04: Esc cierra el prompt sin mutar nada.
func TestKEY04EscCancelaPrompt(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	a.Update(press("a"))
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if inst.prompt {
		t.Fatal("Esc debe cerrar el prompt")
	}
	if inst.tab != 2 {
		t.Fatalf("debe volver a la pestaña Jugadores, tengo %d", inst.tab)
	}
	if len(inst.access.Whitelist) != 0 {
		t.Fatal("Esc no debe ejecutar ninguna operación")
	}
}

// KEY-05: crear instancia desde Inicio (n y Ctrl+N) pero no desde el detalle.
func TestKEY05CrearSoloDesdeInicio(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)

	a.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if a.router.Top().ID() != "wizard" {
		t.Fatal("Ctrl+N debe abrir el asistente desde cualquier pantalla")
	}
	_, esc := a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if esc != nil {
		a.Update(esc())
	}
	if a.router.Top().ID() != "home" {
		t.Fatalf("Esc debe volver a Inicio, tengo %s", a.router.Top().ID())
	}

	// Desde el detalle, 'n' no debe abrir el asistente.
	inst := openInstance(t, a, "survival")
	_ = inst
	a.Update(press("n"))
	if a.router.Top().ID() != "instance" {
		t.Fatalf("'n' no debe crear desde el detalle, tengo %s", a.router.Top().ID())
	}
}

// KEY-06: con la consola enfocada, letras imprimibles se escriben y no cambian
// de pantalla ni abren ayuda ni salen.
func TestKEY06ConsolaCapturaLetras(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	a.Update(press("e"))
	if !inst.focus {
		t.Fatal("'e' debe enfocar la consola")
	}
	for _, r := range []string{"N", "A", "q", "?"} {
		a.Update(press(r))
	}
	if inst.input.Value() != "NAq?" {
		t.Fatalf("las letras deben escribirse en la consola, tengo %q", inst.input.Value())
	}
	if a.router.Overlay() != nil {
		t.Fatal("'?' no debe abrir la ayuda mientras se escribe")
	}
	if a.quitting {
		t.Fatal("'q' no debe salir mientras se escribe")
	}
}

// KEY-07: Enter con el campo vacío no envía comando ni muestra éxito.
func TestKEY07ConsolaEnterVacio(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	a.Update(press("e"))
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("Enter con el campo vacío no debe lanzar ningún comando")
	}
	if len(a.toasts) != 0 {
		t.Fatalf("no debe mostrarse confirmación falsa, tengo %v", a.toasts)
	}
}

// KEY-08: escribir un comando y Enter lo envía una vez y muestra confirmación.
func TestKEY08ConsolaEnviaComando(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	a.Update(press("e"))
	for _, r := range "list" {
		a.Update(press(string(r)))
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := ejecutaCmd(cmd)
	if _, ok := msg.(toastMsg); !ok {
		t.Fatalf("se esperaba confirmación del envío, tengo %T", msg)
	}
	if inst.input.Value() != "" {
		t.Fatalf("el campo debe quedar limpio, tengo %q", inst.input.Value())
	}
}

// KEY-09: con la paleta abierta, las letras van a ella y Esc la cierra.
func TestKEY09PaletaCapturaTeclas(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if a.router.Overlay() == nil || a.router.Overlay().ID() != "palette" {
		t.Fatal("Ctrl+P debe abrir la paleta")
	}
	a.Update(press("H")) // no debe abrir Salud
	if a.router.Overlay() == nil || a.router.Overlay().ID() != "palette" {
		t.Fatal("las letras no deben cambiar de pantalla con la paleta abierta")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if a.router.Overlay() != nil {
		t.Fatal("Esc debe cerrar la paleta")
	}
}

// KEY-10: una respuesta tardía de otra instancia no reemplaza el detalle actual.
func TestKEY10RespuestaTardiaDescartada(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	a.Update(pollRowsMsg{rows: []svc.Row{
		{Slug: "a", Name: "A", State: "running"},
		{Slug: "b", Name: "B", State: "running"},
	}})
	hs, ok := a.router.Top().(*homeScreen)
	if !ok {
		t.Fatal("se esperaba home")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyDown}) // selecciona B
	genB := hs.detailGen
	// Respuesta tardía de A con la generación anterior.
	a.Update(homeDetailMsg{d: &svc.Detail{Spec: &instance.Spec{Slug: "a", Name: "A"}, State: "running"}, slug: "a", gen: genB - 1})
	if hs.detail != nil && hs.detail.Spec != nil && hs.detail.Spec.Slug == "a" {
		t.Fatal("no debe aplicarse una respuesta tardía de A")
	}
}

// --- ACL: whitelist / consistencia ---

// ACL-01: estado live con whitelist activada y sus usuarios, con fuente/tiempo.
func TestACL01EstadoLive(t *testing.T) {
	a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	a.Update(ejecutaDirecto(inst.accessCmd()))
	out := inst.playersView()
	for _, want := range []string{"ACTIVADA", "ana", "luis"} {
		if !strings.Contains(out, want) {
			t.Fatalf("la vista de acceso debe contener %q:\n%s", want, out)
		}
	}
	if inst.access.Source != svc.AccessLive {
		t.Fatalf("debe ser estado live, tengo %q", inst.access.Source)
	}
}

// ACL-06: sin lectura en vivo, se distingue la configuración guardada.
func TestACL06EstadoNoVerificable(t *testing.T) {
	s := svc.NewFakeWith([]*instance.Spec{goldenSpec()}, svc.FakeOptions{IdleWatcherActive: true, AccessSavedOnly: true})
	a := NewApp(s, darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	a.Update(ejecutaDirecto(inst.accessCmd()))
	out := inst.playersView()
	if !strings.Contains(out, "no verificable") {
		t.Fatalf("debe indicar que no es verificable:\n%s", out)
	}
	if inst.access.Source != svc.AccessSavedConfig {
		t.Fatalf("debe ser configuración guardada, tengo %q", inst.access.Source)
	}
}

// ACL-07: añadir dos veces el mismo jugador no lo duplica.
func TestACL07AltaIdempotente(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	add := func() {
		a.Update(press("a"))
		for _, r := range "ana" {
			a.Update(press(string(r)))
		}
		_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
		a.Update(ejecutaCmd(cmd))
	}
	add()
	add()
	if got := countStr(inst.access.Whitelist, "ana"); got != 1 {
		t.Fatalf("«ana» debe aparecer una sola vez, tengo %d: %v", got, inst.access.Whitelist)
	}
}

// ACL-08: quitar un jugador lo elimina tras confirmación.
func TestACL08QuitarJugador(t *testing.T) {
	a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	a.Update(ejecutaDirecto(inst.accessCmd()))
	a.Update(press("d"))
	for _, r := range "ana" {
		a.Update(press(string(r)))
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	a.Update(ejecutaCmd(cmd))
	if containsStr(inst.access.Whitelist, "ana") {
		t.Fatalf("«ana» debe desaparecer: %v", inst.access.Whitelist)
	}
}

// ACL-10: un nombre vacío/espacios no emite ningún comando al servidor.
func TestACL10ValidacionLocal(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	a.Update(press("a"))
	a.Update(press(" "))
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("un nombre vacío no debe emitir ningún comando")
	}
	if len(inst.access.Whitelist) != 0 {
		t.Fatal("la lista no debe cambiar con entrada inválida")
	}
}

// --- UI: layout y medición por celdas ---

// UI-04: una línea de log de 500+ caracteres no desborda ni desplaza el footer.
func TestUI04LineaLargaNoDesborda(t *testing.T) {
	a := testApp()
	resize(a, 80, 24)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	inst.logs = strings.Repeat("X", 500)
	out := stripANSI(a.View())
	for _, ln := range strings.Split(out, "\n") {
		if layout.Width(ln) > 80 {
			t.Fatalf("línea de %d celdas excede 80: %q", layout.Width(ln), ln)
		}
	}
	if !strings.Contains(out, "Ayuda") {
		t.Fatal("la barra de atajos debe seguir visible")
	}
}

// UI-03/UI-10: con 2000 líneas de consola, la vista respeta el alto y el footer
// permanece en su sitio.
func TestUI10FooterEstableConMuchosLogs(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	inst.logs = strings.Repeat("línea de log\n", 2000)
	out := stripANSI(a.View())
	lines := strings.Split(out, "\n")
	if len(lines) > 30 {
		t.Fatalf("la vista excede 30 líneas: %d", len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "Ayuda") {
		t.Fatalf("el footer debe ser la última línea: %q", lines[len(lines)-1])
	}
}

// UI-09: Inicio no muestra líneas crudas de consola fuera de su pestaña.
func TestUI09InicioSinLogsCrudos(t *testing.T) {
	a := goldenApp()
	resize(a, 120, 40)
	hs, ok := a.router.Top().(*homeScreen)
	if !ok {
		t.Fatal("se esperaba home")
	}
	hs.logs = "RAWLOG_SENTINEL_123"
	out := stripANSI(a.View())
	if strings.Contains(out, "RAWLOG_SENTINEL_123") {
		t.Fatal("Inicio no debe mostrar logs crudos")
	}
}

// UI-05: el prompt de formulario mantiene visibles etiqueta, valor y teclas.
func TestUI05PromptCompactoVisible(t *testing.T) {
	a := testApp()
	resize(a, 80, 24)
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	a.Update(press("a"))
	a.Update(press("z"))
	out := stripANSI(a.View())
	if !strings.Contains(out, "Nombre del jugador") {
		t.Fatalf("la etiqueta del prompt debe verse:\n%s", out)
	}
	if !strings.Contains(out, "z") {
		t.Fatal("el valor escrito debe verse en el input")
	}
}

// UI-10: la barra de estado y la de acciones ocupan siempre su región fija
// (penúltima y última línea del marco), con banner de Docker, toasts y 2.000
// líneas de consola.
func TestUI10BarraEstadoYAccionesFijas(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	inst.logs = strings.Repeat("línea de log\n", 2000)
	a.Update(pollDockerMsg{diag: dockerx.Diagnosis{State: dockerx.StateStarting}})
	a.pushToast("✓ barra de estado de prueba")
	out := stripANSI(a.View())
	lines := strings.Split(out, "\n")
	if len(lines) != 30 {
		t.Fatalf("la vista debe ocupar exactamente 30 líneas, tengo %d", len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "Ayuda") {
		t.Fatalf("la barra de acciones debe ser la última línea: %q", lines[len(lines)-1])
	}
	if !strings.Contains(lines[len(lines)-2], "barra de estado de prueba") {
		t.Fatalf("la barra de estado debe ser la penúltima línea: %q", lines[len(lines)-2])
	}
}

// TestShellNavSeccionActiva: la navegación marca la sección con corchetes (visible
// sin depender del color) y cambia al abrir el detalle, también en 40×12.
func TestShellNavSeccionActiva(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	a.Update(pollRowsMsg{rows: []svc.Row{{Slug: "survival", Name: "Survival", State: "running"}}})
	if out := stripANSI(a.View()); !strings.Contains(out, "[Inicio]") {
		t.Fatalf("Inicio debe estar marcado en la nav:\n%s", out)
	}
	openInstance(t, a, "survival")
	if out := stripANSI(a.View()); !strings.Contains(out, "[Instancias]") {
		t.Fatalf("Instancias debe estar marcada en el detalle:\n%s", out)
	}
	resize(a, 40, 12)
	if out := stripANSI(a.View()); !strings.Contains(out, "[Instancias]") {
		t.Fatalf("la sección activa debe verse en 40×12:\n%s", out)
	}
}

// assertWithinSize comprueba que una vista no excede ni el ancho ni el alto del
// terminal (medición por celdas visuales).
func assertWithinSize(t *testing.T, out string, w, h int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) > h {
		t.Fatalf("%dx%d: la vista excede el alto (%d líneas)", w, h, len(lines))
	}
	for i, ln := range lines {
		if layout.Width(ln) > w {
			t.Fatalf("%dx%d: línea %d mide %d celdas: %q", w, h, i+1, layout.Width(ln), ln)
		}
	}
}

// UI-01: Inicio vacío y poblado en los tamaños de referencia. Sin desborde, con
// la acción primaria visible en vacío y una selección identificable en poblado.
func TestUI01InicioVacioYPoblado(t *testing.T) {
	for _, sz := range [][2]int{{40, 12}, {70, 20}, {110, 30}, {132, 40}} {
		w, h := sz[0], sz[1]

		empty := NewApp(svc.NewFake(nil), darkOpts())
		resize(empty, w, h)
		empty.Update(homeReqMsg{dockerOK: true, tsIP: "100.64.0.1", tsOK: true})
		empty.Update(pollRowsMsg{rows: nil})
		out := stripANSI(empty.View())
		if !strings.Contains(out, "Crear") {
			t.Fatalf("%dx%d: el Inicio vacío debe ofrecer la acción primaria Crear:\n%s", w, h, out)
		}
		assertWithinSize(t, out, w, h)

		populated := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
		resize(populated, w, h)
		populated.Update(pollRowsMsg{rows: []svc.Row{
			{Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4", Memory: "4G", Port: 25565, State: "running", MaxPlayers: 20, Host: "100.64.0.1"},
			{Slug: "creativo", Name: "Creativo", Engine: "vanilla", Version: "1.21", Memory: "2G", Port: 25566, State: "exited"},
		}})
		populated.Update(homeDetailMsg{d: &svc.Detail{Spec: goldenSpec(), State: "running", Host: "100.64.0.1"}, slug: "survival"})
		out = stripANSI(populated.View())
		if !strings.Contains(out, "Survival") {
			t.Fatalf("%dx%d: el Inicio poblado debe listar Survival:\n%s", w, h, out)
		}
		if !strings.Contains(out, components.Pointer(false)) {
			t.Fatalf("%dx%d: debe verse el puntero de selección:\n%s", w, h, out)
		}
		assertWithinSize(t, out, w, h)
	}
}

// TestUI01VacioCTAConBannerMinimo: en 40×12 con banner de Docker caído (que
// resta una línea al cuerpo) la acción primaria y el banner corto siguen
// visibles y sin desbordar.
func TestUI01VacioCTAConBannerMinimo(t *testing.T) {
	a := NewApp(svc.NewFake(nil), darkOpts())
	resize(a, 40, 12)
	a.Update(pollDockerMsg{diag: dockerx.Diagnosis{State: dockerx.StateDaemonDown}})
	a.Update(homeReqMsg{dockerOK: false, tsOK: false})
	a.Update(pollRowsMsg{rows: nil})
	out := stripANSI(a.View())
	lines := strings.Split(out, "\n")
	if len(lines) != 12 {
		t.Fatalf("la vista debe ocupar 12 líneas, tengo %d", len(lines))
	}
	if !strings.Contains(out, "Crear") {
		t.Fatalf("con banner y 40×12 el CTA debe seguir visible:\n%s", out)
	}
	if !strings.Contains(out, "Ctrl+R") {
		t.Fatalf("el banner corto debe conservar la acción de reparar:\n%s", out)
	}
	assertWithinSize(t, out, 40, 12)
}
