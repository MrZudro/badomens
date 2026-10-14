package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/events"
	"github.com/MrZudro/badomens/internal/idle"
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

	// Vigilante de auto-apagado en el proceso de la TUI (RF-8 §6).
	idleEvents   chan events.Event
	watchCtx     context.Context
	watchCancel  context.CancelFunc
	watchStarted bool
	idleStandby  bool
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
	zone.NewGlobal()
	pal := theme.For(opts.Theme)
	home := newHomeScreen(sv, pal, opts.ASCII)
	a := &App{
		svc:        sv,
		pal:        pal,
		keys:       DefaultKeys,
		cat:        i18n.Es(),
		ascii:      opts.ASCII,
		reduced:    opts.ReducedMotion,
		anim:       anim.New(opts.ReducedMotion),
		idleEvents: make(chan events.Event, 64),
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
		var cmds []tea.Cmd
		if msg.err == nil {
			a.noteIdleBanner(msg.rows)
			if c := a.maybeStartIdleWatch(msg.rows); c != nil {
				cmds = append(cmds, c)
			}
		}
		cmds = append(cmds, a.router.Update(msg))
		return a, tea.Batch(cmds...)
	case idleEventMsg:
		return a, tea.Batch(a.handleIdleEvent(msg.ev), a.waitIdleEvent())
	case idleWatchDoneMsg:
		if a.watchCancel != nil {
			a.watchCancel()
			a.watchCancel = nil
		}
		if errors.Is(msg.err, idle.ErrStandby) {
			// Hay un demonio vigilando: la TUI no necesita correr el suyo.
			a.idleStandby = true
			return a, nil
		}
		a.watchStarted = false
		return a, nil
	case pollDockerMsg:
		wasReady := a.docker.State.Ready()
		wasSeen := a.dockerSeen
		if msg.err == nil {
			a.docker = msg.diag
			a.dockerSeen = true
			a.poller.NoteDockerResult(msg.diag.State.Ready())
			if !msg.diag.State.Ready() {
				if wasReady {
					a.pushToast(a.cat.T("app.toast.docker_down"), components.Destructive)
				}
			} else if wasSeen && !wasReady {
				a.pushToast(a.cat.T("app.toast.docker_up"))
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
			a.pushToast(a.cat.Tf("app.toast.docker_repair_fail", map[string]string{"cause": msg.err.Error()}), components.Destructive)
			return a, nil
		}
		a.docker = msg.diag
		if msg.diag.State.Ready() {
			a.pushToast(a.cat.T("app.toast.docker_ready"))
		} else {
			a.pushToast(a.cat.Tf("app.toast.docker_still", map[string]string{"cause": msg.diag.Message}), components.Destructive)
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
			a.pushToast(a.cat.Tf("app.toast.create_fail", map[string]string{"cause": msg.err.Error()}), components.Destructive)
			return a, nil
		}
		a.router.Pop()
		a.pushToast(a.cat.Tf("app.toast.created", map[string]string{"name": msg.slug}))
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
	// Captura de entrada (§5): si la pantalla activa tiene un campo de texto o
	// modal con foco, las teclas imprimibles van a ella y no disparan atajos
	// globales. Solo Ctrl+C sigue siendo global para poder salir.
	if c, ok := a.router.Active().(InputCaptor); ok && c.CapturesInput() {
		if key == "ctrl+c" {
			if a.busy {
				return a.askQuit()
			}
			a.quitting = true
			a.stopWatch()
			return a, tea.Quit
		}
		return a, a.router.Update(msg)
	}
	switch key {
	case "ctrl+c", "ctrl+q":
		if a.busy {
			return a.askQuit()
		}
		a.quitting = true
		a.stopWatch()
		return a, tea.Quit
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
	case "f1":
		if a.router.Overlay() != nil && a.router.Overlay().ID() == "help" {
			a.router.ClearOverlay()
			return a, nil
		}
		a.router.SetOverlay(newHelpScreen(a.keys, a.pal, a.ascii))
		return a, nil
	case "f2":
		if a.router.Overlay() == nil {
			a.router.Push(newHealthScreen(a.svc, a.pal, a.ascii))
			return a, a.svcInit()
		}
	case "ctrl+p":
		if a.router.Overlay() == nil {
			a.router.SetOverlay(newPaletteScreen(a.actions(), a.pal, a.ascii))
		}
		return a, nil
	case "ctrl+n":
		if a.router.Overlay() == nil && a.router.Top().ID() != "wizard" {
			a.router.Push(newWizardScreen(a.svc, a.pal, a.ascii))
			return a, nil
		}
	case "n":
		// 'n' crea solo desde Inicio (sin input activo) para no chocar con el
		// texto de un formulario. El atajo global es Ctrl+N.
		if a.router.Overlay() == nil && a.router.Top().ID() == "home" {
			a.router.Push(newWizardScreen(a.svc, a.pal, a.ascii))
			return a, nil
		}
	case "ctrl+r":
		// Atajo de reparación cuando Docker está caído (no imprimible, para no
		// chocar con las teclas de las pantallas).
		if !a.docker.State.Ready() && a.router.Overlay() == nil {
			return a, a.repairDocker()
		}
	case "ctrl+t":
		return a, func() tea.Msg { return themeChangedMsg{name: nextTheme(a.pal.Name)} }
	}
	if a.router.Overlay() != nil && key == "esc" {
		a.router.ClearOverlay()
		return a, nil
	}
	return a, a.router.Update(msg)
}

// handleMouse traduce la rueda del ratón a las teclas de navegación y resuelve
// los clics en botones (zonas) y filas clicables (§4.1).
func (a *App) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return a.Update(tea.KeyMsg{Type: tea.KeyUp})
	case tea.MouseButtonWheelDown:
		return a.Update(tea.KeyMsg{Type: tea.KeyDown})
	case tea.MouseButtonLeft:
		if msg.Action == tea.MouseActionPress {
			return a.handleClick(msg)
		}
	}
	return a, nil
}

