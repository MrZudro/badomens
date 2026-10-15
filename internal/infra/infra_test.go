package infra

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MrZudro/badomens/internal/execx"
)

// fakeComp implementa Component controlando el estado y el plan desde el test.
type fakeComp struct {
	domain Domain
	detect func() State
	plan   []Step
	hint   string
	manual []string
}

func (f fakeComp) Domain() Domain                 { return f.domain }
func (f fakeComp) Detect(context.Context) State   { return f.detect() }
func (f fakeComp) StartPlan(State, string) []Step { return f.plan }
func (f fakeComp) InstallHint(string) string      { return f.hint }
func (f fakeComp) ManualGuide(State, string) []string {
	if f.manual == nil {
		return []string{"paso manual del SO"}
	}
	return f.manual
}

// fakeEnv es un Env inyectable que registra los procesos lanzados.
type fakeEnv struct {
	goos       string
	elevated   bool
	runCalls   [][]string
	spawnCalls [][]string
	onRun      func(name string, args []string)
	sleepErr   error
}

func (f *fakeEnv) env() Env {
	return Env{
		GOOS:     f.goos,
		Elevated: func() bool { return f.elevated },
		LookPath: func(string) bool { return true },
		Sleep:    func(context.Context, time.Duration) error { return f.sleepErr },
		Run: func(_ context.Context, name string, args ...string) (execx.Result, error) {
			f.runCalls = append(f.runCalls, append([]string{name}, args...))
			if f.onRun != nil {
				f.onRun(name, args)
			}
			return execx.Result{}, nil
		},
		Spawn: func(_ context.Context, exe string, args ...string) error {
			f.spawnCalls = append(f.spawnCalls, append([]string{exe}, args...))
			return nil
		},
	}
}

func TestEnsureReadyNoAction(t *testing.T) {
	f := &fakeEnv{goos: "linux"}
	c := fakeComp{domain: DomainDocker, detect: func() State { return State{Status: StatusReady, Message: "listo"} }}
	rep := Ensure(context.Background(), f.env(), c, Options{})
	if !rep.Ready || rep.Status != StatusReady {
		t.Fatalf("quiero ready, tengo %+v", rep)
	}
	if len(f.runCalls) != 0 {
		t.Fatalf("no debe lanzar nada si ya está listo: %v", f.runCalls)
	}
}

func TestEnsureNotInstalledNoSilentInstall(t *testing.T) {
	f := &fakeEnv{goos: "linux"}
	c := fakeComp{
		domain: DomainDocker,
		detect: func() State { return State{Status: StatusNotInstalled, Message: "falta"} },
		hint:   "instala Docker desde la web",
	}
	rep := Ensure(context.Background(), f.env(), c, Options{})
	if rep.Status != StatusNotInstalled {
		t.Fatalf("quiero not_installed, tengo %+v", rep)
	}
	if len(f.runCalls) != 0 {
		t.Fatalf("jamás debe instalar en silencio: %v", f.runCalls)
	}
	if len(rep.Pending) == 0 || !strings.Contains(rep.Pending[0].Instruction, "instala") {
		t.Fatalf("quiero la instrucción de instalación: %+v", rep.Pending)
	}
	if len(rep.Manual) == 0 {
		t.Fatalf("quiero pasos manuales: %+v", rep)
	}
}

func TestEnsureStartsAndWaits(t *testing.T) {
	f := &fakeEnv{goos: "linux"}
	up := false
	f.onRun = func(name string, args []string) {
		if name == "systemctl" {
			up = true
		}
	}
	c := fakeComp{
		domain: DomainDocker,
		detect: func() State {
			if up {
				return State{Status: StatusReady, Message: "listo"}
			}
			return State{Status: StatusDown, Message: "apagado"}
		},
		plan: []Step{{Description: "arrancar", Command: []string{"systemctl", "start", "docker"}}},
	}
	rep := Ensure(context.Background(), f.env(), c, Options{Timeout: time.Second, PollInterval: time.Millisecond})
	if !rep.Ready {
		t.Fatalf("quiero ready tras arrancar, tengo %+v", rep)
	}
	if len(f.runCalls) != 1 || strings.Join(f.runCalls[0], " ") != "systemctl start docker" {
		t.Fatalf("comando inesperado: %v", f.runCalls)
	}
	if len(rep.Phases) < 3 {
		t.Fatalf("quiero fases detect/start/wait: %+v", rep.Phases)
	}
}

func TestEnsureElevationRefusedDegrades(t *testing.T) {
	f := &fakeEnv{goos: "linux"}
	c := fakeComp{
		domain: DomainDocker,
		detect: func() State { return State{Status: StatusDown, Message: "apagado"} },
		plan: []Step{{
			Description:       "con permisos",
			Command:           []string{"sudo", "-n", "systemctl", "start", "docker"},
			RequiresElevation: true,
		}},
	}
	rep := Ensure(context.Background(), f.env(), c, Options{AllowElevation: false})
	if len(f.runCalls) != 0 {
		t.Fatalf("sin autorización no debe elevar: %v", f.runCalls)
	}
	if !rep.NeedsElevation || !rep.Degraded {
		t.Fatalf("quiero degradación con aviso: %+v", rep)
	}
	if rep.Status != StatusNeedsElevation {
		t.Fatalf("quiero needs_elevation, tengo %s", rep.Status)
	}
	if len(rep.Pending) == 0 {
		t.Fatalf("quiero el paso pendiente explicado: %+v", rep)
	}
}

