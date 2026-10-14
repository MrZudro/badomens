package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// helpScreen es la hoja de atajos de la pantalla actual (tecla ?).
type helpScreen struct {
	keys  KeyMap
	pal   theme.Palette
	ascii bool
}

func newHelpScreen(keys KeyMap, pal theme.Palette, ascii bool) *helpScreen {
	return &helpScreen{keys: keys, pal: pal, ascii: ascii}
}

func (h *helpScreen) Init() tea.Cmd { return nil }

func (h *helpScreen) Update(tea.Msg) (Screen, tea.Cmd) { return h, nil }

func (h *helpScreen) View(w, hgt int) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(h.pal.Accent)).Render("Atajos de esta pantalla"))
	b.WriteString("\n\n")
	for _, group := range h.keys.FullHelp() {
		for _, binding := range group {
			if !binding.Enabled() {
				continue
			}
			help := binding.Help()
			b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(h.pal.Accent)).Render(help.Key) + "  " + help.Desc + "\n")
		}
	}
	b.WriteString("\n  Esc  cerrar ayuda")
	return components.Modal{
		Title:   "Ayuda",
		Body:    b.String(),
		Actions: nil,
	}.Render(w, h.pal, h.ascii)
}

func (h *helpScreen) Actions() []components.Action { return nil }
func (h *helpScreen) Title() string                { return "Ayuda" }
func (h *helpScreen) ID() string                   { return "help" }

// NavSection indica la sección de primer nivel activa.
func (h *helpScreen) NavSection() string { return "help" }

// Status no aporta mensaje propio.
func (h *helpScreen) Status() string { return "" }
