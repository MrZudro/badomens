package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// paletteScreen es la paleta de comandos (Ctrl+P): búsqueda difusa sobre todas
// las acciones de la pantalla actual y las globales (P-02).
type paletteScreen struct {
	picker *components.Picker
	pal    theme.Palette
	chosen components.Action
	has    bool
}

func newPaletteScreen(actions []components.Action, pal theme.Palette) *paletteScreen {
	items := make([]components.PickerItem, 0, len(actions))
	for _, a := range actions {
		items = append(items, components.PickerItem{Label: a.Label(), Value: a.ID, Detail: a.Key})
	}
	return &paletteScreen{picker: components.NewPicker("Comandos", items), pal: pal}
}

func (p *paletteScreen) Init() tea.Cmd { return nil }

func (p *paletteScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	switch km.String() {
	case "up", "k":
		p.picker.Move(-1)
	case "down", "j":
		p.picker.Move(1)
	case "backspace":
		p.picker.Backspace()
	case "enter":
		if it, ok := p.picker.Selected(); ok {
			p.chosen = components.Action{ID: it.Value}
			p.has = true
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
	return components.Modal{Title: "Paleta de comandos", Body: p.picker.Render(w-6, h-4, p.pal)}.Render(w, p.pal)
}

func (p *paletteScreen) Actions() []components.Action { return nil }
func (p *paletteScreen) Title() string                { return "Comandos" }
func (p *paletteScreen) ID() string                   { return "palette" }