func TestEnsureElevationAllowedRuns(t *testing.T) {
	f := &fakeEnv{goos: "linux"}
	up := false
	f.onRun = func(name string, args []string) {
		if name == "sudo" {
			up = true
		}
	}
	c := fakeComp{
		domain: DomainDocker,
		detect: func() State {
			if up {
				return State{Status: StatusReady, Message: "listo"}
			}
			return State{Status: StatusDown, Message: "apagado"}
		},
		plan: []Step{{
			Description:       "con permisos",
			Command:           []string{"sudo", "-n", "systemctl", "start", "docker"},
			RequiresElevation: true,
		}},
	}
	rep := Ensure(context.Background(), f.env(), c, Options{AllowElevation: true, Timeout: time.Second, PollInterval: time.Millisecond})
	if !rep.Ready {
		t.Fatalf("con autorización debe elevar y quedar listo: %+v", rep)
	}
	if len(f.runCalls) != 1 || f.runCalls[0][0] != "sudo" {
		t.Fatalf("quiero el paso elevado: %v", f.runCalls)
	}
}

func TestEnsureTimeoutGivesCauseAndManual(t *testing.T) {
	f := &fakeEnv{goos: "windows", sleepErr: errors.New("fin de espera")}
	c := fakeComp{
		domain: DomainDocker,
		detect: func() State { return State{Status: StatusDown, Reason: "docker_daemon_down", Message: "apagado"} },
		plan:   []Step{{Description: "abrir", Instruction: "abre Docker Desktop"}},
		manual: []string{"Abre Docker Desktop desde el menú Inicio."},
	}
	rep := Ensure(context.Background(), f.env(), c, Options{Timeout: 10 * time.Millisecond, PollInterval: time.Millisecond})
	if rep.Ready {
		t.Fatal("no debe declararse listo")
	}
	if rep.Reason != "docker_daemon_down" || len(rep.Manual) == 0 {
		t.Fatalf("quiero causa y pasos manuales: %+v", rep)
	}
}

func TestEnsureDetachedStepUsesSpawn(t *testing.T) {
	f := &fakeEnv{goos: "linux"}
	up := false
	c := fakeComp{
		domain: DomainDaemon,
		detect: func() State {
			if up {
				return State{Status: StatusReady, Message: "en marcha"}
			}
			return State{Status: StatusDown, Message: "parado"}
		},
		plan: []Step{{Description: "daemon de sesión", Command: []string{"badomens", "idle", "daemon"}, Detached: true}},
	}
	// El spawn de prueba levanta el estado.
	env := f.env()
	env.Spawn = func(_ context.Context, exe string, args ...string) error {
		f.spawnCalls = append(f.spawnCalls, append([]string{exe}, args...))
		up = true
		return nil
	}
	rep := Ensure(context.Background(), env, c, Options{Timeout: time.Second, PollInterval: time.Millisecond})
	if !rep.Ready {
		t.Fatalf("quiero ready tras spawn: %+v", rep)
	}
	if len(f.runCalls) != 0 || len(f.spawnCalls) != 1 {
		t.Fatalf("quiero spawn y no run: run=%v spawn=%v", f.runCalls, f.spawnCalls)
	}
	if strings.Join(f.spawnCalls[0], " ") != "badomens idle daemon" {
		t.Fatalf("argv del daemon inesperado: %v", f.spawnCalls[0])
	}
}

func TestEnsureCancelable(t *testing.T) {
	f := &fakeEnv{goos: "linux"}
	ctx, cancel := context.WithCancel(context.Background())
	c := fakeComp{
		domain: DomainDocker,
		detect: func() State { return State{Status: StatusDown, Message: "apagado"} },
		plan:   []Step{{Description: "arrancar", Command: []string{"systemctl", "start", "docker"}}},
	}
	env := f.env()
	env.Sleep = func(context.Context, time.Duration) error { cancel(); return context.Canceled }
	ch := EnsureAsync(ctx, env, c, Options{Timeout: time.Minute, PollInterval: time.Millisecond})
	select {
	case rep := <-ch:
		if !rep.Canceled || rep.Ready {
			t.Fatalf("quiero cancelado y no listo: %+v", rep)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("EnsureAsync no respetó la cancelación")
	}
}

func TestEnsureProgressReportsStarting(t *testing.T) {
	f := &fakeEnv{goos: "linux"}
	var sawStarting bool
	c := fakeComp{
		domain: DomainDocker,
		detect: func() State { return State{Status: StatusDown, Message: "apagado"} },
		plan:   []Step{{Description: "arrancar", Command: []string{"systemctl", "start", "docker"}}},
		manual: []string{"x"},
	}
	Ensure(context.Background(), f.env(), c, Options{
		Timeout:      10 * time.Millisecond,
		PollInterval: time.Millisecond,
		Progress: func(p Progress) {
			if p.Status == StatusStarting {
				sawStarting = true
			}
		},
	})
	if !sawStarting {
		t.Fatal("quiero el aviso de estado «Cargando»")
	}
}
