package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// Options controla la apertura del panel.
type Options struct {
	// Preflight comprueba y muestra los requisitos (Docker, red) al abrir.
	Preflight bool
}

// Run lanza el panel interactivo en la terminal actual (pantalla alternativa)
// y devuelve nil al salir. Requiere una terminal; el comando CLI lo comprueba
// antes con isTTY.
func Run(cfg *config.Config, opts Options) error {
	services, err := svc.New(cfg)
	if err != nil {
		return err
	}
	app := NewApp(services, AppOptions{
		Preflight:     opts.Preflight,
		Theme:         theme.Parse(cfg.UI.Theme),
		ReducedMotion: cfg.UI.ReducedMotion,
		ASCII:         cfg.UI.ASCII,
	})
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

// RenderSnapshot construye el panel fuera de bubbletea, carga los datos una vez
// y devuelve el frame renderizado de la pantalla indicada (list, detail, logs,
// backups, health o help). Sirve para pruebas, capturas y documentación sin
// terminal interactiva. slug solo aplica a detail/logs/backups.
func RenderSnapshot(cfg *config.Config, slug, view string, w, h int) (string, error) {
	services, err := svc.New(cfg)
	if err != nil {
		return "", err
	}
	pal := theme.Parse(cfg.UI.Theme)
	app := NewApp(services, AppOptions{
		Theme:         pal,
		ReducedMotion: cfg.UI.ReducedMotion,
		ASCII:         cfg.UI.ASCII,
	})
	if w <= 0 {
		w = 110
	}
	if h <= 0 {
		h = 30
	}
	app.size = layout.Size{W: w, H: h, Class: layout.Classify(w, h)}
	ctx := context.Background()

	rows, err := services.Instances.Rows(ctx)
	if err != nil {
		return "", err
	}
	app.Update(pollRowsMsg{rows: rows})
	if slug == "" && len(rows) > 0 {
		slug = rows[0].Slug
	}

	// Estado inicial de la pantalla raíz (requisitos de la home, etc.).
	app.Update(ejecutaDirecto(app.router.Top().Init()))

	if slug != "" {
		if d, derr := services.Instances.Detail(ctx, slug); derr == nil {
			app.Update(homeDetailMsg{d: d})
		}
		if hs, ok := app.router.Top().(*homeScreen); ok {
			app.Update(ejecutaDirecto(hs.logsCmd()))
		}
	}

	openDetail := func(tab int) {
		if slug == "" {
			return
		}
		is := newInstanceScreen(services, theme.For(pal), cfg.UI.ASCII, slug)
		is.tab = tab
		if d, derr := services.Instances.Detail(ctx, slug); derr == nil {
			is.detail = d
		}
		app.router.Push(is)
	}

	switch view {
	case "detail":
		openDetail(0)
	case "logs":
		openDetail(1)
	case "backups":
		openDetail(3)
	case "players":
		openDetail(2)
	case "net":
		openDetail(6)
	case "idle":
		openDetail(7)
	case "settings":
		openDetail(8)
	case "health":
		app.router.Push(newHealthScreen(services, theme.For(pal), cfg.UI.ASCII))
	case "wizard":
		app.router.Push(newWizardScreen(services, theme.For(pal), cfg.UI.ASCII))
	case "palette":
		app.router.SetOverlay(newPaletteScreen(app.actions(), theme.For(pal), cfg.UI.ASCII))
	case "help":
		app.router.SetOverlay(newHelpScreen(DefaultKeys, theme.For(pal), cfg.UI.ASCII))
	}
	if view != "" && view != "list" {
		app.Update(ejecutaDirecto(app.router.Active().Init()))
	}
	return stripANSI(app.View()), nil
}

// ejecutaDirecto ejecuta un tea.Cmd y devuelve su mensaje (nil si no hay).
func ejecutaDirecto(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}
