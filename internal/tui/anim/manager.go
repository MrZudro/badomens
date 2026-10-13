// Package anim gestiona el tick global de animaciones de la TUI v2 (RF-1/§4.3):
// un único tea.Tick a ~16 fps que solo existe mientras haya al menos una
// animación activa. Sin animaciones, no hay ticks (0 % de CPU). Con movimiento
// reducido, nunca hay ticks.
package anim

import (
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Interval es el latido por defecto (≈16 fps).
const Interval = 60 * time.Millisecond

// MinInterval es el tope duro de 30 fps.
const MinInterval = 33 * time.Millisecond

// TickMsg es el mensaje que emite el gestor en cada latido.
type TickMsg struct {
	At time.Time
}

// Manager es el gestor de animaciones. Es seguro para uso concurrente.
type Manager struct {
	mu      sync.Mutex
	active  map[string]bool
	reduced bool
	ttf     time.Duration // tiempo entre ticks
}

// New crea un gestor. reduced (movimiento reducido) desactiva las animaciones.
func New(reduced bool) *Manager {
	return &Manager{active: map[string]bool{}, reduced: reduced}
}

// SetReduced cambia el modo de movimiento reducido.
func (m *Manager) SetReduced(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reduced = v
	if v {
		m.active = map[string]bool{}
	}
}

// Start marca una animación como activa. Reduce los ticks al arrancar.
func (m *Manager) Start(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.reduced {
		return
	}
	m.active[id] = true
	m.ttf = Interval
}

// Stop marca una animación como inactiva.
func (m *Manager) Stop(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.active, id)
}

// Active devuelve cuántas animaciones están activas.
func (m *Manager) Active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.active)
}

// Interval devuelve el intervalo de tick actual (nunca por debajo del tope).
func (m *Manager) Interval() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ttf < MinInterval {
		return Interval
	}
	return m.ttf
}

// Tick devuelve un tea.Cmd que emite TickMsg a intervalos fijos mientras haya
// animaciones activas; si no hay ninguna (o hay movimiento reducido), devuelve
// nil y la TUI queda en reposo.
func (m *Manager) Tick() tea.Cmd {
	m.mu.Lock()
	active := len(m.active) > 0 && !m.reduced
	interval := m.ttf
	if interval < MinInterval {
		interval = Interval
	}
	m.mu.Unlock()
	if !active {
		return nil
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return TickMsg{At: t}
	})
}
