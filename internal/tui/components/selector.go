package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// SelectMode es el modo de navegación del selector.
type SelectMode int

// Modos del selector.
const (
	// SelectList navega con ↑/↓ en una sola columna.
	SelectList SelectMode = iota
	// SelectGrid navega con las cuatro flechas sobre una rejilla alineada.
	SelectGrid
)

// SelectItem es una opción del selector. Value es la identidad estable con la
// que se expone la selección (T-04): nunca se trabaja por índice.
type SelectItem struct {
	Value       string
	Label       string
	Detail      string
	Disabled    bool
	DisabledWhy string
	Recommended bool
}

// cellText es el texto de una celda sin el puntero.
func (it SelectItem) cellText(current string) string {
	t := it.Label
	if it.Recommended {
		t += " ★"
	}
	if current != "" && it.Value == current {
		t += " (actual)"
	}
	return t
}

// disableReason devuelve el motivo de una opción deshabilitada (con respaldo).
func disableReason(it SelectItem) string {
	if strings.TrimSpace(it.DisabledWhy) != "" {
		return it.DisabledWhy
	}
	return "no disponible"
}

// Selector es el componente ÚNICO de selección (P-03): lo usan Ajustes, los
// flujos de creación (rápida y avanzada), la regeneración y cualquier lista. El
// cursor es interno, pero la selección se expone por VALOR, no por índice.
type Selector struct {
	Title string
	Items []SelectItem
	Mode  SelectMode
	// ColW fuerza el ancho de columna en modo rejilla (0 = calculado).
	ColW int
	// Cols fuerza el número de columnas en modo rejilla (0 = calculado).
	Cols int
	// Pad prefija cada celda de rejilla con un margen (para incrustar el selector
	// bajo una etiqueta, p. ej. en Ajustes).
	Pad    string
	cursor int
}

// NewSelector crea un selector en modo lista vertical.
func NewSelector(title string, items []SelectItem) *Selector {
	return &Selector{Title: title, Items: items, Mode: SelectList}
}

// NewSelectorMode crea un selector con un modo explícito.
func NewSelectorMode(title string, items []SelectItem, mode SelectMode) *Selector {
	return &Selector{Title: title, Items: items, Mode: mode}
}

// Len devuelve el número de opciones.
func (s *Selector) Len() int { return len(s.Items) }

// Cursor devuelve la posición actual (solo para pintar; no es la selección).
func (s *Selector) Cursor() int { return s.cursor }

