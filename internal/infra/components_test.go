package infra

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/execx"
	"github.com/MrZudro/badomens/internal/tailscale"
)

// dockerEnv construye un dockerx.Env inyectable con resultados por comando.
func dockerEnv(goos string, inPath bool, desktop bool, results map[string]execx.Result, errs map[string]error) dockerx.Env {
	return dockerx.Env{
		GOOS:         goos,
		LookPath:     func(string) bool { return inPath },
		DesktopAlive: func() bool { return desktop },
		Sleep:        func(context.Context, time.Duration) error { return nil },
		Run: func(_ context.Context, name string, args ...string) (execx.Result, error) {
			k := name + " " + strings.Join(args, " ")
			return results[k], errs[k]
		},
	}
}

func k(name string, args ...string) string { return name + " " + strings.Join(args, " ") }

func TestDockerStateMapping(t *testing.T) {
	infoK := k("docker", "info", "--format", "{{.ServerVersion}}")
	composeK := k("docker", "compose", "version", "--short")

	cases := []struct {
		name    string
		inPath  bool
		results map[string]execx.Result
		errs    map[string]error
		want    Status
	}{
		{
			name:    "listo",
			inPath:  true,
			results: map[string]execx.Result{infoK: {Stdout: "26.1"}, composeK: {Stdout: "2.30"}},
			want:    StatusReady,
		},
		{
			name:   "no instalado",
			inPath: false,
			want:   StatusNotInstalled,
		},
		{
			name:   "daemon apagado",
			inPath: true,
			errs:   map[string]error{infoK: errors.New("exit 1")},
			results: map[string]execx.Result{
				infoK: {Stderr: "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?"},
			},
			want: StatusDown,
		},
		{
			name:   "permiso denegado",
			inPath: true,
			errs:   map[string]error{infoK: errors.New("exit 1")},
			results: map[string]execx.Result{
				infoK: {Stderr: "Got permission denied while trying to connect to the Docker daemon socket"},
			},
			want: StatusNeedsElevation,
		},
		{
			name:   "compose ausente",
			inPath: true,
			errs:   map[string]error{composeK: errors.New("exit 1")},
			results: map[string]execx.Result{
				infoK:    {Stdout: "26.1"},
				composeK: {Stderr: "docker: 'compose' is not a docker command"},
			},
			want: StatusDegraded,
		},
		{
			name:   "virtualizacion apagada",
			inPath: true,
			errs:   map[string]error{infoK: errors.New("exit 1")},
			results: map[string]execx.Result{
				infoK: {Stderr: "WSL2 is not available. Please enable virtualization in BIOS."},
			},
			want: StatusUnavailable,
		},
	}
	for _, tc := range cases {
		d := NewDocker(dockerEnv("linux", tc.inPath, false, tc.results, tc.errs))
		st := d.Detect(context.Background())
		if st.Status != tc.want {
			t.Fatalf("%s: quiero %s, tengo %s (%+v)", tc.name, tc.want, st.Status, st)
		}
		if st.Domain != DomainDocker {
			t.Fatalf("%s: dominio %s", tc.name, st.Domain)
		}
	}
}

func TestDockerDetectPermissionMarksElevation(t *testing.T) {
	infoK := k("docker", "info", "--format", "{{.ServerVersion}}")
	d := NewDocker(dockerEnv("linux", true, false,
		map[string]execx.Result{infoK: {Stderr: "permission denied"}},
		map[string]error{infoK: errors.New("exit 1")}))
	st := d.Detect(context.Background())
	if st.Status != StatusNeedsElevation || !st.Elevation {
		t.Fatalf("quiero needs_elevation con elevación: %+v", st)
	}
}

func TestDockerEnsureArrancaConFake(t *testing.T) {
	infoK := k("docker", "info", "--format", "{{.ServerVersion}}")
	composeK := k("docker", "compose", "version", "--short")
	results := map[string]execx.Result{
		infoK: {Stderr: "Cannot connect to the Docker daemon. Is the docker daemon running?"},
	}
	errs := map[string]error{infoK: errors.New("exit 1")}
	denv := dockerEnv("linux", true, false, results, errs)
	// El motor de infra lanza los pasos de arranque con su propio Env: al ver
	// 'systemctl', el entorno falso hace que docker responda.
	fe := &fakeEnv{goos: "linux"}
	fe.onRun = func(name string, _ []string) {
		if name == "systemctl" {
			delete(errs, infoK)
			results[infoK] = execx.Result{Stdout: "26.1"}
			results[composeK] = execx.Result{Stdout: "2.30"}
		}
	}
	rep := Ensure(context.Background(), fe.env(), NewDocker(denv),
		Options{Timeout: time.Second, PollInterval: time.Millisecond})
	if !rep.Ready {
		t.Fatalf("quiero Docker listo tras arrancar: %+v", rep)
	}
}

