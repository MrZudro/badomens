package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/tailscale"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// --- Fase 6.2/UX-01: barra inferior única, contextual y sin globales ---

// La barra inferior usa un formato único, muestra solo atajos contextuales más
// los esenciales y NO incluye Ctrl+P, F2 ni Ctrl+Q (que siguen en Ayuda).
func TestBarraInferiorUnicaYContextual(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	navConInstancia(a, "survival")

	bar := stripANSI(a.actionsText(120))
	for _, want := range []string{"[F1] Ayuda", "[TAB] Desplazarse"} {
		if !strings.Contains(bar, want) {
			t.Fatalf("la barra debe incluir %q:\n%s", want, bar)
		}
	}
	if !strings.Contains(bar, "[B] Backup") {
		t.Fatalf("la barra debe mostrar el atajo contextual de backup:\n%s", bar)
	}
	for _, bad := range []string{"Ctrl+P", "F2", "Ctrl+Q"} {
		if strings.Contains(bar, bad) {
			t.Fatalf("la barra no debe mostrar %q (sigue en Ayuda):\n%s", bad, bar)
		}
	}

	// La ayuda sí documenta los atajos retirados de la barra.
	help := stripANSI(newHelpScreen(a.keys, a.actions(), a.pal, a.ascii).View(110, 30))
	for _, want := range []string{"Ctrl+P", "F2", "Ctrl+Q"} {
		if !strings.Contains(help, want) {
			t.Fatalf("la ayuda debe documentar %q:\n%s", want, help)
		}
	}

	// En el detalle aparecen los contextuales propios: [D] Borrar y [Esc] Atrás.
	openInstance(t, a, "survival")
	labels := map[string]string{}
	for _, it := range a.footerContextKeys() {
		labels[it.Key] = it.Label
	}
	if labels["D"] != "Borrar" {
		t.Fatalf("el borrado debe etiquetarse «Borrar» en el detalle, tengo %q", labels["D"])
	}
	bar = stripANSI(a.actionsText(220))
	for _, want := range []string{"[D] Borrar", "[Esc] Atrás"} {
		if !strings.Contains(bar, want) {
			t.Fatalf("en el detalle (ancho) la barra debe incluir %q:\n%s", want, bar)
		}
	}
}

// UX-15/§6: con el input de comandos enfocado, «[Esc] Atrás» va resaltado y Esc
// devuelve el foco sin ejecutar nada.
func TestEscAtrasResaltadoConInputEnfocado(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 1

	a.Update(press("/"))
	if !inst.InputFocused() || !a.inputFocused() {
		t.Fatal("la tecla / debe dar el foco al input de comandos")
	}
	esc, ok := footerItem(a.footerEssentials(), "Esc")
	if !ok {
		t.Fatal("la barra debe ofrecer «[Esc] Atrás»")
	}
	if !esc.Highlight {
		t.Fatal("con el input enfocado, «[Esc] Atrás» debe ir resaltado")
	}

	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if inst.InputFocused() {
		t.Fatal("Esc debe devolver el foco sin ejecutar nada")
	}
	if command := inst.input.Value(); command != "" {
		t.Fatalf("Esc no debe dejar texto en el campo, tengo %q", command)
	}
	if esc, ok := footerItem(a.footerEssentials(), "Esc"); ok && esc.Highlight {
		t.Fatal("sin el foco, «[Esc] Atrás» no debe seguir resaltado")
	}
}

func footerItem(items []components.FooterKey, key string) (components.FooterKey, bool) {
	for _, it := range items {
		if it.Key == key {
			return it, true
		}
	}
	return components.FooterKey{}, false
}

// --- Fase 6.4/UX-11: notificaciones con TTL, máximo 30 s y descarte manual ---

