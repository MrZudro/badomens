package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/connection"
	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// Modal es un diálogo modal con acciones.
type Modal struct {
	Title   string
	Body    string
	Actions []Action
	Focused int
	// RequireText, si no está vacío, exige escribirlo para habilitar la acción
	// destructiva (confirmación reforzada, P-03).
	RequireText string
	// MaxWidth acota el ancho del cuadro (0 = 60 por defecto). La hoja de Ayuda
	// lo sube para aprovechar terminales anchas y repartir sus atajos en más
	// columnas sin romper la alineación (SPEC 2 §13.2).
	MaxWidth int
	// Typed es lo que el usuario ha escrito para RequireText.
	Typed string
}

// DestroyConfirm construye el modal de confirmación de una acción irreversible.
// El botón seguro ("No, volver") tiene el foco por defecto (RF-7 regla 5).
func DestroyConfirm(verb, object string) Modal {
	return Modal{
		Title: "Confirmar " + verb,
		Body:  "Esta acción no se puede deshacer.",
		Actions: []Action{
			{ID: "confirm", Verb: "Sí, " + verb + " " + object + " para siempre", Key: "y", Hint: "Aplica el cambio de forma irreversible.", Kind: Destructive, Disabled: true, DisabledReason: "Escribe el nombre para confirmar"},
			{ID: "cancel", Verb: "No, volver", Key: "n", Hint: "Cierra el diálogo sin cambiar nada.", Kind: Secondary},
		},
		Focused:     1,
		RequireText: object,
	}
}

// Enabled indica si la acción destructiva puede pulsarse.
func (m Modal) Enabled() bool {
	if m.RequireText == "" {
		return true
	}
	return strings.TrimSpace(m.Typed) == strings.TrimSpace(strings.Trim(m.RequireText, "«»"))
}

// modalBoxWidth acota el ancho del cuadro del modal: el ancho disponible menos el
// margen, con el tope de MaxWidth y un mínimo legible.
func modalBoxWidth(w, max int) int {
	boxW := w - 4
	if max <= 0 {
		max = 60
	}
	if boxW > max {
		boxW = max
	}
	if boxW < 20 {
		boxW = 20
	}
	return boxW
}

// actions es la lista de acciones con la confirmación reforzada ya habilitada (si
// el texto exigido coincide).
func (m Modal) actions() []Action {
	actions := m.Actions
	if !m.Enabled() {
		return actions
	}
	enabled := make([]Action, len(actions))
	copy(enabled, actions)
	for i := range enabled {
		if enabled[i].ID == "confirm" {
			enabled[i].Disabled = false
			enabled[i].DisabledReason = ""
		}
	}
	return enabled
}

// actionBlock pinta la barra de acciones del modal como líneas.
func (m Modal) actionBlock(w int, p theme.Palette) []string {
	return strings.Split(ActionBar(m.actions(), w, m.Focused, p), "\n")
}

// box envuelve el contenido en el cuadro del modal.
func (m Modal) box(boxW int, content string, ascii bool, p theme.Palette) string {
	return lipgloss.NewStyle().
		Border(Border(ascii)).
		BorderForeground(lipgloss.Color(p.Border)).
		Padding(0, 1).
		Width(boxW).
		Render(content)
}

// Render pinta el modal centrado en el ancho w.
func (m Modal) Render(w int, p theme.Palette, ascii bool) string {
	boxW := modalBoxWidth(w, m.MaxWidth)
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Accent)).Render(m.Title))
	if m.Body != "" {
		b.WriteString("\n" + m.Body)
	}
	if m.RequireText != "" {
		b.WriteString("\n\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(p.Muted)).Render("Escribe «"+m.RequireText+"»: "))
		b.WriteString(m.Typed)
		if !m.Enabled() {
			b.WriteString("▌")
		}
	}
	b.WriteString("\n\n")
	b.WriteString(strings.Join(m.actionBlock(boxW, p), "\n"))
	return m.box(boxW, b.String(), ascii, p)
}

// RenderFit pinta el modal ajustado a `h` líneas: reserva SIEMPRE la altura del
// título y de la barra de acciones y recorta el CUERPO cuando la terminal es
// compacta, de modo que las acciones siguen visibles y el usuario no confirma a
// ciegas (§5.15/UX-05). Si el modal ya cabe en `h`, el resultado es idéntico a
// Render (ningún golden cambia por este camino).
func (m Modal) RenderFit(w, h int, p theme.Palette, ascii bool) string {
	if h <= 0 {
		return m.Render(w, p, ascii)
	}
	full := m.Render(w, p, ascii)
	if strings.Count(full, "\n")+1 <= h {
		return full
	}
	return m.clip(w, h, p, ascii)
}

