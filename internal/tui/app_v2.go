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
	"github.com/MrZudro/badomens/internal/version"
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
	styles  shellStyles
	keys    KeyMap
	cat     *i18n.Catalog
	size    layout.Size
	toasts  []toastEntry
	toastID int
	status  string
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

	// Estado de la cabecera: Tailscale y contadores de instancias (§2).
	tsIP         string
	tsOK         bool
	tsSeen       bool
	instCount    int
	runningCount int
	countsSeen   bool

	// Estado EN VIVO de MagicDNS (Fase 8 §5): lo comparte con pantallas y Salud.
	magic magicState

	// Arranque garantizado de infraestructura (Fase 8 §8.2/§8.3): se intenta una
	// sola vez por componente para no duplicar tareas.
	infraTried     map[string]bool
	daemonPrompted bool

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
		infraTried: map[string]bool{},
	}
	a.router = NewRouter(home)
	a.poller = &Poller{Svc: sv}
	a.styles = newShellStyles(pal, opts.ASCII)
	a.status = ""
	return a
}

// Init arranca la pantalla inicial y el latido del poller.
func (a *App) Init() tea.Cmd {
	// Arranque garantizado del daemon (Fase 8 §8.2): se comprueba y, si hace
	// falta, se levanta. Se avisa del estado «Cargando» (🟡) sin bloquear el bucle.
	starting := a.pushToast("🟡 Comprobando el daemon de badomens…")
	return tea.Batch(a.router.Top().Init(), a.poller.Schedule(), a.poller.Cmds(nowUTC(), true, true), a.tickCmd(), magicProbeCmd(), a.ensureDaemonCmd(), starting)
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
			a.noteCounts(msg.rows)
			a.noteIdleBanner(msg.rows)
			if c := a.maybeStartIdleWatch(msg.rows); c != nil {
				cmds = append(cmds, c)
			}
		}
		cmds = append(cmds, a.router.Update(msg))
		return a, tea.Batch(cmds...)
	case homeReqMsg:
		// Tailscale para la cabecera; la pantalla sigue procesando el mensaje.
		a.tsIP, a.tsOK, a.tsSeen = msg.tsIP, msg.tsOK, true
		return a, a.router.Update(msg)
	case magicProbeMsg:
		// Estado en vivo de MagicDNS: se conserva para la cabecera y para Salud, y
		// las pantallas lo reciben para reevaluar el aviso (§5).
		a.magic = magicStateFrom(msg.rep)
		return a, a.router.Update(msg)
	case idleEventMsg:
		// El aviso global (toast) va al App y el evento se reenvía a la pantalla
		// activa para que la pestaña Auto-apagado refleje la cuenta atrás (§5.10).
		return a, tea.Batch(a.handleIdleEvent(msg.ev), a.waitIdleEvent(), a.router.Update(msg))
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
	case infraReadyMsg:
		// Resultado del mecanismo común de infraestructura: avisos accionables y,
		// si toca, oferta de instalar el daemon como servicio persistente.
		cmd := a.noteInfra(msg.what, msg.rep)
		return a, tea.Batch(cmd, a.router.Update(msg))
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
		// Docker apagado: se intenta levantar UNA vez con el mecanismo común (§8.3),
		// con estado «Cargando» (🟡); no se duplican intentos.
		cmds := []tea.Cmd{a.router.Update(msg)}
		if !a.infraTried["docker"] && !msg.diag.State.Ready() {
			a.infraTried["docker"] = true
			cmds = append(cmds, a.pushToast("🟡 Levantando Docker…"), a.ensureDockerCmd())
		}
		return a, tea.Batch(cmds...)
	case toastMsg:
		a.busy = false
		return a, a.pushToast(msg.Text, msg.Kind)
	case instanceOpMsg:
		// Resultado de una operación del detalle: se libera siempre el estado
		// ocupado y se avisa aunque la pantalla que la pidió ya no esté activa.
		a.busy = false
		cmd := a.router.Update(msg)
		return a, tea.Batch(cmd, a.pushToast(msg.text(), msg.kind()))
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
			return a, a.router.Update(msg)
		}
		a.router.Pop()
		a.pushToast(a.cat.Tf("app.toast.created", map[string]string{"name": msg.slug}))
		return a, a.poller.Cmds(nowUTC(), true, false)
	case themeChangedMsg:
		a.pal = theme.For(msg.name)
		a.styles = newShellStyles(a.pal, a.ascii)
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
	case "tab":
		return a, a.cycleSection(1)
	case "shift+tab":
		return a, a.cycleSection(-1)
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
		a.router.SetOverlay(newHelpScreen(a.keys, a.actions(), a.pal, a.ascii))
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
		a.router.SetOverlay(newHelpScreen(a.keys, a.actions(), a.pal, a.ascii))
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
	case "daemon.install":
		return a.installDaemonCmd()
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
	// La barra de estado conserva el último mensaje de dominio (§3), aunque el
	// toast desaparezca.
	a.status = text
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
	// TTL por tipo (SPEC 2 §12): info/éxito 6–8 s, avisos 20 s, errores 30 s, con
	// tope absoluto de 30 s. Ninguna notificación es permanente.
	dur := components.NotificationTTL(kind, text)
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

