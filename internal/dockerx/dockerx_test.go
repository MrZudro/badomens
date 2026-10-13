package dockerx

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MrZudro/badomens/internal/execx"
)

// fakeEnv simula el entorno Docker: resultados por comando, PATH y procesos.
type fakeEnv struct {
	goos    string
	desktop bool
	inPath  bool
	results map[string]execx.Result
	errs    map[string]error
	calls   []string
	after   func(name string, args []string)
}

func newFake(goos string) *fakeEnv {
	return &fakeEnv{goos: goos, inPath: true, results: map[string]execx.Result{}, errs: map[string]error{}}
}

func (f *fakeEnv) env() Env {
	return Env{
		Run: func(_ context.Context, name string, args ...string) (execx.Result, error) {
			key := key(name, args)
			f.calls = append(f.calls, key)
			if f.after != nil {
				f.after(name, args)
			}
			return f.results[key], f.errs[key]
		},
		LookPath:     func(string) bool { return f.inPath },
		GOOS:         f.goos,
		DesktopAlive: func() bool { return f.desktop },
		Sleep:        func(context.Context, time.Duration) error { return nil },
	}
}

func key(name string, args []string) string { return name + " " + strings.Join(args, " ") }

func TestDiagnoseOK(t *testing.T) {
	f := newFake("linux")
	f.results[key("docker", []string{"info", "--format", "{{.ServerVersion}}"})] = execx.Result{Stdout: "26.1"}
	f.results[key("docker", []string{"compose", "version", "--short"})] = execx.Result{Stdout: "2.30"}
	f.results[key("docker", []string{"context", "show"})] = execx.Result{Stdout: "default"}
	d := Diagnose(context.Background(), f.env())
	if d.State != StateOK || !d.State.Ready() {
		t.Fatalf("quiero OK, tengo %+v", d)
	}
	if d.ServerVersion != "26.1" || d.ComposeVersion != "2.30" {
		t.Fatalf("versiones: %+v", d)
	}
}

func TestDiagnoseNotInstalled(t *testing.T) {
	f := newFake("linux")
	f.inPath = false
	d := Diagnose(context.Background(), f.env())
	if d.State != StateNotInstalled {
		t.Fatalf("quiero NotInstalled, tengo %+v", d)
	}
	if d.State.Reason() != "docker_not_installed" {
		t.Fatalf("reason: %q", d.State.Reason())
	}
}

func TestClassifyFixtures(t *testing.T) {
	daemonErr := errors.New("exit 1")
	cases := []struct {
		name string
		in   classifyInput
		want State
	}{
		{
			name: "linux sin daemon",
			in:   classifyInput{InfoErr: daemonErr, InfoStderr: "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?"},
			want: StateDaemonDown,
		},
		{
			name: "linux permiso denegado",
			in:   classifyInput{InfoErr: daemonErr, InfoStderr: "Got permission denied while trying to connect to the Docker daemon socket at unix:///var/run/docker.sock"},
			want: StatePermissionDenied,
		},
		{
			name: "windows pipe ausente",
			in:   classifyInput{InfoErr: daemonErr, InfoStderr: "error during connect: This error may indicate that the docker daemon is not running.: open //./pipe/docker_engine: The system cannot find the file specified."},
			want: StateDaemonDown,
		},
		{
			name: "escritorio arrancando",
			in:   classifyInput{InfoErr: daemonErr, InfoStderr: "error during connect: connection refused", DesktopAlive: true},
			want: StateStarting,
		},
		{
			name: "contexto inalcanzable",
			in:   classifyInput{InfoErr: daemonErr, InfoStderr: "failed to connect to the docker API at tcp://10.0.0.9:2375; context unreachable: no such host"},
			want: StateWrongContext,
		},
		{
			name: "virtualizacion apagada",
			in:   classifyInput{InfoErr: daemonErr, InfoStderr: "WSL2 is not available. Please enable virtualization in BIOS."},
			want: StateVirtualizationOff,
		},
		{
			name: "compose ausente",
			in:   classifyInput{InfoErr: nil, ComposeErr: errors.New("exit 1"), ComposeStderr: "docker: 'compose' is not a docker command"},
			want: StateComposeMissing,
		},
	}
	for _, tc := range cases {
		if got := classify(tc.in); got != tc.want {
			t.Fatalf("%s: quiero %s, tengo %s", tc.name, tc.want, got)
		}
	}
}

func TestPlanPorPlataforma(t *testing.T) {
	for _, platform := range []string{"linux", "darwin", "windows"} {
		steps := Plan(Diagnosis{State: StateDaemonDown, Platform: platform}, platform)
		if len(steps) == 0 {
			t.Fatalf("%s: quiero pasos de arranque", platform)
		}
	}
	if got := Plan(Diagnosis{State: StateComposeMissing}, "linux"); len(got) == 0 {
		t.Fatal("compose ausente debe tener plan")
	}
}

func TestEnsureLevantaYPasaAReady(t *testing.T) {
	f := newFake("linux")
	infoKey := key("docker", []string{"info", "--format", "{{.ServerVersion}}"})
	composeKey := key("docker", []string{"compose", "version", "--short"})
	f.errs[infoKey] = errors.New("exit 1")
	f.results[infoKey] = execx.Result{Stderr: "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?"}
	f.desktop = false
	// Tras ejecutar cualquier paso de reparación, el daemon responde.
	f.after = func(name string, args []string) {
		if name == "systemctl" {
			delete(f.errs, infoKey)
			f.results[infoKey] = execx.Result{Stdout: "26.1"}
			f.results[composeKey] = execx.Result{Stdout: "2.30"}
		}
	}
	res := Ensure(context.Background(), f.env(), Options{Timeout: time.Second})
	if !res.Ready {
		t.Fatalf("quiero Ready tras reparar: %+v", res)
	}
	if len(res.Attempts) == 0 {
		t.Fatal("quiero al menos un intento registrado")
	}
	found := false
	for _, c := range f.calls {
		if c == "systemctl start docker" {
			found = true
		}
	}
	if !found {
		t.Fatalf("quiero intentar systemctl start docker: %v", f.calls)
	}
}

func TestEnsurePendienteSiNoSeAutomatiza(t *testing.T) {
	f := newFake("windows")
	infoKey := key("docker", []string{"info", "--format", "{{.ServerVersion}}"})
	f.errs[infoKey] = errors.New("exit 1")
	f.results[infoKey] = execx.Result{Stderr: "error during connect: open //./pipe/docker_engine: The system cannot find the file specified."}
	res := Ensure(context.Background(), f.env(), Options{Timeout: time.Second})
	if res.Ready {
		t.Fatal("no debe declararse listo sin daemon")
	}
	if len(res.Pending) == 0 {
		t.Fatal("quiero pasos pendientes de intervención")
	}
}
