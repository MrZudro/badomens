package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// Field es un campo del asistente. Short es la descripción corta (≤ 90
// caracteres) que SIEMPRE se muestra bajo la etiqueta (RF-4).
type Field struct {
	Key         string
	Label       string
	Short       string
	Value       string
	Placeholder string
	Long        string
	// Disabled marca un campo no editable con motivo.
	Disabled       bool
	DisabledReason string
}

// Form es un formulario por pasos con ayuda por campo.
type Form struct {
	Title  string
	Fields []Field
	cursor int
}

// NewForm crea un formulario.
func NewForm(title string, fields []Field) *Form {
	return &Form{Title: title, Fields: fields}
}

// Move desplaza el campo activo.
func (f *Form) Move(d int) {
	if len(f.Fields) == 0 {
		return
	}
	f.cursor += d
	if f.cursor < 0 {
		f.cursor = len(f.Fields) - 1
	}
	if f.cursor >= len(f.Fields) {
		f.cursor = 0
	}
}

// Current devuelve el campo activo.
func (f *Form) Current() (Field, bool) {
	if len(f.Fields) == 0 || f.cursor < 0 || f.cursor >= len(f.Fields) {
		return Field{}, false
	}
	return f.Fields[f.cursor], true
}

// SetValue asigna el valor del campo activo.
func (f *Form) SetValue(v string) {
	if f.cursor >= 0 && f.cursor < len(f.Fields) {
		f.Fields[f.cursor].Value = v
	}
}

// Render pinta el formulario. Todo campo muestra su descripción corta.
func (f *Form) Render(w int, pal theme.Palette) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(pal.Accent)).Render(f.Title))
	b.WriteString("\n")
	for i, field := range f.Fields {
		marker := "  "
		labelStyle := lipgloss.NewStyle()
		if i == f.cursor {
			marker = "▶ "
			labelStyle = labelStyle.Bold(true).Foreground(lipgloss.Color(pal.Accent))
		}
		value := field.Value
		if value == "" {
			value = lipgloss.NewStyle().Foreground(lipgloss.Color(pal.Muted)).Render(field.Placeholder)
		}
		b.WriteString(marker + labelStyle.Render(field.Label) + ": " + value)
		b.WriteString("\n")
		if field.Disabled {
			b.WriteString("    " + lipgloss.NewStyle().Foreground(lipgloss.Color(pal.Muted)).Render("("+field.DisabledReason+")"))
		} else if field.Short != "" {
			b.WriteString("    " + lipgloss.NewStyle().Foreground(lipgloss.Color(pal.Muted)).Render(layout.Truncate(field.Short, 88)))
		}
		b.WriteString("\n")
	}
	return b.String()
}
