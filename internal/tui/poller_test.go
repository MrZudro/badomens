package tui

import (
	"testing"
	"time"

	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tui/svc"
)

func testPoller(t *testing.T) (*Poller, *time.Time) {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Poller{
		Svc:      svc.NewFake([]*instance.Spec{{Slug: "alpha", Name: "A", Engine: "paper", Version: "1.21", Memory: "4G", Port: 25565, MaxPlayers: 20}}),
		Now:      func() time.Time { return now },
		BaseTick: time.Second,
	}
	return p, &now
}

// TestPollerPresupuesto comprueba que el primer ciclo sondea estado y Docker, y
// que dentro de los intervalos no vuelve a sondear.
func TestPollerPresupuesto(t *testing.T) {
	p, now := testPoller(t)
	if cmd := p.Cmds(*now, true, true); cmd == nil {
		t.Fatal("el primer ciclo debe sondear")
	}
	// 1 s después: nada todavía (estado a 3 s, docker a 5 s).
	*now = now.Add(time.Second)
	if cmd := p.Cmds(*now, true, true); cmd != nil {
		t.Fatal("no debe sondear antes de los intervalos")
	}
	// 3 s: toca estado.
	*now = now.Add(2 * time.Second)
	if cmd := p.Cmds(*now, true, true); cmd == nil {
		t.Fatal("a los 3 s debe sondear el estado")
	}
	// 6 s: toca estado otra vez (Docker aún no, fue a los 0 s y toca a los 5 s:
	// sí toca). Avanzamos a 6 s.
	*now = now.Add(3 * time.Second)
	if cmd := p.Cmds(*now, true, true); cmd == nil {
		t.Fatal("a los 6 s debe sondear de nuevo")
	}
}

func TestPollerNoSondeaSiNoSeNecesita(t *testing.T) {
	p, now := testPoller(t)
	if cmd := p.Cmds(*now, false, false); cmd != nil {
		t.Fatal("sin necesidad no debe sondear")
	}
}

func TestPollerBackoffDocker(t *testing.T) {
	p, now := testPoller(t)
	p.Cmds(*now, false, true)
	for i := 0; i < 3; i++ {
		p.NoteDockerResult(false)
	}
	*now = now.Add(6 * time.Second)
	// Con backoff (30 s) no debe tocar Docker aunque hayan pasado 6 s.
	if cmd := p.Cmds(*now, false, true); cmd != nil {
		t.Fatal("con backoff no debe sondear Docker a los 6 s")
	}
	p.NoteDockerResult(true)
	*now = now.Add(31 * time.Second)
	if cmd := p.Cmds(*now, false, true); cmd == nil {
		t.Fatal("tras recuperarse debe volver a sondear")
	}
}

func TestPollerEjecutaComandos(t *testing.T) {
	p, now := testPoller(t)
	cmd := p.Cmds(*now, true, false)
	if cmd == nil {
		t.Fatal("quiero comando de estado")
	}
	msg := cmd()
	if _, ok := msg.(pollRowsMsg); !ok {
		t.Fatalf("mensaje inesperado: %T", msg)
	}
	// Docker
	p2, now2 := testPoller(t)
	cmd = p2.Cmds(*now2, false, true)
	msg = cmd()
	if _, ok := msg.(pollDockerMsg); !ok {
		t.Fatalf("mensaje inesperado: %T", msg)
	}
	if d, ok := msg.(pollDockerMsg); !ok || !d.diag.State.Ready() {
		t.Fatalf("el fake debe devolver Docker OK: %+v", d)
	}
}
