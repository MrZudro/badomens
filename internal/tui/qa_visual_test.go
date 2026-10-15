package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/infra"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// Tamaños obligatorios de la matriz de aceptación (incluye el compacto 60×18).
var qaSizes = [][2]int{{60, 18}, {80, 24}, {100, 30}, {132, 40}}

// qaLineas divide la vista en líneas sin ANSI.
func qaLineas(a *App) []string { return strings.Split(stripANSI(a.View()), "\n") }

// qaEstado es la penúltima línea del marco: la barra de estado.
func qaEstado(a *App) string {
	lines := qaLineas(a)
	if len(lines) < 2 {
		return ""
	}
	return lines[len(lines)-2]
}

// qaFooter revisa que TODOS los atajos de la barra inferior estén completos: la
// regla del repo es descartar el ítem entero, nunca dejarlo a medias («[S] Ini…»).
func qaFooter(t *testing.T, a *App, w int) string {
	t.Helper()
	bar := stripANSI(a.actionsText(w))
	for _, frag := range strings.Split(bar, "   ") {
		frag = strings.TrimSpace(frag)
		if frag == "" {
			continue
		}
		if strings.Contains(frag, "…") {
			t.Fatalf("la barra inferior no puede dejar un atajo a medias: %q (barra: %q)", frag, bar)
		}
		if !strings.HasPrefix(frag, "[") || !strings.Contains(frag, "] ") {
			t.Fatalf("la barra inferior debe usar «[Tecla] Etiqueta» en todos los atajos: %q", frag)
		}
	}
	return bar
}

// QA-01/UX-10 (§9): en los cuatro tamaños obligatorios el diálogo de regeneración
// mantiene VISIBLES sus tres acciones (no se puede confirmar a ciegas) y la barra
// de atajos sigue completa en la última línea.
func TestQA01ModalRegenAccionesVisiblesEnTodosLosTamanos(t *testing.T) {
	for _, sz := range qaSizes {
		w, h := sz[0], sz[1]
		a, _, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
		resize(a, w, h)
		inst := openInstance(t, a, "survival")
		inst.tab = 4 // Mundos
		// Servidor detenido: el cuerpo es más largo (como en la captura de QA).
		inst.detail.State = "exited"
		a.Update(press("g"))
		a.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if inst.modal == nil || inst.modalKind != "world_regen" {
			t.Fatalf("%dx%d: debía abrirse el modal de regeneración: %+v", w, h, inst.modal)
		}
		out := stripANSI(a.View())
		lines := strings.Split(out, "\n")
		if len(lines) != h {
			t.Fatalf("%dx%d: la vista debe ocupar %d líneas, tengo %d", w, h, h, len(lines))
		}
		for _, want := range []string{"[Y] Sí, apagar y regenerar", "[B] Backup antes y regenerar", "[N] No, conservar el mundo actual"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%dx%d: la acción %q debe ser visible:\n%s", w, h, want, out)
			}
		}
		if last := strings.TrimRight(lines[len(lines)-1], " "); !strings.Contains(last, "[F1] Ayuda") {
			t.Fatalf("%dx%d: la barra de atajos debe seguir en la última línea: %q", w, h, last)
		}
		qaFooter(t, a, w-5)
	}
}

// QA-02/UX-02: con el cuadro del modal abierto en compacto, el borde inferior se
// ve (el cuerpo se recorta, no el marco) y el aviso de pérdida de datos sigue
// explicándose.
func TestQA02ModalCompactoNoPierdeElMarcoNiElAviso(t *testing.T) {
	a, _, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
	resize(a, 60, 18)
	inst := openInstance(t, a, "survival")
	inst.tab = 4
	inst.detail.State = "exited"
	a.Update(press("g"))
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	out := stripANSI(a.View())
	n := strings.Count(out, "╰")
	if n < 1 {
		t.Fatalf("el modal debe cerrar su marco (borde inferior visible):\n%s", out)
	}
	if !strings.Contains(out, "Se BORRARÁ el mundo actual") {
		t.Fatalf("el aviso de pérdida de datos debe seguir viéndose:\n%s", out)
	}
}

// QA-03/UX-01: a 60×18 la barra inferior no trunca ningún atajo a medias.
func TestQA03PieCompactoDescartarEntero(t *testing.T) {
	a, _, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
	resize(a, 60, 18)
	openInstance(t, a, "survival")
	bar := qaFooter(t, a, 55)
	if strings.Contains(bar, "Ini…") || strings.Contains(bar, "[S] Ini") {
		t.Fatalf("el atajo no aplicable o que no cabe se descarta entero: %q", bar)
	}
	if !strings.Contains(bar, "[F1] Ayuda") {
		t.Fatalf("los esenciales deben seguir en la barra: %q", bar)
	}
}

