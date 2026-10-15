package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// footerSep separa los atajos de la barra inferior.
const footerSep = "   "

// footerLabels fija las etiquetas definitivas de la barra inferior (SPEC 2 §1.2).
// Cualquier otra tecla usa como etiqueta el verbo de su acción, pero TODAS se
// pintan con el mismo formato «[Tecla] Etiqueta».
var footerLabels = map[string]string{
	"b":   "Backup",
	"f1":  "Ayuda",
	"tab": "Desplazarse",
	"d":   "Borrar",
	"esc": "Atrás",
}

// footerKeyText fija la forma visible de las teclas no imprimibles.
var footerKeyText = map[string]string{
	"f1": "F1", "f2": "F2", "f3": "F3",
	"tab": "TAB", "shift+tab": "Shift+Tab", "esc": "Esc", "enter": "Enter",
	"ctrl+p": "Ctrl+P", "ctrl+q": "Ctrl+Q", "ctrl+n": "Ctrl+N",
	"ctrl+c": "Ctrl+C", "ctrl+r": "Ctrl+R", "ctrl+t": "Ctrl+T",
	"up": "↑", "down": "↓", "left": "←", "right": "→", " ": "Espacio",
}

// FooterKey es un atajo de la barra inferior: tecla, etiqueta y si va resaltado.
type FooterKey struct {
	Key       string
	Label     string
	Highlight bool
}

// FooterKeyFor construye un atajo con la etiqueta definitiva si la tecla la
// tiene y, si no, con el texto dado (normalmente el verbo de la acción).
func FooterKeyFor(key, fallback string) FooterKey {
	canon := strings.ToLower(strings.TrimSpace(key))
	label := strings.TrimSpace(fallback)
	if fixed, ok := footerLabels[canon]; ok {
		label = fixed
	}
	return FooterKey{Key: DisplayKey(key), Label: label}
}

// DisplayKey devuelve la tecla tal y como se muestra («f1» → «F1», «tab» → «TAB»,
// «s» → «S»). Es la única traducción de teclas de la barra inferior.
func DisplayKey(key string) string {
	canon := strings.ToLower(strings.TrimSpace(key))
	if text, ok := footerKeyText[canon]; ok {
		return text
	}
	trimmed := strings.TrimSpace(key)
	if len([]rune(trimmed)) == 1 {
		return strings.ToUpper(trimmed)
	}
	return trimmed
}

// Text es la representación plana «[Tecla] Etiqueta».
func (f FooterKey) Text() string { return "[" + f.Key + "] " + f.Label }

// RenderFooter compone la barra de atajos en una línea con el formato único
// «[Tecla] Etiqueta»: los resaltados (p. ej. «[Esc] Atrás» con el input de
// comandos enfocado) van en banda invertida y los atajos que no caben se
// descartan enteros, nunca a medias.
func RenderFooter(items []FooterKey, w int, pal theme.Palette) string {
	if w < 1 {
		w = 1
	}
	var b strings.Builder
	used := 0
	for i, it := range items {
		text := it.Text()
		tw := layout.Width(text)
		sep := 0
		if i > 0 {
			sep = layout.Width(footerSep)
		}
		switch {
		case used == 0 && tw > w:
			return layout.Truncate(text, w)
		case used+sep+tw > w:
			return b.String()
		}
		if i > 0 {
			b.WriteString(footerSep)
			used += sep
		}
		used += tw
		if it.Highlight {
			b.WriteString(lipgloss.NewStyle().Bold(true).
				Foreground(lipgloss.Color(pal.AccentText)).
				Background(lipgloss.Color(pal.Accent)).Render(text))
			continue
		}
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(pal.Muted)).Render(text))
	}
	return b.String()
}
