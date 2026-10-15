package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap reúne los atajos globales de la TUI v2 (RF-7/§4.4). Las descripciones
// son las que documenta la hoja de Ayuda (SPEC 2 §13): el formato visible de la
// barra inferior lo aplica components.FooterKey, de modo que aquí no se
// hardcodea ninguna etiqueta de la barra.
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

// DefaultKeys es el mapa por defecto. Tab/Shift+Tab desplazan el foco entre las
// zonas/paneles de la pantalla actual («Desplazarse», SPEC 2 §1.2/§15) y ya no
// cambian de pantalla de primer nivel: esa fila de pestañas se retiró. Las
// flechas ←/→ cambian de pestaña dentro de una instancia.
var DefaultKeys = KeyMap{
	Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "subir")),
	Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "bajar")),
	Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "abrir o confirmar")),
	Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "volver o cancelar")),
	Help:     key.NewBinding(key.WithKeys("f1"), key.WithHelp("F1", "ayuda")),
	Palette:  key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("Ctrl+P", "paleta")),
	NextPane: key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "desplazarse")),
	PrevPane: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("Shift+Tab", "zona anterior")),
	New:      key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("Ctrl+N", "nueva instancia")),
	Health:   key.NewBinding(key.WithKeys("f2"), key.WithHelp("F2", "salud")),
	Copy:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copiar")),
	Quit:     key.NewBinding(key.WithKeys("ctrl+q", "ctrl+c"), key.WithHelp("Ctrl+Q", "salir")),
	Yes:      key.NewBinding(key.WithKeys("y")),
	No:       key.NewBinding(key.WithKeys("n")),
}
