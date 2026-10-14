package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// paletteScreen es la paleta de comandos (Ctrl+P): búsqueda difusa sobre todas
// las acciones de la pantalla actual y las globales (P-02). Captura todas las
// teclas mientras está abierta: las letras filtran y no disparan atajos.
type paletteScreen struct {
	picker *components.Picker
	pal    theme.Palette
	ascii  bool
}

func newPaletteScreen(actions []components.Action, pal theme.Palette, ascii bool) *paletteScreen {
	items := make([]components.PickerItem, 0, len(actions))
	for _, a := range actions {
		items = append(items, components.PickerItem{Label: a.Label(), Value: a.ID, Detail: a.Key})
	}
	return &paletteScreen{picker: components.NewPicker("Comandos", items), pal: pal, ascii: ascii}
}

func (p *paletteScreen) Init() tea.Cmd { return nil }

func (p *paletteScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	switch km.String() {
	case "up", "k", "ctrl+p":
		p.picker.Move(-1)
	case "down", "j":
		p.picker.Move(1)
	case "backspace":
		p.picker.Backspace()
	case "enter":
		if it, ok := p.picker.Selected(); ok {
			id := it.Value
			return nil, func() tea.Msg { return paletteChosenMsg{id: id} }
		}
		return nil, nil
	case "esc":
		return nil, nil
	default:
		if r := []rune(km.String()); len(r) == 1 && r[0] >= 32 {
			p.picker.Type(r[0])
		}
	}
	return p, nil
}

func (p *paletteScreen) View(w, h int) string {
	body := p.picker.Render(w-6, h-4, p.pal, p.ascii)
	body += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(p.pal.Muted)).
		Render("Escribe para filtrar…  Enter ejecutar · Esc cerrar")
	return components.Modal{Title: "Paleta de comandos", Body: body}.Render(w, p.pal, p.ascii)
}

func (p *paletteScreen) Actions() []components.Action { return nil }
func (p *paletteScreen) Title() string                { return "Comandos" }
func (p *paletteScreen) ID() string                   { return "palette" }

// CapturesInput: la paleta filtra por texto, así que captura las teclas.
func (p *paletteScreen) CapturesInput() bool { return true }
