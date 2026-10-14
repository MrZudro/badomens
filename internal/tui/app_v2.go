package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/tui/anim"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/i18n"
	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// AppOptions controla la app rediseñada.
type AppOptions struct {
	Preflight     bool
	Theme         theme.Name
	ReducedMotion bool
	ASCII         bool
}

// themeChangedMsg pide cambiar el tema activo.
type themeChangedMsg struct{ name theme.Name }

// App es el modelo raíz de la TUI v2: router de pantallas, overlays, toasts,
// barra de estado y layout responsive (RF-1).
type App struct {
	svc    *svc.Services
	router *Router
	poller *Poller

	pal      theme.Palette
	keys     KeyMap
	cat      *i18n.Catalog
	size     layout.Size
	toasts   []toastMsg
	status   string
	ascii    bool
	reduced  bool
	anim     *anim.Manager
	docker   dockerx.Diagnosis
	quitting bool
}

// NewApp construye la app sobre la fachada de servicios.
func NewApp(sv *svc.Services, opts AppOptions) *App {
	pal := theme.For(opts.Theme)
	home := newHomeScreen(sv, pal, opts.ASCII)
	a := &App{
		svc:     sv,
		pal:     pal,
		keys:    DefaultKeys,
		cat:     i18n.Es(),
		ascii:   opts.ASCII,
		reduced: opts.ReducedMotion,
		anim:    anim.New(opts.ReducedMotion),
	}
	a.router = NewRouter(home)
	a.poller = &Poller{Svc: sv}
	a.status = a.cat.T("home.title")
	return a
}

// Init arranca la pantalla inicial y el latido del poller.
func (a *App) Init() tea.Cmd {
	return tea.Batch(a.router.Top().Init(), a.poller.Schedule(), a.poller.Cmds(nowUTC(), true, true))
}

// Update procesa mensajes.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.size = layout.Size{W: msg.Width, H: msg.Height, Class: layout.Classify(msg.Width, msg.Height)}
		return a, nil
	case pollTickMsg:
		return a, tea.Batch(a.poller.Schedule(), a.poller.Cmds(msg.At, a.needsState(), a.needsDocker()))
	case pollDockerMsg:
		if msg.err == nil {
			a.docker = msg.diag
			a.poller.NoteDockerResult(msg.diag.State.Ready())
			if !msg.diag.State.Ready() {
				a.pushToast(msg.diag.Message, components.Destructive)
			}
		} else {
			a.poller.NoteDockerResult(false)
		}
		return a, a.router.Update(msg)
	case toastMsg:
		a.pushToast(msg.Text, msg.Kind)
		return a, nil
	case openInstanceMsg:
		a.router.Push(newInstanceScreen(a.svc, a.pal, a.ascii, msg.slug))
		return a, a.router.Top().Init()
	case popScreenMsg:
		a.router.Pop()
		return a, nil
	case wizardCreatedMsg:
		if msg.err != nil {
			a.pushToast("✖ No se pudo crear: "+msg.err.Error(), components.Destructive)
			return a, nil
		}
		a.router.Pop()
		a.pushToast("✓ Instancia «"+msg.slug+"» creada", components.Secondary)
		return a, a.poller.Cmds(nowUTC(), true, false)
	case themeChangedMsg:
		a.pal = theme.For(msg.name)
		return a, nil
	case tea.KeyMsg:
		return a.handleGlobalKey(msg)
	}
	return a, a.router.Update(msg)
}

func (a *App) needsState() bool  { return true }
func (a *App) needsDocker() bool { return true }

// handleGlobalKey atiende los atajos globales antes de delegar en la pantalla.
func (a *App) handleGlobalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "ctrl+c":
		a.quitting = true
		return a, tea.Quit
	case "q":
		// q sale solo si no lo consume la pantalla activa (Home usa q para salir).
		if a.router.Overlay() == nil {
			a.quitting = true
			return a, tea.Quit
		}
	case "?":
		if a.router.Overlay() != nil && a.router.Overlay().ID() == "help" {
			a.router.ClearOverlay()
			return a, nil
		}
		a.router.SetOverlay(newHelpScreen(a.keys, a.pal))
		return a, nil
	case "H":
		if a.router.Overlay() == nil {
			a.router.Push(newHealthScreen(a.svc, a.pal, a.ascii))
			return a, a.svcInit()
		}
	case "ctrl+p", ":":
		if a.router.Overlay() == nil {
			a.router.SetOverlay(newPaletteScreen(a.actions(), a.pal))
		}
		return a, nil
	case "n":
		if a.router.Overlay() == nil && a.router.Top().ID() != "wizard" {
			a.router.Push(newWizardScreen(a.svc, a.pal))
			return a, nil
		}
	case "T":
		return a, func() tea.Msg { return themeChangedMsg{name: nextTheme(a.pal.Name)} }
	}
	if a.router.Overlay() != nil && key == "esc" {
		a.router.ClearOverlay()
		return a, nil
	}
	return a, a.router.Update(msg)
}