// tailscaleEnv construye un tailscale.Env inyectable.
func tailscaleEnv(inPath bool, stdout string, runErr error) tailscale.Env {
	return tailscale.Env{
		LookPath: func(string) bool { return inPath },
		Run: func(_ context.Context, name string, args ...string) (execx.Result, error) {
			return execx.Result{Stdout: stdout}, runErr
		},
	}
}

func TestTailscaleStateMapping(t *testing.T) {
	runningJSON := `{"BackendState":"Running","Self":{"HostName":"maquina","DNSName":"maquina.tail1234.ts.net.","TailscaleIPs":["100.64.0.1"],"Online":true}}`
	noMagicJSON := `{"BackendState":"Running","Self":{"HostName":"maquina","DNSName":"maquina.tail1234.ts.net.invalid.","TailscaleIPs":["100.64.0.1"],"Online":true}}`
	noIPJSON := `{"BackendState":"Running","Self":{"HostName":"maquina","DNSName":"maquina.tail1234.ts.net.","TailscaleIPs":[],"Online":true}}`
	loginJSON := `{"BackendState":"NeedsLogin","Self":null}`
	stoppedJSON := `{"BackendState":"Stopped","Self":null}`

	cases := []struct {
		name      string
		inPath    bool
		stdout    string
		runErr    error
		want      Status
		wantMagic bool
	}{
		{name: "no instalado", inPath: false, want: StatusNotInstalled},
		{name: "servicio no responde", inPath: true, runErr: errors.New("exit 1"), want: StatusDown},
		{name: "listo", inPath: true, stdout: runningJSON, want: StatusReady, wantMagic: true},
		{name: "magicdns desactivado", inPath: true, stdout: noMagicJSON, want: StatusReady, wantMagic: false},
		{name: "sin ip", inPath: true, stdout: noIPJSON, want: StatusDegraded},
		{name: "necesita login", inPath: true, stdout: loginJSON, want: StatusNeedsLogin},
		{name: "detenido", inPath: true, stdout: stoppedJSON, want: StatusDown},
	}
	for _, tc := range cases {
		c := NewTailscale(tailscaleEnv(tc.inPath, tc.stdout, tc.runErr))
		st := c.Detect(context.Background())
		if st.Status != tc.want {
			t.Fatalf("%s: quiero %s, tengo %s (%+v)", tc.name, tc.want, st.Status, st)
		}
		if tc.wantMagic {
			if v, _ := st.Extra["magic_dns_enabled"].(bool); !v {
				t.Fatalf("%s: quiero magic_dns_enabled=true: %+v", tc.name, st.Extra)
			}
		}
	}
}

func TestTailscaleMagicDNSReasonIsTyped(t *testing.T) {
	noMagicJSON := `{"BackendState":"Running","Self":{"DNSName":"maquina.tail1234.ts.net.invalid.","TailscaleIPs":["100.64.0.1"],"Online":true}}`
	rep := tailscale.ProbeWith(context.Background(), tailscaleEnv(true, noMagicJSON, nil))
	if rep.MagicDNSEnabled || rep.MagicDNSReason != tailscale.ReasonMagicDNSDisabled {
		t.Fatalf("quiero motivo tipado magicdns_disabled: %+v", rep)
	}
	okJSON := `{"BackendState":"Running","Self":{"DNSName":"maquina.tail1234.ts.net.","TailscaleIPs":["100.64.0.1"],"Online":true}}`
	rep = tailscale.ProbeWith(context.Background(), tailscaleEnv(true, okJSON, nil))
	if !rep.MagicDNSEnabled || rep.MagicDNSReason != tailscale.ReasonMagicDNSOK {
		t.Fatalf("quiero magicdns_ok: %+v", rep)
	}
}

func TestDaemonDetectAndSessionStart(t *testing.T) {
	dir := t.TempDir()
	lock := DaemonLockPath(dir)

	// Sin latido: apagado.
	c := NewDaemon(DaemonOptions{Exe: "badomens", LockPath: lock})
	if st := c.Detect(context.Background()); st.Status != StatusDown {
		t.Fatalf("quiero down sin latido: %+v", st)
	}

	// Sin binario: no instalado.
	if st := NewDaemon(DaemonOptions{}).Detect(context.Background()); st.Status != StatusNotInstalled {
		t.Fatalf("quiero not_installed sin binario: %+v", st)
	}

	// El spawn simulado crea el latido: debe quedar listo.
	f := &fakeEnv{goos: "linux"}
	env := f.env()
	env.Spawn = func(_ context.Context, exe string, args ...string) error {
		f.spawnCalls = append(f.spawnCalls, append([]string{exe}, args...))
		return writeHeartbeat(lock)
	}
	rep := Ensure(context.Background(), env, NewDaemon(DaemonOptions{Exe: "badomens", LockPath: lock}),
		Options{Timeout: time.Second, PollInterval: time.Millisecond})
	if !rep.Ready {
		t.Fatalf("quiero daemon en marcha tras el spawn: %+v", rep)
	}
	if len(rep.Attempts) == 0 || !rep.Attempts[0].OK {
		t.Fatalf("quiero el intento de spawn registrado: %+v", rep.Attempts)
	}
}

