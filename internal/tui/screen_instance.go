package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

type instanceLogsMsg struct {
	slug string
	text string
	err  error
}

// instanceScreen es el detalle de una instancia con pestañas.
type instanceScreen struct {
	svc    *svc.Services
	pal    theme.Palette
	ascii  bool
	slug   string
	detail *svc.Detail
	tab    int
	logs   string
	input  textinput.Model
	focus  bool
	status string
}

var instanceTabs = []string{"Resumen", "Consola", "Red"}

func newInstanceScreen(sv *svc.Services, pal theme.Palette, ascii bool, slug string) *instanceScreen {
	in := textinput.New()
	in.Placeholder = "comando para la consola (ej. say hola)"
	in.Width = 50
	return &instanceScreen{svc: sv, pal: pal, ascii: ascii, slug: slug, input: in, status: "cargando…"}
}

func (s *instanceScreen) Init() tea.Cmd {
	return s.load()
}

func (s *instanceScreen) load() tea.Cmd {
	ref, slug := s.svc, s.slug
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		d, err := ref.Instances.Detail(ctx, slug)
		return homeDetailMsg{d: d, err: err}
	}
}

func (s *instanceScreen) logsCmd() tea.Cmd {
	ref, slug := s.svc, s.slug
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		text, err := ref.Instances.Logs(ctx, slug, 100)
		return instanceLogsMsg{slug: slug, text: text, err: err}
	}
}

func (s *instanceScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case homeDetailMsg:
		s.detail = msg.d
		if msg.err != nil {
			s.status = msg.err.Error()
		} else if s.tab == 1 {
			return s, s.logsCmd()
		}
	case instanceLogsMsg:
		if msg.slug == s.slug {
			s.logs = msg.text
		}
	case tea.KeyMsg:
		return s.handleKey(msg)
	}
	return s, nil
}

func (s *instanceScreen) handleKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	if s.focus {
		if msg.String() == "enter" {
			cmd := strings.TrimSpace(s.input.Value())
			s.input.SetValue("")
			s.focus = false
			if cmd == "" {
				return s, nil
			}
			return s, s.consoleCmd(cmd)
		}
		if msg.String() == "esc" {
			s.focus = false
			s.input.Blur()
			return s, nil
		}
		var c tea.Cmd
		s.input, c = s.input.Update(msg)
		return s, c
	}
	switch msg.String() {
	case "esc":
		return s, func() tea.Msg { return popScreenMsg{} }
	case "tab", "right", "l":
		s.tab = (s.tab + 1) % len(instanceTabs)
		if s.tab == 1 {
			return s, s.logsCmd()
		}
	case "shift+tab", "left", "h":
		s.tab = (s.tab + len(instanceTabs) - 1) % len(instanceTabs)
	case "e":
		s.tab = 1
		s.focus = true
		return s, s.input.Focus()
	case "s":
		return s, s.lifecycle("start")
	case "p":
		return s, s.lifecycle("stop")
	case "r":
		return s, s.lifecycle("restart")
	}
	return s, nil
}

func (s *instanceScreen) consoleCmd(command string) tea.Cmd {
	ref, slug := s.svc, s.slug
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := ref.Instances.Console(ctx, slug, command); err != nil {
			return toastMsg{Text: "✖ consola: " + err.Error(), Kind: components.Destructive}
		}
		return toastMsg{Text: "✓ comando enviado: " + command}
	}
}

func (s *instanceScreen) lifecycle(op string) tea.Cmd {
	ref, slug := s.svc, s.slug
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		var err error
		switch op {
		case "start":
			err = ref.Lifecycle.Start(ctx, slug)
		case "stop":
			err = ref.Lifecycle.Stop(ctx, slug)
		case "restart":
			err = ref.Lifecycle.Restart(ctx, slug)
		}
		if err != nil {
			return toastMsg{Text: "✖ " + op + " «" + slug + "»: " + err.Error(), Kind: components.Destructive}
		}
		return toastMsg{Text: "✓ «" + slug + "» " + op}
	}
}

