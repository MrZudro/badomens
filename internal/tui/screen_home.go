package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/connection"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

type homeDetailMsg struct {
	d   *svc.Detail
	err error
}

type openInstanceMsg struct{ slug string }

// homeScreen es el dashboard: lista de instancias + detalle + tarjeta de conexión.
type homeScreen struct {
	svc    *svc.Services
	pal    theme.Palette
	ascii  bool
	rows   []svc.Row
	cursor int
	detail *svc.Detail
	status string
}

func newHomeScreen(sv *svc.Services, pal theme.Palette, ascii bool) *homeScreen {
	return &homeScreen{svc: sv, pal: pal, ascii: ascii, status: "cargando instancias…"}
}

func (s *homeScreen) Init() tea.Cmd { return nil }

func (s *homeScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case pollRowsMsg:
		if msg.err != nil {
			s.status = "error al listar: " + msg.err.Error()
			return s, nil
		}
		prev := s.selected()
		s.rows = msg.rows
		s.cursor = 0
		if prev != "" {
			for i, r := range s.rows {
				if r.Slug == prev {
					s.cursor = i
				}
			}
		}
		if len(s.rows) == 0 {
			s.status = "no hay instancias todavía"
			return s, nil
		}
		s.status = ""
		return s, s.detailCmd()
	case homeDetailMsg:
		if msg.err == nil {
			s.detail = msg.d
		}
		return s, nil
	case tea.KeyMsg:
		return s.handleKey(msg)
	}
	return s, nil
}

func (s *homeScreen) handleKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		return s.move(1)
	case "k", "up":
		return s.move(-1)
	case "enter":
		if slug, ok := s.selectedSlug(); ok {
			slugCopy := slug
			return s, func() tea.Msg { return openInstanceMsg{slug: slugCopy} }
		}
	case "s":
		return s, s.lifecycle("start")
	case "p":
		return s, s.lifecycle("stop")
	case "r":
		return s, s.lifecycle("restart")
	case "b":
		return s, s.backup()
	}
	return s, nil
}

func (s *homeScreen) move(d int) (Screen, tea.Cmd) {
	if len(s.rows) == 0 {
		return s, nil
	}
	s.cursor += d
	if s.cursor < 0 {
		s.cursor = 0
	}
	if s.cursor >= len(s.rows) {
		s.cursor = len(s.rows) - 1
	}
	return s, s.detailCmd()
}

func (s *homeScreen) selectedSlug() (string, bool) {
	if len(s.rows) == 0 || s.cursor < 0 || s.cursor >= len(s.rows) {
		return "", false
	}
	return s.rows[s.cursor].Slug, true
}

func (s *homeScreen) selected() string {
	slug, _ := s.selectedSlug()
	return slug
}

func (s *homeScreen) detailCmd() tea.Cmd {
	slug := s.selected()
	if slug == "" {
		return nil
	}
	ref := s.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		d, err := ref.Instances.Detail(ctx, slug)
		return homeDetailMsg{d: d, err: err}
	}
}

func (s *homeScreen) lifecycle(op string) tea.Cmd {
	slug := s.selected()
	if slug == "" {
		return nil
	}
	ref := s.svc
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
			return toastMsg{Text: "✖ No se pudo " + op + " «" + slug + "»: " + err.Error(), Kind: components.Destructive}
		}
		return toastMsg{Text: "✓ «" + slug + "» " + op + " correctamente"}
	}
}

func (s *homeScreen) backup() tea.Cmd {
	slug := s.selected()
	if slug == "" {
		return nil
	}
	ref := s.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		if err := ref.Backups.Create(ctx, slug); err != nil {
			return toastMsg{Text: "✖ No se pudo crear el backup de «" + slug + "»: " + err.Error(), Kind: components.Destructive}
		}
		return toastMsg{Text: "✓ «" + slug + "»: backup creado"}
	}
}

// View pinta el dashboard según la clase de tamaño.
func (s *homeScreen) View(w, h int) string {
	class := layout.Classify(w, h)
	if len(s.rows) == 0 {
		return s.emptyView(w)
	}
	list := s.listView(w, h, class)
	if class == layout.Compact {
		return list
	}
	detailW := w - lipgloss.Width(list) - 2
	if detailW < 20 {
		detailW = 20
	}
	detail := s.detailView(detailW, h)
	return lipgloss.JoinHorizontal(lipgloss.Top, list, "  ", detail)
}

