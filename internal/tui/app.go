package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/config"
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
// backups o help). Sirve para pruebas, capturas y documentación sin terminal
// interactiva. slug solo aplica a detail/logs/backups.
func RenderSnapshot(cfg *config.Config, slug, view string) (string, error) {
	b, err := newBackend(cfg)
	if err != nil {
		return "", err
	}
	m, err := NewModel(b)
	if err != nil {
		return "", err
	}
	m.width, m.height = 84, 26
	ctx := context.Background()
	rows, running, err := b.ListRows(ctx)
	if err != nil {
		return "", err
	}
	mm, _ := m.Update(rowsMsg{rows: rows, running: running})
	m = mm.(Model)

	switch view {
	case "detail":
		d, derr := b.Detail(ctx, slug)
		if derr != nil {
			return "", derr
		}
		m.detail = d
		m.mode = modeDetail
	case "logs":
		if slug == "" {
			slug, _ = m.selectedSlug()
		}
		m.mode = modeLogs
		m.logSlug = slug
		text, lerr := b.Logs(ctx, slug, 60)
		if lerr != nil {
			return "", lerr
		}
		mm, _ = m.Update(logsMsg{slug: slug, text: text})
		m = mm.(Model)
	case "backups":
		if slug == "" {
			slug, _ = m.selectedSlug()
		}
		m.mode = modeBackups
		m.backupsSlug = slug
		items, berr := b.Backups(ctx, slug)
		if berr != nil {
			return "", berr
		}
		mm, _ = m.Update(backupsMsg{slug: slug, items: items})
		m = mm.(Model)
	case "help":
		m.mode = modeHelp
	}
	return m.View(), nil
}
