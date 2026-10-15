package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/backup"
	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/idle"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/ports"
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

// frameLeft es la esquina izquierda del separador horizontal del marco.
const frameLeft = "├"

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

// KEY-06: con la consola enfocada (con «/»), letras imprimibles se escriben y no
// cambian de pantalla ni abren ayuda ni salen.
func TestKEY06ConsolaCapturaLetras(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	a.Update(press("/"))
	if !inst.focus {
		t.Fatal("«/» debe enfocar la consola")
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

// KEY-07: con la consola enfocada con «/», Enter con el campo vacío no envía
// comando ni muestra éxito.
func TestKEY07ConsolaEnterVacio(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	a.Update(press("/"))
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
	a.Update(press("/"))
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
	out := inst.playersView(110, 30)
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
	out := inst.playersView(110, 30)
	if !strings.Contains(out, "no verificable") {
		t.Fatalf("debe indicar que no es verificable:\n%s", out)
	}
	if inst.access.Source != svc.AccessSavedConfig {
		t.Fatalf("debe ser configuración guardada, tengo %q", inst.access.Source)
	}
	// Una lectura guardada no es una verificación: no debe afirmarse "verificado".
	if strings.Contains(strings.ToLower(inst.accessStatus), "verificado") {
		t.Fatalf("el estado guardado no debe afirmar verificación, tengo %q", inst.accessStatus)
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

// accessAltaFalla envuelve el servicio de acceso para forzar el fallo del alta
// (ACL-04) sin tocar el fake: la lectura sigue siendo en vivo y el alta devuelve
// el estado previo junto con el error, sin aplicar nada.
type accessAltaFalla struct{ svc.Access }

func (f accessAltaFalla) AddWhitelist(ctx context.Context, slug, player string) (svc.AccessState, error) {
	st, _ := f.Read(ctx, slug)
	return st, errors.New("el servidor rechazó el comando")
}

// ACL-04: si el alta falla, se conserva el estado previo, el jugador no se añade
// de forma optimista y el error queda visible sin afirmar ningún éxito.
func TestACL04AltaFallidaConservaEstado(t *testing.T) {
	s := svc.NewFake([]*instance.Spec{goldenSpec()})
	s.Access = accessAltaFalla{s.Access}
	a := NewApp(s, darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	a.Update(ejecutaDirecto(inst.accessCmd()))
	antes := append([]string(nil), inst.access.Whitelist...)
	if len(antes) == 0 {
		t.Fatal("el escenario necesita un estado previo con jugadores")
	}

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
	if am.err == nil {
		t.Fatal("el escenario exige que el alta falle")
	}
	a.Update(am)

	if containsStr(inst.access.Whitelist, "maria") {
		t.Fatalf("un alta fallida no debe aplicarse de forma optimista: %v", inst.access.Whitelist)
	}
	if strings.Join(inst.access.Whitelist, ",") != strings.Join(antes, ",") {
		t.Fatalf("el estado previo debe conservarse: %v → %v", antes, inst.access.Whitelist)
	}
	out := stripANSI(inst.playersView(110, 30))
	if !strings.Contains(out, "Error") || !strings.Contains(out, "rechazó") {
		t.Fatalf("el error del alta debe verse en el panel de acceso:\n%s", out)
	}
	if strings.Contains(out, "añadido a la lista blanca") {
		t.Fatalf("un alta fallida no debe mostrar el mensaje de éxito:\n%s", out)
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

// TestShellSinFilaDeNavegacion (§15/UX-17): el marco ya no dibuja la fila de
// pestañas de primer nivel y sus regiones fijas (cabecera, separador, estado y
// acciones) siguen en su sitio en cualquier tamaño.
func TestShellSinFilaDeNavegacion(t *testing.T) {
	for _, sz := range [][2]int{{40, 12}, {80, 24}, {110, 30}, {132, 40}} {
		w, h := sz[0], sz[1]
		a := testApp()
		resize(a, w, h)
		a.Update(pollRowsMsg{rows: []svc.Row{{Slug: "survival", Name: "Survival", State: "running"}}})
		out := stripANSI(a.View())
		lines := strings.Split(out, "\n")
		if len(lines) != h {
			t.Fatalf("%dx%d: el marco debe ocupar %d líneas, tengo %d", w, h, h, len(lines))
		}
		if !strings.Contains(lines[0], "badomens") {
			t.Fatalf("%dx%d: la cabecera debe seguir siendo la primera línea: %q", w, h, lines[0])
		}
		if !strings.HasPrefix(strings.TrimSpace(lines[1]), frameLeft) {
			t.Fatalf("%dx%d: tras la cabecera va el separador: %q", w, h, lines[1])
		}
		for _, ln := range lines {
			if strings.Contains(ln, "Inicio") && strings.Contains(ln, "Salud") {
				t.Fatalf("%dx%d: la fila de pestañas superior debe haber desaparecido: %q", w, h, ln)
			}
		}
		if last := lines[h-1]; !strings.Contains(last, "Ayuda") {
			t.Fatalf("%dx%d: la barra de acciones debe seguir en la última línea: %q", w, h, last)
		}
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

// --- Fase H: UI-08 (ANSI, emojis, ZWJ, acentos y CJK) ---

// ui08Logs es un buffer de consola con lo que suele romper la medición por celdas:
// color ANSI (SGR), emoji, familia con ZWJ, acentos y CJK de ancho doble, más una
// línea muy larga que debe envolverse sin desbordar.
//
// Nota de medición (UI-08): runewidth y ansi.StringWidth discrepan en los clusters
// ZWJ —«👨‍👩‍👧» mide 6 celdas con runewidth (2+2+2) y 2 con ansi.StringWidth—.
// Como wrapWidth envuelve con runewidth y el panel recorta y rellena con
// ansi.StringWidth, el resultado es conservador: la línea se envuelve antes y el
// panel rellena el hueco con espacios. Nunca desborda, pero un cluster ZWJ ocupa 2
// celdas en pantalla y 6 en el cálculo de envoltura; alinearlo al ancho real del
// terminal exigiría unificar el medidor, no cambiar la dependencia.
func ui08Logs() string {
	return strings.Join([]string{
		"\x1b[31m[ERROR]\x1b[0m fallo pintado en rojo",
		"🚀 el cohete despega sin desalinear la columna",
		"👨‍👩‍👧 familia con ZWJ en una sola línea",
		"acentos: áéíóú ÁÉÍÓÚ ñÑüÜ ¿qué tal?",
		"你好世界 · CJK de ancho doble",
		"\x1b[1;32m[OK]\x1b[0m verde \x1b[33mamarillo\x1b[0m y \x1b[0mreset",
		strings.Repeat("🚀á", 120),
	}, "\n")
}

// TestUI08ConsolaAnsiEmojiAcentos (UI-08): con ANSI, emoji, ZWJ, acentos y CJK en
// los logs, la consola mide por celdas visuales: ninguna línea desborda el ancho,
// el pie sigue siendo la última línea y no queda ninguna secuencia de escape a la
// vista. Se prueba en 80×24 y 110×30.
func TestUI08ConsolaAnsiEmojiAcentos(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {110, 30}} {
		w, h := sz[0], sz[1]
		a := testApp()
		resize(a, w, h)
		inst := openInstance(t, a, "survival")
		inst.tab = 1
		inst.logs = ui08Logs()
		out := stripANSI(a.View())
		if strings.Contains(out, "\x1b") {
			t.Fatalf("%dx%d: quedaron secuencias ANSI sin eliminar", w, h)
		}
		lines := strings.Split(out, "\n")
		if len(lines) > h {
			t.Fatalf("%dx%d: la vista excede el alto (%d líneas)", w, h, len(lines))
		}
		if last := lines[len(lines)-1]; !strings.Contains(last, "Ayuda") {
			t.Fatalf("%dx%d: el pie debe ser la última línea: %q", w, h, last)
		}
		for i, ln := range lines {
			if got := layout.Width(ln); got > w {
				t.Fatalf("%dx%d: la línea %d mide %d celdas (máx %d): %q", w, h, i+1, got, w, ln)
			}
		}
		// Medición directa del componente: components.Panel mide el cuerpo por
		// celdas (padCells usa layout.Width), así que con ANSI/emoji/CJK el panel
		// mide exactamente el ancho pedido en todas sus líneas.
		panel := components.Panel("CONSOLA", "", stripANSI(ui08Logs()), 40, 6, false, a.pal, a.ascii)
		panelLines := strings.Split(panel, "\n")
		if len(panelLines) != 6 {
			t.Fatalf("%dx%d: el panel debe ocupar 6 líneas, tengo %d", w, h, len(panelLines))
		}
		for i, ln := range panelLines {
			if got := layout.Width(ln); got != 40 {
				t.Fatalf("%dx%d: la línea %d del panel mide %d celdas, quiero 40: %q", w, h, i+1, got, ln)
			}
		}
		// El color no debe borrar el contenido ni el ancho doble hacer desaparecer
		// la línea: el texto legible sigue ahí.
		for _, want := range []string{"fallo pintado en rojo", "🚀", "你好世界", "áéíóú"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%dx%d: la consola debe mostrar %q:\n%s", w, h, want, out)
			}
		}
	}
}

// TestUI08StripANSIEliminaSecuencias (UI-08): stripANSI quita CSI/SGR, OSC y
// escapes simples, y conserva intacto el texto legible (emoji, ZWJ, acentos y CJK).
func TestUI08StripANSIEliminaSecuencias(t *testing.T) {
	in := "\x1b[31mrojo\x1b[0m 🚀 👨‍👩‍👧 áéíóú 你好\x1b]0;título\x07 y \x1b(0\x1b(B fin"
	out := stripANSI(in)
	if strings.Contains(out, "\x1b") {
		t.Fatalf("stripANSI dejó un escape: %q", out)
	}
	if strings.Contains(out, "31m") || strings.Contains(out, "0;título") {
		t.Fatalf("stripANSI dejó restos de las secuencias: %q", out)
	}
	for _, want := range []string{"rojo", "🚀", "👨‍👩‍👧", "áéíóú", "你好", "fin"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stripANSI no debe borrar %q: %q", want, out)
		}
	}
}

// TestUI08NombreConEmojiNoDesborda (UI-08): el nombre de una instancia con emoji,
// ZWJ, acentos y CJK no desborda el Inicio ni en 80×24 ni en 110×30.
func TestUI08NombreConEmojiNoDesborda(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {110, 30}} {
		w, h := sz[0], sz[1]
		a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
		resize(a, w, h)
		a.Update(pollRowsMsg{rows: []svc.Row{
			{Slug: "survival", Name: "🚀 áéíóú 你好 世界", Engine: "paper", Version: "1.21.4", Memory: "4G", Port: 25565, State: "running", MaxPlayers: 20, Host: "100.64.0.1"},
			{Slug: "otra", Name: "👨‍👩‍👧 familia ñÑ", Engine: "vanilla", Version: "1.21", State: "exited"},
		}})
		a.Update(homeDetailMsg{d: &svc.Detail{Spec: goldenSpec(), State: "running", Host: "100.64.0.1"}, slug: "survival"})
		out := stripANSI(a.View())
		if !strings.Contains(out, "🚀") {
			t.Fatalf("%dx%d: el nombre con emoji debe verse en Inicio:\n%s", w, h, out)
		}
		assertWithinSize(t, out, w, h)
	}
}

// --- Fase D: Jugadores y acceso en paneles (§5.5) ---

// ACL-02: la configuración guardada no tapa el estado real; la vista muestra la
// lista del servidor (con «maria») y la etiqueta como estado en vivo.
func TestACL02LiveDifiereDeGuardado(t *testing.T) {
	s := svc.NewFake([]*instance.Spec{goldenSpec()})
	if _, err := s.Access.AddWhitelist(context.Background(), "survival", "maria"); err != nil {
		t.Fatalf("alta en el estado en vivo: %v", err)
	}
	a := NewApp(s, darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	a.Update(ejecutaDirecto(inst.accessCmd()))
	out := inst.playersView(110, 30)
	if inst.access.Source != svc.AccessLive {
		t.Fatalf("el estado debe ser live, tengo %q", inst.access.Source)
	}
	if containsStr(inst.detail.Spec.Whitelist, "maria") {
		t.Fatal("el escenario exige que la configuración guardada NO tenga «maria»")
	}
	if !strings.Contains(out, "maria") {
		t.Fatalf("debe verse la lista en vivo, con «maria»:\n%s", out)
	}
	if strings.Contains(out, "Configuración guardada") {
		t.Fatalf("el estado en vivo no debe presentarse como guardado:\n%s", out)
	}
	if !strings.Contains(out, "verificado") || !strings.Contains(out, "en vivo") {
		t.Fatalf("debe indicarse la fuente y la hora de verificación:\n%s", out)
	}
}

// ACL-03: la lista cambia SOLO al aplicar el read-back, no al enviar el comando.
func TestACL03ListaCambiaTrasReadBack(t *testing.T) {
	a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	a.Update(ejecutaDirecto(inst.accessCmd()))
	if antes := inst.playersView(110, 30); strings.Contains(antes, "maria") {
		t.Fatalf("«maria» no debe aparecer antes de la operación:\n%s", antes)
	}
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
	// El comando ya se envió (el fake mutó su estado), pero la pantalla no lo
	// aplica hasta recibir el mensaje de read-back.
	if out := inst.playersView(110, 30); strings.Contains(out, "maria") {
		t.Fatalf("sin read-back aplicado la lista no debe cambiar:\n%s", out)
	}
	a.Update(am)
	out := inst.playersView(110, 30)
	if !strings.Contains(out, "maria") {
		t.Fatalf("tras aplicar el read-back «maria» debe estar en la lista:\n%s", out)
	}
	if !strings.Contains(out, "Verificado") {
		t.Fatalf("el alta confirmada debe indicarse como verificada:\n%s", out)
	}
}

// ACL-05: si el read-back no puede verificar, se avisa «pendiente de verificar»
// y nunca se afirma que el cambio esté verificado.
func TestACL05EnviadoPendienteDeVerificar(t *testing.T) {
	s := svc.NewFakeWith([]*instance.Spec{goldenSpec()},
		svc.FakeOptions{IdleWatcherActive: true, AccessVerifyFail: true})
	a := NewApp(s, darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	a.Update(ejecutaDirecto(inst.accessCmd()))
	a.Update(press("a"))
	for _, r := range "maria" {
		a.Update(press(string(r)))
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	a.Update(ejecutaCmd(cmd))
	if inst.access.Source != svc.AccessSavedConfig {
		t.Fatalf("sin lectura en vivo la fuente debe ser guardada, tengo %q", inst.access.Source)
	}
	out := inst.playersView(110, 30)
	if !strings.Contains(out, "pendiente de verificar") {
		t.Fatalf("debe avisar de que el resultado está pendiente de verificar:\n%s", out)
	}
	if strings.Contains(out, "Verificado") {
		t.Fatalf("no debe afirmar que el cambio está verificado:\n%s", out)
	}
}

// ACL-09: activar/desactivar la lista blanca se refleja solo tras el read-back y
// no confunde el interruptor con la lista de nombres.
func TestACL09ActivarDesactivarConReadBack(t *testing.T) {
	a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	a.Update(ejecutaDirecto(inst.accessCmd()))
	if !inst.access.WhitelistEnabled {
		t.Fatal("el estado inicial debe tener la lista blanca activada")
	}
	antes := append([]string(nil), inst.access.Whitelist...)
	_, cmd := a.Update(press("w"))
	if cmd == nil {
		t.Fatal("W debe pedir el cambio de estado con read-back")
	}
	msg := ejecutaCmd(cmd)
	am, ok := msg.(accessMsg)
	if !ok {
		t.Fatalf("se esperaba accessMsg, tengo %T", msg)
	}
	if !inst.access.WhitelistEnabled {
		t.Fatal("el estado no debe cambiar antes de aplicar el read-back")
	}
	a.Update(am)
	if inst.access.WhitelistEnabled {
		t.Fatal("tras el read-back la lista blanca debe quedar desactivada")
	}
	if inst.access.Source != svc.AccessLive {
		t.Fatalf("el cambio confirmado debe ser live, tengo %q", inst.access.Source)
	}
	if strings.Join(inst.access.Whitelist, ",") != strings.Join(antes, ",") {
		t.Fatalf("la lista de nombres no debe cambiar: %v → %v", antes, inst.access.Whitelist)
	}
	out := inst.playersView(110, 30)
	if !strings.Contains(out, "DESACTIVADA") || !strings.Contains(out, "Verificado") {
		t.Fatalf("la vista debe mostrar el estado desactivado y verificado:\n%s", out)
	}
}

// --- ACL/UI: layout en paneles de la pestaña Jugadores ---

// TestUI06AccesoEstados: cargando, error y éxito se distinguen por texto y
// etiqueta (no solo por color), y ninguna línea desborda en los tamaños de
// referencia.
func TestUI06AccesoEstados(t *testing.T) {
	cargandoInst := func() *instanceScreen {
		a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
		resize(a, 120, 40)
		inst := openInstance(t, a, "survival")
		inst.tab = 2
		return inst
	}

	// Cargando: todavía sin respuesta del servicio de acceso.
	cargando := cargandoInst().playersView(110, 30)
	if !strings.Contains(cargando, "verificando estado de acceso") || !strings.Contains(cargando, "NO VERIFICADA") {
		t.Fatalf("el estado de carga debe estar etiquetado:\n%s", cargando)
	}

	// Error de consulta.
	errSvc := svc.NewFakeWith([]*instance.Spec{goldenSpec()},
		svc.FakeOptions{IdleWatcherActive: true, AccessReadErr: errors.New("docker no responde")})
	errApp := NewApp(errSvc, darkOpts())
	resize(errApp, 120, 40)
	errInst := openInstance(t, errApp, "survival")
	errInst.tab = 2
	errApp.Update(ejecutaDirecto(errInst.accessCmd()))
	fallo := errInst.playersView(110, 30)
	if !strings.Contains(fallo, "ERROR DE CONSULTA") {
		t.Fatalf("la consulta fallida debe etiquetarse:\n%s", fallo)
	}

	// Éxito: estado en vivo verificado.
	okApp := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
	resize(okApp, 120, 40)
	okInst := openInstance(t, okApp, "survival")
	okInst.tab = 2
	okApp.Update(ejecutaDirecto(okInst.accessCmd()))
	vivo := okInst.playersView(110, 30)
	if !strings.Contains(vivo, "ACTIVADA") || !strings.Contains(vivo, "en vivo") {
		t.Fatalf("el éxito debe indicar estado y fuente:\n%s", vivo)
	}
	if strings.Contains(vivo, "ERROR DE CONSULTA") || strings.Contains(vivo, "NO VERIFICADA") {
		t.Fatalf("el éxito no debe mostrar etiquetas de error ni de estado desconocido:\n%s", vivo)
	}
	if cargando == fallo || cargando == vivo || fallo == vivo {
		t.Fatal("cargando, error y éxito deben ser distinguibles entre sí")
	}

	// Sin desborde en los tamaños de referencia, también con prompt abierto.
	for _, sz := range [][2]int{{40, 12}, {80, 24}, {100, 30}, {132, 40}} {
		resize(okApp, sz[0], sz[1])
		assertWithinSize(t, stripANSI(okApp.View()), sz[0], sz[1])
	}
	resize(okApp, 80, 24)
	okApp.Update(press("a"))
	assertWithinSize(t, stripANSI(okApp.View()), 80, 24)
	out := stripANSI(okApp.View())
	if !strings.Contains(out, "Nombre del jugador") {
		t.Fatalf("el prompt debe seguir visible con el panel de acceso:\n%s", out)
	}
}

// TestPlayersPanelesPorAncho: en medio/ancho hay dos paneles cuyos anchos suman
// el ancho interior y en compacto una sola columna, sin desbordar.
func TestPlayersPanelesPorAncho(t *testing.T) {
	a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	a.Update(ejecutaDirecto(inst.accessCmd()))

	normal := inst.playersView(106, 20)
	lines := strings.Split(stripANSI(normal), "\n")
	if len(lines) != 20 {
		t.Fatalf("el panel doble debe ocupar 20 líneas, tengo %d", len(lines))
	}
	for i, ln := range lines {
		if layout.Width(ln) != 106 {
			t.Fatalf("línea %d mide %d celdas, quiero 106: %q", i+1, layout.Width(ln), ln)
		}
	}
	if !strings.Contains(normal, "ACCESO") || !strings.Contains(normal, "OPERADORES") {
		t.Fatalf("en ancho medio deben verse los dos paneles:\n%s", normal)
	}

	compacto := stripANSI(inst.playersView(60, 20))
	if strings.Contains(compacto, "OPERADORES") {
		t.Fatalf("en compacto hay una sola columna (sin panel OPERADORES):\n%s", compacto)
	}
	if !strings.Contains(compacto, "Operadores") {
		t.Fatalf("en compacto los operadores van dentro del panel ACCESO:\n%s", compacto)
	}
	for _, ln := range strings.Split(compacto, "\n") {
		if layout.Width(ln) > 60 {
			t.Fatalf("compacto desborda 60 celdas: %q", ln)
		}
	}
}

// --- Fase E: Resumen, Backups y Ajustes en paneles (§5.3/§5.6/§5.11) ---

// UI-02: las nueve pestañas del detalle en los tamaños de referencia. Cada vista
// ocupa exactamente el alto del terminal, no desborda el ancho, marca la pestaña
// activa (completa, sin truncarla a ciegas) y en medio/ancho deja reconocibles las
// demás por texto; ninguna pestaña revienta.
func TestUI02NuevePestanasDetalle(t *testing.T) {
	for _, sz := range [][2]int{{40, 12}, {80, 24}, {110, 30}, {132, 40}} {
		w, h := sz[0], sz[1]
		inner := w - 4
		for tab := 0; tab < len(instanceTabs); tab++ {
			a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
			resize(a, w, h)
			inst := openInstance(t, a, "survival")
			inst.tab = tab
			out := stripANSI(a.View())
			lines := strings.Split(out, "\n")
			if len(lines) != h {
				t.Fatalf("%dx%d pestaña %q: la vista debe ocupar %d líneas, tengo %d", w, h, instanceTabs[tab], h, len(lines))
			}
			for i, ln := range lines {
				if layout.Width(ln) > w {
					t.Fatalf("%dx%d pestaña %q: la línea %d mide %d celdas: %q", w, h, instanceTabs[tab], i+1, layout.Width(ln), ln)
				}
			}
			chrome := stripANSI(inst.Chrome(inner))
			if !strings.Contains(chrome, "["+instanceTabs[tab]+"]") {
				t.Fatalf("%dx%d: la pestaña activa %q debe verse marcada y completa:\n%s", w, h, instanceTabs[tab], chrome)
			}
			if layout.ClassifyWidth(inner) == layout.Compact {
				if !strings.Contains(chrome, itoa(tab+1)+"/"+itoa(len(instanceTabs))) {
					t.Fatalf("%dx%d: en compacto debe verse el indicador %d/%d:\n%s", w, h, tab+1, len(instanceTabs), chrome)
				}
				continue
			}
			others := 0
			for i, name := range instanceTabs {
				if i != tab && strings.Contains(chrome, name) {
					others++
				}
			}
			if others == 0 {
				t.Fatalf("%dx%d: con la pestaña %q activa no queda ninguna otra reconocible por texto:\n%s", w, h, instanceTabs[tab], chrome)
			}
		}
	}
}

// §5.3: el Resumen usa dos paneles contiguos (RESUMEN y CÓMO CONECTARSE) cuyos
// anchos suman el ancho interior, con los datos en dos columnas etiqueta/valor; en
// compacto, un solo panel a ancho completo con las líneas de conexión debajo. Sin
// desbordes en los tamaños de referencia.
func TestResumenPanelesYColumnas(t *testing.T) {
	a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 0
	inst.playersOnline, inst.playersKnown = 3, true
	inst.memHist = []float64{100, 200, 300, 400}
	inst.memUsedMB, inst.memLimitMB = 512, 4096
	inst.idleCfg = idle.Config{Enabled: true, Timeout: 15 * time.Minute}
	inst.idleLoaded = true

	ancho := stripANSI(inst.summaryView(106, 24))
	lines := strings.Split(ancho, "\n")
	if len(lines) != 24 {
		t.Fatalf("el Resumen en ancho debe ocupar 24 líneas, tengo %d", len(lines))
	}
	for i, ln := range lines {
		if layout.Width(ln) != 106 {
			t.Fatalf("línea %d mide %d celdas, quiero 106: %q", i+1, layout.Width(ln), ln)
		}
	}
	for _, want := range []string{
		"RESUMEN", "CÓMO CONECTARSE", "Motor", "Memoria", "Puerto", "Jugadores",
		"Dificultad", "Estado", "Auto-apagado", "CPU/RAM",
	} {
		if !strings.Contains(ancho, want) {
			t.Fatalf("el Resumen en ancho debe contener %q:\n%s", want, ancho)
		}
	}
	for _, want := range []string{"paper 1.21.4", "3/20", "100.64.0.1:25565", "15 min"} {
		if !strings.Contains(ancho, want) {
			t.Fatalf("el Resumen debe mostrar %q:\n%s", want, ancho)
		}
	}
	if strings.Contains(ancho, "Cómo conectarse") {
		t.Fatalf("el encabezado de conexión no debe repetirse dentro del panel:\n%s", ancho)
	}

	compacto := stripANSI(inst.summaryView(64, 14))
	cl := strings.Split(compacto, "\n")
	if len(cl) > 14 {
		t.Fatalf("el Resumen compacto excede 14 líneas: %d", len(cl))
	}
	if !strings.Contains(compacto, "RESUMEN") || strings.Contains(compacto, "CÓMO CONECTARSE") {
		t.Fatalf("en compacto debe haber un solo panel RESUMEN:\n%s", compacto)
	}
	if !strings.Contains(compacto, "Dirección:") {
		t.Fatalf("en compacto deben verse las líneas de conexión debajo del panel:\n%s", compacto)
	}
	for i, ln := range cl {
		if layout.Width(ln) > 64 {
			t.Fatalf("compacto: la línea %d mide %d celdas: %q", i+1, layout.Width(ln), ln)
		}
	}

	for _, sz := range [][2]int{{40, 12}, {80, 24}, {110, 30}, {132, 40}} {
		resize(a, sz[0], sz[1])
		assertWithinSize(t, stripANSI(a.View()), sz[0], sz[1])
	}
}

// §5.3/P-08: la caída detectada se avisa en el Resumen con su causa probable y,
// cuando hubo bucle (tres caídas en diez minutos), con el aviso correspondiente.
func TestResumenCaidaDetectada(t *testing.T) {
	a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 0
	inst.detail.State = "exited"
	inst.crash = true
	inst.crashCause = "falta memoria (OutOfMemoryError): sube la RAM"

	ancho := stripANSI(inst.summaryView(106, 24))
	for _, want := range []string{"se cayó inesperadamente", "Causa probable", "falta memoria"} {
		if !strings.Contains(ancho, want) {
			t.Fatalf("el aviso de caída debe incluir %q:\n%s", want, ancho)
		}
	}
	if strings.Contains(ancho, "3 veces en 10 min") {
		t.Fatalf("sin bucle de caídas no debe aparecer el aviso de 3 caídas:\n%s", ancho)
	}
	inst.crashLoop = true
	ancho = stripANSI(inst.summaryView(106, 24))
	if !strings.Contains(ancho, "3 veces en 10 min") {
		t.Fatalf("con bucle debe avisarse de 3 caídas en 10 min:\n%s", ancho)
	}
	if n := len(strings.Split(ancho, "\n")); n != 24 {
		t.Fatalf("el panel con aviso debe ocupar 24 líneas, tengo %d", n)
	}

	for _, w := range []int{64, 36} {
		out := stripANSI(inst.summaryView(w, 12))
		if !strings.Contains(out, "se cayó") {
			t.Fatalf("compacto %d: falta el aviso de caída:\n%s", w, out)
		}
		for i, ln := range strings.Split(out, "\n") {
			if layout.Width(ln) > w {
				t.Fatalf("compacto %d: la línea %d desborda: %q", w, i+1, ln)
			}
		}
	}
}

// §5.6: la pestaña Backups dentro de un panel distingue cargando, vacío, lista y
// operación en curso, y muestra nombre, tamaño y checksum corto con puntero en la
// fila seleccionada.
func TestBackupsEstadosYFilas(t *testing.T) {
	a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 3

	cargando := stripANSI(inst.backupsView(106, 24))
	if !strings.Contains(cargando, "BACKUPS") || !strings.Contains(cargando, "cargando…") {
		t.Fatalf("el estado de carga debe estar etiquetado:\n%s", cargando)
	}

	a.Update(instanceBackupsMsg{items: nil})
	if !inst.backupsLoaded {
		t.Fatal("la respuesta de la lista debe marcar backupsLoaded")
	}
	vacio := stripANSI(inst.backupsView(106, 24))
	if !strings.Contains(vacio, "(sin backups todavía)") || !strings.Contains(vacio, "[B] Crear ahora") {
		t.Fatalf("el estado vacío debe decir qué falta y con qué tecla se resuelve:\n%s", vacio)
	}

	a.Update(instanceBackupsMsg{items: []backup.Info{
		{Name: "20260114T101500", Size: 2 << 20, Sha256: strings.Repeat("ab", 32)},
		{Name: "20260113T220000", Size: 1 << 20, Sha256: strings.Repeat("cd", 32)},
	}})
	lista := stripANSI(inst.backupsView(106, 24))
	for _, want := range []string{
		"20260114T101500", "2.0 MB", strings.Repeat("ab", 6), "20260113T220000",
		"[B] Crear backup ahora", "[X] Restaurar", "[Shift+X] Podar",
	} {
		if !strings.Contains(lista, want) {
			t.Fatalf("la lista debe mostrar %q:\n%s", want, lista)
		}
	}
	if !strings.Contains(lista, components.Pointer(false)) {
		t.Fatalf("la fila seleccionada debe llevar puntero:\n%s", lista)
	}
	if n := len(strings.Split(lista, "\n")); n != 24 {
		t.Fatalf("el panel de backups debe ocupar 24 líneas, tengo %d", n)
	}

	a.Update(instanceBackupsMsg{err: errors.New("docker no responde")})
	errVista := stripANSI(inst.backupsView(106, 24))
	if !strings.Contains(errVista, "no se pudieron leer los backups") {
		t.Fatalf("el error de lectura debe explicarse en el panel:\n%s", errVista)
	}
	a.Update(instanceBackupsMsg{items: inst.backups})

	_, cmd := a.Update(press("b"))
	if cmd == nil {
		t.Fatal("B debe lanzar la creación del backup")
	}
	if inst.bkBusy == "" {
		t.Fatal("crear un backup debe marcar la operación en curso")
	}
	enCurso := stripANSI(inst.backupsView(106, 24))
	if !strings.Contains(enCurso, "creando backup") {
		t.Fatalf("la operación en curso debe verse con texto claro:\n%s", enCurso)
	}
	a.Update(ejecutaCmd(cmd))
	if inst.bkBusy != "" {
		t.Fatalf("el resultado debe limpiar el estado ocupado, tengo %q", inst.bkBusy)
	}
	assertWithinSize(t, stripANSI(a.View()), 120, 40)
}

// §5.11/WS-3: el campo enfocado se abre con Enter (selector para la dificultad), se
// elige con ↓ y se confirma con Enter, que persiste el cambio y anuncia el estado
// (guardando → guardado) sin mutar el Spec antes de guardar.
func TestAjustesCicloYGuardado(t *testing.T) {
	a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 8
	inst.setCursor = 1 // Dificultad: campo con selector
	antes := inst.detail.Spec.Difficulty

	// Enter abre el selector del campo enfocado (todavía no guarda nada).
	if _, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("abrir el selector no debe pedir ningún guardado")
	}
	if !inst.selOpen() || inst.selKey() != "difficulty" {
		t.Fatalf("Enter debe abrir el selector de dificultad; selecting=%v key=%q", inst.selOpen(), inst.selKey())
	}
	if inst.selCursor() != 2 { // «normal» es la tercera opción
		t.Fatalf("el selector debe arrancar en el valor actual (normal), tengo %d", inst.selCursor())
	}
	vistaSelector := stripANSI(inst.settingsView(106, 24))
	for _, want := range []string{"Pacífico", "Fácil", "Normal", "Difícil", "elegir", "Enter: guardar", "(actual)"} {
		if !strings.Contains(vistaSelector, want) {
			t.Fatalf("el selector debe mostrar %q:\n%s", want, vistaSelector)
		}
	}
	if !strings.Contains(vistaSelector, components.Pointer(false)) {
		t.Fatalf("la opción señalada debe llevar puntero:\n%s", vistaSelector)
	}

	// ↓ mueve la selección y Enter aplica la opción y pide el guardado.
	a.Update(tea.KeyMsg{Type: tea.KeyDown})
	if inst.selCursor() != 3 {
		t.Fatalf("↓ debe mover la selección a la opción siguiente, tengo %d", inst.selCursor())
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("confirmar el selector debe pedir el guardado")
	}
	if inst.selOpen() {
		t.Fatal("confirmar debe cerrar el selector")
	}
	// El Spec persistido no se muta antes de que el guardado se confirme (§5.11).
	if inst.detail.Spec.Difficulty != antes {
		t.Fatalf("el Spec no debe mutarse antes de persistir, tengo %q", inst.detail.Spec.Difficulty)
	}
	if inst.pendingSave == nil {
		t.Fatal("el cambio debe quedar preparado en pendingSave")
	}
	nuevo := inst.pendingSave.Difficulty
	if nuevo == antes {
		t.Fatalf("el selector debe cambiar el valor preparado (sigue en %q)", antes)
	}
	if inst.status != "guardando…" {
		t.Fatalf("el estado debe indicar el guardado, tengo %q", inst.status)
	}
	vista := stripANSI(inst.settingsView(106, 24))
	if !strings.Contains(vista, "guardando…") {
		t.Fatalf("el panel debe mostrar el estado de guardado:\n%s", vista)
	}
	if !strings.Contains(vista, "Requiere reiniciar") {
		t.Fatalf("el aviso de reinicio debe seguir visible:\n%s", vista)
	}
	if !strings.Contains(vista, "Dificultad: Difícil") {
		t.Fatalf("el panel debe mostrar el valor preparado:\n%s", vista)
	}

	msg := ejecutaCmd(cmd)
	sm, ok := msg.(instanceSavedMsg)
	if !ok {
		t.Fatalf("se esperaba instanceSavedMsg, tengo %T", msg)
	}
	if sm.err != nil {
		t.Fatalf("guardar: %v", sm.err)
	}
	a.Update(sm)
	if inst.status == "guardando…" {
		t.Fatal("el resultado debe cerrar el estado de guardado")
	}
	if inst.pendingSave != nil {
		t.Fatal("el guardado confirmado no debe dejar cambios pendientes")
	}
	if inst.detail.Spec.Difficulty != nuevo {
		t.Fatalf("tras confirmar debe adoptarse el Spec guardado, tengo %q", inst.detail.Spec.Difficulty)
	}
	guardado := stripANSI(inst.settingsView(106, 24))
	if !strings.Contains(guardado, "guardado") {
		t.Fatalf("tras guardar debe verse el estado guardado:\n%s", guardado)
	}
	d, err := a.svc.Instances.Detail(context.Background(), "survival")
	if err != nil {
		t.Fatalf("detalle tras guardar: %v", err)
	}
	if d.Spec.Difficulty != nuevo {
		t.Fatalf("la dificultad persistida debe ser %q, tengo %q", nuevo, d.Spec.Difficulty)
	}

	// Esc cierra el selector sin guardar ni mutar nada.
	inst.setCursor = 1
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	a.Update(tea.KeyMsg{Type: tea.KeyUp})
	if _, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatal("Esc no debe guardar nada")
	}
	if inst.selOpen() || inst.pendingSave != nil {
		t.Fatal("Esc debe cerrar el selector sin dejar cambios preparados")
	}
	if inst.detail.Spec.Difficulty != nuevo {
		t.Fatalf("Esc no debe mutar el Spec, tengo %q", inst.detail.Spec.Difficulty)
	}

	// Un fallo de guardado se queda visible en el panel.
	a.Update(instanceSavedMsg{err: errors.New("disco lleno")})
	if out := stripANSI(inst.settingsView(106, 24)); !strings.Contains(out, "no se pudo guardar") {
		t.Fatalf("el error de guardado debe verse en el panel:\n%s", out)
	}
}

// WS-3/1.2.1: dentro de una instancia ←/→ cambian de pestaña (también en
// Ajustes), pero NUNCA actúan como Enter: no cambian el valor del campo, no
// guardan y no abren la edición ni el selector. Tampoco mueven la selección de un
// selector abierto.
func TestAjustesFlechasNoMutanNiGuardan(t *testing.T) {
	a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 8
	inst.setCursor = 1 // Dificultad
	antes := inst.detail.Spec.Difficulty

	// → cambia de pestaña (sale de Ajustes) sin pedir ningún guardado.
	if _, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRight}); cmd != nil {
		t.Fatal("←/→ no deben pedir ningún guardado")
	}
	if inst.tab == 8 {
		t.Fatal("→ debe cambiar de pestaña dentro de la instancia")
	}
	// ← vuelve a Ajustes, tampoco guarda nada.
	a.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if inst.tab != 8 {
		t.Fatalf("← debe volver a Ajustes, tengo %d", inst.tab)
	}
	if inst.pendingSave != nil {
		t.Fatal("←/→ no deben preparar ningún cambio")
	}
	if inst.status == "guardando…" {
		t.Fatal("←/→ no deben anunciar un guardado")
	}
	if inst.editing || inst.selOpen() {
		t.Fatal("←/→ no deben abrir la edición ni el selector")
	}
	if inst.detail.Spec.Difficulty != antes {
		t.Fatalf("←/→ no deben mutar la dificultad, tengo %q", inst.detail.Spec.Difficulty)
	}
	d, err := a.svc.Instances.Detail(context.Background(), "survival")
	if err != nil {
		t.Fatalf("detalle: %v", err)
	}
	if d.Spec.Difficulty != antes {
		t.Fatalf("←/→ no deben persistir nada, tengo %q", d.Spec.Difficulty)
	}

	// Con el selector abierto tampoco mueven la selección ni lo cierran.
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !inst.selOpen() {
		t.Fatal("Enter debe abrir el selector")
	}
	sel := inst.selCursor()
	a.Update(tea.KeyMsg{Type: tea.KeyRight})
	a.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if inst.selCursor() != sel {
		t.Fatalf("←/→ no deben mover la selección (%d → %d)", sel, inst.selCursor())
	}
	if !inst.selOpen() {
		t.Fatal("←/→ no deben cerrar el selector")
	}
}

// §4.2: en modo ASCII el Resumen no dibuja bloques Unicode (la minigráfica de
// CPU/RAM se sustituye por las cifras), para que los terminales de respaldo y las
// capturas ASCII sigan siendo legibles.
func TestResumenAsciiSinBloquesUnicode(t *testing.T) {
	opts := darkOpts()
	opts.ASCII = true
	a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), opts)
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 0
	inst.memHist = []float64{100, 200, 300, 400}
	inst.memUsedMB, inst.memLimitMB = 512, 4096

	out := inst.summaryView(106, 24)
	for _, r := range out {
		if (r >= 0x2500 && r <= 0x257F) || (r >= 0x2580 && r <= 0x259F) {
			t.Fatalf("el Resumen en ASCII no debe dibujar el carácter Unicode %q:\n%s", r, stripANSI(out))
		}
	}
	if plano := stripANSI(out); !strings.Contains(plano, "512/4096") {
		t.Fatalf("en ASCII deben verse las cifras de CPU/RAM:\n%s", plano)
	}
}

// --- Fase E: Mundos, Plugins/Mods, Red y Auto-apagado (§5.7–§5.10) ---

// §5.7: la pestaña Mundos muestra la semilla del Spec, el aviso de pérdida de
// datos al regenerar y la biblioteca de mundos (que llega por mensaje, nunca con
// E/S en View). La TUI ya no delega en el CLI: [G] regenera el mundo y [L] carga
// un .zip. Sin desbordes en los tamaños de referencia.
func TestMundosSemillaEInstrucciones(t *testing.T) {
	a := NewApp(svc.NewFakeWith([]*instance.Spec{goldenSpec()}, svc.FakeOptions{WorldLibrary: []string{"arena", "ciudad"}}), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 4

	inst.detail = nil
	cargando := stripANSI(inst.worldsView(106, 24))
	if !strings.Contains(cargando, "MUNDO") || !strings.Contains(cargando, "cargando…") {
		t.Fatalf("el estado de carga debe estar etiquetado:\n%s", cargando)
	}

	inst.detail = &svc.Detail{Spec: goldenSpec(), State: "running", Host: "100.64.0.1"}
	// La biblioteca se lee con un comando (nunca en View): se inyecta su respuesta.
	inst.Update(worldsLibMsg{names: []string{"arena", "ciudad"}})
	conSemilla := stripANSI(inst.worldsView(106, 24))
	for _, want := range []string{
		"MUNDO", "Semilla: 42", "Biblioteca de mundos", "arena", "ciudad",
		"se pierden Overworld, Nether y End", "[G] Regenerar", "[L] Cargar",
	} {
		if !strings.Contains(conSemilla, want) {
			t.Fatalf("la vista de mundos debe contener %q:\n%s", want, conSemilla)
		}
	}
	for _, viejo := range []string{"badomens world list", "world save", "world use", "world export", "world import"} {
		if strings.Contains(conSemilla, viejo) {
			t.Fatalf("la vista ya no debe delegar en el CLI (%q):\n%s", viejo, conSemilla)
		}
	}
	if n := len(strings.Split(conSemilla, "\n")); n != 24 {
		t.Fatalf("el panel de mundos debe ocupar 24 líneas, tengo %d", n)
	}

	// Estado vacío: sin semilla se explica que el mundo se genera aleatorio.
	inst.detail.Spec.Seed = ""
	vacio := stripANSI(inst.worldsView(106, 24))
	if !strings.Contains(vacio, "sin semilla") {
		t.Fatalf("sin semilla debe verse el aviso:\n%s", vacio)
	}
	if !strings.Contains(vacio, "Semilla: —") {
		t.Fatalf("la semilla vacía debe mostrarse como —:\n%s", vacio)
	}

	inst.detail.Spec.Seed = "42"
	for _, sz := range [][2]int{{40, 12}, {80, 24}, {110, 30}, {132, 40}} {
		resize(a, sz[0], sz[1])
		inst.tab = 4
		assertWithinSize(t, stripANSI(a.View()), sz[0], sz[1])
	}
}

// §5.8: la pestaña Plugins/Mods lista spec.Plugins con su contador; vacía dice qué
// falta y con qué comando se resuelve; sin desbordes en los tamaños de referencia.
func TestPluginsVacioYLista(t *testing.T) {
	a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 5

	lista := stripANSI(inst.pluginsView(106, 24))
	for _, want := range []string{"PLUGINS/MODS", "LuckPerms", "1 instalado", "plugin list", "plugin add", "plugin remove"} {
		if !strings.Contains(lista, want) {
			t.Fatalf("la lista de plugins debe contener %q:\n%s", want, lista)
		}
	}
	if n := len(strings.Split(lista, "\n")); n != 24 {
		t.Fatalf("el panel de plugins debe ocupar 24 líneas, tengo %d", n)
	}

	inst.detail.Spec.Plugins = nil
	vacio := stripANSI(inst.pluginsView(106, 24))
	if !strings.Contains(vacio, "(ninguno instalado)") || !strings.Contains(vacio, "plugin add") {
		t.Fatalf("el estado vacío debe explicar qué falta y cómo añadir:\n%s", vacio)
	}
	if strings.Contains(vacio, "1 instalado") {
		t.Fatalf("sin plugins no debe mostrarse contador:\n%s", vacio)
	}

	inst.detail.Spec.Plugins = []string{"LuckPerms", "EssentialsX"}
	for _, sz := range [][2]int{{40, 12}, {80, 24}, {110, 30}} {
		resize(a, sz[0], sz[1])
		inst.tab = 5
		assertWithinSize(t, stripANSI(a.View()), sz[0], sz[1])
	}
}

// §5.9: la pestaña Red en medio/ancho reparte dos paneles (CONEXIÓN y PUERTO) y
// en compacto apila una columna. Distingue los cuatro estados del puerto
// (comprobando, libre, ocupado con conflictos/sugerido y error) y expone [R]/[C].
func TestRedEstadosPuerto(t *testing.T) {
	s := svc.NewFakeWith([]*instance.Spec{goldenSpec()}, svc.FakeOptions{
		PortResult: &ports.Result{Port: 25565, Free: true},
	})
	a := NewApp(s, darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 6

	// Sin resultado todavía: comprobando. El panel CONEXIÓN usa ConnectLines.
	inst.portRes = nil
	inst.portErr = ""
	comprobando := stripANSI(inst.netView(106, 24))
	if !strings.Contains(comprobando, "CONEXIÓN") || !strings.Contains(comprobando, "PUERTO") {
		t.Fatalf("en ancho deben verse los dos paneles:\n%s", comprobando)
	}
	if !strings.Contains(comprobando, "Comprobando el puerto") {
		t.Fatalf("sin resultado debe indicarse que se está comprobando:\n%s", comprobando)
	}
	if !strings.Contains(comprobando, "100.64.0.1:25565") {
		t.Fatalf("el panel de conexión debe mostrar la dirección:\n%s", comprobando)
	}
	for i, ln := range strings.Split(comprobando, "\n") {
		if layout.Width(ln) != 106 {
			t.Fatalf("línea %d mide %d celdas, quiero 106: %q", i+1, layout.Width(ln), ln)
		}
	}

	// Libre: ✓ y "libre", con el resultado que llega por instancePortMsg.
	a.Update(instancePortMsg{res: ports.Result{Port: 25565, Free: true}})
	outLibre := stripANSI(inst.netView(106, 24))
	if !strings.Contains(outLibre, "✓ Puerto 25565") || !strings.Contains(outLibre, "libre") {
		t.Fatalf("el puerto libre debe mostrarse con ✓:\n%s", outLibre)
	}

	// Ocupado: ✖, conflictos y puerto sugerido.
	inst.portRes = &ports.Result{
		Port: 25565, Free: false,
		Conflicts: []ports.Conflict{{
			Port: 25565, Severity: ports.SeverityBlock,
			Owner: ports.Owner{Kind: ports.OwnerHostProcess, Detail: "java (pid 4321)"},
		}},
		Suggested: 25566,
	}
	outOcupado := stripANSI(inst.netView(106, 24))
	if !strings.Contains(outOcupado, "✖ Puerto 25565") {
		t.Fatalf("el puerto ocupado debe mostrarse con ✖:\n%s", outOcupado)
	}
	if !strings.Contains(outOcupado, "java (pid 4321)") {
		t.Fatalf("debe listarse el conflicto:\n%s", outOcupado)
	}
	if !strings.Contains(outOcupado, "Puerto sugerido: 25566") {
		t.Fatalf("debe sugerirse un puerto libre:\n%s", outOcupado)
	}

	// Error de comprobación.
	inst.portRes = nil
	inst.portErr = "docker no responde"
	outErr := stripANSI(inst.netView(106, 24))
	if !strings.Contains(outErr, "No se pudo comprobar el puerto") || !strings.Contains(outErr, "docker no responde") {
		t.Fatalf("el error de comprobación debe verse:\n%s", outErr)
	}
	inst.portErr = ""

	// La pista de atajos y las teclas [R]/[C] de la pestaña Red.
	if !strings.Contains(stripANSI(inst.netView(106, 24)), "[R] Comprobar de nuevo · [C] Copiar dirección") {
		t.Fatal("la pista de atajos [R]/[C] debe verse en el panel de puerto")
	}
	if _, cmd := a.Update(press("R")); cmd == nil {
		t.Fatal("R debe pedir una nueva comprobación del puerto")
	}
	if _, cmd := a.Update(press("C")); cmd == nil {
		t.Fatal("C debe copiar la dirección de conexión")
	}

	// Compacto: una sola columna (paneles apilados), sin desborde.
	for _, sz := range [][2]int{{40, 12}, {80, 24}, {110, 30}} {
		resize(a, sz[0], sz[1])
		inst.tab = 6
		assertWithinSize(t, stripANSI(a.View()), sz[0], sz[1])
	}
}

// §5.10: la pestaña Auto-apagado muestra el estado, el backup y los presets con
// puntero; Enter aplica el preset (Idle.Set) y B alterna el backup.
func TestAutoApagadoPresetsYAplicar(t *testing.T) {
	a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 7
	inst.idleCfg = idle.Config{Enabled: true, Timeout: 15 * time.Minute, BackupBeforeStop: true}
	inst.idleLoaded = true
	inst.idleCursor = 0

	vista := stripANSI(inst.idleView(106, 24))
	for _, want := range []string{
		"AUTO-APAGADO", "activado", "se apaga tras 15 min", "Backup antes de apagar: sí",
		"5 min", "10 min", "15 min", "30 min", "1 h", "2 h", "Nunca", "Desactivar",
		"[Enter] Aplicar", "[B] Alternar backup", "[T] Probar detección ahora",
	} {
		if !strings.Contains(vista, want) {
			t.Fatalf("la vista de auto-apagado debe contener %q:\n%s", want, vista)
		}
	}
	if !strings.Contains(vista, components.Pointer(false)) {
		t.Fatalf("el preset seleccionado debe llevar puntero:\n%s", vista)
	}
	if n := len(strings.Split(vista, "\n")); n != 24 {
		t.Fatalf("el panel de auto-apagado debe ocupar 24 líneas, tengo %d", n)
	}

	// Enter aplica el preset seleccionado (llama a Idle.Set).
	inst.idleCursor = 2 // 15 min
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter debe aplicar el preset de auto-apagado")
	}
	sm, ok := ejecutaCmd(cmd).(instanceIdleSetMsg)
	if !ok {
		t.Fatalf("se esperaba instanceIdleSetMsg, tengo %T", ejecutaCmd(cmd))
	}
	if sm.err != nil {
		t.Fatalf("aplicar auto-apagado: %v", sm.err)
	}
	a.Update(sm)
	cfg, err := a.svc.Idle.Config(context.Background(), "survival")
	if err != nil {
		t.Fatalf("leer auto-apagado: %v", err)
	}
	if !cfg.Enabled || cfg.Timeout != 15*time.Minute {
		t.Fatalf("el auto-apagado debe quedar activado a 15 min, tengo %+v", cfg)
	}

	// B alterna el backup antes de apagar.
	antes := cfg.BackupBeforeStop
	_, cmd = a.Update(press("b"))
	if cmd == nil {
		t.Fatal("B debe alternar el backup")
	}
	a.Update(ejecutaCmd(cmd))
	cfg2, _ := a.svc.Idle.Config(context.Background(), "survival")
	if cfg2.BackupBeforeStop == antes {
		t.Fatalf("B debe alternar BackupBeforeStop (sigue en %v)", cfg2.BackupBeforeStop)
	}
	if inst.idleCfg.BackupBeforeStop != cfg2.BackupBeforeStop {
		t.Fatal("la vista debe reflejar el backup alternado")
	}

	// Estado desactivado.
	inst.idleCfg.Enabled = false
	off := stripANSI(inst.idleView(106, 24))
	if !strings.Contains(off, "desactivado") {
		t.Fatalf("sin auto-apagado debe verse desactivado:\n%s", off)
	}
	for _, sz := range [][2]int{{40, 12}, {80, 24}, {110, 30}} {
		resize(a, sz[0], sz[1])
		inst.tab = 7
		assertWithinSize(t, stripANSI(a.View()), sz[0], sz[1])
	}
}

// WS-3: [c] copia la dirección de conexión en cualquier pestaña del detalle (no solo
// en Red, donde [C] sigue funcionando). El aviso llega por toast y no depende del
// portapapeles real: si el copiado falla se dice, nunca un falso «copiado».
func TestCopiarDireccionEnCualquierPestana(t *testing.T) {
	a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	direccion := connectionFromSpecWithHost(inst.detail.Spec, inst.detail.Host).Preferred()
	if strings.TrimSpace(direccion) == "" {
		t.Fatal("el escenario debe tener una dirección de conexión")
	}
	for _, tab := range []int{0, 1, 2, 3, 5, 7, 8} {
		inst.tab = tab
		_, cmd := a.Update(press("c"))
		if cmd == nil {
			t.Fatalf("«c» en la pestaña %d debe copiar la dirección", tab)
		}
		msg := ejecutaCmd(cmd)
		aviso, ok := msg.(toastMsg)
		if !ok {
			t.Fatalf("la pestaña %d debe avisar del copiado, tengo %T", tab, msg)
		}
		if strings.HasPrefix(aviso.Text, "✓") && !strings.Contains(aviso.Text, direccion) {
			t.Fatalf("el aviso de éxito debe citar %q, tengo %q", direccion, aviso.Text)
		}
	}

	// En la pestaña Red, [C] sigue copiando además de [c].
	inst.tab = 6
	if _, cmd := a.Update(press("C")); cmd == nil {
		t.Fatal("C debe seguir copiando en la pestaña Red")
	}

	// La barra de acciones anuncia las teclas que de verdad funcionan.
	teclas := map[string]string{}
	for _, act := range inst.Actions() {
		teclas[act.ID] = act.Key
	}
	if teclas["instance.copy"] != "c" {
		t.Fatalf("Copiar debe anunciarse con [c], tengo %q", teclas["instance.copy"])
	}
	if teclas["instance.console"] != "/" {
		t.Fatalf("Escribir en la consola debe anunciarse con [/], tengo %q", teclas["instance.console"])
	}
}
