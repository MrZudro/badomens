package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/connection"
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

// Render pinta el modal centrado en el ancho w.
func (m Modal) Render(w int, p theme.Palette) string {
	boxW := w - 4
	if boxW > 60 {
		boxW = 60
	}
	if boxW < 20 {
		boxW = 20
	}
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
	actions := m.Actions
	if m.Enabled() {
		enabled := make([]Action, len(actions))
		copy(enabled, actions)
		for i := range enabled {
			if enabled[i].ID == "confirm" {
				enabled[i].Disabled = false
				enabled[i].DisabledReason = ""
			}
		}
		actions = enabled
	}
	b.WriteString(ActionBar(actions, boxW, m.Focused, p))
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(p.Border)).
		Padding(0, 1).
		Width(boxW).
		Render(b.String())
	return box
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

// ConnectCard pinta la tarjeta "Cómo conectarse" (RF-2).
func ConnectCard(info connection.Info, w int, p theme.Palette) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Accent)).Render("Cómo conectarse"))
	if info.Address != "" {
		b.WriteString("\nDirección:  " + info.Address + "   (c = copiar)")
	} else if info.AddressDNS != "" {
		b.WriteString("\nDirección:  " + info.AddressDNS + "   (c = copiar)")
	}
	if info.AddressDNS != "" && info.Address != "" {
		b.WriteString("\nNombre:     " + info.AddressDNS)
	}
	b.WriteString(fmt.Sprintf("\nVersión: Minecraft %s · Modo %s", info.McVersion, modoTexto(info.OnlineMode)))
	if info.Loader != "" {
		b.WriteString("\nLoader:     " + info.Loader)
	}
	for _, warn := range info.Warnings {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(p.Warn)).Render("⚠ "+warn))
	}
	boxW := w
	if boxW <= 0 {
		boxW = 60
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(p.Border)).
		Padding(0, 1).
		Width(boxW - 2).
		Render(b.String())
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
