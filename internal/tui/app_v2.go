package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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

// toastExpireMsg retira un toast pasado su tiempo.
type toastExpireMsg struct{ seq int }

// App es el modelo raíz de la TUI v2: router de pantallas, overlays, toasts,
// barra de estado y layout responsive (RF-1).
type App struct {
	svc    *svc.Services
	router *Router
	poller *Poller

	pal     theme.Palette
	keys    KeyMap
	cat     *i18n.Catalog
	size    layout.Size
	toasts  []toastEntry
	toastID int
	status  string
	hint    string
	ascii   bool
	reduced bool
	anim    *anim.Manager
	animOn  bool
	pulse   bool
	docker  dockerx.Diagnosis
	busy    bool
	confirm *components.Modal
	// repairing indica que hay una reparación de Docker en curso.
	repairing  bool
	idleWarned bool
	dockerSeen bool
	quitting   bool
}

// toastEntry es un toast con su caducidad.
type toastEntry struct {
	Text string
	Kind components.Kind
	Seq  int
	Done bool
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
	return tea.Batch(a.router.Top().Init(), a.poller.Schedule(), a.poller.Cmds(nowUTC(), true, true), a.tickCmd())
}

// tickCmd arranca el gestor de animaciones (pulso) si está activo.
func (a *App) tickCmd() tea.Cmd {
	if a.reduced {
		return nil
	}
	a.anim.Start("pulse")
	a.animOn = true
	return a.anim.Tick()
}

// Update procesa mensajes.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.size = layout.Size{W: msg.Width, H: msg.Height, Class: layout.Classify(msg.Width, msg.Height)}
		return a, nil
	case anim.TickMsg:
		a.pulse = !a.pulse
		var cmds []tea.Cmd
		if c := a.anim.Tick(); c != nil {
			cmds = append(cmds, c)
		}
		if c := a.router.Update(msg); c != nil {
			cmds = append(cmds, c)
		}
		return a, tea.Batch(cmds...)
	case pollTickMsg:
		return a, tea.Batch(a.poller.Schedule(), a.poller.Cmds(msg.At, a.needsState(), a.needsDocker()))
	case pollRowsMsg:
		if msg.err == nil {
			a.noteIdleBanner()
		}
		return a, a.router.Update(msg)
	case pollDockerMsg:
		wasReady := a.docker.State.Ready()
		wasSeen := a.dockerSeen
		if msg.err == nil {
			a.docker = msg.diag
			a.dockerSeen = true
			a.poller.NoteDockerResult(msg.diag.State.Ready())
			if !msg.diag.State.Ready() {
				if wasReady {
					a.pushToast("✖ Docker se detuvo — tus servidores no responden. [H] Reparar", components.Destructive)
				}
			} else if wasSeen && !wasReady {
				a.pushToast("✓ Docker volvió")
			}
		} else {
			a.poller.NoteDockerResult(false)
		}
		return a, a.router.Update(msg)
	case toastMsg:
		a.busy = false
		return a, a.pushToast(msg.Text, msg.Kind)
	case toastExpireMsg:
		a.expireToast(msg.seq)
		return a, nil
	case paletteChosenMsg:
		a.router.ClearOverlay()
		return a, a.invoke(msg.id)
	case busyMsg:
		a.busy = msg.on
		return a, nil
	case confirmResultMsg:
		a.confirm = nil
		if !msg.confirmed {
			return a, nil
		}
		return a, a.invoke(msg.action)
	case dockerRepairMsg:
		a.repairing = false
		if msg.err != nil {
			a.pushToast("✖ No se pudo reparar Docker: "+msg.err.Error(), components.Destructive)
			return a, nil
		}
		a.docker = msg.diag
		if msg.diag.State.Ready() {
			a.pushToast("✓ Docker está listo")
		} else {
			a.pushToast("⚠ Docker sigue sin responder: "+msg.diag.Message, components.Destructive)
		}
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
		a.pushToast("✓ Instancia «" + msg.slug + "» creada")
		return a, a.poller.Cmds(nowUTC(), true, false)
	case themeChangedMsg:
		a.pal = theme.For(msg.name)
		return a, nil
	case tea.KeyMsg:
		return a.handleGlobalKey(msg)
	case tea.MouseMsg:
		return a.handleMouse(msg)
	}
	return a, a.router.Update(msg)
}