func TestDaemonServiceStartUsesServiceArgs(t *testing.T) {
	spec, err := DaemonService("linux", "/home/ana", "/opt/badomens", ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	c := NewDaemon(DaemonOptions{Exe: "/opt/badomens", Service: &spec, Installed: func() bool { return true }})
	plan := c.StartPlan(State{Status: StatusDown}, "linux")
	if len(plan) != 1 {
		t.Fatalf("quiero un paso: %+v", plan)
	}
	got := strings.Join(plan[0].Command, " ")
	if got != "systemctl --user start badomens-idle.service" {
		t.Fatalf("argv del servicio inesperado: %q", got)
	}
	if plan[0].Detached {
		t.Fatal("arrancar el servicio no debe ser un spawn desacoplado")
	}
}

func TestNoShellConcatenationInPlans(t *testing.T) {
	metachars := []string{"&&", "||", ";", "|", ">", "<", "`", "$(", "\n"}
	// Herramientas que siempre deben recibir sus argumentos por separado.
	multiArg := map[string]bool{
		"systemctl": true, "sudo": true, "sc": true, "service": true,
		"launchctl": true, "schtasks": true, "tailscale": true, "docker": true, "open": true,
	}
	check := func(name string, steps []Step) {
		for _, s := range steps {
			if len(s.Command) == 0 {
				continue
			}
			for _, a := range s.Command {
				if strings.TrimSpace(a) == "" {
					t.Fatalf("%s: argumento vacío en el paso %q: %v", name, s.Description, s.Command)
				}
				for _, m := range metachars {
					if strings.Contains(a, m) {
						t.Fatalf("%s: el argumento %q del paso %q contiene %q (¿concatenación de shell?)", name, a, s.Description, m)
					}
				}
			}
			if multiArg[s.Command[0]] && len(s.Command) < 2 {
				t.Fatalf("%s: %q debe recibir argumentos separados, no una cadena: %v", name, s.Command[0], s.Command)
			}
		}
	}
	for _, platform := range []string{"linux", "darwin", "windows"} {
		d := NewDocker(dockerx.Env{GOOS: platform})
		check("docker/"+platform, d.StartPlan(State{Status: StatusDown}, platform))
		check("docker/"+platform+"/permiso", d.StartPlan(State{Status: StatusNeedsElevation}, platform))
		t := NewTailscale(tailscale.Env{})
		check("tailscale/"+platform, t.StartPlan(State{Status: StatusDown}, platform))
		check("tailscale/"+platform+"/login", t.StartPlan(State{Status: StatusNeedsLogin}, platform))
	}
	// El arranque de Docker en Linux debe ir con argumentos separados.
	d := NewDocker(dockerx.Env{GOOS: "linux"})
	found := false
	for _, s := range d.StartPlan(State{Status: StatusDown}, "linux") {
		if strings.Join(s.Command, " ") == "systemctl start docker" {
			found = true
		}
	}
	if !found {
		t.Fatal("quiero el paso 'systemctl start docker' con argumentos separados")
	}
}

func TestManualGuidePerOS(t *testing.T) {
	d := NewDocker(dockerx.Env{})
	for _, tc := range []struct {
		platform string
		want     string
	}{
		{"windows", "Docker Desktop"},
		{"darwin", "open -a Docker"},
		{"linux", "systemctl"},
	} {
		guides := d.ManualGuide(State{Status: StatusDown}, tc.platform)
		joined := strings.Join(guides, " | ")
		if !strings.Contains(joined, tc.want) {
			t.Fatalf("%s: quiero guía con %q, tengo %q", tc.platform, tc.want, joined)
		}
	}
	ts := NewTailscale(tailscale.Env{})
	for _, platform := range []string{"windows", "darwin", "linux"} {
		if len(ts.ManualGuide(State{Status: StatusDown}, platform)) == 0 {
			t.Fatalf("tailscale %s: quiero pasos manuales", platform)
		}
		if !strings.Contains(ts.InstallHint(platform), "tailscale.com") {
			t.Fatalf("tailscale %s: quiero la instrucción de instalación", platform)
		}
	}
}

func TestDockerInstallHintPerOS(t *testing.T) {
	d := NewDocker(dockerx.Env{})
	for _, platform := range []string{"windows", "darwin", "linux"} {
		if strings.TrimSpace(d.InstallHint(platform)) == "" {
			t.Fatalf("docker %s: quiero instrucción de instalación", platform)
		}
	}
}