// noteCounts actualiza los contadores de la cabecera (§2) a partir del listado.
func (a *App) noteCounts(rows []svc.Row) {
	run := 0
	for _, r := range rows {
		if r.State == "running" {
			run++
		}
	}
	a.instCount, a.runningCount, a.countsSeen = len(rows), run, true
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

// shellStyles agrupa los estilos del marco para no reconstruirlos en cada frame
// (§6.5). Se crean con el tema activo y se reutilizan tal cual.
type shellStyles struct {
	title    lipgloss.Style
	border   lipgloss.Style
	muted    lipgloss.Style
	good     lipgloss.Style
	warn     lipgloss.Style
	bad      lipgloss.Style
	navOn    lipgloss.Style
	navOff   lipgloss.Style
	barLeft  string
	barRight string
}

func newShellStyles(p theme.Palette, ascii bool) shellStyles {
	v := "│"
	if ascii {
		v = "|"
	}
	border := lipgloss.NewStyle().Foreground(lipgloss.Color(p.Border))
	return shellStyles{
		title:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Accent)),
		border:   border,
		muted:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.Muted)),
		good:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.Good)),
		warn:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.Warn)),
		bad:      lipgloss.NewStyle().Foreground(lipgloss.Color(p.Bad)),
		navOn:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.AccentText)).Background(lipgloss.Color(p.Accent)),
		navOff:   lipgloss.NewStyle().Foreground(lipgloss.Color(p.Muted)),
		barLeft:  border.Render(v + " "),
		barRight: border.Render(" " + v),
	}
}

// frameChars devuelve las esquinas y el trazo horizontal del marco: caja Unicode
// o ASCII (+ - |) según el modo (§2/§4.2).
func frameChars(ascii bool) (tl, tr, bl, br, hz string) {
	if ascii {
		return "+", "+", "+", "+", "-"
	}
	return "┌", "┐", "└", "┘", "─"
}