// SetCursor fija el cursor, acotado a los límites de la lista.
func (s *Selector) SetCursor(i int) {
	n := len(s.Items)
	if n == 0 {
		s.cursor = 0
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= n {
		i = n - 1
	}
	s.cursor = i
}

// Move mueve el cursor d posiciones en modo lista (↑/↓).
func (s *Selector) Move(d int) { s.SetCursor(s.cursor + d) }

// MoveGrid mueve el cursor en una rejilla con las cuatro flechas. dy salta de
// fila en fila y dx se mueve dentro de la fila, sin escapar de la rejilla.
func (s *Selector) MoveGrid(dx, dy, cols int) {
	n := len(s.Items)
	if n == 0 {
		return
	}
	if cols < 1 {
		cols = 1
	}
	if dx != 0 {
		row := s.cursor / cols
		col := s.cursor%cols + dx
		if col < 0 {
			col = 0
		}
		if col >= cols {
			col = cols - 1
		}
		s.SetCursor(row*cols + col)
		return
	}
	s.SetCursor(s.cursor + dy*cols)
}

// Selected devuelve el elemento bajo el cursor.
func (s *Selector) Selected() (SelectItem, bool) {
	if s.cursor < 0 || s.cursor >= len(s.Items) {
		return SelectItem{}, false
	}
	return s.Items[s.cursor], true
}

// Value devuelve el VALUE seleccionado (vacío si no hay opciones). La selección
// se expone por valor, nunca por índice.
func (s *Selector) Value() string {
	if it, ok := s.Selected(); ok {
		return it.Value
	}
	return ""
}

// SetValue coloca el cursor sobre el elemento con ese valor y devuelve si lo
// encontró. Es la forma de restaurar una selección guardada por valor.
func (s *Selector) SetValue(v string) bool {
	for i, it := range s.Items {
		if it.Value == v {
			s.cursor = i
			return true
		}
	}
	return false
}

// ColumnWidth devuelve el ancho de columna fijo de la rejilla: el mayor texto de
// celda más el puntero y una separación uniforme.
func (s *Selector) ColumnWidth(current string) int {
	if s.ColW > 0 {
		return s.ColW
	}
	max := 0
	for _, it := range s.Items {
		if n := layout.Width(it.cellText(current)); n > max {
			max = n
		}
	}
	colW := max + 3 // puntero + espacio + separador
	if colW < 8 {
		colW = 8
	}
	return colW
}

// GridColumns calcula cuántas columnas caben en el ancho w (adaptable al terminal).
func (s *Selector) GridColumns(w int, current string) int {
	if s.Cols > 0 {
		return s.Cols
	}
	colW := s.ColumnWidth(current)
	if w <= 0 {
		return 1
	}
	n := w / colW
	if n < 1 {
		n = 1
	}
	if len(s.Items) > 0 && n > len(s.Items) {
		n = len(s.Items)
	}
	return n
}

// Render pinta el selector según su modo.
func (s *Selector) Render(w int, current string, pal theme.Palette, ascii bool) []string {
	if s.Mode == SelectGrid {
		return s.RenderGrid(w, current, pal, ascii)
	}
	return s.RenderList(w, current, pal, ascii)
}

// RenderList pinta una fila por opción: la enfocada lleva puntero y banda, la
// guardada se marca «(actual)» y las deshabilitadas explican el motivo.
func (s *Selector) RenderList(w int, current string, pal theme.Palette, ascii bool) []string {
	if w < 4 {
		w = 4
	}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(pal.Muted))
	band := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(pal.AccentText)).Background(lipgloss.Color(pal.Accent))
	out := make([]string, 0, len(s.Items))
	for i, it := range s.Items {
		label := it.Label
		if it.Recommended {
			label = "★ " + label
		}
		row := label
		if it.Detail != "" {
			row += "  " + it.Detail
		}
		if current != "" && it.Value == current {
			row += "  (actual)"
		}
		switch {
		case i == s.cursor:
			// La opción enfocada lleva puntero y banda; si además está deshabilitada,
			// conserva el motivo (no se puede elegir sin saber por qué).
			if it.Disabled {
				row += "  — " + disableReason(it)
			}
			bandRow := s.Pad + Pointer(ascii) + " " + row
			plain := layout.Truncate(bandRow, w)
			if pad := w - layout.Width(plain); pad > 0 {
				plain += strings.Repeat(" ", pad)
			}
			out = append(out, band.Render(plain))
		case it.Disabled:
			out = append(out, muted.Render(layout.Truncate(s.Pad+"  "+row+"  — "+disableReason(it), w)))
		default:
			out = append(out, layout.Truncate(s.Pad+"  "+row, w))
		}
	}
	return out
}

// RenderGrid pinta la rejilla alineada: ancho de columna fijo y columnas
// adaptables al ancho de terminal. La celda enfocada lleva banda invertida.
func (s *Selector) RenderGrid(w int, current string, pal theme.Palette, ascii bool) []string {
	inner := w - layout.Width(s.Pad)
	if inner < 4 {
		inner = 4
	}
	colW := s.ColumnWidth(current)
	cols := s.GridColumns(inner, current)
	if cols < 1 {
		cols = 1
	}
	band := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(pal.AccentText)).Background(lipgloss.Color(pal.Accent))
	plain := lipgloss.NewStyle().Foreground(lipgloss.Color(pal.Foreground))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(pal.Muted))
	var out []string
	for start := 0; start < len(s.Items); start += cols {
		end := start + cols
		if end > len(s.Items) {
			end = len(s.Items)
		}
		var b strings.Builder
		b.WriteString(s.Pad)
		for i := start; i < end; i++ {
			if i > start {
				b.WriteString(" ")
			}
			it := s.Items[i]
			// Ancho de la celda: fijo salvo la última de la fila (sin relleno ciego).
			cellW := colW - 1
			if i == end-1 {
				cellW = inner - (end-start-1)*colW
				if cellW < 1 {
					cellW = 1
				}
			}
			text := layout.Truncate(it.cellText(current), cellW)
			switch {
			case i == s.cursor:
				if pad := cellW - layout.Width(text); pad > 0 {
					text += strings.Repeat(" ", pad)
				}
				b.WriteString(band.Render(text))
			case it.Disabled:
				b.WriteString(dim.Render(text))
			default:
				b.WriteString(plain.Render(text))
			}
		}
		out = append(out, b.String())
	}
	return out
}
