package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/ports"
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