// View pinta la app completa: marco con cabecera, navegación, cuerpo, estado y
// barra de acciones (§2/§3). Cabecera, nav, separador, estado y acciones tienen
// altura fija; solo el cuerpo se desplaza.
func (a *App) View() string {
	w, h := a.size.W, a.size.H
	if w <= 0 {
		w, h = 100, 30
	}
	if a.size.TooSmall() {
		return zone.Scan(lipgloss.NewStyle().Foreground(lipgloss.Color(a.pal.Warn)).
			Render(a.cat.Tf("app.too_small", map[string]string{"w": itoa(w), "h": itoa(h)})))
	}
	// Ancho interior: dentro de "│ " … " │" (2 celdas de marco + 2 de relleno).
	inner := w - 4
	if inner < 1 {
		inner = 1
	}

	// Franja fija de la pantalla (título y pestañas), si la expone (§4).
	chrome := ""
	if a.router.Overlay() == nil && a.confirm == nil {
		if c, ok := a.router.Top().(Chromed); ok {
			chrome = c.Chrome(inner)
		}
	}
	var chromeLines []string
	if chrome != "" {
		chromeLines = strings.Split(chrome, "\n")
	}

	banners := a.bannerView(inner)
	toastLines := make([]string, 0, len(a.toasts))
	for _, t := range a.toasts {
		toastLines = append(toastLines, components.RenderToast(components.Toast{Text: t.Text, Kind: t.Kind}, a.pal))
	}

	// Reserva fija: cabecera (marca) 1 + nav 1 + separador 1 + estado 1 +
	// acciones (marca) 1 = 5 líneas. El resto lo ocupan banners, toasts, pestañas
	// y el cuerpo (el único que se desplaza).
	extra := 0
	if banners != "" {
		extra++
	}
	extra += len(toastLines) + len(chromeLines)
	bodyH := h - 5 - extra
	if bodyH < 1 {
		bodyH = 1
	}

	body := a.router.Active().View(inner, bodyH)
	if overlay := a.router.Overlay(); overlay != nil {
		body = overlay.View(inner, bodyH)
	}
	if a.confirm != nil {
		body = a.confirm.Render(inner, a.pal, a.ascii)
	}
	bodyLines := strings.Split(body, "\n")
	if len(bodyLines) > bodyH {
		bodyLines = bodyLines[:bodyH]
	}
	for len(bodyLines) < bodyH {
		bodyLines = append(bodyLines, "")
	}

	lines := make([]string, 0, h)
	lines = append(lines, a.headerView(w))
	lines = append(lines, a.barLine(padLine(a.navView(inner), inner)))
	lines = append(lines, a.sepLine(w))
	if banners != "" {
		lines = append(lines, a.barLine(padLine(banners, inner)))
	}
	for _, tl := range toastLines {
		lines = append(lines, a.barLine(padLine(tl, inner)))
	}
	for _, cl := range chromeLines {
		lines = append(lines, a.barLine(padLine(cl, inner)))
	}
	for _, bl := range bodyLines {
		lines = append(lines, a.barLine(padLine(bl, inner)))
	}
	lines = append(lines, a.barLine(padLine(a.statusView(inner), inner)))
	lines = append(lines, a.bottomLine(w, a.actionsText(w-5)))
	if len(lines) > h {
		lines = lines[:h]
	}
	// Registra las zonas de ratón y quita sus marcadores (bubblezone, §4.1).
	return zone.Scan(strings.Join(lines, "\n"))
}

// barLine envuelve una línea interior con los laterales del marco.
func (a *App) barLine(content string) string {
	return a.styles.barLeft + content + a.styles.barRight
}

// sepLine dibuja el separador horizontal entre la navegación y el cuerpo.
func (a *App) sepLine(w int) string {
	_, _, _, _, hz := frameChars(a.ascii)
	l, r := "├", "┤"
	if a.ascii {
		l, r = "+", "+"
	}
	n := w - 2
	if n < 0 {
		n = 0
	}
	return a.styles.border.Render(l + strings.Repeat(hz, n) + r)
}

// bottomLine dibuja el borde inferior con la barra de acciones dentro.
func (a *App) bottomLine(w int, actions string) string {
	_, _, bl, br, hz := frameChars(a.ascii)
	avail := w - 5
	if avail < 1 {
		avail = 1
	}
	content := a.styles.muted.Render(layout.Truncate(actions, avail))
	fill := w - 5 - layout.Width(content)
	if fill < 0 {
		fill = 0
	}
	return a.styles.border.Render(bl+hz+" ") + content +
		a.styles.border.Render(" "+strings.Repeat(hz, fill)+br)
}

