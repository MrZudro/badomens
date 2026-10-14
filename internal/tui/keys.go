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

// DefaultKeys es el mapa por defecto. Tab/Shift+Tab cambian de pantalla de primer
// nivel (inicio → instancias → salud → ayuda → inicio); las flechas ←/→ cambian
// de pestaña dentro de una instancia.
var DefaultKeys = KeyMap{
	Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "subir")),
	Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "bajar")),
	Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "abrir/confirmar")),
	Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "volver")),
	Help:     key.NewBinding(key.WithKeys("f1"), key.WithHelp("F1", "ayuda")),
	Palette:  key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("Ctrl+P", "comandos")),
	NextPane: key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "cambiar pantalla")),
	PrevPane: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("Shift+Tab", "pantalla anterior")),
	New:      key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("Ctrl+N", "nueva instancia")),
	Health:   key.NewBinding(key.WithKeys("f2"), key.WithHelp("F2", "salud del sistema")),
	Copy:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copiar")),
	Quit:     key.NewBinding(key.WithKeys("ctrl+q", "ctrl+c"), key.WithHelp("Ctrl+Q", "salir")),
	Yes:      key.NewBinding(key.WithKeys("y")),
	No:       key.NewBinding(key.WithKeys("n")),
}

// ShortHelp es la barra de ayuda global (siempre visible en la última línea).
func (k KeyMap) ShortHelp() string {
	return "F1 Ayuda · Tab Pantalla · Ctrl+P Comandos · F2 Salud · Ctrl+Q Salir"
}

// ShortHelpASCII es la barra de ayuda en modo ASCII (separadores planos).
func (k KeyMap) ShortHelpASCII() string {
	return "F1 Ayuda | Tab Pantalla | Ctrl+P Comandos | F2 Salud | Ctrl+Q Salir"
}

// GlobalHelp es la barra compacta de atajos globales no imprimibles para la
// línea de acciones (§2): deja sitio a los atajos contextuales de la pantalla.
// Se mantiene corta (misma longitud que antes de añadir Tab) para que en 70
// columnas sigan viéndose los atajos de la pantalla; el detalle de Tab vive en
// la ayuda (F1).
func (k KeyMap) GlobalHelp() string {
	return "F1 Ayuda · Tab · Ctrl+P · F2 · Ctrl+Q Salir"
}

// GlobalHelpASCII es la variante ASCII de GlobalHelp.
func (k KeyMap) GlobalHelpASCII() string {
	return "F1 Ayuda | Tab | Ctrl+P | F2 | Ctrl+Q Salir"
}

// GlobalHelpTiny es la barra de atajos mínima para terminales estrechas, para no
// perder «Salir» por truncado. Conserva «Tab» (navegación entre pantallas, que
// incluye Salud) y «Ayuda»; F2 se omite porque Tab ya llega a Salud a ese ancho.
func (k KeyMap) GlobalHelpTiny() string {
	return "F1 Ayuda · Tab · ^P · ^Q Salir"
}

// GlobalHelpTinyASCII es la variante ASCII de GlobalHelpTiny.
func (k KeyMap) GlobalHelpTinyASCII() string {
	return "F1 Ayuda | Tab | ^P | ^Q Salir"
}

// FullHelp devuelve la hoja de atajos completa de la pantalla actual.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter, k.Back},
		{k.Help, k.Palette, k.NextPane, k.PrevPane},
		{k.New, k.Health, k.Copy, k.Quit},
	}
}