func (a *App) needsState() bool  { return true }
func (a *App) needsDocker() bool { return true }

// handleGlobalKey atiende los atajos globales antes de delegar en la pantalla.
func (a *App) handleGlobalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	// Un modal de confirmación captura las teclas.
	if a.confirm != nil {
		return a.handleConfirmKey(msg)
	}
	switch key {
	case "ctrl+c":
		if a.busy {
			return a.askQuit()
		}
		a.quitting = true
		return a, tea.Quit
	case "q":
		if a.router.Overlay() == nil {
			if a.busy {
				return a.askQuit()
			}
			a.quitting = true
			return a, tea.Quit
		}
	case "esc":
		if a.router.Overlay() != nil && a.router.Overlay().ID() != "palette" {
			if a.router.Overlay().ID() == "help" {
				a.router.ClearOverlay()
				return a, nil
			}
		}
		if len(a.toasts) > 0 {
			a.toasts = nil
			return a, nil
		}
	case "?":
		if a.router.Overlay() != nil && a.router.Overlay().ID() == "help" {
			a.router.ClearOverlay()
			return a, nil
		}
		a.router.SetOverlay(newHelpScreen(a.keys, a.pal, a.ascii))
		return a, nil
	case "H":
		if a.router.Overlay() == nil {
			a.router.Push(newHealthScreen(a.svc, a.pal, a.ascii))
			return a, a.svcInit()
		}
	case "ctrl+p", ":":
		if a.router.Overlay() == nil {
			a.router.SetOverlay(newPaletteScreen(a.actions(), a.pal, a.ascii))
		}
		return a, nil
	case "n":
		if a.router.Overlay() == nil && a.router.Top().ID() != "wizard" {
			a.router.Push(newWizardScreen(a.svc, a.pal, a.ascii))
			return a, nil
		}
	case "d":
		// Atajo de reparación cuando Docker está caído.
		if !a.docker.State.Ready() && a.router.Overlay() == nil {
			return a, a.repairDocker()
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

// handleMouse traduce la rueda del ratón a las teclas de navegación.
func (a *App) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return a.Update(tea.KeyMsg{Type: tea.KeyUp})
	case tea.MouseButtonWheelDown:
		return a.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	return a, nil
}

// askQuit pide confirmación para salir con una operación en curso (RF-7).
func (a *App) askQuit() (tea.Model, tea.Cmd) {
	a.confirm = &components.Modal{
		Title: "Hay una operación en curso",
		Body:  "Si sales ahora se cancelará la operación pendiente.",
		Actions: []components.Action{
			{ID: "quit.wait", Verb: "No, esperar", Object: "a que termine", Key: "n", Hint: "Cancela la salida y deja terminar la operación.", Kind: components.Secondary},
			{ID: "quit.now", Verb: "Sí, salir", Object: "y cancelar la operación", Key: "y", Hint: "Cierra la TUI cancelando lo pendiente.", Kind: components.Destructive},
		},
		Focused: 0,
	}
	return a, nil
}

func (a *App) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := *a.confirm
	switch msg.String() {
	case "esc", "n":
		return a.Update(confirmResultMsg{})
	case "left", "h":
		if m.Focused > 0 {
			m.Focused--
		}
		a.confirm = &m
	case "right", "l", "tab":
		if m.Focused < len(m.Actions)-1 {
			m.Focused++
		}
		a.confirm = &m
	case "enter", "y":
		act := m.Actions[m.Focused]
		return a.Update(confirmResultMsg{confirmed: true, action: act.ID})
	}
	return a, nil
}

func (a *App) svcInit() tea.Cmd { return a.router.Top().Init() }