func (s *homeScreen) emptyView(w int) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(s.pal.Accent)).Render("Todavía no hay instancias"))
	b.WriteString("\n\n")
	b.WriteString("Crea tu primer servidor y badomens te dirá cómo conectarte.\n\n")
	b.WriteString(components.Button(components.Action{Verb: "Crear", Object: "mi primer servidor", Key: "n", Kind: components.Primary, Hint: "Abre el asistente."}, true, s.pal))
	b.WriteString("\n\nAntes de nada, comprueba Docker y Tailscale con la tecla H.")
	return b.String()
}

func (s *homeScreen) listView(w, h int, class layout.Class) string {
	width := 30
	if class == layout.Wide {
		width = 24
	}
	if class == layout.Compact {
		width = w
	}
	var b strings.Builder
	head := "INSTANCIAS (" + itoa(len(s.rows)) + "/" + itoa(s.svc.MaxInstances) + ")"
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(s.pal.Accent)).Render(head))
	b.WriteString("\n")
	max := h - 2
	for i, r := range s.rows {
		if max > 0 && i >= max {
			break
		}
		line := components.Badge(stateKey(r.State), s.pal, s.ascii) + " " + r.Name
		extra := "  " + r.Engine + " " + r.Version + "  pto " + itoa(r.Port)
		if i == s.cursor {
			b.WriteString(lipgloss.NewStyle().Background(lipgloss.Color(s.pal.Accent)).Foreground(lipgloss.Color(s.pal.AccentText)).Render("▶ " + layout.Truncate(stripBadge(line)+extra, width-2)))
		} else {
			b.WriteString("  " + layout.Truncate(line+extra, width-2))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (s *homeScreen) detailView(w, h int) string {
	if s.detail == nil || s.detail.Spec == nil {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("Selecciona una instancia (↑/↓).")
	}
	spec := s.detail.Spec
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(spec.Name + " · " + spec.Engine + " " + spec.Version))
	b.WriteString("\n" + components.Badge(stateKey(s.detail.State), s.pal, s.ascii))
	b.WriteString("\n\n")
	conn := connectionFromSpec(spec)
	b.WriteString(components.ConnectCard(conn, w, s.pal))
	b.WriteString("\n\n")
	b.WriteString(s.actionsBar(w))
	return b.String()
}

func (s *homeScreen) actionsBar(w int) string {
	return components.ActionBar(s.Actions(), w, 0, s.pal)
}

func (s *homeScreen) Actions() []components.Action {
	slug := s.selected()
	name := "«" + slug + "»"
	state := ""
	if len(s.rows) > 0 && s.cursor < len(s.rows) {
		state = s.rows[s.cursor].State
	}
	running := state == "running"
	return []components.Action{
		{ID: "instance.start", Verb: "Iniciar", Object: name, Key: "s", Hint: "Arranca el servidor y recalcula cómo conectarse.", Kind: components.Primary, Disabled: running, DisabledReason: "Ya está en marcha"},
		{ID: "instance.stop", Verb: "Detener", Object: name + " (guarda el mundo)", Key: "p", Hint: "Apaga el servidor guardando el mundo.", Kind: components.Secondary, Disabled: !running, DisabledReason: "El servidor no está en marcha"},
		{ID: "instance.restart", Verb: "Reiniciar", Object: name, Key: "r", Hint: "Recrea el contenedor aplicando cambios.", Kind: components.Secondary, Disabled: !running, DisabledReason: "El servidor no está en marcha"},
		{ID: "instance.backup", Verb: "Crear", Object: "backup ahora", Key: "b", Hint: "Guarda una copia de seguridad con checksum.", Kind: components.Secondary},
		{ID: "instance.open", Verb: "Ver", Object: "detalle de " + name, Key: "enter", Hint: "Abre consola, logs, jugadores, backups y ajustes.", Kind: components.Navigation},
	}
}

func (s *homeScreen) Title() string { return "Instancias" }
func (s *homeScreen) ID() string    { return "home" }

// stateKey traduce el estado crudo de Docker a una clave de insignia.
func stateKey(state string) string {
	switch state {
	case "running":
		return "running"
	case "restarting":
		return "starting"
	case "exited", "created", "":
		return "stopped"
	case "paused":
		return "problems"
	}
	return state
}

func stripBadge(s string) string {
	// Quita el posible prefijo de insignia para recalcular con color de selección.
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[i+1:]
	}
	return s
}

func connectionFromSpec(spec *instance.Spec) connection.Info {
	if spec.Connection != nil {
		return *spec.Connection
	}
	return connection.FromData(spec.Slug, spec.Name, spec.Engine, spec.Version, spec.Port, spec.Pins, spec.OnlineMode, spec.WhitelistEnabled, "", "", time.Now())
}
