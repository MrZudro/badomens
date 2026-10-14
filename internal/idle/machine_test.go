package idle

import (
	"context"
	"testing"
	"time"

	"github.com/MrZudro/badomens/internal/events"
)

var start = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func cfg5() Config {
	return Config{Enabled: true, Timeout: 5 * time.Minute, WarnBefore: 30 * time.Second}
}

var obs0 = Observation{Online: 0, Known: true}

func TestMachineApagaTrasTimeout(t *testing.T) {
	m := NewMachine(cfg5(), start)
	if m.Phase() != PhaseGrace {
		t.Fatalf("inicial: quiero Grace, tengo %s", m.Phase())
	}
	d := m.Tick(start, obs0)
	if d.Phase != PhaseArmed {
		t.Fatalf("sin jugadores al arrancar: quiero Armed, tengo %s", d.Phase)
	}
	d = m.Tick(start.Add(4*time.Minute+40*time.Second), obs0)
	if d.Phase != PhaseCountdown {
		t.Fatalf("al cuarto minuto quiero Countdown, tengo %s (r=%s)", d.Phase, d.Remaining)
	}
	if d.Remaining > 30*time.Second {
		t.Fatalf("la cuenta atrás debe ser ≤ 30 s: %s", d.Remaining)
	}
	d = m.Tick(start.Add(5*time.Minute), obs0)
	if !d.Stop {
		t.Fatalf("tras 5 min sin jugadores quiero Stop, tengo %+v", d)
	}
	d = m.Tick(start.Add(6*time.Minute), obs0)
	if d.Stop {
		t.Fatalf("no se debe repetir el apagado: %+v", d)
	}
	if m.Phase() != PhaseStopped {
		t.Fatalf("fase final: quiero Stopped, tengo %s", m.Phase())
	}
}

func TestMachineJugadorReiniciaContador(t *testing.T) {
	m := NewMachine(cfg5(), start)
	_ = m.Tick(start, obs0)
	// Alguien entra a los 4m50s: se reinicia.
	_ = m.Tick(start.Add(4*time.Minute+50*time.Second), Observation{Online: 1, Known: true})
	if m.Phase() != PhaseArmed {
		t.Fatalf("con jugadores quiero Armed, tengo %s", m.Phase())
	}
	// El contador vuelve a contar desde la entrada (¡no acumula los 4m50s!).
	d := m.Tick(start.Add(5*time.Minute+50*time.Second), obs0) // 1m de inactividad
	if d.Phase != PhaseArmed {
		t.Fatalf("con 1m de inactividad quiero Armed, tengo %s", d.Phase)
	}
	d = m.Tick(start.Add(9*time.Minute+49*time.Second), obs0) // 4m59s de inactividad
	if d.Phase != PhaseCountdown {
		t.Fatalf("a los 4m59s quiero Countdown, tengo %s", d.Phase)
	}
	d = m.Tick(start.Add(9*time.Minute+50*time.Second), obs0) // 5m de inactividad
	if !d.Stop {
		t.Fatalf("tras 5 min de la salida quiero Stop, tengo %+v", d)
	}
}

func TestMachineSigueEnGraceAntesDeTocarElContador(t *testing.T) {
	cfg := Config{Enabled: true, Timeout: 10 * time.Minute, FirstJoinGrace: 10 * time.Minute, WarnBefore: time.Minute}
	m := NewMachine(cfg, start)
	d := m.Tick(start.Add(5*time.Minute), obs0)
	if d.Phase != PhaseGrace {
		t.Fatalf("durante la gracia quiero Grace, tengo %s", d.Phase)
	}
	d = m.Tick(start.Add(9*time.Minute+59*time.Second), obs0)
	if d.Phase != PhaseGrace {
		t.Fatalf("justo antes de la gracia: %s", d.Phase)
	}
	d = m.Tick(start.Add(19*time.Minute+59*time.Second), obs0)
	if d.Phase != PhaseCountdown {
		t.Fatalf("a los 19m59s quiero Countdown, tengo %s", d.Phase)
	}
	d = m.Tick(start.Add(20*time.Minute), obs0)
	if !d.Stop {
		t.Fatalf("a los 20m quiero Stop, tengo %+v", d)
	}
}

