package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap reúne los atajos globales de la TUI v2 (RF-7/§4.4).
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Enter    key.Binding
	Back     key.Binding
	Help     key.Binding
	Palette  key.Binding
	NextPane key.Binding
	PrevPane key.Binding
	New      key.Binding
	Health   key.Binding
	Copy     key.Binding
	Quit     key.Binding
	Yes      key.Binding
	No       key.Binding
}

// DefaultKeys es el mapa por defecto.
var DefaultKeys = KeyMap{
	Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "subir")),
	Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "bajar")),
	Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "abrir/confirmar")),
	Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "volver")),
	Help:     key.NewBinding(key.WithKeys("f1"), key.WithHelp("F1", "ayuda")),
	Palette:  key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("Ctrl+P", "comandos")),
	NextPane: key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "cambiar panel")),
	PrevPane: key.NewBinding(key.WithKeys("shift+tab")),
	New:      key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("Ctrl+N", "nueva instancia")),
	Health:   key.NewBinding(key.WithKeys("f2"), key.WithHelp("F2", "salud del sistema")),
	Copy:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copiar")),
	Quit:     key.NewBinding(key.WithKeys("ctrl+q", "ctrl+c"), key.WithHelp("Ctrl+Q", "salir")),
	Yes:      key.NewBinding(key.WithKeys("y")),
	No:       key.NewBinding(key.WithKeys("n")),
}

// ShortHelp es la barra de ayuda global (siempre visible en la última línea).
func (k KeyMap) ShortHelp() string {
	return "F1 Ayuda · Ctrl+P Comandos · Tab Panel · F2 Salud · Ctrl+Q Salir"
}

// ShortHelpASCII es la barra de ayuda en modo ASCII (separadores planos).
func (k KeyMap) ShortHelpASCII() string {
	return "F1 Ayuda | Ctrl+P Comandos | Tab Panel | F2 Salud | Ctrl+Q Salir"
}

// FullHelp devuelve la hoja de atajos completa de la pantalla actual.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter, k.Back},
		{k.Help, k.Palette, k.NextPane, k.PrevPane},
		{k.New, k.Health, k.Copy, k.Quit},
	}
}
