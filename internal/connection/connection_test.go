package connection

import (
	"strings"
	"testing"
	"time"
)

func TestDeriveCompleta(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	info := Derive(Input{
		Instance: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4",
		Port: 25565, HostIP: "100.64.1.7", MagicDNS: "maquina.tail1234.ts.net",
		OnlineMode: true, Now: now,
	})
	if info.Address != "100.64.1.7:25565" {
		t.Fatalf("address: %q", info.Address)
	}
	if info.AddressDNS != "maquina.tail1234.ts.net:25565" {
		t.Fatalf("address_dns: %q", info.AddressDNS)
	}
	if info.Preferred() != info.Address {
		t.Fatalf("preferida debe ser la IP: %q", info.Preferred())
	}
	if len(info.Warnings) != 0 {
		t.Fatalf("sin avisos esperados: %v", info.Warnings)
	}
	if !info.ResolvedAt.Equal(now) {
		t.Fatalf("resolved_at: %v", info.ResolvedAt)
	}
	if !strings.Contains(info.Invitation(), "100.64.1.7:25565") {
		t.Fatalf("la invitación debe incluir la dirección:\n%s", info.Invitation())
	}
	if !strings.Contains(info.Invitation(), "cuenta oficial") {
		t.Fatalf("online debe mencionar la cuenta oficial:\n%s", info.Invitation())
	}
}

func TestDeriveSinTailnet(t *testing.T) {
	info := Derive(Input{Instance: "x", Port: 25565, HostIP: "", MagicDNS: "solo.ts.net"})
	if info.Address != "" {
		t.Fatalf("sin IP no hay dirección IP: %q", info.Address)
	}
	if info.Preferred() != info.AddressDNS {
		t.Fatalf("preferida debe caer al nombre: %q", info.Preferred())
	}
	if len(info.Warnings) != 1 || !strings.Contains(info.Warnings[0], "Tailscale") {
		t.Fatalf("quiero aviso de Tailscale, tengo %v", info.Warnings)
	}

	both := Derive(Input{Instance: "x", Port: 1})
	if len(both.Warnings) != 2 {
		t.Fatalf("sin IP ni MagicDNS quiero dos avisos, tengo %v", both.Warnings)
	}
}

func TestInvitationConModsYWhitelist(t *testing.T) {
	info := FromData("mods", "Mods", "fabric", "1.21.1", 25566,
		map[string]string{"FABRIC_LOADER_VERSION": "0.16.5"}, false, true, "100.64.1.9", "", time.Now())
	inv := info.Invitation()
	for _, want := range []string{"«Mods»", "Fabric 0.16.5", "mods", "lista blanca", "cualquier nombre"} {
		if !strings.Contains(inv, want) {
			t.Fatalf("la invitación debe incluir %q:\n%s", want, inv)
		}
	}
	if !info.ModsNeeded {
		t.Fatal("fabric debe marcar ModsNeeded")
	}
}

func TestLoaderLabel(t *testing.T) {
	cases := []struct {
		engine string
		pins   map[string]string
		want   string
	}{
		{"fabric", map[string]string{"FABRIC_LOADER_VERSION": "0.16.5"}, "Fabric 0.16.5"},
		{"forge", map[string]string{"FORGE_VERSION": "52.0.1"}, "Forge 52.0.1"},
		{"neoforge", map[string]string{"NEOFORGE_VERSION": "21.1.72"}, "NeoForge 21.1.72"},
		{"paper", nil, ""},
	}
	for _, tc := range cases {
		if got := LoaderLabel(tc.engine, tc.pins); got != tc.want {
			t.Fatalf("LoaderLabel(%s): quiero %q, tengo %q", tc.engine, tc.want, got)
		}
	}
}
