package idle

import "time"

// Phase es la fase de la máquina de estados de inactividad.
type Phase string

// Fases posibles.
const (
	// PhaseDisabled: el auto-apagado no está activo.
	PhaseDisabled Phase = "disabled"
	// PhaseGrace: recién arrancado, tiempo para que entre alguien.
	PhaseGrace Phase = "grace"
	// PhaseArmed: nadie conectado, contando el tiempo.
	PhaseArmed Phase = "armed"
	// PhaseUnknown: no se pudo medir la actividad; el contador está congelado.
	PhaseUnknown Phase = "unknown"
	// PhaseCountdown: queda poco para apagar (aviso).
	PhaseCountdown Phase = "countdown"
	// PhaseStopping: se ha decidido apagar.
	PhaseStopping Phase = "stopping"
	// PhaseStopped: apagado por inactividad.
	PhaseStopped Phase = "stopped_by_idle"
)

// UnknownWarnAfter es cuánto tiempo en estado desconocido antes de avisar.
const UnknownWarnAfter = 5 * time.Minute

// Observation es lo que se ha medido de la instancia en un tick.
type Observation struct {
	// Online es el número de jugadores conectados.
	Online int
	// Known indica si la medición fue posible. false → desconocido.
	Known bool
}

// Decision es el resultado de avanzar la máquina.
type Decision struct {
	// Phase es la fase resultante.
	Phase Phase
	// Previous es la fase anterior (para detectar cambios).
	Previous Phase
	// Stop indica que hay que apagar la instancia ahora.
	Stop bool
	// Remaining es el tiempo restante hasta el apagado (en countdown).
	Remaining time.Duration
	// Notice es un aviso puntual que se debe emitir una sola vez.
	Notice string
}

// Changed indica si la fase cambió en este tick.
func (d Decision) Changed() bool { return d.Phase != d.Previous }

// Machine es la máquina de estados por instancia. No es segura para uso
// concurrente: el vigilante la usa desde un único goroutine.
type Machine struct {
	cfg          Config
	startedAt    time.Time
	lastActiveAt time.Time
	lastTickAt   time.Time

	phase          Phase
	unknownSince   time.Time
	noticedUnknown bool
	stopped        bool
}

// NewMachine crea la máquina de una instancia arrancada en startedAt.
func NewMachine(cfg Config, startedAt time.Time) *Machine {
	cfg = cfg.Normalized()
	m := &Machine{
		cfg:        cfg,
		startedAt:  startedAt,
		lastTickAt: startedAt,
		phase:      PhaseGrace,
	}
	if cfg.FirstJoinGrace > 0 {
		m.lastActiveAt = startedAt.Add(cfg.FirstJoinGrace)
	} else {
		m.lastActiveAt = startedAt
	}
	if !cfg.Enabled {
		m.phase = PhaseDisabled
	}
	return m
}

// Phase devuelve la fase actual.
func (m *Machine) Phase() Phase { return m.phase }

// Config devuelve la configuración normalizada.
func (m *Machine) Config() Config { return m.cfg }

// Remaining devuelve el tiempo restante hasta el apagado según el reloj dado.
func (m *Machine) Remaining(now time.Time) time.Duration {
	if !m.cfg.Enabled || m.phase == PhaseStopped {
		return 0
	}
	if now.Before(m.lastActiveAt) {
		return m.cfg.Timeout
	}
	r := m.cfg.Timeout - now.Sub(m.lastActiveAt)
	if r < 0 {
		return 0
	}
	return r
}

// Tick avanza la máquina con una observación en el instante now.
func (m *Machine) Tick(now time.Time, obs Observation) Decision {
	prev := m.phase
	if !m.cfg.Enabled {
		m.phase = PhaseDisabled
		return Decision{Phase: PhaseDisabled, Previous: prev}
	}
	if m.stopped {
		m.phase = PhaseStopped
		return Decision{Phase: PhaseStopped, Previous: prev}
	}

	delta := now.Sub(m.lastTickAt)
	if delta < 0 {
		delta = 0
	}
	m.lastTickAt = now

	// Sin medición: se congela el contador (ante la duda, no apagar).
	if !obs.Known {
		m.lastActiveAt = m.lastActiveAt.Add(delta)
		if m.unknownSince.IsZero() {
			m.unknownSince = now
		}
		m.phase = PhaseUnknown
		d := Decision{Phase: PhaseUnknown, Previous: prev, Remaining: m.Remaining(now)}
		if !m.noticedUnknown && now.Sub(m.unknownSince) >= UnknownWarnAfter {
			m.noticedUnknown = true
			d.Notice = "no puedo comprobar los jugadores desde hace " + HumanDuration(UnknownWarnAfter) + "; no apagaré por inactividad hasta medirlo"
		}
		return d
	}
	m.unknownSince = time.Time{}
	m.noticedUnknown = false

	if obs.Online > 0 {
		m.lastActiveAt = now
		m.phase = PhaseArmed
		return Decision{Phase: PhaseArmed, Previous: prev}
	}

	// online == 0
	if now.Before(m.lastActiveAt) {
		m.phase = PhaseGrace
		return Decision{Phase: PhaseGrace, Previous: prev}
	}
	elapsed := now.Sub(m.lastActiveAt)
	if elapsed >= m.cfg.Timeout {
		m.phase = PhaseStopping
		m.stopped = true
		return Decision{Phase: PhaseStopping, Stop: true, Remaining: 0, Previous: prev}
	}
	remaining := m.cfg.Timeout - elapsed
	if remaining <= m.cfg.WarnBefore {
		m.phase = PhaseCountdown
		return Decision{Phase: PhaseCountdown, Remaining: remaining, Previous: prev}
	}
	m.phase = PhaseArmed
	return Decision{Phase: PhaseArmed, Remaining: remaining, Previous: prev}
}
