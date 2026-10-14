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
	s.Update(instanceLogsMsg{slug: "survival", text: "java.lang.OutOfMemoryError: Java heap space"})
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

// TestInstanciaBorrarExigeNombre (P-03): borrar sin el nombre exacto no hace nada.
func TestInstanciaBorrarExigeNombre(t *testing.T) {
	s, _ := testInstance()
	_ = s.startDeletePrompt()
	if s.promptAct != "__delete__" {
		t.Fatal("debía abrir el prompt de borrado")
	}
	cmd := s.runPrompt("__delete__", "otro-nombre")
	msg := ejecutaCmd(cmd)
	if _, ok := msg.(toastMsg); !ok {
		t.Fatalf("un nombre que no coincide debía avisar, tengo %T", msg)
	}

	cmd = s.runPrompt("__delete__", "survival")
	msg = ejecutaCmd(cmd)
	if _, ok := msg.(instanceDeletedMsg); !ok {
		t.Fatalf("con el nombre exacto debía borrar, tengo %T", msg)
	}
}

// TestInstanciaCambiarVersion (P-03): cambiar la versión exige confirmación y
// guarda la nueva versión tras el backup.
func TestInstanciaCambiarVersion(t *testing.T) {
	s, spec := testInstance()
	_ = s.startVersionPrompt()
	if cmd := s.runPrompt("__version__", "1.20.1"); cmd != nil {
		t.Fatal("fijar la versión no debe ejecutar todavía")
	}
	if s.confirmKind != "version" || s.confirmValue != "1.20.1" {
		t.Fatalf("debía quedar pendiente de confirmación: %q %q", s.confirmKind, s.confirmValue)
	}
	s.prompt = false // el prompt ya se cerró al aceptar el valor
	// Confirmación con Enter.
	_, cmd := s.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("confirmar debía lanzar el cambio con backup")
	}
	_ = ejecutaCmd(cmd)
	if spec.Version != "1.20.1" {
		t.Fatalf("versión = %q, quiero 1.20.1", spec.Version)
	}
}
