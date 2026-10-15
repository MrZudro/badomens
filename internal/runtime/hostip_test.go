package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
)

// TestHostIPNoPublicaEnTodasLasInterfaces (plan 12.4) comprueba que el puerto
// nunca se publica en 0.0.0.0 ni con una IP vacía.
func TestHostIPNoPublicaEnTodasLasInterfaces(t *testing.T) {
	base := func(hostIP string, enabled bool) *Runtime {
		return New(&config.Config{Tailscale: config.Tailscale{Enabled: enabled, HostIP: hostIP}})
	}

	rt := base("", false)
	ip, err := rt.HostIP(context.Background())
	if err != nil || ip != "127.0.0.1" {
		t.Fatalf("sin Tailscale = %q (%v)", ip, err)
	}

	rt = base("100.64.0.7", true)
	if ip, err = rt.HostIP(context.Background()); err != nil || ip != "100.64.0.7" {
		t.Fatalf("con IP fijada = %q (%v)", ip, err)
	}

	for _, bad := range []string{"0.0.0.0", "::", "no-es-una-ip", "example.test", "100.64.0.1/32"} {
		if ip, err := base(bad, true).HostIP(context.Background()); err == nil {
			t.Fatalf("HostIP(%q) = %q; debería rechazarse", bad, ip)
		}
	}
}

// TestRenderRechazaIPDePublicacionInsegura: la plantilla no se genera con una IP
// vacía, comodín o inválida.
func TestRenderRechazaIPDePublicacionInsegura(t *testing.T) {
	rt := New(config.Default())
	spec := &instance.Spec{Slug: "demo", Engine: "paper", Version: "1.21.1", Memory: "4G", Port: 25565}

	for _, bad := range []string{"", " ", "0.0.0.0", "::", "127.0.0.1:25565"} {
		if _, err := rt.Render(spec, rt.Cfg, bad); err == nil {
			t.Fatalf("Render(%q) debería fallar", bad)
		}
	}
	raw, err := rt.Render(spec, rt.Cfg, "127.0.0.1")
	if err != nil {
		t.Fatalf("Render(127.0.0.1): %v", err)
	}
	if !strings.Contains(string(raw), `"127.0.0.1:25565:25565"`) {
		t.Fatalf("compose = %s", raw)
	}

	// El comodín lleva el motivo estable para que el consumidor lo distinga.
	_, err = rt.Render(spec, rt.Cfg, "0.0.0.0")
	if reason, _ := errx.Details(err)["reason"].(string); reason != "host_ip_comodin" {
		t.Fatalf("reason = %q (%v)", reason, err)
	}
}
