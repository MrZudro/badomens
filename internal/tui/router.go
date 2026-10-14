package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/tui/components"
)

// Screen es la interfaz Elm de la TUI v2 (§3.3). Cada pantalla recibe el tamaño
// disponible en View(w, h) y no consulta estado global; las acciones que expone
// alimentan la barra de botones y la paleta de comandos.
type Screen interface {
	// Init arranca las cargas iniciales de la pantalla.
	Init() tea.Cmd
	// Update procesa un mensaje y devuelve la pantalla (posiblemente nueva).
	Update(tea.Msg) (Screen, tea.Cmd)
	// View pinta la pantalla en el espacio w×h.
	View(w, h int) string
	// Actions devuelve las acciones disponibles (para la barra y la paleta).
	Actions() []components.Action
	// Title es el título de la pantalla.
	Title() string
	// ID es el identificador estable de la pantalla.
	ID() string
}

// Router mantiene la pila de pantallas y un overlay modal (§3.2). El overlay,
// si existe, se dibuja encima de la pantalla superior y recibe las teclas.
type Router struct {
	stack   []Screen
	overlay Screen
}

// NewRouter crea un router con la pantalla inicial.
func NewRouter(root Screen) *Router {
	r := &Router{}
	if root != nil {
		r.stack = append(r.stack, root)
	}
	return r
}

// Push apila una pantalla nueva y devuelve su Init.
func (r *Router) Push(s Screen) tea.Cmd {
	if s == nil {
		return nil
	}
	r.stack = append(r.stack, s)
	return s.Init()
}

// Pop desapila la pantalla superior. Devuelve la nueva cima y si quedó alguna.
func (r *Router) Pop() (Screen, bool) {
	if len(r.stack) == 0 {
		return nil, false
	}
	r.stack = r.stack[:len(r.stack)-1]
	if len(r.stack) == 0 {
		return nil, false
	}
	return r.stack[len(r.stack)-1], true
}

// Replace sustituye la pantalla superior.
func (r *Router) Replace(s Screen) tea.Cmd {
	if len(r.stack) == 0 {
		return r.Push(s)
	}
	r.stack[len(r.stack)-1] = s
	return s.Init()
}

// Top devuelve la pantalla superior (o nil).
func (r *Router) Top() Screen {
	if len(r.stack) == 0 {
		return nil
	}
	return r.stack[len(r.stack)-1]
}

// Depth devuelve cuántas pantallas hay apiladas.
func (r *Router) Depth() int { return len(r.stack) }

// SetOverlay muestra un modal por encima de la pantalla actual.
func (r *Router) SetOverlay(s Screen) tea.Cmd {
	r.overlay = s
	if s == nil {
		return nil
	}
	return s.Init()
}

// ClearOverlay cierra el modal.
func (r *Router) ClearOverlay() { r.overlay = nil }

// Overlay devuelve el modal actual (o nil).
func (r *Router) Overlay() Screen { return r.overlay }

// Active devuelve la pantalla que recibe los mensajes: el overlay si lo hay, o
// la cima de la pila.
func (r *Router) Active() Screen {
	if r.overlay != nil {
		return r.overlay
	}
	return r.Top()
}

// Update enruta un mensaje a la pantalla activa y aplica el resultado.
func (r *Router) Update(msg tea.Msg) tea.Cmd {
	active := r.Active()
	if active == nil {
		return nil
	}
	next, cmd := active.Update(msg)
	if r.overlay != nil {
		if next == nil {
			r.overlay = nil
		} else {
			r.overlay = next
		}
		return cmd
	}
	if next != nil && len(r.stack) > 0 {
		r.stack[len(r.stack)-1] = next
	}
	return cmd
}

// Root devuelve la primera pantalla de la pila (la raíz del panel), o nil si la
// pila está vacía.
func (r *Router) Root() Screen {
	if len(r.stack) == 0 {
		return nil
	}
	return r.stack[0]
}

// Contains indica si hay alguna pantalla apilada con ese ID estable.
func (r *Router) Contains(id string) bool {
	for _, s := range r.stack {
		if s != nil && s.ID() == id {
			return true
		}
	}
	return false
}

// PopTo desapila hasta que la cima tenga ese ID y devuelve true. Si ninguna
// pantalla apilada lo tiene, no toca la pila y devuelve false.
func (r *Router) PopTo(id string) bool {
	for i := len(r.stack) - 1; i >= 0; i-- {
		if r.stack[i] != nil && r.stack[i].ID() == id {
			r.stack = r.stack[:i+1]
			return true
		}
	}
	return false
}
