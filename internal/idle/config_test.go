package idle

import (
	"testing"
	"time"
)

func TestConfigValidacion(t *testing.T) {
	bad := []Config{
		{Enabled: true, Timeout: 30 * time.Second},                        // < 1 min
		{Enabled: true, Timeout: 25 * time.Hour},                          // > 24 h
		{Enabled: true, Timeout: time.Hour, WarnBefore: 90 * time.Minute}, // aviso ≥ timeout
		{Enabled: true, Timeout: time.Hour, Action: "pause"},
	}
	for i, c := range bad {
		if err := c.Validate(); err == nil {
			t.Fatalf("caso %d debe fallar la validación: %+v", i, c)
		}
	}
	good := Config{Enabled: true, Timeout: 15 * time.Minute, WarnBefore: time.Minute}
	if err := good.Validate(); err != nil {
		t.Fatalf("config válida: %v", err)
	}
	if err := (Config{Enabled: false, Timeout: -time.Hour}).Validate(); err != nil {
		t.Fatalf("deshabilitada no se valida: %v", err)
	}
}

func TestNormalizedRellena(t *testing.T) {
	c := Config{Enabled: true, Timeout: 5 * time.Minute}.Normalized()
	if c.WarnBefore <= 0 || c.PollInterval <= 0 || c.Action != "stop" {
		t.Fatalf("normalizada incompleta: %+v", c)
	}
}

func TestParseDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"45m": 45 * time.Minute,
		"1h":  time.Hour,
		"90s": 90 * time.Second,
		"30":  30 * time.Minute,
	}
	for in, want := range cases {
		got, err := ParseDuration(in)
		if err != nil || got != want {
			t.Fatalf("ParseDuration(%q): quiero %s, tengo %s err=%v", in, want, got, err)
		}
	}
	if _, err := ParseDuration("nope"); err == nil {
		t.Fatal("duración inválida debe fallar")
	}
}

func TestPresetsContienenNunca(t *testing.T) {
	ps := Presets()
	if len(ps) < 6 {
		t.Fatalf("quiero al menos 6 presets, tengo %d", len(ps))
	}
	found := false
	for _, p := range ps {
		if p.Timeout == 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("quiero un preset «Nunca» (timeout 0)")
	}
}

func TestHumanDuration(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{0, "nunca"},
		{30 * time.Second, "30 s"},
		{5 * time.Minute, "5 min"},
		{time.Hour, "1 hora"},
		{90 * time.Minute, "1h 30min"},
	} {
		if got := HumanDuration(c.d); got != c.want {
			t.Fatalf("HumanDuration(%v): quiero %q, tengo %q", c.d, c.want, got)
		}
	}
}