// actions devuelve las acciones globales más las de la pantalla activa, y las
// marca deshabilitadas si Docker no está listo (guarda de prerrequisitos).
func (a *App) actions() []components.Action {
	globals := []components.Action{
		{ID: "global.health", Verb: "Abrir", Object: "Salud del sistema", Key: "H", Hint: "Muestra Docker, Tailscale, disco y RAM.", Kind: components.Navigation},
		{ID: "global.new", Verb: "Crear", Object: "una instancia nueva", Key: "n", Hint: "Abre el asistente de nueva instancia.", Kind: components.Primary, Disabled: a.dockerDown(), DisabledReason: "Docker no está activo — pulsa H para repararlo"},
		{ID: "global.help", Verb: "Ver", Object: "la ayuda de esta pantalla", Key: "?", Hint: "Muestra todos los atajos.", Kind: components.Navigation},
	}
	out := append([]components.Action{}, a.router.Active().Actions()...)
	// Guarda de prerrequisitos: si Docker no está listo, las acciones que
	// dependen de él se muestran atenuadas con su motivo.
	if a.dockerDown() {
		for i := range out {
			if strings.HasPrefix(out[i].ID, "instance.") && !out[i].Disabled {
				out[i].Disabled = true
				out[i].DisabledReason = "Docker no está activo — pulsa H para repararlo"
			}
		}
	}
	return append(out, globals...)
}

// dockerDown indica que Docker no está disponible y no es un estado desconocido
// (sin comprobar todavía).
func (a *App) dockerDown() bool {
	return !a.docker.State.Ready() && a.docker.State != dockerx.StateUnknown && a.docker.State != ""
}

// invoke ejecuta una acción por su ID estable (paleta, botones).
func (a *App) invoke(id string) tea.Cmd {
	switch id {
	case "global.health":
		a.router.Push(newHealthScreen(a.svc, a.pal, a.ascii))
		return a.svcInit()
	case "global.new":
		a.router.Push(newWizardScreen(a.svc, a.pal, a.ascii))
		return nil
	case "global.help":
		a.router.SetOverlay(newHelpScreen(a.keys, a.pal, a.ascii))
		return nil
	case "global.copy":
		return a.copyAction()
	case "quit.now":
		a.quitting = true
		return tea.Quit
	case "quit.wait":
		return nil
	case "health.repair", "docker.repair":
		return a.repairDocker()
	}
	if inv, ok := a.router.Active().(Actioner); ok {
		return inv.Invoke(id)
	}
	if inv, ok := a.router.Top().(Actioner); ok {
		return inv.Invoke(id)
	}
	return nil
}

// copyAction copia la dirección de conexión de la instancia seleccionada.
func (a *App) copyAction() tea.Cmd {
	if inv, ok := a.router.Active().(Actioner); ok {
		return inv.Invoke("instance.copy")
	}
	return nil
}

// repairDocker lanza la reparación de Docker con espera activa (RF-6).
func (a *App) repairDocker() tea.Cmd {
	if a.repairing {
		return nil
	}
	a.repairing = true
	a.pushToast("⟳ Intentando iniciar Docker…")
	ref := a.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		diag, err := ref.DockerState.Ensure(ctx)
		return dockerRepairMsg{diag: diag, err: err}
	}
}

// pushToast añade un toast (máximo 3, se descarta el más antiguo).
func (a *App) pushToast(text string, kinds ...components.Kind) tea.Cmd {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	kind := components.Secondary
	if len(kinds) > 0 {
		kind = kinds[0]
	}
	a.toastID++
	seq := a.toastID
	a.toasts = append(a.toasts, toastEntry{Text: text, Kind: kind, Seq: seq})
	if len(a.toasts) > 3 {
		a.toasts = a.toasts[len(a.toasts)-3:]
	}
	dur := 4 * time.Second
	if kind == components.Destructive {
		dur = 8 * time.Second
	}
	return tea.Tick(dur, func(time.Time) tea.Msg { return toastExpireMsg{seq: seq} })
}

func (a *App) expireToast(seq int) {
	out := a.toasts[:0]
	for _, t := range a.toasts {
		if t.Seq != seq {
			out = append(out, t)
		}
	}
	a.toasts = out
}