func TestMachineUnknownCongelaContador(t *testing.T) {
	m := NewMachine(cfg5(), start)
	_ = m.Tick(start, obs0)
	// Desde los 2m no se puede medir; el contador queda congelado.
	_ = m.Tick(start.Add(2*time.Minute), Observation{Known: false})
	if m.Phase() != PhaseUnknown {
		t.Fatalf("sin medición quiero Unknown, tengo %s", m.Phase())
	}
	_ = m.Tick(start.Add(4*time.Minute), Observation{Known: false})
	// Vuelve la medición: solo han pasado 30 s desde la última actividad real.
	d := m.Tick(start.Add(4*time.Minute+30*time.Second), obs0)
	if d.Phase != PhaseArmed {
		t.Fatalf("30 s de inactividad real deben dejar Armed, tengo %s", d.Phase)
	}
	if r := m.Remaining(start.Add(4*time.Minute + 30*time.Second)); r > 5*time.Minute-30*time.Second {
		t.Fatalf("restante: %s (debe ser ≈ 4m30s)", r)
	}
}

func TestMachineUnknownNuncaApaga(t *testing.T) {
	m := NewMachine(cfg5(), start)
	_ = m.Tick(start, obs0)
	var warned bool
	for i := 1; i <= 30; i++ {
		d := m.Tick(start.Add(time.Duration(i)*time.Minute), Observation{Known: false})
		if d.Stop {
			t.Fatalf("con medición desconocida nunca debe apagar (minuto %d)", i)
		}
		if d.Notice != "" {
			warned = true
		}
	}
	if !warned {
		t.Fatal("quiero el aviso de estado desconocido tras 5 min")
	}
}

func TestMachineDeshabilitadaNoHaceNada(t *testing.T) {
	m := NewMachine(Config{Enabled: false}, start)
	for _, obs := range []Observation{obs0, {Online: 5, Known: true}, {Known: false}} {
		if d := m.Tick(start.Add(time.Minute), obs); d.Stop {
			t.Fatalf("deshabilitada no debe apagar: %+v", d)
		}
	}
	if m.Phase() != PhaseDisabled {
		t.Fatalf("fase: %s", m.Phase())
	}
}

func TestWatcherApagaYPublicaEvento(t *testing.T) {
	bus := events.New()
	defer bus.Close()
	ch, cancel := bus.Subscribe(2)
	defer cancel()

	ctrl := &fakeController{calls: map[string]int{}}
	now := start
	w := NewWatcher()
	w.Clock = func() time.Time { return now }
	w.Counter = CounterFunc(func(context.Context, string) (int, bool, error) { return 0, true, nil })
	w.Controller = ctrl
	w.Bus = bus
	w.Targets = func(context.Context) ([]Target, error) {
		return []Target{{Instance: "alpha", Config: cfg5(), Running: true, StartedAt: start}}, nil
	}

	w.TickOnce(context.Background())
	now = start.Add(5 * time.Minute)
	ds := w.TickOnce(context.Background())
	if len(ds) != 1 || !ds[0].Stop {
		t.Fatalf("quiero un Stop, tengo %+v", ds)
	}
	if ctrl.calls["stop:alpha"] != 1 {
		t.Fatalf("quiero un stop registrado, tengo %v", ctrl.calls)
	}
	select {
	case e := <-ch:
		if e.Kind != events.IdleStopped || e.Instance != "alpha" {
			t.Fatalf("evento: %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("quiero IdleStopped publicado")
	}
}

type fakeController struct {
	calls map[string]int
}

func (f *fakeController) Running(context.Context, string) (bool, error) { return true, nil }
func (f *fakeController) Flush(_ context.Context, i string) error       { f.calls["flush:"+i]++; return nil }

func (f *fakeController) Stop(_ context.Context, i string) error { f.calls["stop:"+i]++; return nil }

func (f *fakeController) Backup(_ context.Context, i string) error {
	f.calls["backup:"+i]++
	return nil
}
