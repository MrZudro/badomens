package idle

import (
	"context"
	"time"

	"github.com/MrZudro/badomens/internal/events"
)

// Controller ejecuta las acciones sobre una instancia.
type Controller interface {
	// Running indica si el contenedor está corriendo.
	Running(ctx context.Context, instance string) (bool, error)
	// Flush pide al servidor que guarde el mundo (save-all flush).
	Flush(ctx context.Context, instance string) error
	// Stop detiene el contenedor.
	Stop(ctx context.Context, instance string) error
	// Backup hace una copia de seguridad (solo si backup_before_stop).
	Backup(ctx context.Context, instance string) error
}

// CleanStop apaga una instancia con cuidado: backup opcional → guardar → parar.
// Devuelve las fases completadas para dejar rastro.
func CleanStop(ctx context.Context, ctrl Controller, instance string, cfg Config) ([]string, error) {
	var phases []string
	if cfg.BackupBeforeStop {
		if err := ctrl.Backup(ctx, instance); err != nil {
			return phases, err
		}
		phases = append(phases, "backup")
	}
	// Guardar no es fatal: si el servidor no responde, se continúa con la parada.
	if err := ctrl.Flush(ctx, instance); err != nil {
		phases = append(phases, "flush_failed")
	} else {
		phases = append(phases, "flush")
	}
	if err := ctrl.Stop(ctx, instance); err != nil {
		return phases, err
	}
	phases = append(phases, "stop")
	return phases, nil
}

// Target es una instancia que el vigilante observa.
type Target struct {
	Instance  string
	Config    Config
	Running   bool
	StartedAt time.Time
}

// Watcher vigila varias instancias con un solo bucle.
type Watcher struct {
	// Targets devuelve las instancias y su estado actual.
	Targets func(ctx context.Context) ([]Target, error)
	// Counter mide los jugadores conectados.
	Counter Counter
	// Controller ejecuta la parada.
	Controller Controller
	// Bus es el bus de eventos (puede ser nil).
	Bus *events.Bus
	// Clock es el reloj (por defecto time.Now).
	Clock func() time.Time
	// Announce envía un aviso al chat del juego (opcional).
	Announce func(ctx context.Context, instance, message string) error
	// Record deja rastro de la decisión (opcional).
	Record func(instance string, phase Phase, d Decision)

	machines map[string]*Machine
}

// NewWatcher crea un vigilante.
func NewWatcher() *Watcher {
	return &Watcher{machines: map[string]*Machine{}}
}

func (w *Watcher) now() time.Time {
	if w.Clock != nil {
		return w.Clock()
	}
	return time.Now()
}

// TickOnce avanza todas las instancias una vez y devuelve las decisiones.
func (w *Watcher) TickOnce(ctx context.Context) []Decision {
	if w.machines == nil {
		w.machines = map[string]*Machine{}
	}
	var out []Decision
	if w.Targets == nil {
		return out
	}
	targets, err := w.Targets(ctx)
	if err != nil {
		return out
	}
	seen := map[string]bool{}
	now := w.now()
	for _, t := range targets {
		seen[t.Instance] = true
		if !t.Running || !t.Config.Enabled {
			delete(w.machines, t.Instance)
			continue
		}
		m := w.machines[t.Instance]
		if m == nil {
			started := t.StartedAt
			if started.IsZero() {
				started = now
			}
			m = NewMachine(t.Config, started)
			w.machines[t.Instance] = m
		}
		online, known, _ := 0, false, error(nil)
		if w.Counter != nil {
			online, known, _ = w.Counter.Online(ctx, t.Instance)
		}
		d := m.Tick(now, Observation{Online: online, Known: known})
		if d.Stop {
			phases, serr := CleanStop(ctx, w.Controller, t.Instance, m.Config())
			_ = phases
			if serr == nil {
				d.Phase = PhaseStopped
			} else {
				d.Phase = PhaseStopping
			}
		}
		w.emit(t.Instance, d)
		if w.Record != nil {
			w.Record(t.Instance, d.Phase, d)
		}
		out = append(out, d)
	}
	// Instancias desaparecidas: olvida su máquina.
	for name := range w.machines {
		if !seen[name] {
			delete(w.machines, name)
		}
	}
	return out
}

// emit publica eventos y avisa al chat cuando cambia la fase.
func (w *Watcher) emit(instance string, d Decision) {
	if !d.Changed() {
		return
	}
	switch d.Phase {
	case PhaseCountdown:
		if w.Bus != nil {
			w.Bus.Publish(events.Event{Kind: events.IdleCountdownStarted, Instance: instance, Data: map[string]any{"remaining_seconds": int(d.Remaining.Seconds())}})
		}
		if w.Announce != nil {
			_ = w.Announce(context.Background(), instance, "El servidor se apagará en "+HumanDuration(d.Remaining)+" si nadie entra.")
		}
	case PhaseArmed:
		if d.Previous == PhaseCountdown || d.Previous == PhaseUnknown {
			if w.Bus != nil {
				w.Bus.Publish(events.Event{Kind: events.IdleCountdownCancelled, Instance: instance})
			}
		}
	case PhaseStopped:
		if w.Bus != nil {
			w.Bus.Publish(events.Event{Kind: events.IdleStopped, Instance: instance})
		}
	}
}