// noteIdleBanner avisa si hay auto-apagado configurado sin vigilante activo.
func (a *App) noteIdleBanner() {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	rows, err := a.svc.Instances.Rows(ctx)
	if err != nil {
		return
	}
	wanted := false
	for _, r := range rows {
		if r.IdleEnabled {
			wanted = true
			break
		}
	}
	if wanted && !a.svc.Idle.WatcherActive() && !a.idleWarned {
		a.idleWarned = true
		a.pushToast("⚠ El auto-apagado no está activo: nadie lo vigila. Actívalo con 'badomens idle daemon'.")
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
	if a.confirm != nil {
		body = a.confirm.Render(w, a.pal, a.ascii)
	}
	// Guarda de tamaño: el cuerpo nunca puede superar el ancho ni el alto.
	body = clampWidth(body, w)
	body = clampLines(body, bodyH)
	banners := a.bannerView(w)
	toasts := ""
	if len(a.toasts) > 0 {
		lines := make([]string, 0, len(a.toasts))
		for _, t := range a.toasts {
			lines = append(lines, components.RenderToast(components.Toast{Text: t.Text, Kind: t.Kind}, a.pal))
		}
		toasts = strings.Join(lines, "\n") + "\n"
	}
	view := strings.Join([]string{header, banners + toasts + body, footer}, "\n")
	// Guarda final de tamaño: ninguna línea puede superar w ni el total, h.
	view = clampWidth(view, w)
	view = clampLines(view, h)
	return view
}

// bannerView pinta el banner fijo de prerrequisitos (Docker/Tailscale).
func (a *App) bannerView(w int) string {
	if a.docker.State.Ready() || a.docker.State == dockerx.StateUnknown || a.docker.State == "" {
		return ""
	}
	msg := "✖ Docker no está disponible — tus servidores no responden. [D] Reparar Docker"
	if a.docker.State == dockerx.StateStarting {
		msg = "◐ Docker está arrancando… [D] Reintentar"
	}
	if a.ascii {
		if a.docker.State == dockerx.StateStarting {
			msg = "[..] Docker esta arrancando... [D] Reintentar"
		} else {
			msg = "[XX] Docker no esta disponible - tus servidores no responden. [D] Reparar Docker"
		}
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(a.pal.Bad)).Render(layout.Truncate(msg, w)) + "\n"
}

func (a *App) headerView(w int) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(a.pal.Accent)).Render("badomens")
	docker := "● Docker OK"
	color := a.pal.Good
	switch {
	case a.docker.State == "":
		docker = "Docker sin comprobar"
		color = a.pal.Muted
	case !a.docker.State.Ready():
		docker = "✖ Docker " + string(a.docker.State)
		color = a.pal.Bad
	}
	if a.ascii {
		switch {
		case a.docker.State == "":
			docker = "[..] Docker sin comprobar"
			color = a.pal.Muted
		case a.docker.State.Ready():
			docker = "[OK] Docker"
		default:
			docker = "[XX] Docker " + string(a.docker.State)
		}
	}
	right := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(docker)
	pad := w - lipgloss.Width(title) - lipgloss.Width(right) - 4
	if pad < 1 {
		pad = 1
	}
	if a.ascii {
		return "+ " + title + strings.Repeat(" ", pad) + right + " +"
	}
	return "┌ " + title + strings.Repeat(" ", pad) + right + " ┐"
}

func (a *App) footerView(w int) string {
	help := a.keys.ShortHelp()
	second := ""
	if top := a.router.Active(); top != nil {
		second = top.Title()
	}
	if a.status != "" {
		second = a.status
	}
	if a.hint != "" {
		second = "Pista: " + a.hint
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(a.pal.Muted)).
		Render(layout.Truncate("  "+help+"    "+second, w))
}

// clampLines recorta un texto al número de líneas indicado.
func clampLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n")
}

// clampWidth recorta cada línea al ancho indicado respetando las secuencias
// ANSI (x/ansi.Truncate) para no romper los estilos.
func clampWidth(s string, w int) string {
	if w <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if ansi.StringWidth(l) > w {
			lines[i] = ansi.Truncate(l, w, "…")
		}
	}
	return strings.Join(lines, "\n")
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