// bannerView pinta el banner fijo de prerrequisitos (Docker/Tailscale). Si el
// mensaje completo no cabe, usa una variante corta que conserva la acción.
func (a *App) bannerView(w int) string {
	if a.docker.State.Ready() || a.docker.State == dockerx.StateUnknown || a.docker.State == "" {
		return ""
	}
	starting := a.docker.State == dockerx.StateStarting
	msg := a.bannerText(starting, false)
	if layout.Width(msg) > w {
		msg = a.bannerText(starting, true)
	}
	return a.styles.bad.Render(layout.Truncate(msg, w))
}

// bannerText devuelve el texto del banner según el estado y la variante.
func (a *App) bannerText(starting, short bool) string {
	switch {
	case starting && a.ascii && short:
		return a.cat.T("app.banner.ascii_start_short")
	case starting && short:
		return a.cat.T("app.banner.starting_short")
	case starting && a.ascii:
		return a.cat.T("app.banner.ascii_starting")
	case starting:
		return a.cat.T("app.banner.docker_starting")
	case a.ascii && short:
		return a.cat.T("app.banner.ascii_down_short")
	case short:
		return a.cat.T("app.banner.docker_down_short")
	case a.ascii:
		return a.cat.T("app.banner.ascii_down")
	default:
		return a.cat.T("app.banner.docker_down")
	}
}

// headerView pinta la cabecera en el borde superior: producto + versión, estado
// Docker/Tailscale y contadores (§2). Se sacrifican contadores y Tailscale si no
// caben; nunca desborda el ancho.
func (a *App) headerView(w int) string {
	tl, tr, _, _, hz := frameChars(a.ascii)
	leftFull := "badomens"
	if version.Version != "" {
		leftFull = "badomens " + version.Version
	}
	left := leftFull
	lw := layout.Width(left)

	parts := make([]string, 0, 3)
	parts = append(parts, a.dockerHeader())
	if t := a.tailscaleHeader(); t != "" {
		parts = append(parts, t)
	}
	if c := a.countersHeader(); c != "" {
		parts = append(parts, c)
	}
	// Se sacrifican contadores, Tailscale y, por último, la versión, antes de
	// truncar; la cabecera nunca desborda el ancho.
	for w-8-lw-layout.Width(strings.Join(parts, "  ")) < 1 {

		if len(parts) > 1 {
			parts = parts[:len(parts)-1]
			continue
		}
		if left != "badomens" {
			left = "badomens"
			lw = layout.Width(left)
			continue
		}
		break
	}
	right := strings.Join(parts, "  ")
	if avail := w - 8 - layout.Width(right); avail < lw {
		if avail < 1 {
			avail = 1
		}
		left = layout.Truncate(left, avail)
		lw = layout.Width(left)
	}
	fill := w - 8 - lw - layout.Width(right)
	if fill < 0 {
		fill = 0
	}
	return a.styles.border.Render(tl+hz+" ") + a.styles.title.Render(left) +
		a.styles.border.Render(" "+strings.Repeat(hz, fill)+" ") + right +
		a.styles.border.Render(" "+hz+tr)
}

// dockerHeader describe el estado de Docker con icono + texto (nunca solo color).
func (a *App) dockerHeader() string {
	switch {
	case a.docker.State == "" || a.docker.State == dockerx.StateUnknown:
		if a.ascii {
			return a.styles.muted.Render(a.cat.T("app.header.ascii_unchecked"))
		}
		return a.styles.muted.Render(a.cat.T("app.header.docker_unchecked"))
	case a.docker.State.Ready():
		if a.ascii {
			return a.styles.good.Render(a.cat.T("app.header.ascii_ok"))
		}
		return a.styles.good.Render(a.cat.T("app.header.docker_ok"))
	default:
		vars := map[string]string{"state": string(a.docker.State)}
		if a.ascii {
			return a.styles.bad.Render(a.cat.Tf("app.header.ascii_bad", vars))
		}
		return a.styles.bad.Render(a.cat.Tf("app.header.docker_bad", vars))
	}
}