// clip ensambla el modal en exactamente `h` líneas dando prioridad al título y a
// la barra de acciones; el cuerpo cede el espacio que haga falta y marca con «…»
// que queda texto oculto. Las frases del cuerpo se envuelven con el mismo
// envoltorio del modal completo: nunca se pierde un aviso por truncar a ciegas.
func (m Modal) clip(w, h int, p theme.Palette, ascii bool) string {
	boxW := modalBoxWidth(w, m.MaxWidth)
	inner := boxW - 2 // el relleno del cuadro ocupa una celda a cada lado
	if inner < 1 {
		inner = 1
	}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(p.Muted))
	var require []string
	if m.RequireText != "" {
		line := "Escribe «" + m.RequireText + "»: " + m.Typed
		if !m.Enabled() {
			line += "▌"
		}
		for _, ln := range strings.Split(lipgloss.NewStyle().Width(inner).Render(line), "\n") {
			require = append(require, muted.Render(ln))
		}
	}
	// La barra de acciones se mide con el ancho INTERIOR: ninguna línea desborda
	// el cuadro y el alto reservado es exacto.
	actions := strings.Split(ActionBar(m.actions(), inner, m.Focused, p), "\n")
	var body []string
	if strings.TrimSpace(m.Body) != "" {
		body = strings.Split(lipgloss.NewStyle().Width(inner).Render(m.Body), "\n")
	}
	// Prioridad al recortar: bordes > título > acciones > texto exigido > blanco >
	// cuerpo. El blanco solo se conserva si sobra sitio para el cuerpo.
	room := h - 2 - 1 - len(require) - len(actions)
	blank := 0
	if room >= 1 {
		blank, room = 1, room-1
	} else {
		room = 0
	}
	if len(body) > room {
		body = body[:room]
		if room > 0 {
			body[room-1] = clipMark(body[room-1], inner)
		}
	}
	lines := make([]string, 0, h)
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Accent)).Render(layout.Truncate(m.Title, inner)))
	lines = append(lines, body...)
	if len(require) > 0 {
		lines = append(lines, "")
		lines = append(lines, require...)
	}
	if blank > 0 {
		lines = append(lines, "")
	}
	lines = append(lines, actions...)
	return m.box(boxW, strings.Join(lines, "\n"), ascii, p)
}

// clipMark marca que una línea del cuerpo quedó recortada sin desbordar el ancho.
func clipMark(line string, w int) string {
	line = strings.TrimRight(line, " ")
	if layout.Width(line)+2 > w {
		line = layout.Truncate(line, w-1)
	}
	return line + "…"
}

// Progress es una barra de progreso por fases.
type Progress struct {
	Label   string
	Percent float64 // 0..1
	Width   int
}

// Render dibuja la barra con porcentaje y etiqueta.
func (pr Progress) Render(p theme.Palette) string {
	w := pr.Width
	if w <= 0 {
		w = 30
	}
	pct := pr.Percent
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	filled := int(pct * float64(w))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", w-filled)
	out := lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)).Render(bar)
	out += fmt.Sprintf(" %3.0f%%", pct*100)
	if pr.Label != "" {
		out += " " + pr.Label
	}
	return out
}

// ConnectLines devuelve las líneas de la tarjeta «Cómo conectarse» sin borde,
// para incrustarlas en un panel (Inicio §5.2 evita cajas anidadas). La primera
// línea es el encabezado.
func ConnectLines(info connection.Info, p theme.Palette, ascii bool) []string {
	out := []string{lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Accent)).Render("Cómo conectarse")}
	if info.Address != "" {
		out = append(out, "Dirección:  "+info.Address+"   (c = copiar)")
	} else if info.AddressDNS != "" {
		out = append(out, "Dirección:  "+info.AddressDNS+"   (c = copiar)")
	}
	if info.AddressDNS != "" && info.Address != "" {
		out = append(out, "Nombre:     "+info.AddressDNS)
	}
	out = append(out, fmt.Sprintf("Versión: Minecraft %s · Modo %s", info.McVersion, modoTexto(info.OnlineMode)))
	if info.Loader != "" {
		out = append(out, "Loader:     "+info.Loader)
	}
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(p.Warn))
	for _, warn := range info.Warnings {
		out = append(out, warnStyle.Render(Warn(ascii)+" "+warn))
	}
	return out
}

// ConnectCard pinta la tarjeta "Cómo conectarse" (RF-2).
func ConnectCard(info connection.Info, w int, p theme.Palette, ascii bool) string {
	boxW := w
	if boxW <= 0 {
		boxW = 60
	}
	return lipgloss.NewStyle().
		Border(Border(ascii)).
		BorderForeground(lipgloss.Color(p.Border)).
		Padding(0, 1).
		Width(boxW - 2).
		Render(strings.Join(ConnectLines(info, p, ascii), "\n"))
}

func modoTexto(online bool) string {
	if online {
		return "online (cuenta oficial)"
	}
	return "offline (cualquier nombre)"
}

// StatusHint devuelve la línea de pista de una acción (≤ 90 caracteres).
func StatusHint(a Action) string {
	h := strings.TrimSpace(a.Hint)
	if len([]rune(h)) > 90 {
		r := []rune(h)
		h = string(r[:89]) + "…"
	}
	return h
}