func TestNotificacionesSeDescartanAlVencerYAMano(t *testing.T) {
	a := testApp()
	cmd := a.pushToast("✖ error grave", components.Destructive)
	if cmd == nil {
		t.Fatal("pushToast debe programar la expiración (nunca es permanente)")
	}
	if len(a.toasts) != 1 {
		t.Fatalf("debe haber un toast, tengo %d", len(a.toasts))
	}
	if ttl := components.NotificationTTL(components.Destructive, "✖ error grave"); ttl > components.MaxNotificationTTL {
		t.Fatalf("el TTL no puede superar 30 s, tengo %v", ttl)
	}
	// El reloj avanza: llega el aviso de expiración y el toast se descarta.
	seq := a.toasts[0].Seq
	a.Update(toastExpireMsg{seq: seq})
	if len(a.toasts) != 0 {
		t.Fatalf("al vencer el TTL el toast debe desaparecer, tengo %d", len(a.toasts))
	}

	// Descarte manual con Esc.
	a.pushToast("✓ creado")
	a.pushToast("⚠ ojo")
	if len(a.toasts) != 2 {
		t.Fatalf("debería haber dos avisos, tengo %d", len(a.toasts))
	}
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if len(a.toasts) != 0 {
		t.Fatalf("Esc debe descartar los avisos manualmente, tengo %d", len(a.toasts))
	}
}

// --- Fase 6.5/UX-09/§8: estado único + semáforo con texto ---

func TestSemaforoTransicionesYTexto(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival") // arranca en línea
	if got := inst.serverState(); got != components.ServerOnline {
		t.Fatalf("el estado inicial debe ser En línea, tengo %q", got)
	}
	if !strings.Contains(stripANSI(inst.Chrome(100)), "En línea") {
		t.Fatalf("el semáforo debe verse con texto en la franja:\n%s", stripANSI(inst.Chrome(100)))
	}

	// apagado
	a.Update(pollRowsMsg{rows: []svc.Row{{Slug: "survival", State: "exited"}}})
	if got := inst.serverState(); got != components.ServerStopped {
		t.Fatalf("tras apagarse debe ser Apagado, tengo %q", got)
	}
	if !strings.Contains(stripANSI(inst.Chrome(100)), "Apagado") {
		t.Fatal("el semáforo debe decir «Apagado» con texto")
	}

	// cargando (operación en vuelo)
	inst.lifecycle("start")
	if got := inst.serverState(); got != components.ServerStarting {
		t.Fatalf("con una operación en curso debe ser Cargando, tengo %q", got)
	}
	if !strings.Contains(stripANSI(inst.Chrome(100)), "Cargando") {
		t.Fatal("el semáforo debe decir «Cargando» con texto")
	}

	// fin de la operación → vuelve al estado sondeado
	a.Update(lifecycleDoneMsg{slug: "survival"})
	if got := inst.serverState(); got != components.ServerStopped {
		t.Fatalf("al terminar la operación debe volver al estado real, tengo %q", got)
	}

	// en línea de nuevo
	a.Update(pollRowsMsg{rows: []svc.Row{{Slug: "survival", State: "running"}}})
	if got := inst.serverState(); got != components.ServerOnline {
		t.Fatalf("debe volver a En línea, tengo %q", got)
	}
}

// §7/§8: una respuesta tardía de otra instancia no pisa el estado de la actual.
func TestRespuestaTardiaDeOtraInstanciaNoPisaElEstado(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival") // en línea

	a.Update(homeDetailMsg{d: &svc.Detail{Spec: goldenSpec(), State: "exited"}, slug: "otra"})
	if got := inst.serverState(); got != components.ServerOnline {
		t.Fatalf("un detalle tardío de otra instancia no debe cambiar el estado, tengo %q", got)
	}
	a.Update(pollRowsMsg{rows: []svc.Row{{Slug: "otra", State: "exited"}}})
	if got := inst.serverState(); got != components.ServerOnline {
		t.Fatalf("el sondeo de otra instancia no debe cambiar el estado, tengo %q", got)
	}
}

// --- §5/UX-07: MagicDNS en vivo, aviso que desaparece y no es flotante ---

