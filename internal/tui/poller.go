package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/svc"
)

// Mensajes de la app v2 (prefijo propio para no chocar con los del modelo
// antiguo, que sigue disponible para --snapshot).
type pollTickMsg struct{ At time.Time }

type pollRowsMsg struct {
	rows []svc.Row
	err  error
}

type pollDockerMsg struct {
	diag dockerx.Diagnosis
	err  error
}

type toastMsg struct {
	Text string
	Kind components.Kind
}

// Poller central (RF-1/§3.3): un único presupuesto de sondeo. El estado de las
// instancias se refresca cada 3 s, Docker cada 5 s (con backoff). Un solo
// Poller evita lanzar N `docker ps`.
type Poller struct {
	Svc *svc.Services

	// Intervalos por defecto (0 = valor de la spec).
	StateEvery  time.Duration
	DockerEvery time.Duration
	// BaseTick es el latido del reloj del Poller.
	BaseTick time.Duration

	Now func() time.Time

	lastState  time.Time
	lastDocker time.Time
	started    bool
	// FailStreak cuenta los fallos consecutivos de Docker para el backoff.
	failStreak int
}

// Base devuelve el intervalo de latido.
func (p *Poller) Base() time.Duration {
	if p.BaseTick > 0 {
		return p.BaseTick
	}
	return time.Second
}

func (p *Poller) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// Schedule agenda el siguiente latido.
func (p *Poller) Schedule() tea.Cmd {
	return tea.Tick(p.Base(), func(t time.Time) tea.Msg { return pollTickMsg{At: t} })
}

// Cmds devuelve los comandos de sondeo que tocan en `now`. running indica si la
// pantalla necesita métricas/estado; si no, no se sondea Docker.
func (p *Poller) Cmds(now time.Time, needState, needDocker bool) tea.Cmd {
	if p.Svc == nil {
		return nil
	}
	var cmds []tea.Cmd
	stateEvery := p.StateEvery
	if stateEvery <= 0 {
		stateEvery = 3 * time.Second
	}
	dockerEvery := p.DockerEvery
	if dockerEvery <= 0 {
		dockerEvery = 5 * time.Second
	}
	if p.failStreak >= 3 {
		dockerEvery = 30 * time.Second // backoff cuando Docker lleva cayéndose
	}

	if needState && (!p.started || now.Sub(p.lastState) >= stateEvery) {
		p.lastState = now
		cmds = append(cmds, p.rowsCmd())
	}
	if needDocker && (p.lastDocker.IsZero() || now.Sub(p.lastDocker) >= dockerEvery) {
		p.lastDocker = now
		cmds = append(cmds, p.dockerCmd())
	}
	p.started = true
	return tea.Batch(cmds...)
}

func (p *Poller) rowsCmd() tea.Cmd {
	svcRef := p.Svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		rows, err := svcRef.Instances.Rows(ctx)
		return pollRowsMsg{rows: rows, err: err}
	}
}

func (p *Poller) dockerCmd() tea.Cmd {
	svcRef := p.Svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		diag, err := svcRef.DockerState.Status(ctx)
		return pollDockerMsg{diag: diag, err: err}
	}
}

// NoteDockerResult actualiza el backoff según el resultado de Docker.
func (p *Poller) NoteDockerResult(ok bool) {
	if ok {
		p.failStreak = 0
		return
	}
	p.failStreak++
}
