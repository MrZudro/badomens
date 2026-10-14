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

// goldenEmptyApp es el Inicio sin instancias (estado vacío determinista).
func goldenEmptyApp() *App {
	a := NewApp(svc.NewFake(nil), darkOpts())
	a.Update(pollRowsMsg{rows: nil})
	a.Update(homeReqMsg{dockerOK: true, tsIP: "100.64.0.1", tsOK: true})
	a.Update(pollDockerMsg{diag: dockerOKDiagnosis()})
	return a
}

// TestGoldenHome (UI-01/§5.2): capturas doradas del Inicio vacío y poblado en
// 40×12, 70×20, 110×30 y 132×40. Se regeneran con `go test ./internal/tui/ -update`.
func TestGoldenHome(t *testing.T) {
	sizes := [][2]int{{40, 12}, {70, 20}, {110, 30}, {132, 40}}
	cases := []struct {
		prefix string
		build  func() *App
	}{
		{"home_empty", goldenEmptyApp},
		{"home_populated", goldenApp},
	}
	for _, c := range cases {
		for _, sz := range sizes {
			c, sz := c, sz
			gname := c.prefix + "_" + name(sz)
			t.Run(gname, func(t *testing.T) {
				a := c.build()
				a.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
				got := stripANSI(a.View())
				path := filepath.Join("testdata", "golden", gname+".txt")
				if *update {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(path) // #nosec G304 -- fichero golden dentro de testdata
				if err != nil {
					t.Skipf("golden %s ausente; genera con `go test ./internal/tui/ -update`: %v", path, err)
				}
				if strings.TrimRight(got, "\n") != strings.TrimRight(string(want), "\n") {
					t.Fatalf("el golden %s no coincide.\n--- esperado ---\n%s\n--- obtenido ---\n%s", path, want, got)
				}
			})
		}
	}
}

func name(sz [2]int) string { return itoa(sz[0]) + "x" + itoa(sz[1]) }
