// Package layout define los breakpoints responsivos de la TUI v2 (RF-1/§4.1) y
// utilidades de truncado seguro por ancho real. Prohibido ancho/alto fijo en
// componentes: todo se deriva del Size recibido en View(w, h).
package layout

import "github.com/charmbracelet/x/ansi"

// Class es la clase de tamaño de la terminal.
type Class int

// Clases de tamaño.
const (
	// TooSmall: no se puede pintar nada útil.
	TooSmall Class = iota
	// Compact: una columna.
	Compact
	// Normal: dos paneles.
	Normal
	// Wide: tres zonas.
	Wide
)

// String devuelve el nombre estable de la clase.
func (c Class) String() string {
	switch c {
	case TooSmall:
		return "too-small"
	case Compact:
		return "compact"
	case Normal:
		return "normal"
	case Wide:
		return "wide"
	}
	return "unknown"
}

// Size es el tamaño disponible derivado de tea.WindowSizeMsg.
type Size struct {
	W, H  int
	Class Class
}

// Classify calcula la clase según la tabla de la spec.
func Classify(w, h int) Class {
	if w < 40 || h < 12 {
		return TooSmall
	}
	switch {
	case w < 70:
		return Compact
	case w < 110:
		return Normal
	}
	return Wide
}

// TooSmall indica que la terminal no puede pintar nada.
func (s Size) TooSmall() bool { return s.Class == TooSmall }

// Truncate recorta una cadena al ancho pedido usando el ancho real de pantalla
// (ansi.StringWidth), con elipsis al final.
func Truncate(s string, w int) string {
	if w <= 1 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	ell := "…"
	runes := []rune(s)
	for len(runes) > 0 {
		slice := string(runes)
		if ansi.StringWidth(slice)+ansi.StringWidth(ell) <= w {
			return slice + ell
		}
		runes = runes[:len(runes)-1]
	}
	return ell
}
