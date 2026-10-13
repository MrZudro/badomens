// Package events implementa un bus de eventos de dominio ligero y en memoria.
// Lo usan el vigilante de inactividad, el CLI y la TUI para reaccionar a cambios
// sin acoplarse entre sí: el publicador no conoce a los suscriptores.
//
// El diseño es deliberadamente sencillo (sin persistencia ni reintentos): el bus
// vive en un proceso. Dos vigilantes simultáneos no comparten bus; para eso está
// el bloqueo de `internal/idle`.
package events

import (
	"sync"
	"time"
)

// Kind identifica el tipo de evento de dominio. Es un identificador estable.
type Kind string

// Tipos de evento del dominio.
const (
	// InstanceStateChanged indica que el estado de un contenedor cambió.
	InstanceStateChanged Kind = "instance.state_changed"
	// PlayerJoined indica que un jugador entró al servidor.
	PlayerJoined Kind = "player.joined"
	// PlayerLeft indica que un jugador salió del servidor.
	PlayerLeft Kind = "player.left"
	// IdleCountdownStarted indica que empezó la cuenta atrás de auto-apagado.
	IdleCountdownStarted Kind = "idle.countdown_started"
	// IdleCountdownCancelled indica que la cuenta atrás se canceló (entró gente).
	IdleCountdownCancelled Kind = "idle.countdown_cancelled"
	// IdleStopped indica que una instancia se apagó por inactividad.
	IdleStopped Kind = "idle.stopped"
	// DockerDown indica que Docker dejó de responder.
	DockerDown Kind = "docker.down"
	// DockerUp indica que Docker volvió a responder.
	DockerUp Kind = "docker.up"
	// TailscaleDown indica que la red privada dejó de estar disponible.
	TailscaleDown Kind = "tailscale.down"
	// TailscaleUp indica que la red privada volvió.
	TailscaleUp Kind = "tailscale.up"
	// BackupFinished indica que terminó una copia de seguridad.
	BackupFinished Kind = "backup.finished"
)

// Event es un suceso de dominio.
type Event struct {
	// Kind es el tipo de evento.
	Kind Kind `json:"kind"`
	// Instance es el slug de la instancia afectada (vacío para eventos globales).
	Instance string `json:"instance,omitempty"`
	// At es el instante del suceso (UTC). Si es cero, Publish usa time.Now.
	At time.Time `json:"at"`
	// Data lleva detalles libres del evento (estado, jugador, motivo…).
	Data map[string]any `json:"data,omitempty"`
}

// Bus es un bus de eventos con suscripción por canal. Es seguro para uso
// concurrente.
type Bus struct {
	mu     sync.Mutex
	next   int
	subs   map[int]chan Event
	closed bool
}

// New crea un bus vacío.
func New() *Bus {
	return &Bus{subs: map[int]chan Event{}}
}

// Subscribe registra un suscriptor. Devuelve el canal de eventos y una función
// de baja idempotente que lo cierra. El buffer evita que un suscriptor lento
// bloquee al publicador; si el buffer se llena, los eventos se descartan.
func (b *Bus) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer < 0 {
		buffer = 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	ch := make(chan Event, buffer)
	if b.closed {
		close(ch)
		return ch, func() {}
	}
	b.subs[id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
	}
}

// Publish entrega un evento a todos los suscriptores actuales sin bloquear:
// cada canal recibe el evento si tiene hueco; si está lleno, se descarta para
// ese suscriptor. Si e.At es cero, se sella con la hora actual.
func (b *Bus) Publish(e Event) {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default:
			// Suscriptor saturado: se descarta el evento para no bloquear.
		}
	}
}

// Close cierra todos los canales de suscripción. Es idempotente.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, ch := range b.subs {
		delete(b.subs, id)
		close(ch)
	}
}

// Subscribers devuelve cuántos suscriptores hay activos (útil en pruebas).
func (b *Bus) Subscribers() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}
