package tui

import (
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tui/layout"
)

// TestSnapshotRespetaElTamano (§4.1) renderiza el panel en varios tamaños con
// una instancia registrada y falla si alguna línea supera el ancho o el número
// de líneas supera el alto. Es la guarda contra el desbordamiento responsive.
func TestSnapshotRespetaElTamano(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	m := instance.New(cfg.DataDir, []string{"paper"})
	if err := m.Save(&instance.Spec{
		Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4",
		Memory: "4G", Port: 25565, MaxPlayers: 20, Difficulty: "normal", Gamemode: "survival",
	}); err != nil {
		t.Fatal(err)
	}

	views := []string{"list", "detail", "wizard"}
	for _, sz := range [][2]int{{40, 12}, {70, 20}, {110, 30}, {200, 50}} {
		for _, v := range views {
			out, err := RenderSnapshot(cfg, "survival", v, sz[0], sz[1])
			if err != nil {
				t.Fatalf("snapshot %s %dx%d: %v", v, sz[0], sz[1], err)
			}
			lines := strings.Split(out, "\n")
			if len(lines) > sz[1] {
				t.Errorf("view %s %dx%d: %d líneas (máx %d)", v, sz[0], sz[1], len(lines), sz[1])
			}
			for i, l := range lines {
				if w := layout.Width(l); w > sz[0] {
					t.Errorf("view %s %dx%d: línea %d mide %d (máx %d): %.60q", v, sz[0], sz[1], i+1, w, sz[0], l)
				}
			}
		}
	}
}
