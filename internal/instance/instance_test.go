package instance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeSlug(t *testing.T) {
	cases := map[string]string{
		"Survival World": "survival-world",
		"ñandú":          "nandu",
		"paper_2":        "paper-2",
	}
	for in, want := range cases {
		if got, err := NormalizeSlug(in); err != nil || got != want {
			t.Errorf("NormalizeSlug(%q) = %q, %v; quiero %q", in, got, err, want)
		}
	}
	if _, err := NormalizeSlug("a"); err == nil {
		t.Error("slug de 1 carácter debe fallar")
	}
}

func TestValidate(t *testing.T) {
	s := &Spec{Slug: "x", Engine: "paper", Version: "1.21.6", Memory: "4G", Port: 25565, MaxPlayers: 20}
	if err := s.Validate([]string{"paper", "vanilla"}); err != nil {
		t.Fatalf("spec válido rechazado: %v", err)
	}
	bad := *s
	bad.Engine = "spigot"
	if err := bad.Validate([]string{"paper"}); err == nil {
		t.Fatal("motor inválido aceptado")
	}
	bad = *s
	bad.Memory = "abc"
	if err := bad.Validate([]string{"paper"}); err == nil {
		t.Fatal("memoria inválida aceptada")
	}
}

func TestManagerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := New(dir, []string{"paper", "vanilla"})
	s := &Spec{
		Slug: "test", Engine: "paper", Version: "1.21.6", Memory: "4G", Port: 25565,
		Difficulty: "normal", Gamemode: "survival", MaxPlayers: 20, Whitelist: []string{"ana"},
	}
	if err := m.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := m.Load("test")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Whitelist[0] != "ana" || got.MaxPlayers != 20 {
		t.Fatalf("round trip = %+v", got)
	}

	port, err := m.NextPort(25565)
	if err != nil || port != 25566 {
		t.Fatalf("next port = %d, %v", port, err)
	}

	if err := m.Remove("test"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.Dir("test"), "instance.yaml")); !os.IsNotExist(err) {
		t.Fatalf("la carpeta no se borró: %v", err)
	}
}
