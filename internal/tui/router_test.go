package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/tui/components"
)

// fakeScreen es una pantalla de prueba que registra los mensajes recibidos.
type fakeScreen struct {
	id      string
	actions []components.Action
	seen    int
}

func (f *fakeScreen) Init() tea.Cmd                { return nil }
func (f *fakeScreen) View(w, h int) string         { return f.id }
func (f *fakeScreen) Title() string                { return f.id }
func (f *fakeScreen) ID() string                   { return f.id }
func (f *fakeScreen) Actions() []components.Action { return f.actions }
func (f *fakeScreen) Update(tea.Msg) (Screen, tea.Cmd) {
	f.seen++
	return f, nil
}

func TestRouterApilaYDesapila(t *testing.T) {
	root := &fakeScreen{id: "home"}
	r := NewRouter(root)
	if r.Depth() != 1 || r.Top() != root {
		t.Fatalf("router inicial: depth=%d top=%v", r.Depth(), r.Top())
	}
	detail := &fakeScreen{id: "detail"}
	r.Push(detail)
	if r.Depth() != 2 || r.Top() != detail {
		t.Fatalf("tras push: depth=%d", r.Depth())
	}
	r.Update(tea.KeyMsg{})
	if detail.seen != 1 {
		t.Fatalf("el push debe recibir los mensajes: %d", detail.seen)
	}
	top, ok := r.Pop()
	if !ok || top != root || r.Depth() != 1 {
		t.Fatalf("tras pop: ok=%v top=%v depth=%d", ok, top, r.Depth())
	}
	if _, ok := r.Pop(); ok {
		t.Fatal("no se debe poder vaciar la pila por debajo de 1")
	}
	if r.Depth() != 0 {
		t.Fatalf("depth tras pop de la raíz: %d", r.Depth())
	}
}

func TestRouterReplaceYOverlay(t *testing.T) {
	r := NewRouter(&fakeScreen{id: "home"})
	r.Replace(&fakeScreen{id: "instance"})
	if r.Top().ID() != "instance" {
		t.Fatalf("replace: %s", r.Top().ID())
	}
	modal := &fakeScreen{id: "modal"}
	r.SetOverlay(modal)
	if r.Active() != modal {
		t.Fatal("el overlay debe ser la pantalla activa")
	}
	r.Update(tea.KeyMsg{})
	if modal.seen != 1 {
		t.Fatalf("el overlay debe recibir mensajes: %d", modal.seen)
	}
	// Un overlay que devuelve nil se cierra.
	closing := &fakeScreen{id: "closing"}
	r.SetOverlay(closing)
	r.overlay = closing
	r.Update(tea.KeyMsg{}) // closing devuelve sí misma; no se cierra
	if r.Overlay() == nil {
		t.Fatal("el overlay no debe cerrarse si Update devuelve pantalla")
	}
	r.ClearOverlay()
	if r.Overlay() != nil || r.Active() != r.Top() {
		t.Fatal("tras ClearOverlay vuelve la cima")
	}
}
