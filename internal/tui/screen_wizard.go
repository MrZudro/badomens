package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/provision"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

type wizardCreatedMsg struct {
	slug string
	err  error
}

// wizardScreen es el asistente de nueva instancia (rápido/avanzado).
type wizardScreen struct {
	svc    *svc.Services
	pal    theme.Palette
	mode   int // -1 = elegir modo, 0 = rápido, 1 = avanzado
	fields []components.Field
	cursor int
	engine int
	input  textinput.Model
	step   int // 0 formulario, 1 revisión
	err    string
}

func newWizardScreen(sv *svc.Services, pal theme.Palette) *wizardScreen {
	in := textinput.New()
	in.Width = 40
	w := &wizardScreen{svc: sv, pal: pal, mode: -1, input: in}
	return w
}

func (w *wizardScreen) Init() tea.Cmd { return nil }

func (w *wizardScreen) start(mode int) {
	w.mode = mode
	name := components.Field{Key: "name", Label: "Nombre", Short: "Nombre visible; también da nombre a carpeta y contenedor.", Placeholder: "survival-amigos"}
	engine := components.Field{Key: "engine", Label: "Motor", Short: "Define si puedes usar plugins o mods.", Value: engines.Order[0].Label}
	memory := components.Field{Key: "memory", Label: "Memoria", Short: "RAM máxima; más no siempre es mejor.", Value: "4G"}
	w.fields = []components.Field{name, engine, memory}
	if mode == 1 {
		w.fields = append(w.fields,
			components.Field{Key: "difficulty", Label: "Dificultad", Short: "Cuánto daño hacen los monstruos.", Value: "normal"},
			components.Field{Key: "gamemode", Label: "Modo de juego", Short: "Con el que entran los jugadores nuevos.", Value: "survival"},
			components.Field{Key: "max_players", Label: "Jugadores máx.", Short: "Cuántos pueden estar conectados a la vez.", Value: "20"},
		)
	}
	w.cursor = 0
	w.syncInput()
}

func (w *wizardScreen) syncInput() {
	if w.cursor < 0 || w.cursor >= len(w.fields) {
		return
	}
	f := w.fields[w.cursor]
	if f.Key == "engine" {
		w.input.SetValue("")
		w.input.Blur()
		return
	}
	w.input.SetValue(f.Value)
	_ = w.input.Focus()
}

func (w *wizardScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		if w.mode >= 0 {
			var c tea.Cmd
			w.input, c = w.input.Update(msg)
			return w, c
		}
		return w, nil
	}
	if w.mode < 0 {
		switch km.String() {
		case "r":
			w.start(0)
		case "a":
			w.start(1)
		case "esc":
			return w, func() tea.Msg { return popScreenMsg{} }
		}
		return w, nil
	}
	switch km.String() {
	case "esc":
		if w.step == 1 {
			w.step = 0
			return w, nil
		}
		return w, func() tea.Msg { return popScreenMsg{} }
	case "tab", "down":
		w.commitField()
		w.cursor = (w.cursor + 1) % len(w.fields)
		w.syncInput()
	case "shift+tab", "up":
		w.commitField()
		w.cursor = (w.cursor + len(w.fields) - 1) % len(w.fields)
		w.syncInput()
	case "left":
		if w.currentKey() == "engine" {
			w.engine = (w.engine + len(engines.Order) - 1) % len(engines.Order)
			w.fields[w.cursor].Value = engines.Order[w.engine].Label
			return w, nil
		}
	case "right":
		if w.currentKey() == "engine" {
			w.engine = (w.engine + 1) % len(engines.Order)
			w.fields[w.cursor].Value = engines.Order[w.engine].Label
			return w, nil
		}
	case "enter":
		w.commitField()
		if w.step == 0 {
			if w.fields[0].Value == "" {
				w.err = "el nombre es obligatorio"
				return w, nil
			}
			w.step = 1
			w.err = ""
			return w, nil
		}
		return w, w.create()
	}
	var c tea.Cmd
	w.input, c = w.input.Update(msg)
	return w, c
}

func (w *wizardScreen) currentKey() string {
	if w.cursor < 0 || w.cursor >= len(w.fields) {
		return ""
	}
	return w.fields[w.cursor].Key
}

