package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/host"
	"github.com/MrZudro/badomens/internal/idle"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// goldenSpec es una instancia determinista para las capturas doradas.
func goldenSpec() *instance.Spec {
	return &instance.Spec{
		Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4",
		Memory: "4G", Port: 25565, MaxPlayers: 20,
		Difficulty: "normal", Gamemode: "survival",
		OnlineMode: true, WhitelistEnabled: true,
		Whitelist: []string{"ana", "luis"}, Ops: []string{"ana"},
		Seed: "42", Motd: "Bienvenidos al servidor", Plugins: []string{"LuckPerms"},
	}
}

func goldenSvc() *svc.Services { return svc.NewFake([]*instance.Spec{goldenSpec()}) }

func darkOpts() AppOptions { return AppOptions{Theme: theme.Dark} }

func goldenVersions() []engines.Version {
	return []engines.Version{
		{Version: "1.21.4", Channel: engines.ChannelRelease, Recommended: true, Compatible: true, JavaMajor: 21, Build: "123"},
		{Version: "1.21.1", Channel: engines.ChannelRelease, Compatible: true, JavaMajor: 21, Build: "110"},
		{Version: "1.20.6", Channel: engines.ChannelRelease, Compatible: true, JavaMajor: 21, Build: "97"},
		{Version: "1.20.1", Channel: engines.ChannelRelease, Compatible: true, JavaMajor: 17, Build: "45"},
	}
}

// --- Constructores de pantalla para las capturas ---

func gDetail(tab int) func() *App {
	return func() *App {
		a := NewApp(goldenSvc(), darkOpts())
		a.router.Push(newInstanceScreen(a.svc, a.pal, a.ascii, "survival"))
		if inst, ok := a.router.Top().(*instanceScreen); ok {
			inst.detail = &svc.Detail{Spec: goldenSpec(), State: "running", Host: "100.64.0.1"}
			inst.idleCfg = idle.Config{Enabled: true, Timeout: 15 * time.Minute, BackupBeforeStop: true}
			inst.idleLoaded = true
			if tab == 2 {
				inst.access = svc.AccessState{
					WhitelistEnabled: true,
					Whitelist:        []string{"ana", "luis"},
					Operators:        []string{"ana"},
					Source:           svc.AccessLive,
				}
				inst.accessLoaded = true
			}
			inst.tab = tab
		}
		return a
	}
}

// gConsole fija la pestaña Consola con logs normales y una línea muy larga.
func gConsole() *App {
	a := NewApp(goldenSvc(), darkOpts())
	a.router.Push(newInstanceScreen(a.svc, a.pal, a.ascii, "survival"))
	if inst, ok := a.router.Top().(*instanceScreen); ok {
		inst.detail = &svc.Detail{Spec: goldenSpec(), State: "running", Host: "100.64.0.1"}
		inst.tab = 1
		inst.logs = strings.Join([]string{
			"[22:10:03 INFO]: Alex joined the game",
			"[22:10:10 INFO]: There are 3 of a max of 20 players online",
			strings.Repeat("Z", 240),
			"[22:10:19 INFO]: Done (1.234s)! For help, type \"help\"",
		}, "\n")
	}
	return a
}

func gWizard(mode, step int) func() *App {
	return func() *App {
		a := NewApp(goldenSvc(), darkOpts())
		a.router.Push(newWizardScreen(a.svc, a.pal, a.ascii))
		if wz, ok := a.router.Top().(*wizardScreen); ok && mode >= 0 {
			wz.start(mode)
			wz.versions = goldenVersions()
			wz.verLoading = false
			wz.versionsSeen = true
			wz.pickRecommended()
			wz.step = step
			wz.cur = 0
			wz.syncInput()
		}
		return a
	}
}

func gHealth() *App {
	a := NewApp(goldenSvc(), darkOpts())
	a.router.Push(newHealthScreen(a.svc, a.pal, a.ascii))
	if h, ok := a.router.Top().(*healthScreen); ok {
		h.docker = dockerx.Diagnosis{State: dockerx.StateOK, Message: "Docker está listo.", ComposeVersion: "v2.30.1"}
		h.capacity = host.Capacity{NCPU: 8, MemTotalBytes: 16 << 30, Source: "runtime"}
		h.ip, h.magic = "100.64.0.1", "maquina.tail1234.ts.net"
		h.freeGB, h.totalGB, h.diskOK = 120, 500, true
		h.portsOK = true
		h.checkedUp, h.latest, h.hasUpdate = true, "1.0.1", false
	}
	return a
}

func gHelp() *App {
	a := NewApp(goldenSvc(), darkOpts())
	a.router.SetOverlay(newHelpScreen(a.keys, a.pal, a.ascii))
	return a
}

func gPalette() *App {
	a := NewApp(goldenSvc(), darkOpts())
	a.Update(pollRowsMsg{rows: []svc.Row{{
		Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4",
		Port: 25565, State: "running", MaxPlayers: 20, PlayersOnline: 3, Host: "100.64.0.1",
	}}})
	a.router.SetOverlay(newPaletteScreen(a.actions(), a.pal, a.ascii))
	return a
}

type gcase struct {
	name  string
	w, h  int
	build func() *App
}

