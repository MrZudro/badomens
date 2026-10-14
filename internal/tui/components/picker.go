package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// PickerItem es un elemento de un selector.
type PickerItem struct {
	Label  string
	Value  string
	Detail string
}

// Picker es un selector filtrable (motores, versiones…).
type Picker struct {
	Title  string
	Items  []PickerItem
	Filter string
	cursor int
}

// NewPicker crea un selector.
func NewPicker(title string, items []PickerItem) *Picker {
	return &Picker{Title: title, Items: items}
}

// Filtered devuelve los elementos que cumplen el filtro actual.
func (p *Picker) Filtered() []PickerItem {
	f := strings.ToLower(strings.TrimSpace(p.Filter))
	if f == "" {
		return p.Items
	}
	out := make([]PickerItem, 0, len(p.Items))
	for _, it := range p.Items {
		if strings.Contains(strings.ToLower(it.Label), f) || strings.Contains(strings.ToLower(it.Detail), f) {
			out = append(out, it)
		}
	}
	return out
}

// Move desplaza el cursor dentro de los elementos filtrados.
func (p *Picker) Move(d int) {
	n := len(p.Filtered())
	if n == 0 {
		p.cursor = 0
		return
	}
	p.cursor += d
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor >= n {
		p.cursor = n - 1
	}
}

// Selected devuelve el elemento seleccionado.
func (p *Picker) Selected() (PickerItem, bool) {
	f := p.Filtered()
	if len(f) == 0 || p.cursor < 0 || p.cursor >= len(f) {
		return PickerItem{}, false
	}
	return f[p.cursor], true
}

// Type añade un carácter al filtro.
func (p *Picker) Type(r rune) {
	p.Filter += string(r)
	p.cursor = 0
}

// Backspace borra el último carácter del filtro.
func (p *Picker) Backspace() {
	if p.Filter == "" {
		return
	}
	r := []rune(p.Filter)
	p.Filter = string(r[:len(r)-1])
}

// Render pinta el selector con scroll.
func (p *Picker) Render(w, h int, pal theme.Palette) string {
	var b strings.Builder
	title := p.Title
	if p.Filter != "" {
		title += "  /" + p.Filter
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(pal.Accent)).Render(title))
	b.WriteString("\n")

	items := p.Filtered()
	if len(items) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(pal.Muted)).Render("  Sin coincidencias"))
		return b.String()
	}
	visible := h - 2
	if visible <= 0 {
		visible = len(items)
	}
	start := 0
	if p.cursor >= visible {
		start = p.cursor - visible + 1
	}
	end := start + visible
	if end > len(items) {
		end = len(items)
	}
	for i := start; i < end; i++ {
		it := items[i]
		line := "  " + it.Label
		if it.Detail != "" {
			line += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(pal.Muted)).Render(it.Detail)
		}
		if i == p.cursor {
			b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(pal.AccentText)).Background(lipgloss.Color(pal.Accent)).Render("▶ " + it.Label))
			if it.Detail != "" {
				b.WriteString("  " + it.Detail)
			}
		} else {
			b.WriteString(layout.Truncate(line, w))
		}
		b.WriteString("\n")
	}
	if start > 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(pal.Muted)).Render("▲ " + itoa(start) + " más"))
		b.WriteString("\n")
	}
	if end < len(items) {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(pal.Muted)).Render("▼ " + itoa(len(items)-end) + " más"))
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
