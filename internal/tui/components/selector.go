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
	// Group, si no está vacío, agrupa la opción bajo una cabecera de sección
	// (p. ej. la versión mayor de Minecraft). Las opciones deben llegar YA
	// agrupadas y en el orden de la fuente: el selector no reordena ni filtra
	// nada (T-01).
	Group string
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
// fila en fila y dx se mueve dentro de la fila, sin escapar de la rejilla. Con
// opciones agrupadas, las filas se calculan por grupo (cada grupo conserva su
// propia rejilla y ↑/↓ no saltan de un grupo a otro por una columna inexistente).
func (s *Selector) MoveGrid(dx, dy, cols int) {
	n := len(s.Items)
	if n == 0 {
		return
	}
	if cols < 1 {
		cols = 1
	}
	if s.grouped() {
		s.moveGrouped(dx, dy, cols)
		return
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

// grouped indica si alguna opción declara grupo.
func (s *Selector) grouped() bool {
	for _, it := range s.Items {
		if strings.TrimSpace(it.Group) != "" {
			return true
		}
	}
	return false
}

// gridRow es una fila de la rejilla: la cabecera de su grupo (si la abre) y las
// opciones que contiene.
type gridRow struct {
	header string
	items  []int
}

// gridRows reparte las opciones en filas de `cols` celdas respetando los grupos:
// cada grupo se abre con su propio reparto, de modo que una versión mayor nunca
// comparte fila con otra.
func (s *Selector) gridRows(cols int) []gridRow {
	if cols < 1 {
		cols = 1
	}
	var rows []gridRow
	for i := 0; i < len(s.Items); {
		group := s.Items[i].Group
		end := i
		for end < len(s.Items) && s.Items[end].Group == group {
			end++
		}
		for start := i; start < end; start += cols {
			stop := start + cols
			if stop > end {
				stop = end
			}
			row := gridRow{}
			if start == i {
				row.header = group
			}
			for k := start; k < stop; k++ {
				row.items = append(row.items, k)
			}
			rows = append(rows, row)
		}
		i = end
	}
	return rows
}

// moveGrouped mueve el cursor con las cuatro flechas sobre la rejilla agrupada.
func (s *Selector) moveGrouped(dx, dy, cols int) {
	rows := s.gridRows(cols)
	row, col := -1, -1
	for ri, r := range rows {
		for ci, idx := range r.items {
			if idx == s.cursor {
				row, col = ri, ci
			}
		}
	}
	if row < 0 {
		return
	}
	if dx != 0 {
		col += dx
		if col < 0 {
			col = 0
		}
		if col >= len(rows[row].items) {
			col = len(rows[row].items) - 1
		}
		s.SetCursor(rows[row].items[col])
		return
	}
	row += dy
	if row < 0 {
		row = 0
	}
	if row >= len(rows) {
		row = len(rows) - 1
	}
	if col >= len(rows[row].items) {
		col = len(rows[row].items) - 1
	}
	s.SetCursor(rows[row].items[col])
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

// visualLine es una línea pintable del selector: o una cabecera de grupo o una
// fila con una o varias opciones. Mantener una sola descripción de la rejilla
// evita que la navegación y el pintado se desalineen.
type visualLine struct {
	header string
	items  []int
	focus  int // índice de la opción enfocada en la línea (-1 si ninguna)
}

// lines describe las líneas visibles del selector con el reparto de columnas
// indicado (1 = una opción por línea).
func (s *Selector) lines(cols int) []visualLine {
	if s.Mode != SelectGrid {
		return s.listLines()
	}
	if cols < 1 {
		cols = 1
	}
	rows := s.gridRows(cols)
	out := make([]visualLine, 0, len(rows)*2)
	for _, r := range rows {
		// La cabecera del grupo es su PROPIA línea: así el pintado y la ventana del
		// foco no confunden la cabecera con una fila de celdas.
		if r.header != "" {
			out = append(out, visualLine{header: r.header, focus: -1})
		}
		vl := visualLine{items: r.items, focus: -1}
		for _, idx := range r.items {
			if idx == s.cursor {
				vl.focus = idx
			}
		}
		out = append(out, vl)
	}
	return out
}

// listLines describe el selector en modo lista: una línea por opción, con la
// cabecera de grupo delante de la primera opción de cada grupo.
func (s *Selector) listLines() []visualLine {
	out := make([]visualLine, 0, len(s.Items)*2)
	last := ""
	for i, it := range s.Items {
		if g := strings.TrimSpace(it.Group); g != "" && g != last {
			out = append(out, visualLine{header: g, focus: -1})
			last = g
		}
		focus := -1
		if i == s.cursor {
			focus = i
		}
		out = append(out, visualLine{items: []int{i}, focus: focus})
	}
	return out
}

// FocusLine devuelve el índice de la línea visible que contiene la opción
// enfocada, con el reparto de columnas indicado (1 en modo lista). Lo usan los
// llamantes para mantener el foco dentro de su ventana.
func (s *Selector) FocusLine(cols int) int {
	for i, vl := range s.lines(cols) {
		if vl.focus == s.cursor {
			return i
		}
	}
	return 0
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
	out := make([]string, 0, len(s.Items))
	for _, vl := range s.listLines() {
		if vl.header != "" {
			out = append(out, renderGroupHeader(vl.header, w, pal, false))
			continue
		}
		out = append(out, s.renderListRow(vl, w, current, pal, ascii))
	}
	return out
}

// renderGroupHeader pinta la cabecera de un grupo: texto atenuado y en negrita,
// recortado al ancho disponible (nunca a medias por el color).
func renderGroupHeader(group string, w int, pal theme.Palette, padded bool) string {
	prefix := ""
	if padded {
		prefix = "  "
	}
	text := prefix + strings.ToUpper(strings.TrimSpace(group))
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(pal.Muted)).Render(layout.Truncate(text, w))
}

// renderListRow pinta una fila de una sola opción.
func (s *Selector) renderListRow(vl visualLine, w int, current string, pal theme.Palette, ascii bool) string {
	if len(vl.items) == 0 {
		return ""
	}
	i := vl.items[0]
	it := s.Items[i]
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(pal.Muted))
	band := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(pal.AccentText)).Background(lipgloss.Color(pal.Accent))
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
		return band.Render(plain)
	case it.Disabled:
		return muted.Render(layout.Truncate(s.Pad+"  "+row+"  — "+disableReason(it), w))
	default:
		return layout.Truncate(s.Pad+"  "+row, w)
	}
}