func goldenScreenCases() []gcase {
	sizes := [][2]int{{40, 12}, {70, 20}, {110, 30}, {200, 50}}
	var out []gcase
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		out = append(out,
			gcase{name: span("detail", w, h), w: w, h: h, build: gDetail(0)},
			gcase{name: span("players", w, h), w: w, h: h, build: gDetail(2)},
			gcase{name: span("console", w, h), w: w, h: h, build: gConsole},
			gcase{name: span("health", w, h), w: w, h: h, build: gHealth},
			gcase{name: span("help", w, h), w: w, h: h, build: gHelp},
			gcase{name: span("palette", w, h), w: w, h: h, build: gPalette},
			gcase{name: span("wizard_mode", w, h), w: w, h: h, build: gWizard(-1, 0)},
		)
		for step := 0; step < 7; step++ {
			out = append(out, gcase{
				name: span("wizard_step"+itoa(step), w, h), w: w, h: h, build: gWizard(1, step),
			})
		}
	}
	return out
}

func span(prefix string, w, h int) string { return prefix + "_" + itoa(w) + "x" + itoa(h) }

// asciiCases son variantes en modo ASCII (iconos y bordes planos).
func asciiCases() []gcase {
	opts := AppOptions{Theme: theme.Dark, ASCII: true}
	return []gcase{
		{name: "home_ascii_110x30", w: 110, h: 30, build: func() *App { return NewApp(goldenSvc(), opts) }},
		{name: "detail_ascii_110x30", w: 110, h: 30, build: func() *App {
			a := NewApp(goldenSvc(), opts)
			a.router.Push(newInstanceScreen(a.svc, a.pal, a.ascii, "survival"))
			if inst, ok := a.router.Top().(*instanceScreen); ok {
				inst.detail = &svc.Detail{Spec: goldenSpec(), State: "running", Host: "100.64.0.1"}
			}
			return a
		}},
		{name: "health_ascii_110x30", w: 110, h: 30, build: func() *App {
			a := NewApp(goldenSvc(), opts)
			a.router.Push(newHealthScreen(a.svc, a.pal, a.ascii))
			if h, ok := a.router.Top().(*healthScreen); ok {
				h.docker = dockerx.Diagnosis{State: dockerx.StateOK, Message: "Docker está listo.", ComposeVersion: "v2.30.1"}
				h.capacity = host.Capacity{NCPU: 8, MemTotalBytes: 16 << 30, Source: "runtime"}
				h.ip, h.magic = "100.64.0.1", "maquina.tail1234.ts.net"
				h.freeGB, h.totalGB, h.diskOK = 120, 500, true
				h.portsOK = true
				h.checkedUp, h.latest, h.hasUpdate = true, "1.0.1", false
			}
			return a
		}},
	}
}

// TestGoldenScreens (RF-1/§10.2): capturas doradas de Detalle, Asistente (cada
// paso), Salud, Ayuda y Paleta en 40×12, 70×20, 110×30 y 200×50, más variantes
// ASCII. Se regeneran con `go test ./internal/tui/ -update`.
func TestGoldenScreens(t *testing.T) {
	for _, c := range append(goldenScreenCases(), asciiCases()...) {
		c := c
		t.Run(c.name, func(t *testing.T) {
			a := c.build()
			a.Update(tea.WindowSizeMsg{Width: c.w, Height: c.h})
			got := stripANSI(a.View())
			path := filepath.Join("testdata", "golden", c.name+".txt")
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path) // #nosec G304 -- ruta fija de testdata del propio paquete
			if err != nil {
				t.Skipf("golden %s ausente; genera con `go test ./internal/tui/ -update`: %v", path, err)
			}
			if strings.TrimRight(got, "\n") != strings.TrimRight(string(want), "\n") {
				t.Fatalf("el golden %s no coincide.\n--- esperado ---\n%s\n--- obtenido ---\n%s", path, want, got)
			}
		})
	}
}

// TestGoldenASCIISinBordesUnicode (§4.2): las capturas en modo ASCII no deben
// contener caracteres de dibujo de cajas (U+2500–U+259F); los bordes e iconos
// deben ser ASCII.
func TestGoldenASCIISinBordesUnicode(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "golden", "*_ascii_*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no hay capturas ASCII todavía")
	}
	for _, f := range files {
		raw, rerr := os.ReadFile(f) // #nosec G304 -- ruta fija de testdata del propio paquete
		if rerr != nil {
			t.Fatal(rerr)
		}
		for _, r := range string(raw) {
			if (r >= 0x2500 && r <= 0x257F) || (r >= 0x2580 && r <= 0x259F) {
				t.Errorf("%s contiene un carácter de dibujo Unicode %q (debe ser ASCII)", f, r)
			}
		}
	}
}

// TestTemasRenderizanSinPanico comprueba que Home se pinta igual de bien en los
// cuatro temas (el color no entra en los goldens, que van sin ANSI).
func TestTemasRenderizanSinPanico(t *testing.T) {
	for _, name := range theme.All() {
		a := NewApp(goldenSvc(), AppOptions{Theme: name})
		a.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
		out := a.View()
		if strings.TrimSpace(stripANSI(out)) == "" {
			t.Fatalf("el tema %s pintó una vista vacía", name)
		}
	}
}
