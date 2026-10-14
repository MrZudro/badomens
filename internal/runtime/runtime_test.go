package runtime

import (
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/instance"
)

// TestRenderEligeJavaPorVersion verifica que el compose usa la imagen de itzg
// con el Java que requiere la versión de Minecraft (RF-5).
func TestRenderEligeJavaPorVersion(t *testing.T) {
	cfg := config.Default()
	rt := New(cfg)
	casos := []struct {
		version string
		want    string
	}{
		{"1.21.4", "image: itzg/minecraft-server:java21"},
		{"1.20.1", "image: itzg/minecraft-server:java17"},
		{"1.17.1", "image: itzg/minecraft-server:java16"},
		{"desconocida", "image: itzg/minecraft-server:java25"},
	}
	for _, c := range casos {
		spec := &instance.Spec{Slug: "demo", Name: "Demo", Engine: "paper", Version: c.version, Memory: "4G", Port: 25565}
		raw, err := rt.Render(spec, cfg, "100.64.0.1")
		if err != nil {
			t.Fatalf("Render(%s): %v", c.version, err)
		}
		if !strings.Contains(string(raw), c.want) {
			t.Errorf("Render(%s) no contiene %q:\n%s", c.version, c.want, raw)
		}
	}
}

// TestImageForRespetaOverride comprueba que una imagen personalizada conserva su
// repositorio al adaptar la etiqueta de Java.
func TestImageForRespetaOverride(t *testing.T) {
	cfg := config.Default()
	cfg.Image = "mi/registro/itzg-mc:java25"
	rt := New(cfg)
	spec := &instance.Spec{Slug: "demo", Engine: "paper", Version: "1.21.4"}
	if got := rt.imageFor(spec); got != "mi/registro/itzg-mc:java21" {
		t.Errorf("imageFor = %q, quiero mi/registro/itzg-mc:java21", got)
	}
}
