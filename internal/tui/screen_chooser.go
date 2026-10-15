package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// chooserCopyAddress es el ID de la pregunta «qué dirección copiar» (§10). El
// App es quien ejecuta el efecto (copiar al portapapeles).
const chooserCopyAddress = "copy.address"

// chooserOpenMsg pide abrir el selector modal del chooser con sus opciones.
type chooserOpenMsg struct {
	Title string
	Hint  string
	ID    string
	Items []components.SelectItem
}

// chooserPickedMsg cierra el chooser con la opción elegida (por VALOR).
type chooserPickedMsg struct {
	ID    string
	Value string
}

// chooserScreen es un selector modal reutilizable (P-03): usa el componente ÚNICO
// de selección y captura las teclas mientras está abierto. Lo usan los flujos que
// necesitan ELEGIR entre varias opciones equivalentes (p. ej. qué dirección de
// conexión copiar).
type chooserScreen struct {
	msg   chooserOpenMsg
	sel   *components.Selector
	pal   theme.Palette
	ascii bool
}

func newChooserScreen(msg chooserOpenMsg, pal theme.Palette, ascii bool) *chooserScreen {
	return &chooserScreen{
		msg:   msg,
		sel:   components.NewSelector(msg.Title, msg.Items),
		pal:   pal,
		ascii: ascii,
	}
}

func (c *chooserScreen) Init() tea.Cmd { return nil }

func (c *chooserScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return c, nil
	}
	switch km.String() {
	case "esc":
		return nil, nil
	case "j", "down":
		c.sel.Move(1)
	case "k", "up":
		c.sel.Move(-1)
	case "enter":
		if it, ok := c.sel.Selected(); ok {
			id, value := c.msg.ID, it.Value
			return nil, func() tea.Msg { return chooserPickedMsg{ID: id, Value: value} }
		}
		return nil, nil
	}
	return c, nil
}

func (c *chooserScreen) View(w, h int) string {
	if w < 20 {
		w = 20
	}
	inner := w - 6
	if inner < 10 {
		inner = 10
	}
	lines := c.sel.Render(inner, "", c.pal, false)
	hint := strings.TrimSpace(c.msg.Hint)
	if hint == "" {
		hint = "↑/↓ eligen · Enter confirma · Esc cierra"
	}
	body := strings.Join(lines, "\n") + "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(c.pal.Muted)).Render(hint)
	return components.Modal{Title: c.msg.Title, Body: body}.Render(w, c.pal, c.ascii)
}

func (c *chooserScreen) Actions() []components.Action { return nil }
func (c *chooserScreen) Title() string                { return c.msg.Title }
func (c *chooserScreen) ID() string                   { return "chooser" }

// CapturesInput: el chooser es modal y consume las teclas (las letras no son
// atajos globales mientras está abierto).
func (c *chooserScreen) CapturesInput() bool { return true }