func TestAvisoMagicDNSSeReevaluaYViveEnSalud(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")

	// MagicDNS activo en vivo: el aviso desaparece de la tarjeta de conexión.
	a.Update(magicProbeMsg{rep: tailscale.Report{
		State: tailscale.StateReady, IP: "100.64.0.1",
		MagicDNS: "maquina.tail1234.ts.net", MagicDNSEnabled: true, MagicDNSReason: tailscale.ReasonMagicDNSOK,
	}})
	if vista := stripANSI(inst.summaryView(106, 24)); strings.Contains(vista, "MagicDNS desactivado") {
		t.Fatalf("con MagicDNS activo no debe quedar el aviso:\n%s", vista)
	}

	// MagicDNS desactivado (advertencia legítima): NO es un banner en la tarjeta.
	a.Update(magicProbeMsg{rep: tailscale.Report{
		State: tailscale.StateReady, IP: "100.64.0.1", MagicDNSEnabled: false,
		MagicDNSReason: tailscale.ReasonMagicDNSDisabled,
	}})
	if vista := stripANSI(inst.summaryView(106, 24)); strings.Contains(vista, "MagicDNS desactivado") {
		t.Fatalf("la advertencia persistente no debe ser un banner flotante:\n%s", vista)
	}

	// …sino que vive en Salud, con su motivo tipado y opción de descartar.
	hs := newHealthScreen(a.svc, a.pal, a.ascii)
	hs.ts = tailscale.Report{State: tailscale.StateReady, MagicDNSEnabled: false, MagicDNSReason: tailscale.ReasonMagicDNSDisabled}
	vista := stripANSI(hs.View(110, 30))
	if !strings.Contains(vista, "MagicDNS") || !strings.Contains(vista, "desactivado") {
		t.Fatalf("la advertencia de MagicDNS debe verse en Salud:\n%s", vista)
	}
	hs.magicDismissed = true
	if vista := stripANSI(hs.View(110, 30)); strings.Contains(vista, "MagicDNS") {
		t.Fatalf("el aviso de MagicDNS debe poder descartarse:\n%s", vista)
	}
}

// --- P-06: con un input capturando texto, las letras se escriben literales ---

func TestConsolaTeclasImprimiblesLiterales(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	a.Update(press("/"))
	for _, r := range "naq?" {
		a.Update(press(string(r)))
	}
	if got := inst.input.Value(); got != "naq?" {
		t.Fatalf("n/a/q/? deben escribirse en el input, tengo %q", got)
	}
	if a.router.Top().ID() != "instance" || a.router.Depth() != 2 {
		t.Fatalf("no debe abrirse otra pantalla: %s (profundidad %d)", a.router.Top().ID(), a.router.Depth())
	}
}

// --- Fase 6.1/P-03: el selector de versiones del asistente es el componente único ---

func TestWizardSelectorVersionPorValor(t *testing.T) {
	w := newWizardScreen(svc.NewFake(nil), theme.For(theme.Dark), false)
	w.start(1)
	w.versions = goldenVersions()
	w.verLoading, w.versionsSeen = false, true
	w.pickRecommended()
	w.step, w.cur = 0, 2 // campo Versión
	w.pickingVer = true
	if got := w.versionValue(); got != "1.21.4" {
		t.Fatalf("la versión por defecto debe ser la recomendada, tengo %q", got)
	}
	w.handleVersionPicker(press("down"))
	w.handleVersionPicker(tea.KeyMsg{Type: tea.KeyEnter})
	if got := w.versionValue(); got != "1.21.1" {
		t.Fatalf("Enter debe confirmar por VALOR la versión señalada, tengo %q", got)
	}
	// Esc cierra el panel sin cambiar la selección.
	w.pickingVer = true
	w.handleVersionPicker(press("down"))
	w.handleVersionPicker(tea.KeyMsg{Type: tea.KeyEsc})
	if got := w.versionValue(); got != "1.21.1" {
		t.Fatalf("Esc no debe cambiar la versión elegida, tengo %q", got)
	}
}
