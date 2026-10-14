package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// Button pinta una acción como "[S] Iniciar «Survival»". Si está deshabilitada,
// muestra el motivo (regla "deshabilitado ≠ oculto", RF-7). Si focused, la
// resalta. El botón queda envuelto en una zona de ratón clicable (§4.1).
func Button(a Action, focused bool, p theme.Palette) string {
	return zone.Mark("action:"+a.ID, buttonRender(a, focused, p))
}

func buttonRender(a Action, focused bool, p theme.Palette) string {
	label := "[" + keyOr(a.Key) + "] " + a.Label()
	switch {
	case a.Disabled:
		motivo := a.DisabledReason
		if motivo == "" {
			motivo = "no disponible"
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color(p.Muted)).Render("· " + a.Label() + " — " + motivo)
	case focused:
		st := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.AccentText)).Background(lipgloss.Color(p.Accent))
		return st.Render(" " + label + " ")
	case a.Kind == Destructive:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(p.Danger)).Render(label + " ⚠")
	case a.Kind == Primary:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)).Render(label)
	}
	return label
}

func keyOr(k string) string {
	if k == "" {
		return "-"
	}
	return strings.ToUpper(k)
}

// ActionBar pinta una barra de acciones por ancho w. En Compact solo caben 3
// botones + "Más (m)". Si los botones no caben en una línea, se reparten en
// varias líneas (nunca desbordan el ancho).
func ActionBar(actions []Action, w int, focused int, p theme.Palette) string {
	if len(actions) == 0 {
		return ""
	}
	limit := len(actions)
	compact := layout.Classify(w, 24) == layout.Compact
	if compact && limit > 4 {
		limit = 3
	}
	parts := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		parts = append(parts, Button(actions[i], i == focused, p))
	}
	if limit < len(actions) {
		parts = append(parts, "[M] Más…")
	}
	return wrapParts(parts, w, "  ")
}

// wrapParts reparte los fragmentos en líneas de ancho máximo w.
func wrapParts(parts []string, w int, sep string) string {
	if w <= 0 {
		return strings.Join(parts, sep)
	}
	var lines []string
	cur := ""
	for _, part := range parts {
		candidate := part
		if cur != "" {
			candidate = cur + sep + part
		}
		if layout.Truncate(part, w) != part || layout.Width(candidate) > w {
			if cur != "" {
				lines = append(lines, cur)
			}
			cur = layout.Truncate(part, w)
			continue
		}
		cur = candidate
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return strings.Join(lines, "\n")
}

// Badge pinta una insignia de estado (icono + texto, nunca solo color).
func Badge(state string, p theme.Palette, ascii bool) string {
	text := theme.Icon(state, ascii)
	color := p.Foreground
	switch state {
	case "running":
		color = p.Good
	case "stopped":
		color = p.Muted
	case "starting":
		color = p.Warn
	case "error":
		color = p.Bad
	case "idle":
		color = p.Accent
	case "problems":
		color = p.Warn
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(text)
}

// Stepper pinta un asistente por pasos: "1 Básico › 2 Mundo › 3 …".
func Stepper(steps []string, current int, p theme.Palette) string {
	parts := make([]string, 0, len(steps)*2)
	for i, s := range steps {
		label := fmt.Sprintf("%d %s", i+1, s)
		switch {
		case i == current:
			parts = append(parts, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Accent)).Render(label))
		case i < current:
			parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color(p.Good)).Render(label))
		default:
			parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color(p.Muted)).Render(label))
		}
		if i < len(steps)-1 {
			parts = append(parts, "›")
		}
	}
	return strings.Join(parts, " ")
}

// Sparkline dibuja una mini-gráfica con los niveles ▁▂▃▄▅▆▇█.
func Sparkline(values []float64, w int) string {
	blocks := []rune("▁▂▃▄▅▆▇█")
	if len(values) == 0 {
		return ""
	}
	if w > 0 && len(values) > w {
		values = values[len(values)-w:]
	}
	min, max := values[0], values[0]
	for _, v := range values {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	span := max - min
	var b strings.Builder
	for _, v := range values {
		idx := 0
		if span > 0 {
			idx = int((v - min) / span * float64(len(blocks)-1))
		}
		if idx < 0 {
			idx = 0
		}
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		b.WriteRune(blocks[idx])
	}
	return b.String()
}

// Toast es una notificación efímera.
type Toast struct {
	Text string
	Kind Kind // Secondary (info), Destructive (error)
}

// RenderToast pinta un toast en una sola línea.
func RenderToast(t Toast, p theme.Palette) string {
	color := p.Accent
	if t.Kind == Destructive {
		color = p.Danger
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true).Render("▸ " + t.Text)
}