// QA-04/P-05/UX-11: la barra de estado muestra el ESTADO ACTUAL, nunca el eco del
// último aviso, y un aviso caducado desaparece de la pantalla por completo.
func TestQA04BarraEstadoNoRetieneAvisosCaducados(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	a.Update(toastMsg{Text: "✓ Copiado: 100.64.0.2:25565"})
	out := stripANSI(a.View())
	if !strings.Contains(out, "✓ Copiado: 100.64.0.2:25565") {
		t.Fatalf("el aviso debe verse en su línea transitoria:\n%s", out)
	}
	if got := qaEstado(a); strings.Contains(got, "Copiado") {
		t.Fatalf("la barra de estado no puede duplicar el aviso: %q", got)
	}
	a.Update(toastExpireMsg{seq: 1})
	if out := stripANSI(a.View()); strings.Contains(out, "Copiado") {
		t.Fatalf("un aviso caducado no debe seguir en pantalla:\n%s", out)
	}
}

// QA-05/P-05: el chequeo del daemon se RESUELVE con un mensaje final; el
// «Comprobando…» es transitorio y nunca se queda fijo en la carcasa.
func TestQA05ChequeoDelDaemonSeResuelve(t *testing.T) {
	a := NewApp(svc.NewFakeWith(nil, svc.FakeOptions{}), darkOpts())
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if cmd := a.Init(); cmd == nil {
		t.Fatal("Init debe arrancar la comprobación del daemon")
	}
	if got := toastCount(a, "Comprobando el daemon"); got != 1 {
		t.Fatalf("el chequeo debe anunciarse una vez, tengo %d", got)
	}
	if got := qaEstado(a); strings.Contains(got, "Comprobando") {
		t.Fatalf("la barra de estado no debe quedarse en «Comprobando…»: %q", got)
	}
	a.Update(infraReadyMsg{what: "daemon", rep: infra.Report{
		Domain: infra.DomainDaemon, Status: infra.StatusReady, Ready: true,
		Message: "El daemon de badomens está en marcha.",
	}})
	if got := toastCount(a, "El daemon de badomens está en marcha"); got != 1 {
		t.Fatalf("el chequeo debe resolverse con el resultado final, tengo %d", got)
	}
	// Al caducar los avisos no queda ningún rastro permanente.
	for _, to := range append([]toastEntry{}, a.toasts...) {
		a.expireToast(to.Seq)
	}
	if out := stripANSI(a.View()); strings.Contains(out, "Comprobando") || strings.Contains(out, "daemon de badomens") {
		t.Fatalf("ninguna notificación puede ser permanente:\n%s", out)
	}
}

// QA-05b: si el daemon no arranca, el mensaje final explica el motivo con su
// hint; tampoco hay un «Comprobando…» eterno.
func TestQA05bDaemonConFalloInformaLaCausa(t *testing.T) {
	a := NewApp(svc.NewFakeWith(nil, svc.FakeOptions{InfraDaemonDown: true, InfraDaemonInstalled: true}), darkOpts())
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a.Init()
	a.Update(infraReadyMsg{what: "daemon", rep: infra.Report{
		Domain: infra.DomainDaemon, Status: infra.StatusDown,
		Message: "El daemon de badomens no está en marcha.",
		Hint:    "badomens lo levantará como daemon de sesión",
		Manual:  []string{"Ejecuta 'badomens idle daemon' en una terminal."},
	}})
	texto := ""
	for _, to := range a.toasts {
		texto += to.Text + "\n"
	}
	if strings.Contains(texto, "Comprobando el daemon") && len(a.toasts) == 1 {
		t.Fatalf("el chequeo debe resolverse, no quedarse en «comprobando»: %v", a.toasts)
	}
	if !strings.Contains(texto, "daemon") {
		t.Fatalf("el fallo del daemon debe informarse con su causa: %v", a.toasts)
	}
}

// QA-06: el pie es CONTEXTUAL: con la instancia detenida no ofrece «[P] Detener»
// ni «[R] Reiniciar» (que además la Ayuda marca como no disponibles).
func TestQA06PieNoOfreceAccionesNoAplicables(t *testing.T) {
	a := testApp()
	resize(a, 132, 40)
	a.Update(pollRowsMsg{rows: []svc.Row{{
		Slug: "survival", Name: "Survival", State: "exited", Port: 25565, MaxPlayers: 20,
	}}})
	bar := qaFooter(t, a, 127)
	if !strings.Contains(bar, "[S] Iniciar") {
		t.Fatalf("con la instancia detenida debe ofrecerse [S] Iniciar: %q", bar)
	}
	for _, no := range []string{"[P] Detener", "[R] Reiniciar"} {
		if strings.Contains(bar, no) {
			t.Fatalf("con la instancia detenida no debe ofrecerse %q: %q", no, bar)
		}
	}
	// En marcha, el pie ofrece lo aplicable y no «[S] Iniciar».
	a.Update(pollRowsMsg{rows: []svc.Row{{
		Slug: "survival", Name: "Survival", State: "running", Port: 25565, MaxPlayers: 20,
	}}})
	bar = qaFooter(t, a, 127)
	if !strings.Contains(bar, "[P] Detener") {
		t.Fatalf("con la instancia en marcha debe ofrecerse [P] Detener: %q", bar)
	}
	if strings.Contains(bar, "[S] Iniciar") {
		t.Fatalf("con la instancia en marcha no debe ofrecerse [S] Iniciar: %q", bar)
	}
}

