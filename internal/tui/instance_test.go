package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

func testInstance() (*instanceScreen, *instance.Spec) {
	spec := &instance.Spec{
		Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4",
		Memory: "4G", Port: 25565, MaxPlayers: 20,
	}
	sv := svc.NewFake([]*instance.Spec{spec})
	s := newInstanceScreen(sv, theme.For(theme.Dark), false, "survival")
	s.detail = &svc.Detail{Spec: spec, State: "running", Host: "100.64.0.1"}
	return s, spec
}

// TestInstanciaDetectaCaida (P-08): running → exited sin acción del usuario
// marca la caída y deduce la causa de los logs.
func TestInstanciaDetectaCaida(t *testing.T) {
	s, _ := testInstance()
	s.applyState("running")
	s.applyState("exited")
	if !s.crash || !s.crashPending {
		t.Fatal("debía detectarse la caída inesperada")
	}
	s.Update(instanceLogsMsg{slug: "survival", text: "java.lang.OutOfMemoryError: Java heap space", gen: s.logsGen})
	if s.crashPending {
		t.Fatal("la causa debía resolverse con los logs")
	}
	if !strings.Contains(s.crashCause, "memoria") {
		t.Fatalf("causa = %q, quiero mencionar memoria", s.crashCause)
	}
}

// TestInstanciaNoCuentaApagadoDelUsuario (P-08): un stop del usuario no es caída.
func TestInstanciaNoCuentaApagadoDelUsuario(t *testing.T) {
	s, _ := testInstance()
	s.applyState("running")
	s.userStopped = true
	s.applyState("exited")
	if s.crash {
		t.Fatal("un apagado pedido por el usuario no debe contar como caída")
	}
}

// TestInstanciaCrashLoop (P-08): tres caídas en 10 min bloquean el reinicio.
func TestInstanciaCrashLoop(t *testing.T) {
	s, _ := testInstance()
	for i := 0; i < 3; i++ {
		s.applyState("running")
		s.applyState("exited")
	}
	if !s.crashLoop {
		t.Fatal("tres caídas seguidas debían activar el crash-loop")
	}
	for _, a := range s.Actions() {
		if a.ID == "instance.restart" {
			if !a.Disabled || !strings.Contains(strings.ToLower(a.DisabledReason), "3 veces") {
				t.Fatalf("reiniciar debía estar deshabilitado con motivo, tengo %+v", a)
			}
		}
	}
}

// TestInstanciaBorrarExigeNombre (P-03/§5.15): el borrado es un modal con borde
// que exige escribir el nombre exacto; sin coincidencia no se borra nada y Esc
// cancela sin mutar el estado.
func TestInstanciaBorrarExigeNombre(t *testing.T) {
	s, _ := testInstance()
	_ = s.startDeletePrompt()
	if s.modal == nil || s.modalKind != "delete" {
		t.Fatal("debía abrirse el modal de borrado")
	}
	if s.modal.RequireText != "survival" {
		t.Fatalf("el modal debe exigir el nombre exacto, tengo %q", s.modal.RequireText)
	}
	if s.modal.Focused != 1 {
		t.Fatal("el foco por defecto debe estar en el botón seguro, no en el destructivo")
	}

	// Nombre que no coincide: Enter no borra nada y el modal sigue abierto.
	for _, r := range "otro-nombre" {
		_, _ = s.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if _, cmd := s.handleKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("un nombre que no coincide no debe ejecutar nada")
	}
	if s.modal == nil {
		t.Fatal("con el nombre equivocado el modal debe seguir abierto")
	}

	// Esc cancela sin borrar.
	if _, cmd := s.handleKey(tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatal("Esc no debe ejecutar nada")
	}
	if s.modal != nil {
		t.Fatal("Esc debe cerrar el modal")
	}

	// El nombre exacto habilita confirmar y borra.
	_ = s.startDeletePrompt()
	for _, r := range "survival" {
		_, _ = s.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, enterCmd := s.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	msg := ejecutaCmd(enterCmd)
	if _, ok := msg.(instanceDeletedMsg); !ok {
		t.Fatalf("con el nombre exacto debía borrar, tengo %T", msg)
	}
	if s.modal != nil {
		t.Fatal("tras confirmar, el modal debe cerrarse")
	}
}

// TestInstanciaCambiarVersion (P-03/§5.15): el cambio de versión se confirma en un
// modal que avisa del backup previo; Esc cancela y Enter aplica el cambio.
func TestInstanciaCambiarVersion(t *testing.T) {
	s, spec := testInstance()
	_ = s.startVersionPrompt()
	if cmd := s.runPrompt("__version__", "1.20.1"); cmd != nil {
		t.Fatal("fijar la versión no debe ejecutar todavía")
	}
	if s.modal == nil || s.modalKind != "version" || s.modalValue != "1.20.1" {
		t.Fatalf("debía quedar pendiente de confirmación modal: %+v", s.modal)
	}
	if !strings.Contains(s.modal.Body, "backup") {
		t.Fatalf("el modal debe avisar del backup previo: %q", s.modal.Body)
	}
	s.prompt = false // el prompt ya se cerró al aceptar el valor

	// Esc cancela sin tocar la versión.
	if _, cmd := s.handleKey(tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatal("Esc no debe cambiar la versión")
	}
	if s.modal != nil || spec.Version != "1.21.4" {
		t.Fatalf("Esc debía cancelar sin tocar la versión (tengo %q)", spec.Version)
	}

	// Enter confirma el cambio con backup.
	_ = s.startVersionPrompt()
	if cmd := s.runPrompt("__version__", "1.20.1"); cmd != nil {
		t.Fatal("fijar la versión no debe ejecutar todavía")
	}
	s.prompt = false // el prompt ya se cerró al aceptar el valor
	_, cmd := s.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("confirmar debía lanzar el cambio con backup")
	}
	_ = ejecutaCmd(cmd)
	if spec.Version != "1.20.1" {
		t.Fatalf("versión = %q, quiero 1.20.1", spec.Version)
	}
}