// tailscaleHeader describe Tailscale (vacío mientras no se haya comprobado).
func (a *App) tailscaleHeader() string {
	if !a.tsSeen {
		return ""
	}
	if a.tsOK {
		if a.ascii {
			return a.styles.good.Render(a.cat.T("app.header.ascii_ts_ok"))
		}
		return a.styles.good.Render(a.cat.T("app.header.ts_ok"))
	}
	if a.ascii {
		return a.styles.warn.Render(a.cat.T("app.header.ascii_ts_bad"))
	}
	return a.styles.warn.Render(a.cat.T("app.header.ts_bad"))
}

// countersHeader resume instancias en marcha y cupos (vacío antes del listado).
func (a *App) countersHeader() string {
	if !a.countsSeen {
		return ""
	}
	return a.cat.Tf("app.header.counters", map[string]string{
		"inst": itoa(a.instCount), "maxInst": itoa(a.svc.MaxInstances),
		"run": itoa(a.runningCount), "maxRun": itoa(a.svc.MaxParallel),
	})
}

// navView dibuja la navegación de primer nivel con la sección activa marcada con
// corchetes (visible sin depender del color) y recortada a lo que quepa (§2).
func (a *App) navView(w int) string {
	active := a.navSection()
	items := []struct{ id, label string }{
		{"home", a.cat.T("nav.home")},
		{"instances", a.cat.T("nav.instances")},
		{"health", a.cat.T("nav.health")},
		{"help", a.cat.T("nav.help")},
	}
	var b strings.Builder
	used := 0
	first := true
	for _, it := range items {
		label := it.label
		on := it.id == active
		if on {
			label = "[" + label + "]"
		}
		lw := layout.Width(label)
		need := lw
		if !first {
			need += 2
		}
		if used+need > w {
			break
		}
		if !first {
			b.WriteString("  ")
			used += 2
		}
		if on {
			b.WriteString(a.styles.navOn.Render(label))
		} else {
			b.WriteString(a.styles.navOff.Render(label))
		}
		used += lw
		first = false
	}
	return b.String()
}

// navSection resuelve la sección activa por la pantalla/overlay vigente.
func (a *App) navSection() string {
	if o := a.router.Overlay(); o != nil {
		if ns, ok := o.(NavSectioner); ok {
			if s := ns.NavSection(); s != "" {
				return s
			}
		}
	}
	if top := a.router.Top(); top != nil {
		if ns, ok := top.(NavSectioner); ok {
			if s := ns.NavSection(); s != "" {
				return s
			}
		}
		switch top.ID() {
		case "instance":
			return "instances"
		case "health":
			return "health"
		case "help":
			return "help"
		}
	}
	return "home"
}

// statusView pinta la barra de estado: último mensaje de dominio con icono +
// texto, e indicador de operación en curso a la derecha (§3).
func (a *App) statusView(w int) string {
	text := strings.TrimSpace(a.statusText())
	style := a.styles.muted
	switch {
	case hasAnyPrefix(text, "✖", "[XX]"):
		style = a.styles.bad
	case hasAnyPrefix(text, "⚠", "[!!]"):
		style = a.styles.warn
	case hasAnyPrefix(text, "✓", "[OK]"):
		style = a.styles.good
	case hasAnyPrefix(text, "⟳", "[..]"):
		style = a.styles.title
	}
	right := ""
	if a.busy {
		right = a.cat.T("app.status.busy")
		if a.ascii {
			right = a.cat.T("app.status.busy_ascii")
		}
	}
	avail := w - layout.Width(right) - 1
	if avail < 1 {
		avail = 1
	}
	text = layout.Truncate(text, avail)
	gap := w - layout.Width(text) - layout.Width(right)
	if gap < 1 {
		gap = 1
	}
	return style.Render(text) + strings.Repeat(" ", gap) + a.styles.muted.Render(right)
}