// handleClick resuelve un clic: primero los botones (por zona de acción) y luego
// los elementos clicables propios de la pantalla activa.
func (a *App) handleClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	for _, act := range a.actions() {
		z := zone.Get("action:" + act.ID)
		if z != nil && z.InBounds(msg) {
			if act.Disabled {
				return a, nil
			}
			return a, a.invoke(act.ID)
		}
	}
	if cz, ok := a.router.Active().(Clicker); ok {
		if cmd := cz.Click(msg); cmd != nil {
			return a, cmd
		}
	}
	return a, nil
}

// askQuit pide confirmación para salir con una operación en curso (RF-7).
func (a *App) askQuit() (tea.Model, tea.Cmd) {
	a.confirm = &components.Modal{
		Title: a.cat.T("app.quit.title"),
		Body:  a.cat.T("app.quit.body"),
		Actions: []components.Action{
			{ID: "quit.wait", Verb: a.cat.T("app.quit.wait"), Object: a.cat.T("app.quit.wait_obj"), Key: "n", Hint: a.cat.T("app.quit.wait_hint"), Kind: components.Secondary},
			{ID: "quit.now", Verb: a.cat.T("app.quit.now"), Object: a.cat.T("app.quit.now_obj"), Key: "y", Hint: a.cat.T("app.quit.now_hint"), Kind: components.Destructive},
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
		{ID: "global.health", Verb: "Abrir", Object: a.cat.T("global.health.obj"), Key: "F2", Hint: a.cat.T("global.health.hint"), Kind: components.Navigation},
		{ID: "global.new", Verb: "Crear", Object: a.cat.T("global.new.obj"), Key: "ctrl+n", Hint: a.cat.T("global.new.hint"), Kind: components.Primary, Disabled: a.dockerDown(), DisabledReason: a.cat.T("app.disabled.docker")},
		{ID: "global.help", Verb: "Ver", Object: a.cat.T("global.help.obj"), Key: "F1", Hint: a.cat.T("global.help.hint"), Kind: components.Navigation},
	}
	out := append([]components.Action{}, a.router.Active().Actions()...)
	// Guarda de prerrequisitos: si Docker no está listo, las acciones que
	// dependen de él se muestran atenuadas con su motivo.
	if a.dockerDown() {
		for i := range out {
			if strings.HasPrefix(out[i].ID, "instance.") && !out[i].Disabled {
				out[i].Disabled = true
				out[i].DisabledReason = a.cat.T("app.disabled.docker")
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
		a.stopWatch()
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
	a.pushToast(a.cat.T("app.toast.repairing"))
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
func (a *App) noteIdleBanner(rows []svc.Row) {
	wanted := false
	for _, r := range rows {
		if r.IdleEnabled {
			wanted = true
			break
		}
	}
	if wanted && !a.watchStarted && !a.svc.Idle.WatcherActive() && !a.idleWarned {
		a.idleWarned = true
		a.pushToast(a.cat.T("app.toast.idle_no_watcher"))
	}
}

// idleEventMsg entrega un evento del vigilante en proceso a la TUI.
type idleEventMsg struct{ ev events.Event }

// idleWatchDoneMsg indica que el vigilante en proceso terminó.
type idleWatchDoneMsg struct{ err error }

// maybeStartIdleWatch arranca el vigilante en el proceso de la TUI si hay
// instancias con auto-apagado activo y nadie más lo vigila (RF-8 §6).
func (a *App) maybeStartIdleWatch(rows []svc.Row) tea.Cmd {
	if a.watchStarted || a.idleStandby {
		return nil
	}
	wanted := false
	for _, r := range rows {
		if r.IdleEnabled {
			wanted = true
			break
		}
	}
	if !wanted || a.svc.Idle.WatcherActive() {
		return nil
	}
	start := a.startIdleWatch()
	return tea.Batch(start, a.waitIdleEvent())
}

// startIdleWatch lanza el vigilante en un Cmd (goroutine) y publica sus eventos.
func (a *App) startIdleWatch() tea.Cmd {
	if a.watchStarted {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.watchCtx, a.watchCancel = ctx, cancel
	a.watchStarted = true
	ref := a.svc
	emit := func(ev events.Event) {
		select {
		case a.idleEvents <- ev:
		default: // la cola va llena: se descarta para no bloquear al vigilante
		}
	}
	return func() tea.Msg {
		return idleWatchDoneMsg{err: ref.Idle.Watch(ctx, emit)}
	}
}

// waitIdleEvent espera el siguiente evento del vigilante (o la cancelación).
func (a *App) waitIdleEvent() tea.Cmd {
	ctx := a.watchCtx
	return func() tea.Msg {
		if ctx == nil {
			return nil
		}
		select {
		case ev, ok := <-a.idleEvents:
			if !ok {
				return nil
			}
			return idleEventMsg{ev: ev}
		case <-ctx.Done():
			return nil
		}
	}
}

// stopWatch cancela el vigilante en proceso (al salir de la TUI).
func (a *App) stopWatch() {
	if a.watchCancel != nil {
		a.watchCancel()
		a.watchCancel = nil
	}
}

// handleIdleEvent convierte un evento de auto-apagado en un aviso visible.
func (a *App) handleIdleEvent(ev events.Event) tea.Cmd {
	switch ev.Kind {
	case events.IdleCountdownStarted:
		secs := 0
		if v, ok := ev.Data["remaining_seconds"].(int); ok {
			secs = v
		}
		return a.pushToast(a.cat.Tf("app.toast.idle_countdown", map[string]string{"name": ev.Instance, "remaining": humanSeconds(secs)}))
	case events.IdleCountdownCancelled:
		return a.pushToast(a.cat.Tf("app.toast.idle_cancelled", map[string]string{"name": ev.Instance}))
	case events.IdleStopped:
		return a.pushToast(a.cat.Tf("app.toast.idle_stopped", map[string]string{"name": ev.Instance}))
	}
	return nil
}

// humanSeconds redacta una duración corta (p. ej. 90 → "1:30").
func humanSeconds(secs int) string {
	if secs <= 0 {
		return "0 s"
	}
	if secs < 60 {
		return itoa(secs) + " s"
	}
	return itoa(secs/60) + ":" + leftPad2(secs%60)
}

func leftPad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

// View pinta la app completa en el tamaño actual.
func (a *App) View() string {
	w, h := a.size.W, a.size.H
	if w <= 0 {
		w, h = 100, 30
	}
	if a.size.TooSmall() {
		return zone.Scan(lipgloss.NewStyle().Foreground(lipgloss.Color(a.pal.Warn)).
			Render(a.cat.Tf("app.too_small", map[string]string{"w": itoa(w), "h": itoa(h)})))
	}
	header := a.headerView(w)
	footer := a.footerView(w)
	banners := a.bannerView(w)
	toasts := ""
	if len(a.toasts) > 0 {
		lines := make([]string, 0, len(a.toasts))
		for _, t := range a.toasts {
			lines = append(lines, components.RenderToast(components.Toast{Text: t.Text, Kind: t.Kind}, a.pal))
		}
		toasts = strings.Join(lines, "\n") + "\n"
	}
	// Franja fija de la pantalla (título y pestañas), si la expone (§4).
	chrome := ""
	if a.router.Overlay() == nil && a.confirm == nil {
		if c, ok := a.router.Top().(Chromed); ok {
			chrome = c.Chrome(w)
		}
	}
	chromeLines := 0
	if chrome != "" {
		chromeLines = strings.Count(chrome, "\n") + 1
	}
	// Reserva fija: 1 línea de cabecera + 1 de barra de atajos + banners + toasts
	// + pestañas. El cuerpo es el único que se desplaza (§4).
	extra := 0
	if banners != "" {
		extra++
	}
	extra += len(a.toasts)
	extra += chromeLines
	bodyH := h - 2 - extra
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
	// Guarda de tamaño: el cuerpo nunca puede superar el ancho ni el alto útil.
	body = clampWidth(body, w)
	middle := banners + toasts
	if chrome != "" {
		middle += chrome + "\n"
	}
	middle += body
	middle = clampLines(middle, h-2)
	view := strings.Join([]string{header, middle, footer}, "\n")
	// Guarda final de ancho: ninguna línea puede superar w.
	view = clampWidth(view, w)
	// Registra las zonas de ratón y quita sus marcadores (bubblezone, §4.1).
	return zone.Scan(view)
}

// bannerView pinta el banner fijo de prerrequisitos (Docker/Tailscale).
func (a *App) bannerView(w int) string {
	if a.docker.State.Ready() || a.docker.State == dockerx.StateUnknown || a.docker.State == "" {
		return ""
	}
	msg := a.cat.T("app.banner.docker_down")
	if a.docker.State == dockerx.StateStarting {
		msg = a.cat.T("app.banner.docker_starting")
	}
	if a.ascii {
		if a.docker.State == dockerx.StateStarting {
			msg = a.cat.T("app.banner.ascii_starting")
		} else {
			msg = a.cat.T("app.banner.ascii_down")
		}
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(a.pal.Bad)).Render(layout.Truncate(msg, w)) + "\n"
}

func (a *App) headerView(w int) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(a.pal.Accent)).Render("badomens")
	docker := a.cat.T("app.header.docker_ok")
	color := a.pal.Good
	switch {
	case a.docker.State == "":
		docker = a.cat.T("app.header.docker_unchecked")
		color = a.pal.Muted
	case !a.docker.State.Ready():
		docker = a.cat.Tf("app.header.docker_bad", map[string]string{"state": string(a.docker.State)})
		color = a.pal.Bad
	}
	if a.ascii {
		switch {
		case a.docker.State == "":
			docker = a.cat.T("app.header.ascii_unchecked")
			color = a.pal.Muted
		case a.docker.State.Ready():
			docker = a.cat.T("app.header.ascii_ok")
		default:
			docker = a.cat.Tf("app.header.ascii_bad", map[string]string{"state": string(a.docker.State)})
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
	if a.ascii {
		help = a.keys.ShortHelpASCII()
	}
	second := ""
	if top := a.router.Active(); top != nil {
		second = top.Title()
	}
	if a.status != "" {
		second = a.status
	}
	if a.hint != "" {
		second = a.cat.Tf("app.footer.hint", map[string]string{"hint": a.hint})
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
