package tui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

func dockerOKDiagnosis() dockerx.Diagnosis {
	return dockerx.Diagnosis{State: dockerx.StateOK, Message: "Docker está listo."}
}

var update = flag.Bool("update", false, "actualiza los ficheros golden")

// goldenApp construye una app determinista con una instancia en marcha.
func goldenApp() *App {
	s := svc.NewFake([]*instance.Spec{{
		Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4",
		Memory: "4G", Port: 25565, MaxPlayers: 20,
	}})
	a := NewApp(s, AppOptions{Theme: theme.Dark})
	a.Update(pollRowsMsg{rows: []svc.Row{
		{Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4", Memory: "4G", Port: 25565, State: "running", MaxPlayers: 20},
		{Slug: "creativo", Name: "Creativo", Engine: "vanilla", Version: "1.21", Memory: "2G", Port: 25566, State: "exited"},
	}})
	a.Update(homeDetailMsg{d: &svc.Detail{Spec: &instance.Spec{
		Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4", Memory: "4G", Port: 25565, Gamemode: "survival", Difficulty: "normal", MaxPlayers: 20,
	}, State: "running"}})
	a.Update(pollDockerMsg{diag: dockerOKDiagnosis()})
	return a
}

// TestGoldenHome (RF-1/§10.2): capturas doradas del Home en 40×12, 70×20,
// 110×30 y 200×50. Se regeneran con `go test ./internal/tui/ -update`.
func TestGoldenHome(t *testing.T) {
	sizes := [][2]int{{40, 12}, {70, 20}, {110, 30}, {200, 50}}
	for _, sz := range sizes {
		sz := sz
		t.Run(name(sz), func(t *testing.T) {
			a := goldenApp()
			a.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			got := stripANSI(a.View())
			path := filepath.Join("testdata", "golden", name(sz)+".txt")
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Skipf("golden %s ausente; genera con `go test ./internal/tui/ -update`: %v", path, err)
			}
			if strings.TrimRight(got, "\n") != strings.TrimRight(string(want), "\n") {
				t.Fatalf("el golden %s no coincide.\n--- esperado ---\n%s\n--- obtenido ---\n%s", path, want, got)
			}
		})
	}
}

func name(sz [2]int) string { return itoa(sz[0]) + "x" + itoa(sz[1]) }