// statusText prefiere el mensaje de la pantalla activa y cae al último mensaje
// de dominio global; si no hay ninguno, usa el título de la pantalla para que la
// barra nunca quede en blanco (§3).
func (a *App) statusText() string {
	if st, ok := a.router.Active().(Statuser); ok {
		if s := strings.TrimSpace(st.Status()); s != "" {
			return s
		}
	}
	if s := strings.TrimSpace(a.status); s != "" {
		return s
	}
	if active := a.router.Active(); active != nil {
		return active.Title()
	}
	return ""
}

// actionsText compone la barra inferior con un ÚNICO componente (SPEC 2 §1):
// formato «[Tecla] Etiqueta», solo atajos contextuales de la pantalla activa
// más los esenciales. Ctrl+P, F2 y Ctrl+Q quedan FUERA de la barra (siguen
// funcionando y se documentan en Ayuda).
func (a *App) actionsText(w int) string {
	// Los esenciales se reservan SIEMPRE: primero se recorta lo contextual y
	// luego se añade la cola fija, de modo que «Ayuda» nunca desaparece.
	ess := components.RenderFooter(a.footerEssentials(), w, a.pal)
	essW := layout.Width(ess)
	if essW == 0 {
		return components.RenderFooter(a.footerContextKeys(), w, a.pal)
	}
	if essW+3 >= w {
		return ess
	}
	head := components.RenderFooter(a.footerContextKeys(), w-essW-3, a.pal)
	if head == "" {
		return ess
	}
	return head + "   " + ess
}

// footerEssentials son las teclas imprescindibles de la barra inferior: [F1]
// Ayuda, [Esc] Atrás (solo cuando aplica, resaltado con el input de comandos
// enfocado) y [TAB] Desplazarse. Ctrl+P, F2 y Ctrl+Q NO van aquí: siguen
// funcionando y se documentan en Ayuda (SPEC 2 §1.2).
func (a *App) footerEssentials() []components.FooterKey {
	items := []components.FooterKey{components.FooterKeyFor("f1", "Ayuda")}
	if a.escApplies() {
		esc := components.FooterKeyFor("esc", "Atrás")
		esc.Highlight = a.inputFocused()
		items = append(items, esc)
	}
	items = append(items, components.FooterKeyFor("tab", "Desplazarse"))
	return items
}

// footerContextKeys lista los atajos contextuales de la pantalla activa sin
// strings hardcodeados por pantalla (Fase 6.2).
func (a *App) footerContextKeys() []components.FooterKey {
	acts := a.router.Active().Actions()
	items := make([]components.FooterKey, 0, len(acts))
	seen := make(map[string]bool, len(acts))
	for _, act := range acts {
		key := strings.ToLower(strings.TrimSpace(act.Key))
		switch {
		case key == "", seen[key], key == "f1", key == "tab", key == "esc":
			continue
		}
		seen[key] = true
		items = append(items, footerKeyForAction(act))
	}
	return items
}

// footerKeyForAction traduce una acción a un atajo de la barra. Las etiquetas
// definitivas de la SPEC (§1.2) se aplican SOLO a las acciones que corresponden
// («[B] Backup», «[D] Borrar»): la tecla por sí sola no basta porque «D» también
// es «Reparar Docker» en Salud.
func footerKeyForAction(act components.Action) components.FooterKey {
	switch act.ID {
	case "instance.backup":
		return components.FooterKey{Key: components.DisplayKey(act.Key), Label: "Backup"}
	case "instance.delete":
		return components.FooterKey{Key: components.DisplayKey(act.Key), Label: "Borrar"}
	}
	return components.FooterKey{Key: components.DisplayKey(act.Key), Label: strings.TrimSpace(act.Verb)}
}

// escApplies indica si puede pintarse «[Esc] Atrás»: cuando la pantalla puede
// retroceder (detalle, asistente, Salud) o cuando el input de comandos tiene foco.
func (a *App) escApplies() bool {
	if a.inputFocused() {
		return true
	}
	active := a.router.Active()
	if active == nil {
		return false
	}
	switch active.ID() {
	case "home", "help", "health":
		// En Inicio, Ayuda y Salud, Esc no retrocede (son pantallas de primer
		// nivel o no implementan el pop): no se promete una tecla que no actúa.
		return false
	}
	return true
}