func (w *wizardScreen) commitField() {
	if w.cursor < 0 || w.cursor >= len(w.fields) {
		return
	}
	if w.currentKey() == "engine" {
		return
	}
	w.fields[w.cursor].Value = strings.TrimSpace(w.input.Value())
}

func (w *wizardScreen) create() tea.Cmd {
	req := provision.Request{}
	for _, f := range w.fields {
		switch f.Key {
		case "name":
			req.Name = f.Value
		case "memory":
			req.Memory = f.Value
		case "difficulty":
			req.Difficulty = f.Value
		case "gamemode":
			req.Gamemode = f.Value
		}
	}
	req.Engine = engines.Order[w.engine].Name
	ref := w.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		slug, err := ref.Creator.Create(ctx, req)
		return wizardCreatedMsg{slug: slug, err: err}
	}
}

func (w *wizardScreen) View(width, height int) string {
	if w.mode < 0 {
		var b strings.Builder
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(w.pal.Accent)).Render("Nueva instancia"))
		b.WriteString("\n\nElige cómo configurarla:\n\n")
		b.WriteString(components.Button(components.Action{Verb: "Configuración", Object: "rápida (3 preguntas)", Key: "r", Hint: "Nombre, motor y memoria.", Kind: components.Primary}, true, w.pal))
		b.WriteString("\n")
		b.WriteString(components.Button(components.Action{Verb: "Configuración", Object: "avanzada (todas las opciones)", Key: "a", Hint: "Los 7 pasos, con ayuda por campo.", Kind: components.Secondary}, false, w.pal))
		b.WriteString("\n\nLos valores recomendados aparecen en el resumen antes de crear.")
		return b.String()
	}
	var b strings.Builder
	steps := []string{"Básico", "Mundo", "Jugadores", "Recursos", "Red", "Automatización", "Revisar"}
	if w.mode == 0 {
		steps = []string{"Básico", "Revisar"}
	}
	idx := 0
	if w.step == 1 {
		idx = len(steps) - 1
	}
	b.WriteString(components.Stepper(steps, idx, w.pal))
	b.WriteString("\n\n")
	if w.step == 0 {
		for i, f := range w.fields {
			marker := "  "
			style := lipgloss.NewStyle()
			if i == w.cursor {
				marker = "▶ "
				style = style.Bold(true).Foreground(lipgloss.Color(w.pal.Accent))
			}
			val := f.Value
			if i == w.cursor && f.Key != "engine" {
				val = w.input.View()
			}
			b.WriteString(marker + style.Render(f.Label) + ": " + val + "\n")
			b.WriteString("    " + lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Muted)).Render(f.Short) + "\n")
		}
		if w.err != "" {
			b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Bad)).Render("⚠ "+w.err))
		}
	} else {
		b.WriteString(lipgloss.NewStyle().Bold(true).Render("Resumen\n"))
		for _, f := range w.fields {
			b.WriteString("  " + f.Label + ": " + f.Value + "\n")
		}
		b.WriteString("\nPulsa Enter para crear y arrancar, o Esc para volver a editar.")
	}
	return b.String()
}

func (w *wizardScreen) Actions() []components.Action {
	if w.mode < 0 {
		return []components.Action{
			{ID: "wizard.quick", Verb: "Configuración", Object: "rápida", Key: "r", Hint: "Tres preguntas y listo.", Kind: components.Primary},
			{ID: "wizard.advanced", Verb: "Configuración", Object: "avanzada", Key: "a", Hint: "Todas las opciones con ayuda.", Kind: components.Secondary},
			{ID: "wizard.cancel", Verb: "Volver", Object: "sin crear", Key: "esc", Hint: "Cancela el asistente.", Kind: components.Navigation},
		}
	}
	return []components.Action{
		{ID: "wizard.next", Verb: "Siguiente:", Object: "Revisar", Key: "enter", Hint: "Avanza al resumen.", Kind: components.Primary},
		{ID: "wizard.cancel", Verb: "Volver", Object: "sin crear", Key: "esc", Hint: "Cancela el asistente.", Kind: components.Navigation},
	}
}

func (w *wizardScreen) Title() string { return "Nueva instancia" }
func (w *wizardScreen) ID() string    { return "wizard" }