// RenderGrid pinta la rejilla alineada: ancho de columna fijo y columnas
// adaptables al ancho de terminal. La celda enfocada lleva banda invertida.
func (s *Selector) RenderGrid(w int, current string, pal theme.Palette, ascii bool) []string {
	inner := w - layout.Width(s.Pad)
	if inner < 4 {
		inner = 4
	}
	cols := s.GridColumns(inner, current)
	lines := s.lines(cols)
	out := make([]string, 0, len(lines))
	for _, vl := range lines {
		if vl.header != "" {
			out = append(out, s.Pad+renderGroupHeader(vl.header, inner, pal, false))
			continue
		}
		out = append(out, s.renderGridRow(vl, inner, cols, current, pal))
	}
	return out
}

// renderGridRow pinta una fila de la rejilla: ancho de celda fijo salvo la
// última de la fila (sin relleno ciego).
func (s *Selector) renderGridRow(vl visualLine, inner, cols int, current string, pal theme.Palette) string {
	if len(vl.items) == 0 {
		return ""
	}
	colW := s.ColumnWidth(current)
	band := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(pal.AccentText)).Background(lipgloss.Color(pal.Accent))
	plain := lipgloss.NewStyle().Foreground(lipgloss.Color(pal.Foreground))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(pal.Muted))
	var b strings.Builder
	b.WriteString(s.Pad)
	for n, i := range vl.items {
		if n > 0 {
			b.WriteString(" ")
		}
		it := s.Items[i]
		cellW := colW - 1
		if n == len(vl.items)-1 {
			cellW = inner - (len(vl.items)-1)*colW
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
	return b.String()
}