// inputFocused informa de si el campo de comandos de la pantalla activa tiene el
// foco (§6): la barra inferior resalta entonces «[Esc] Atrás».
func (a *App) inputFocused() bool {
	if f, ok := a.router.Active().(InputFocuser); ok {
		return f.InputFocused()
	}
	return false
}

// spacesBuf es una reserva de espacios reutilizable para rellenar líneas sin
// asignar con strings.Repeat en cada frame (§6.5).
var spacesBuf = strings.Repeat(" ", 512)

// padLine ajusta una línea al ancho interior exacto, recortando con «…» si sobra
// y rellenando con espacios si falta.
func padLine(s string, w int) string {
	if w <= 0 {
		return s
	}
	switch width := layout.Width(s); {
	case width > w:
		return ansi.Truncate(s, w, "…")
	case width < w:
		n := w - width
		if n <= len(spacesBuf) {
			return s + spacesBuf[:n]
		}
		return s + strings.Repeat(" ", n)
	}
	return s
}

// hasAnyPrefix indica si s empieza por alguno de los prefijos dados.
func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
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

// sectionOrder es el orden de las pantallas de primer nivel que recorre Tab
// (§2): inicio → instancias → salud → ayuda.
var sectionOrder = []string{"home", "instances", "health", "help"}

// cycleSection cambia de pantalla de primer nivel con Tab (d = 1) o Shift+Tab
// (d = -1). Un formulario con el foco (asistente, consola, modal) no llega aquí:
// handleGlobalKey lo entrega antes a la pantalla (§5).
func (a *App) cycleSection(d int) tea.Cmd {
	// El asistente de creación no declara captura mientras elige el modo (no hay
	// campo de texto con foco), pero Tab es suyo: ahí avanza de campo y de paso.
	// Con un overlay abierto (ayuda o paleta) la navegación sí funciona.
	if a.router.Overlay() == nil {
		if top := a.router.Top(); top != nil && top.ID() == "wizard" {
			return nil
		}
	}
	n := len(sectionOrder)
	idx := 0
	cur := a.navSection()
	for i, id := range sectionOrder {
		if id == cur {
			idx = i
			break
		}
	}
	return a.gotoSection(sectionOrder[((idx+d)%n+n)%n])
}

// gotoSection navega a una pantalla de primer nivel (§2):
//   - inicio: vuelve a la raíz de la pila y cierra el overlay;
//   - instancias: sube al detalle si ya está apilado; si no, abre el detalle de
//     la instancia seleccionada en Inicio y, si no hay ninguna, cae a Salud;
//   - salud: cierra el overlay y apila Salud (o se queda si ya es la cima);
//   - ayuda: abre el overlay de ayuda.
func (a *App) gotoSection(target string) tea.Cmd {
	switch target {
	case "home":
		a.router.ClearOverlay()
		if root := a.router.Root(); root != nil {
			a.router.PopTo(root.ID())
		}
		return nil
	case "instances":
		a.router.ClearOverlay()
		if a.router.Contains("instance") {
			a.router.PopTo("instance")
			return nil
		}
		if home, ok := a.router.Root().(*homeScreen); ok {
			if slug, ok := home.selectedSlug(); ok {
				a.router.Push(newInstanceScreen(a.svc, a.pal, a.ascii, slug))
				return a.svcInit()
			}
		}
		return a.gotoSection("health")
	case "health":
		a.router.ClearOverlay()
		if top := a.router.Top(); top != nil && top.ID() == "health" {
			return nil
		}
		a.router.Push(newHealthScreen(a.svc, a.pal, a.ascii))
		return a.svcInit()
	case "help":
		a.router.SetOverlay(newHelpScreen(a.keys, a.actions(), a.pal, a.ascii))
		return nil
	}
	return nil
}