// QA-07/§8: una sola etiqueta para el estado detenido («Apagado») en el semáforo,
// en la insignia de la lista/detalle y en todas las pantallas.
func TestQA07UnaSolaEtiquetaDetenidoApagado(t *testing.T) {
	if got := components.ServerStopped.Label(false); got != "Apagado" {
		t.Fatalf("el semáforo debe decir «Apagado», tengo %q", got)
	}
	if got := components.Badge("stopped", theme.For(theme.Dark), false); got != "○ Apagado" {
		t.Fatalf("la insignia debe decir «Apagado», tengo %q", got)
	}
	if got := components.ServerStarting.Label(false); got != "Cargando…" {
		t.Fatalf("el estado en transición es «Cargando…», tengo %q", got)
	}

	a := testApp()
	resize(a, 132, 40)
	spec := goldenSpec()
	a.Update(pollRowsMsg{rows: []svc.Row{{
		Slug: spec.Slug, Name: spec.Name, State: "exited", Port: spec.Port, MaxPlayers: 20,
	}}})
	a.Update(homeDetailMsg{d: &svc.Detail{Spec: spec, State: "exited"}})
	out := stripANSI(a.View())
	if !strings.Contains(out, "Apagado") {
		t.Fatalf("Inicio debe decir «Apagado»:\n%s", out)
	}
	if strings.Contains(out, "Detenido") {
		t.Fatalf("no puede quedar la etiqueta antigua «Detenido»:\n%s", out)
	}
	inst := openInstance(t, a, spec.Slug)
	inst.detail = &svc.Detail{Spec: spec, State: "exited"}
	if got := stripANSI(inst.Chrome(100)); !strings.Contains(got, "Apagado") {
		t.Fatalf("el detalle de la instancia debe decir «Apagado»: %q", got)
	}
}

// QA-08/§2: la cabecera usa el MISMO orden icono + texto en Docker y Tailscale.
func TestQA08CabeceraOrdenIconoTexto(t *testing.T) {
	a := testApp()
	resize(a, 132, 40)
	a.Update(pollDockerMsg{diag: dockerOKDiagnosis()})
	a.Update(homeReqMsg{dockerOK: true, tsIP: "100.64.0.1", tsOK: true})
	head := qaLineas(a)[0]
	if !strings.Contains(head, "● Docker OK") || !strings.Contains(head, "● Tailscale") {
		t.Fatalf("la cabecera debe poner el icono antes del texto en ambos: %q", head)
	}
	if strings.Contains(head, "Tailscale ●") {
		t.Fatalf("la cabecera no puede mezclar el orden icono/texto: %q", head)
	}
}

// QA-09/UX-03: en la rejilla de versiones del asistente se ve el detalle de la
// opción enfocada (p. ej. «última estable») y la alineación se mantiene.
func TestQA09WizardPickerAportaElDetalleDeLaEnfocada(t *testing.T) {
	a := NewApp(goldenSvc(), darkOpts())
	a.router.Push(newWizardScreen(a.svc, a.pal, a.ascii))
	wz, ok := a.router.Top().(*wizardScreen)
	if !ok {
		t.Fatal("se esperaba el asistente")
	}
	wz.start(1)
	wz.versions = goldenVersions()
	wz.verLoading, wz.versionsSeen = false, true
	wz.pickRecommended()
	wz.pickingVer = true
	out := stripANSI(wz.View(132, 40))
	if !strings.Contains(out, "última estable") {
		t.Fatalf("el detalle de la versión enfocada debe verse en la rejilla:\n%s", out)
	}
	if !strings.Contains(out, "[Enter] Elegir") {
		t.Fatalf("el pie del panel de versiones debe seguir visible:\n%s", out)
	}
	// Al enfocar otra versión, el detalle cambia con ella.
	wz.verCursor = len(wz.versions) - 1
	out = stripANSI(wz.View(132, 40))
	if strings.Contains(out, "última estable") {
		t.Fatalf("el detalle debe ser el de la opción ENFOCADA:\n%s", out)
	}
}

// QA-10 (§4): con Docker caído, el pie no ofrece acciones de instancia que no se
// pueden aplicar (siguen documentadas, con su motivo, en Ayuda/paleta).
func TestQA10SinDockerElPieNoOfreceAccionesDeInstancia(t *testing.T) {
	a := testApp()
	resize(a, 132, 40)
	a.Update(pollDockerMsg{diag: dockerx.Diagnosis{State: dockerx.StateDaemonDown, Message: "Docker apagado"}})
	a.Update(pollRowsMsg{rows: []svc.Row{{
		Slug: "survival", Name: "Survival", State: "exited", Port: 25565, MaxPlayers: 20,
	}}})
	bar := qaFooter(t, a, 127)
	for _, no := range []string{"[S] Iniciar", "[B] Backup", "[D] Borrar"} {
		if strings.Contains(bar, no) {
			t.Fatalf("sin Docker no debe ofrecerse %q: %q", no, bar)
		}
	}
	if !strings.Contains(bar, "[F1] Ayuda") {
		t.Fatalf("los esenciales deben seguir en la barra: %q", bar)
	}
}
