package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// frameChars es el juego de caracteres del borde de un panel: caja redondeada
// Unicode o ASCII plano (modo accesible, §4.2).
type frameChars struct {
	tl, tr, bl, br, hz, vt string
}

func frameFor(ascii bool) frameChars {
	if ascii {
		return frameChars{"+", "+", "+", "+", "-", "|"}
	}
	return frameChars{"╭", "╮", "╰", "╯", "─", "│"}
}

// Panel dibuja una caja con borde. w y h incluyen los bordes; el cuerpo se
// recorta y rellena exactamente a w×h para que las columnas no bailen al lado de
// otros paneles (§5.2). «title» se incrusta en el borde superior y «right» se
// alinea a la derecha (contadores). focused resalta el borde en el acento y pone
// el título en negrita: el foco se ve sin depender solo del color.
func Panel(title, right, body string, w, h int, focused bool, p theme.Palette, ascii bool) string {
	if w < 2 {
		w = 2
	}
	if h < 2 {
		h = 2
	}
	f := frameFor(ascii)
	borderColor := p.Border
	titleColor := p.Foreground
	if focused {
		borderColor = p.Accent
		titleColor = p.Accent
	}
	bs := lipgloss.NewStyle().Foreground(lipgloss.Color(borderColor))
	ts := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(titleColor))
	rs := lipgloss.NewStyle().Foreground(lipgloss.Color(p.Muted))

	innerW := w - 2
	titleTxt := strings.TrimSpace(title)
	rightTxt := strings.TrimSpace(right)

	// Ancho del segmento derecho: " RIGHT " + trazo.
	rightSeg := 0
	if rightTxt != "" {
		rightSeg = 3 + layout.Width(rightTxt)
	}
	// El título manda: si no cabe completo junto al contador, el contador cede;
	// solo si el título tampoco cabe se recorta. Ninguna pieza desborda innerW.
	needTitle := 3 + layout.Width(titleTxt)
	if titleTxt != "" && innerW-1-rightSeg < needTitle {
		rightTxt = ""
		rightSeg = 0
	}
	if titleTxt != "" && needTitle > innerW-1 {
		avail := innerW - 1 - 2 // huecos " " … " "
		if avail < 1 {
			titleTxt = ""
		} else {
			titleTxt = layout.Truncate(titleTxt, avail)
		}
	}
	if rightTxt != "" && rightSeg > innerW-1 {
		rightTxt = ""
		rightSeg = 0
	}
	titleSeg := 0
	if titleTxt != "" {
		titleSeg = 2 + layout.Width(titleTxt)
	}
	fill := innerW - 1 - titleSeg - rightSeg
	if fill < 0 {
		fill = 0
	}

	var b strings.Builder
	b.WriteString(bs.Render(f.tl + f.hz))
	if titleTxt != "" {
		b.WriteString(bs.Render(" "))
		b.WriteString(ts.Render(titleTxt))
		b.WriteString(bs.Render(" "))
	}
	b.WriteString(bs.Render(strings.Repeat(f.hz, fill)))
	if rightTxt != "" {
		b.WriteString(rs.Render(" " + rightTxt + " "))
		b.WriteString(bs.Render(f.hz))
	}
	b.WriteString(bs.Render(f.tr))
	b.WriteString("\n")

	bodyLines := strings.Split(body, "\n")
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(bodyLines) {
			line = strings.TrimRight(bodyLines[i], "\r")
		}
		b.WriteString(bs.Render(f.vt))
		b.WriteString(padCells(line, innerW))
		b.WriteString(bs.Render(f.vt))
		if i < h-3 {
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(bs.Render(f.bl + strings.Repeat(f.hz, innerW) + f.br))
	return b.String()
}

// padCells ajusta una cadena al ancho visual exacto: recorta con «…» si sobra y
// rellena con espacios si falta. Mide en celdas (ANSI-safe), nunca con len().
func padCells(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := layout.Width(s)
	if sw > w {
		s = layout.Truncate(s, w)
		sw = layout.Width(s)
	}
	if sw < w {
		s += strings.Repeat(" ", w-sw)
	}
	return s
}
