package tailscale

import (
	"context"
	"errors"
	"testing"

	"github.com/MrZudro/badomens/internal/execx"
)

// fakeEnv construye un Env inyectable con una salida fija.
func fakeEnv(inPath bool, stdout string, runErr error) Env {
	return Env{
		LookPath: func(string) bool { return inPath },
		Run: func(_ context.Context, _ string, _ ...string) (execx.Result, error) {
			return execx.Result{Stdout: stdout}, runErr
		},
	}
}

// TestInRangeBounds cubre los límites del rango CGNAT 100.64.0.0/10.
func TestInRangeBounds(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"100.64.0.1", true},
		{"100.64.255.255", true},
		{"100.99.0.1", true}, // regresión: dos dígitos comparados como texto fallaba
		{"100.100.0.1", true},
		{"100.127.255.254", true},
		{"100.63.0.1", false},
		{"100.128.0.1", false},
		{"101.64.0.1", false},
		{"10.0.0.1", false},
		{"100.64.0", false},
		{"100.64.0.1.5", false},
		{"100.a.0.1", false},
		{"100.64.0.300", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := InRange(tc.ip); got != tc.want {
			t.Fatalf("InRange(%q): quiero %v, tengo %v", tc.ip, tc.want, got)
		}
	}
}

func TestMagicDNSFromStatus(t *testing.T) {
	cases := []struct {
		name      string
		st        Status
		wantName  string
		wantOK    bool
		wantCause string
	}{
		{
			name:      "activo",
			st:        Status{Self: &Info{DNSName: "maquina.tail1234.ts.net."}},
			wantName:  "maquina.tail1234.ts.net",
			wantOK:    true,
			wantCause: ReasonMagicDNSOK,
		},
		{
			name:      "desactivado",
			st:        Status{Self: &Info{DNSName: "maquina.tail1234.ts.net.invalid."}},
			wantOK:    false,
			wantCause: ReasonMagicDNSDisabled,
		},
		{
			name:      "sin nombre",
			st:        Status{Self: &Info{DNSName: ""}},
			wantOK:    false,
			wantCause: ReasonMagicDNSNoName,
		},
		{
			name:      "sin self",
			st:        Status{},
			wantOK:    false,
			wantCause: ReasonMagicDNSNoSelf,
		},
	}
	for _, tc := range cases {
		name, ok, cause := MagicDNSFromStatus(tc.st)
		if name != tc.wantName || ok != tc.wantOK || cause != tc.wantCause {
			t.Fatalf("%s: quiero (%q,%v,%q), tengo (%q,%v,%q)", tc.name, tc.wantName, tc.wantOK, tc.wantCause, name, ok, cause)
		}
	}
}

func TestProbeWith(t *testing.T) {
	running := `{"BackendState":"Running","Self":{"HostName":"maquina","DNSName":"maquina.tail1234.ts.net.","TailscaleIPs":["100.64.0.1"],"Online":true},"Peer":{"n":{"Online":true}}}`
	cases := []struct {
		name   string
		env    Env
		want   State
		reason string
		ip     string
	}{
		{name: "no instalado", env: fakeEnv(false, "", nil), want: StateNotInstalled, reason: ReasonNotInstalled},
		{name: "no responde", env: fakeEnv(true, "", errors.New("exit 1")), want: StateNotRunning, reason: ReasonNotRunning},
		{name: "listo", env: fakeEnv(true, running, nil), want: StateReady, reason: ReasonOK, ip: "100.64.0.1"},
		{name: "malformado", env: fakeEnv(true, "{nope", nil), want: StateInvalidOutput, reason: ReasonInvalidOutput},
		{name: "necesita login", env: fakeEnv(true, `{"BackendState":"NeedsLogin"}`, nil), want: StateNeedsLogin, reason: ReasonNeedsLogin},
		{name: "detenido", env: fakeEnv(true, `{"BackendState":"Stopped"}`, nil), want: StateStopped, reason: ReasonStopped},
		{name: "arrancando", env: fakeEnv(true, `{"BackendState":"Starting"}`, nil), want: StateStarting, reason: ReasonStarting},
	}
	for _, tc := range cases {
		rep := ProbeWith(context.Background(), tc.env)
		if rep.State != tc.want || rep.Reason != tc.reason {
			t.Fatalf("%s: quiero (%s,%s), tengo (%s,%s)", tc.name, tc.want, tc.reason, rep.State, rep.Reason)
		}
		if tc.ip != "" && rep.IP != tc.ip {
			t.Fatalf("%s: quiero ip %q, tengo %q", tc.name, tc.ip, rep.IP)
		}
	}
	// El nodo propio más el peer en línea.
	rep := ProbeWith(context.Background(), fakeEnv(true, running, nil))
	if rep.PeersOnline != 2 {
		t.Fatalf("quiero 2 nodos en línea, tengo %d", rep.PeersOnline)
	}
}

func TestResolveIPv4With(t *testing.T) {
	env := fakeEnv(true, "100.64.0.7\n", nil)
	ip, err := ResolveIPv4With(context.Background(), env)
	if err != nil || ip != "100.64.0.7" {
		t.Fatalf("quiero 100.64.0.7, tengo %q (%v)", ip, err)
	}
	if _, err := ResolveIPv4With(context.Background(), fakeEnv(false, "", nil)); err == nil {
		t.Fatal("sin tailscale quiero error")
	}
}
