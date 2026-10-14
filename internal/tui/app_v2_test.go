package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

func testApp() *App {
	s := svc.NewFake([]*instance.Spec{{
		Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4",
		Memory: "4G", Port: 25565, MaxPlayers: 20,
	}})
	return NewApp(s, AppOptions{Theme: theme.Dark})
}

func press(r string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r)} }

func resize(a *App, w, h int) { a.Update(tea.WindowSizeMsg{Width: w, Height: h}) }

// TestAppResizeSinPanico (RF-1): la secuencia grande → pequeño → TooSmall →
// grande no debe entrar en pánico ni perder contenido.
func TestAppResizeSinPanico(t *testing.T) {
	a := testApp()
	for _, sz := range [][2]int{{110, 30}, {70, 20}, {40, 12}, {30, 10}, {200, 50}} {
		resize(a, sz[0], sz[1])
		out := a.View()
		if out == "" {
			t.Fatalf("vista vacía en %dx%d", sz[0], sz[1])
		}
	}
	resize(a, 30, 10)
	if !strings.Contains(a.View(), "Agranda la terminal") {
		t.Fatalf("TooSmall debe avisar: %q", a.View())
	}
	resize(a, 120, 40)
	if strings.Contains(a.View(), "Agranda la terminal") {
		t.Fatal("al crecer debe volver al panel")
	}
}

func TestAppHomeMuestraFilasYConectar(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	rows := []svc.Row{{Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4", Port: 25565, State: "running"}}
	a.Update(pollRowsMsg{rows: rows})
	a.Update(homeDetailMsg{d: &svc.Detail{Spec: &instance.Spec{
		Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4", Port: 25565,
	}, State: "running"}})
	out := a.View()
	if !strings.Contains(out, "Survival") {
		t.Fatalf("la home debe listar la instancia:\n%s", out)
	}
	if !strings.Contains(out, "Conectarse") {
		t.Fatalf("la home debe mostrar la tarjeta de conexión:\n%s", out)
	}
}

func TestAppAyudaYPaleta(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	a.Update(press("?"))
	if a.router.Overlay() == nil || a.router.Overlay().ID() != "help" {
		t.Fatal("? debe abrir la ayuda")
	}
	a.Update(press("?"))
	if a.router.Overlay() != nil {
		t.Fatal("? de nuevo cierra la ayuda")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if a.router.Overlay() == nil || a.router.Overlay().ID() != "palette" {
		t.Fatal("Ctrl+P debe abrir la paleta")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if a.router.Overlay() != nil {
		t.Fatal("Esc cierra la paleta")
	}
}

func TestAppWizardCreaInstancia(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	a.Update(press("n"))
	if a.router.Top().ID() != "wizard" {
		t.Fatalf("n debe abrir el asistente, tengo %s", a.router.Top().ID())
	}
	a.Update(press("r")) // modo rápido
	for _, r := range "mi-servidor" {
		a.Update(press(string(r)))
	}
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})           // a revisión
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter}) // crear
	if cmd == nil {
		t.Fatal("enter en revisión debe lanzar la creación")
	}
	msg := cmd()
	created, ok := msg.(wizardCreatedMsg)
	if !ok {
		t.Fatalf("mensaje inesperado: %T", msg)
	}
	if created.err != nil {
		t.Fatalf("creación: %v", created.err)
	}
	a.Update(created)
	if a.router.Top().ID() == "wizard" {
		t.Fatal("tras crear debe salir del asistente")
	}
	if len(a.toasts) == 0 {
		t.Fatal("tras crear debe haber un toast")
	}
}

func TestAppAbreDetalleDeInstancia(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	a.Update(pollRowsMsg{rows: []svc.Row{{Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4", Port: 25565, State: "running"}}})
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter debe pedir abrir el detalle")
	}
	a.Update(cmd()) // openInstanceMsg
	if a.router.Top().ID() != "instance" {
		t.Fatalf("debe abrirse el detalle, tengo %s", a.router.Top().ID())
	}
	// Simular carga del detalle.
	a.Update(homeDetailMsg{d: &svc.Detail{Spec: &instance.Spec{Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4", Port: 25565}, State: "running"}})
	if !strings.Contains(a.router.Top().View(100, 30), "Survival") {
		t.Fatal("el detalle debe mostrar el nombre")
	}
}

func TestPollerCtxNoBloquea(t *testing.T) {
	// El poller debe devolver comandos que terminan rápido con el fake.
	s := svc.NewFake(nil)
	p := &Poller{Svc: s}
	cmd := p.Cmds(nowUTC(), true, true)
	if cmd == nil {
		t.Fatal("se esperaba comando de sondeo")
	}
	done := make(chan struct{})
	go func() { _ = cmd(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("el sondeo no terminó")
	}
}