func (a *App) svcInit() tea.Cmd { return a.router.Top().Init() }

// actions devuelve las acciones globales más las de la pantalla activa.
func (a *App) actions() []components.Action {
	globals := []components.Action{
		{ID: "global.health", Verb: "Abrir", Object: "Salud del sistema", Key: "H", Hint: "Muestra Docker, Tailscale, disco y RAM.", Kind: components.Navigation},
		{ID: "global.new", Verb: "Crear", Object: "una instancia nueva", Key: "n", Hint: "Abre el asistente de nueva instancia.", Kind: components.Primary},
		{ID: "global.help", Verb: "Ver", Object: "la ayuda de esta pantalla", Key: "?", Hint: "Muestra todos los atajos.", Kind: components.Navigation},
	}
	out := append([]components.Action{}, a.router.Active().Actions()...)
	return append(out, globals...)
}

// pushToast añade un toast (máximo 3, se descarta el más antiguo).
func (a *App) pushToast(text string, kind components.Kind) {
	if strings.TrimSpace(text) == "" {
		return
	}
	a.toasts = append(a.toasts, toastMsg{Text: text, Kind: kind})
	if len(a.toasts) > 3 {
		a.toasts = a.toasts[len(a.toasts)-3:]
	}
}

// View pinta la app completa en el tamaño actual.
func (a *App) View() string {
	w, h := a.size.W, a.size.H
	if w <= 0 {
		w, h = 100, 30
	}
	if a.size.TooSmall() {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(a.pal.Warn)).
			Render("Agranda la terminal (mínimo 40×12). Ahora: " + itoa(w) + "×" + itoa(h))
	}
	header := a.headerView(w)
	footer := a.footerView(w)
	bodyH := h - 3
	if bodyH < 1 {
		bodyH = 1
	}
	body := a.router.Active().View(w, bodyH)
	if overlay := a.router.Overlay(); overlay != nil {
		body = overlay.View(w, bodyH)
	}
	toasts := ""
	if len(a.toasts) > 0 {
		lines := make([]string, 0, len(a.toasts))
		for _, t := range a.toasts {
			lines = append(lines, components.RenderToast(components.Toast{Text: t.Text, Kind: t.Kind}, a.pal))
		}
		toasts = strings.Join(lines, "\n") + "\n"
	}
	return strings.Join([]string{header, toasts + body, footer}, "\n")
}

func (a *App) headerView(w int) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(a.pal.Accent)).Render("badomens")
	docker := "● Docker OK"
	color := a.pal.Good
	if !a.docker.State.Ready() {
		docker = "✖ Docker " + string(a.docker.State)
		color = a.pal.Bad
	}
	if a.ascii {
		docker = asciiStatus(a.docker.State.Ready())
	}
	right := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(docker)
	pad := w - lipgloss.Width(title) - lipgloss.Width(right) - 4
	if pad < 1 {
		pad = 1
	}
	return "┌ " + title + strings.Repeat(" ", pad) + right + " ┐"
}

func (a *App) footerView(w int) string {
	help := a.keys.ShortHelp()
	status := a.status
	return lipgloss.NewStyle().Foreground(lipgloss.Color(a.pal.Muted)).
		Render(layout.Truncate("  "+help+"    "+status, w))
}

func asciiStatus(ok bool) string {
	if ok {
		return "[OK] Docker"
	}
	return "[XX] Docker"
}

func nowUTC() time.Time { return time.Now() }

// nextTheme devuelve el tema siguiente en el ciclo de temas.
func nextTheme(name theme.Name) theme.Name {
	all := theme.All()
	for i, n := range all {
		if n == name {
			return all[(i+1)%len(all)]
		}
	}
	return theme.Dark
}

// itoa convierte un entero a string sin depender de strconv.
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