func (s *instanceScreen) View(w, h int) string {
	var b strings.Builder
	name := s.slug
	if s.detail != nil && s.detail.Spec != nil {
		name = s.detail.Spec.Name
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(s.pal.Accent)).Render(name))
	b.WriteString("  ")
	for i, tab := range instanceTabs {
		style := lipgloss.NewStyle()
		if i == s.tab {
			style = style.Bold(true).Foreground(lipgloss.Color(s.pal.AccentText)).Background(lipgloss.Color(s.pal.Accent))
		} else {
			style = style.Foreground(lipgloss.Color(s.pal.Muted))
		}
		b.WriteString(style.Render(" " + tab + " "))
		b.WriteString(" ")
	}
	b.WriteString("\n\n")
	switch s.tab {
	case 0:
		b.WriteString(s.summaryView())
	case 1:
		b.WriteString(s.consoleView(w, h))
	case 2:
		b.WriteString(s.netView(w))
	}
	return b.String()
}

func (s *instanceScreen) summaryView() string {
	if s.detail == nil || s.detail.Spec == nil {
		return "cargando…"
	}
	spec := s.detail.Spec
	var b strings.Builder
	b.WriteString(strings.Join([]string{
		"Motor:      " + spec.Engine + " " + spec.Version,
		"Memoria:    " + spec.Memory,
		"Puerto:     " + itoa(spec.Port),
		"Jugadores:  máx " + itoa(spec.MaxPlayers),
		"Dificultad: " + spec.Difficulty + " · modo " + spec.Gamemode,
		"Estado:     " + string(s.detail.State),
	}, "\n"))
	b.WriteString("\n\n")
	b.WriteString(components.ConnectCard(connectionFromSpec(spec), 60, s.pal))
	return b.String()
}

func (s *instanceScreen) consoleView(w, h int) string {
	var b strings.Builder
	lines := strings.Split(s.logs, "\n")
	max := h - 6
	if max < 1 {
		max = 1
	}
	if len(lines) > max {
		lines = lines[len(lines)-max:]
	}
	for _, l := range lines {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(logColor(l, s.pal))).Render(l))
		b.WriteString("\n")
	}
	b.WriteString("\n> " + s.input.View())
	return b.String()
}

func (s *instanceScreen) netView(w int) string {
	if s.detail == nil || s.detail.Spec == nil {
		return "cargando…"
	}
	return components.ConnectCard(connectionFromSpec(s.detail.Spec), w, s.pal)
}

func (s *instanceScreen) Actions() []components.Action {
	name := "«" + s.slug + "»"
	running := s.detail != nil && s.detail.State == "running"
	return []components.Action{
		{ID: "instance.start", Verb: "Iniciar", Object: name, Key: "s", Hint: "Arranca el servidor.", Kind: components.Primary, Disabled: running, DisabledReason: "Ya está en marcha"},
		{ID: "instance.stop", Verb: "Detener", Object: name, Key: "p", Hint: "Apaga guardando el mundo.", Kind: components.Secondary, Disabled: !running, DisabledReason: "No está en marcha"},
		{ID: "instance.console", Verb: "Escribir", Object: "en la consola", Key: "e", Hint: "Envía un comando al servidor.", Kind: components.Secondary},
		{ID: "instance.back", Verb: "Volver", Object: "a la lista", Key: "esc", Hint: "Cierra el detalle.", Kind: components.Navigation},
	}
}

func (s *instanceScreen) Title() string { return "Instancia " + s.slug }
func (s *instanceScreen) ID() string    { return "instance" }

func logColor(line string, pal theme.Palette) string {
	switch {
	case strings.Contains(line, "/ERROR") || strings.Contains(line, "ERROR"):
		return pal.Bad
	case strings.Contains(line, "/WARN") || strings.Contains(line, "WARN"):
		return pal.Warn
	}
	return pal.Foreground
}

// popScreenMsg pide al App que desapile la pantalla actual.
type popScreenMsg struct{}
