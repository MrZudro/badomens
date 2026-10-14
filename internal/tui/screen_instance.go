package tui

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/MrZudro/badomens/internal/backup"
	"github.com/MrZudro/badomens/internal/connection"
	"github.com/MrZudro/badomens/internal/events"
	"github.com/MrZudro/badomens/internal/idle"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/ports"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/help"
	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// maxLogLines acota el buffer de la consola: solo se conservan las últimas
// líneas para que el detalle no dependa de un log sin límite.
const maxLogLines = 2000

// maxMemHist acota el histórico de memoria que dibuja la minigráfica del Resumen
// (el dato llega con el sondeo; en View no se consulta nada).
const maxMemHist = 40

type instanceLogsMsg struct {
	slug string
	text string
	err  error
	gen  int // generación del buffer: descarta respuestas de una consola abandonada
}

type instanceBackupsMsg struct {
	items []backup.Info
	err   error
}

type instancePortMsg struct {
	res ports.Result
	err error
}

type instanceIdleMsg struct {
	cfg idle.Config
	err error
}

// instanceIdleSetMsg cierra un cambio de auto-apagado: la pantalla adopta la
// configuración solo cuando el servicio confirma que quedó guardada.
type instanceIdleSetMsg struct {
	cfg idle.Config
	err error
}

type instancePlayersMsg struct {
	online int
	known  bool
}

// instanceSavedMsg cierra un guardado de Ajustes: lleva el Spec que se escribió
// (una copia, no la del estado previo) para poder adoptarlo solo cuando el
// servicio confirma que quedó persistido.
type instanceSavedMsg struct {
	spec *instance.Spec
	err  error
}

// instanceOpMsg cierra una operación de la pestaña Backups (crear, restaurar o
// podar): la pantalla limpia su aviso de operación en curso y el App publica el
// resultado como toast aunque la pantalla ya no esté activa (así la barra de
// estado nunca queda bloqueada).
type instanceOpMsg struct {
	what   string // "backup" | "restaurar" | "podar"
	slug   string
	detail string // nombre del backup o número de copias podadas
	err    error
}

// text redacta el resultado de la operación con el icono del desenlace.
func (m instanceOpMsg) text() string {
	name := "«" + m.slug + "»"
	if m.err != nil {
		return "✖ " + m.what + " " + name + ": " + m.err.Error()
	}
	switch m.what {
	case "backup":
		return "✓ " + name + ": backup creado"
	case "restaurar":
		return "✓ " + name + ": restaurado " + m.detail + " (con backup previo)"
	case "podar":
		return "✓ " + name + ": podados " + m.detail + " backups (se conservan 5)"
	}
	return "✓ " + name + ": " + m.what
}

// kind clasifica el desenlace para la barra de estado.
func (m instanceOpMsg) kind() components.Kind {
	if m.err != nil {
		return components.Destructive
	}
	return components.Secondary
}

// accessMsg trae el resultado de leer o mutar la lista blanca/operadores. Si op
// es vacío es una lectura; si no, es la verificación posterior a una mutación.
type accessMsg struct {
	slug   string
	state  svc.AccessState
	err    error
	op     string
	player string
}

// instanceDeletedMsg indica que la instancia se eliminó por completo (P-03).
type instanceDeletedMsg struct{ slug string }

// instanceScreen es el detalle de una instancia con pestañas (RF-1/§5.2).
type instanceScreen struct {
	svc     *svc.Services
	pal     theme.Palette
	ascii   bool
	slug    string
	detail  *svc.Detail
	tab     int
	logs    string
	logsErr string // error recuperable de la última lectura de logs (se conserva el buffer)
	logsGen int    // generación del buffer de la consola (vista que lo pidió)
	input   textinput.Model
	focus   bool
	status  string

	history []string
	histIdx int
	follow  bool
	paused  bool
	filter  string

	pending string // comando peligroso pendiente de confirmación

	backups       []backup.Info
	bkCursor      int
	backupsLoaded bool   // ya hubo una lectura de la lista (distingue vacío de cargando)
	bkBusy        string // operación de backup en curso (crear/restaurar/podar)
	bkErr         string // último error al leer la lista de backups

	// Métricas del contenedor para el Resumen: llegan por pollRowsMsg y alimentan
	// la minigráfica de CPU/RAM, sin ninguna E/S en View (§5.3/§7).
	memHist    []float64
	memUsedMB  float64
	memLimitMB float64

	portRes *ports.Result
	portErr string // último error al comprobar el puerto (se conserva el resultado previo)

	idleCfg    idle.Config
	idleCursor int
	idleLoaded bool
	// idleNote refleja la cuenta atrás del auto-apagado recibida del bus de
	// eventos (App.handleIdleEvent la reenvía a la pantalla activa).
	idleNote string

	setCursor int
	editing   bool
	editKey   string

	playersOnline int
	playersKnown  bool

	// Estado de acceso (whitelist/operadores) con fuente y verificación (§6).
	access       svc.AccessState
	accessLoaded bool
	accessStatus string
	accessBusy   bool

	prompt      bool
	promptLabel string
	promptAct   string

	pendingSave *instance.Spec

	// P-03: confirmación del cambio de versión (backup previo obligatorio).
	confirmKind  string // "", "version"
	confirmValue string

	// P-08: recuperación ante caídas.
	lastState    string
	userStopped  bool
	crash        bool
	crashPending bool
	crashCause   string
	crashHistory []time.Time
	crashLoop    bool
}

var instanceTabs = []string{"Resumen", "Consola", "Jugadores", "Backups", "Mundos", "Plugins/Mods", "Red", "Auto-apagado", "Ajustes"}

var idlePresets = idle.Presets()

var consoleCommands = []string{
	"say ", "list", "save-all", "save-all flush", "whitelist add ", "whitelist remove ",
	"op ", "deop ", "kick ", "ban ", "pardon ", "time set day", "weather clear", "stop",
}

var dangerousPrefixes = []string{"stop", "op ", "deop ", "ban ", "ban-ip ", "kick ", "whitelist off", "pardon "}

func newInstanceScreen(sv *svc.Services, pal theme.Palette, ascii bool, slug string) *instanceScreen {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "comando para la consola (ej. say hola)"
	in.Width = 40
	return &instanceScreen{svc: sv, pal: pal, ascii: ascii, slug: slug, input: in, status: "cargando…", follow: true}
}

func (s *instanceScreen) Init() tea.Cmd {
	// Nueva generación del buffer: lo que llegue de una vista anterior se descarta.
	s.logsGen++
	return tea.Batch(s.load(), s.loadIdle())
}

func (s *instanceScreen) load() tea.Cmd {
	ref, slug := s.svc, s.slug
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		d, err := ref.Instances.Detail(ctx, slug)
		return homeDetailMsg{d: d, err: err, slug: slug}
	}
}

func (s *instanceScreen) loadIdle() tea.Cmd {
	ref, slug := s.svc, s.slug
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cfg, err := ref.Idle.Config(ctx, slug)
		return instanceIdleMsg{cfg: cfg, err: err}
	}
}

func (s *instanceScreen) logsCmd() tea.Cmd {
	// La generación capturada viaja en el mensaje: si mientras tanto el usuario
	// abandona la pestaña Consola, la respuesta se descarta al aplicarla.
	ref, slug, gen := s.svc, s.slug, s.logsGen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		text, err := ref.Instances.Logs(ctx, slug, maxLogLines)
		return instanceLogsMsg{slug: slug, text: text, err: err, gen: gen}
	}
}

func (s *instanceScreen) backupsCmd() tea.Cmd {
	ref, slug := s.svc, s.slug
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		items, err := ref.Backups.List(ctx, slug)
		return instanceBackupsMsg{items: items, err: err}
	}
}

func (s *instanceScreen) portCmd() tea.Cmd {
	ref := s.svc
	port := 0
	if s.detail != nil && s.detail.Spec != nil {
		port = s.detail.Spec.Port
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		res, err := ref.Ports.Check(ctx, port)
		return instancePortMsg{res: res, err: err}
	}
}

func (s *instanceScreen) playersCmd() tea.Cmd {
	host, port := "", 0
	if s.detail != nil && s.detail.Spec != nil {
		port = s.detail.Spec.Port
		host = s.detail.Host
	}
	if host == "" {
		return func() tea.Msg { return instancePlayersMsg{known: false} }
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		st, err := idle.Ping(ctx, host, port, 1500*time.Millisecond)
		if err != nil {
			return instancePlayersMsg{known: false}
		}
		return instancePlayersMsg{online: st.Players.Online, known: true}
	}
}

func (s *instanceScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case homeDetailMsg:
		if msg.slug != "" && msg.slug != s.slug {
			return s, nil // respuesta obsoleta de otra instancia
		}
		s.detail = msg.d
		if msg.err != nil {
			s.status = msg.err.Error()
			return s, nil
		}
		s.status = ""
		return s, s.applyState(s.detailState())
	case pollRowsMsg:
		if msg.err != nil {
			return s, nil
		}
		for _, r := range msg.rows {
			if r.Slug != s.slug {
				continue
			}
			if s.detail != nil {
				s.detail.State = r.State
			}
			s.noteMetrics(r)
			return s, s.applyState(r.State)
		}
		return s, nil
	case instanceLogsMsg:
		// Respuesta obsoleta: de otra instancia o de una vista anterior de la consola.
		// No toca el buffer ni da por resuelta la causa de una caída.
		if msg.slug != s.slug || msg.gen != s.logsGen {
			return s, nil
		}
		if msg.err != nil {
			// Error recuperable (p. ej. contenedor recién recreado): se conserva el
			// buffer visible y se avisa dentro del panel, en vez de vaciar la consola.
			s.logsErr = msg.err.Error()
			if s.crashPending {
				s.crashCause = "no se pudieron leer los logs; revisa la pestaña Consola"
				s.crashPending = false
			}
			return s, nil
		}
		s.logsErr = ""
		s.logs = trimLogs(msg.text, maxLogLines)
		if s.crashPending {
			s.crashCause = crashCause(s.logs)
			s.crashPending = false
		}
	case instanceDeletedMsg:
		return s, tea.Batch(
			func() tea.Msg {
				return toastMsg{Text: "✓ Instancia «" + msg.slug + "» eliminada"}
			},
			func() tea.Msg { return popScreenMsg{} },
		)
	case instanceBackupsMsg:
		// La primera respuesta (aunque venga vacía o con error) cierra el estado
		// «cargando»: así se distingue el vacío real de la carga en curso (§8).
		s.backupsLoaded = true
		// Una lista recién leída cierra el aviso de operación en curso: si la
		// operación terminó mientras la pestaña no estaba visible, el panel no
		// se queda mostrando un progreso que ya no existe.
		s.bkBusy = ""
		if msg.err != nil {
			s.bkErr = msg.err.Error()
		} else {
			s.bkErr = ""
			s.backups = msg.items
			if s.bkCursor >= len(s.backups) {
				s.bkCursor = 0
			}
		}
	case instancePortMsg:
		if msg.err != nil {
			s.portErr = msg.err.Error()
			return s, nil
		}
		s.portErr = ""
		r := msg.res
		s.portRes = &r
	case instanceIdleMsg:
		if msg.err == nil {
			s.idleCfg, s.idleLoaded = msg.cfg, true
		}
	case instanceIdleSetMsg:
		if msg.err != nil {
			return s, func() tea.Msg {
				return toastMsg{Text: "✖ auto-apagado: " + msg.err.Error(), Kind: components.Destructive}
			}
		}
		s.idleCfg, s.idleLoaded = msg.cfg, true
		return s, func() tea.Msg { return toastMsg{Text: "✓ Auto-apagado actualizado"} }
	case idleEventMsg:
		if msg.ev.Instance != s.slug {
			return s, nil
		}
		switch msg.ev.Kind {
		case events.IdleCountdownStarted:
			secs := 0
			if v, ok := msg.ev.Data["remaining_seconds"].(int); ok {
				secs = v
			}
			s.idleNote = "⏳ apagado en " + humanSeconds(secs)
		case events.IdleCountdownCancelled, events.IdleStopped:
			s.idleNote = ""
		}
		return s, nil
	case instancePlayersMsg:
		s.playersOnline, s.playersKnown = msg.online, msg.known
	case accessMsg:
		return s.handleAccessMsg(msg)
	case instanceSavedMsg:
		// Resultado de un guardado anterior que ya no está en vuelo: se descarta
		// para no revertir un cambio más reciente.
		if msg.spec != nil && s.pendingSave != msg.spec {
			return s, nil
		}
		s.pendingSave = nil
		if msg.err != nil {
			// El error se queda visible en el panel de Ajustes y no solo en un
			// aviso fugaz (§8). El Spec persistido conserva su valor anterior.
			text := "✖ no se pudo guardar: " + msg.err.Error()
			s.status = text
			return s, func() tea.Msg { return toastMsg{Text: text, Kind: components.Destructive} }
		}
		// Confirmado el guardado, se adopta la copia ya escrita.
		if msg.spec != nil && s.detail != nil {
			s.detail.Spec = msg.spec
		}
		s.status = "✓ guardado"
		return s, func() tea.Msg { return toastMsg{Text: "✓ Cambios guardados"} }
	case instanceOpMsg:
		// La operación en curso terminó: se limpia el aviso del panel (el toast lo
		// publica el App, que ve este mensaje aunque la pantalla cambie).
		s.bkBusy = ""
		return s, nil
	case tea.KeyMsg:
		return s.handleKey(msg)
	}
	return s, nil
}

func (s *instanceScreen) handleKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	if s.prompt {
		if msg.String() == "enter" {
			val := strings.TrimSpace(s.input.Value())
			act := s.promptAct
			s.prompt = false
			s.input.SetValue("")
			if val == "" {
				return s, nil
			}
			return s, s.runPrompt(act, val)
		}
		if msg.String() == "esc" {
			s.prompt = false
			s.input.Blur()
			return s, nil
		}
		var c tea.Cmd
		s.input, c = s.input.Update(msg)
		return s, c
	}
	if s.confirmKind != "" {
		switch msg.String() {
		case "enter":
			kind, val := s.confirmKind, s.confirmValue
			s.confirmKind, s.confirmValue = "", ""
			if kind == "version" {
				return s, s.changeVersionCmd(val)
			}
		case "esc":
			s.confirmKind, s.confirmValue = "", ""
		}
		return s, nil
	}
	if s.focus {
		return s.handleConsoleKey(msg)
	}
	switch msg.String() {
	case "esc":
		return s, func() tea.Msg { return popScreenMsg{} }
	case "D":
		return s, s.startDeletePrompt()
	case "v":
		return s, s.startVersionPrompt()
	case "tab":
		return s.switchTab(1)
	case "shift+tab":
		return s.switchTab(-1)
	case "right", "l":
		// En Ajustes, → cicla el valor del campo enfocado: el control de la
		// pestaña tiene prioridad sobre el atajo de pantalla (§5).
		if s.tab != 8 {
			return s.switchTab(1)
		}
	case "left", "h":
		if s.tab != 8 {
			return s.switchTab(-1)
		}
	case "s":
		return s, s.lifecycle("start")
	case "p":
		if s.tab == 1 {
			s.paused = !s.paused
			return s, nil
		}
		return s, s.lifecycle("stop")
	case "r":
		return s, s.lifecycle("restart")
	case "e":
		if s.tab == 1 {
			s.focus = true
			return s, s.input.Focus()
		}
	case "f":
		if s.tab == 1 {
			s.follow = !s.follow
			return s, nil
		}
	case "/":
		if s.tab == 1 {
			s.prompt = true
			s.promptAct = "__search__"
			s.promptLabel = "Buscar en los logs"
			s.input.Placeholder = "texto"
			return s, s.input.Focus()
		}
	}
	// Teclas específicas de la pestaña activa.
	switch s.tab {
	case 2:
		return s.playersKey(msg)
	case 3:
		return s.backupsKey(msg)
	case 6:
		return s.netKey(msg)
	case 7:
		return s.idleKey(msg)
	case 8:
		return s.settingsKey(msg)
	}
	return s, nil
}

func (s *instanceScreen) switchTab(d int) (Screen, tea.Cmd) {
	prev := s.tab
	s.tab = (s.tab + len(instanceTabs) + d) % len(instanceTabs)
	// Al abandonar la pestaña Consola se invalida el buffer en vuelo: la respuesta
	// que llegue más tarde se descarta por generación.
	if prev == 1 && s.tab != 1 {
		s.logsGen++
	}
	switch s.tab {
	case 1:
		return s, s.logsCmd()
	case 2:
		return s, s.accessCmd()
	case 3:
		return s, s.backupsCmd()
	case 6:
		return s, s.portCmd()
	}
	return s, nil
}

// handleConsoleKey gestiona la línea de consola (RF-1/P-06).
func (s *instanceScreen) handleConsoleKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	switch msg.String() {
	case "enter":
		cmd := strings.TrimSpace(s.input.Value())
		s.input.SetValue("")
		if cmd == "" {
			s.focus = false
			s.input.Blur()
			return s, nil
		}
		if s.isDangerous(cmd) && s.pending != cmd {
			s.pending = cmd
			s.input.SetValue(cmd)
			s.input.Placeholder = ""
			return s, nil
		}
		s.pending = ""
		s.history = append(s.history, cmd)
		s.histIdx = len(s.history)
		return s, s.consoleCmd(cmd)
	case "esc":
		s.focus = false
		s.pending = ""
		s.input.Blur()
		return s, nil
	case "tab":
		s.autocomplete()
		return s, nil
	case "up":
		if len(s.history) > 0 {
			s.histIdx--
			if s.histIdx < 0 {
				s.histIdx = 0
			}
			s.input.SetValue(s.history[s.histIdx])
		}
		return s, nil
	case "down":
		if len(s.history) > 0 {
			s.histIdx++
			if s.histIdx >= len(s.history) {
				s.histIdx = len(s.history)
				s.input.SetValue("")
			} else {
				s.input.SetValue(s.history[s.histIdx])
			}
		}
		return s, nil
	}
	var c tea.Cmd
	s.input, c = s.input.Update(msg)
	return s, c
}

func (s *instanceScreen) autocomplete() {
	cur := s.input.Value()
	if strings.TrimSpace(cur) == "" {
		s.input.SetValue(consoleCommands[0])
		return
	}
	for _, c := range consoleCommands {
		if strings.HasPrefix(c, cur) && c != cur {
			s.input.SetValue(c)
			s.input.CursorEnd()
			return
		}
	}
}

func (s *instanceScreen) isDangerous(cmd string) bool {
	lc := strings.ToLower(strings.TrimSpace(cmd))
	for _, p := range dangerousPrefixes {
		if lc == strings.TrimSpace(p) || strings.HasPrefix(lc, p) {
			return true
		}
	}
	return false
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

func (s *instanceScreen) runPrompt(act, val string) tea.Cmd {
	switch act {
	case "__search__":
		s.filter = val
		return nil
	case "__delete__":
		if val != s.slug {
			return func() tea.Msg {
				return toastMsg{Text: "✖ El nombre no coincide; no se borró nada", Kind: components.Destructive}
			}
		}
		return s.deleteCmd()
	case "__version__":
		s.confirmKind, s.confirmValue = "version", val
		return nil
	case "acl.add", "acl.remove", "acl.op", "acl.deop":
		return s.accessMutateCmd(act, val)
	}
	return s.consoleCmd(act + " " + val)
}

// deleteCmd borra el contenedor y los datos de la instancia (P-03).
func (s *instanceScreen) deleteCmd() tea.Cmd {
	ref, slug := s.svc, s.slug
	return tea.Batch(busyCmd(true), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		if err := ref.Lifecycle.Delete(ctx, slug); err != nil {
			return toastMsg{Text: "✖ No se pudo borrar «" + slug + "»: " + err.Error(), Kind: components.Destructive}
		}
		return instanceDeletedMsg{slug: slug}
	})
}

// changeVersionCmd hace un backup previo obligatorio y cambia la versión (P-03).
func (s *instanceScreen) changeVersionCmd(v string) tea.Cmd {
	if s.detail == nil || s.detail.Spec == nil || strings.TrimSpace(v) == "" {
		return nil
	}
	ref, spec := s.svc, s.detail.Spec
	return tea.Batch(busyCmd(true), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()
		if err := ref.Backups.Create(ctx, spec.Slug); err != nil {
			return toastMsg{Text: "✖ Backup previo falló: " + err.Error(), Kind: components.Destructive}
		}
		spec.Version = strings.TrimSpace(v)
		if err := ref.Instances.Save(ctx, spec); err != nil {
			return toastMsg{Text: "✖ No se pudo cambiar la versión: " + err.Error(), Kind: components.Destructive}
		}
		return toastMsg{Text: "✓ «" + spec.Slug + "»: backup hecho · versión " + spec.Version + " (reinicia para aplicar)"}
	})
}

// startDeletePrompt pide escribir el nombre para habilitar el borrado (P-03).
func (s *instanceScreen) startDeletePrompt() tea.Cmd {
	s.prompt = true
	s.promptAct = "__delete__"
	s.promptLabel = "Escribe «" + s.slug + "» para borrar la instancia"
	s.input.Placeholder = s.slug
	s.input.SetValue("")
	return s.input.Focus()
}

// startVersionPrompt pide la nueva versión (con backup y confirmación, P-03).
func (s *instanceScreen) startVersionPrompt() tea.Cmd {
	s.prompt = true
	s.promptAct = "__version__"
	s.promptLabel = "Nueva versión de Minecraft (se hará un backup antes)"
	s.input.Placeholder = "1.21.4"
	s.input.SetValue("")
	return s.input.Focus()
}

// --- P-08: recuperación ante caídas ---

// detailState devuelve el estado actual del detalle (vacío si no se cargó).
func (s *instanceScreen) detailState() string {
	if s.detail == nil {
		return ""
	}
	return s.detail.State
}

// applyState detecta un apagado inesperado (running → exited sin acción del
// usuario), lleva el historial de caídas y pide cargar los logs para la causa.
func (s *instanceScreen) applyState(state string) tea.Cmd {
	if state == "running" {
		s.crash, s.crashLoop, s.userStopped = false, false, false
	}
	crashDetected := s.lastState == "running" && state == "exited" && !s.userStopped
	if crashDetected {
		s.noteCrash()
	}
	s.lastState = state
	// Gate de I/O: fuera de la pestaña Consola no se piden logs (tampoco en View).
	// Excepción única: una caída recién detectada, que carga los logs una vez para
	// deducir la causa aunque el usuario no esté en la consola.
	if crashDetected || s.tab == 1 {
		return s.logsCmd()
	}
	return nil
}

// noteCrash registra una caída y activa la política de crash-loop (3 en 10 min).
func (s *instanceScreen) noteCrash() {
	s.crash = true
	s.crashPending = true
	now := time.Now()
	s.crashHistory = append(s.crashHistory, now)
	keep := s.crashHistory[:0]
	for _, t := range s.crashHistory {
		if now.Sub(t) <= 10*time.Minute {
			keep = append(keep, t)
		}
	}
	s.crashHistory = keep
	if len(s.crashHistory) >= 3 {
		s.crashLoop = true
	}
}

// crashCause deduce la causa probable de una caída a partir de los logs.
func crashCause(logs string) string {
	lc := strings.ToLower(logs)
	switch {
	case strings.Contains(lc, "outofmemoryerror"):
		return "falta memoria (OutOfMemoryError): sube la RAM o baja la distancia de render"
	case strings.Contains(lc, "address already in use") || strings.Contains(lc, "already allocated") || strings.Contains(lc, "failed to bind"):
		return "el puerto ya está en uso por otro proceso"
	case strings.Contains(lc, "unsupportedclassversionerror") || strings.Contains(lc, "class file version") || strings.Contains(lc, "requires java"):
		return "versión de Java incorrecta para esta versión de Minecraft"
	case strings.Contains(lc, "eula"):
		return "EULA no aceptado por la imagen"
	}
	return "causa desconocida; revisa la pestaña Consola"
}

func (s *instanceScreen) lifecycle(op string) tea.Cmd {
	ref, slug := s.svc, s.slug
	switch op {
	case "start":
		s.userStopped, s.crash, s.crashLoop = false, false, false
	case "stop", "close":
		s.userStopped = true
	}
	return tea.Batch(busyCmd(true), func() tea.Msg {
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
	})
}

// --- Jugadores ---

func (s *instanceScreen) playersKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	switch msg.String() {
	case "a":
		return s, s.startAccessPrompt("acl.add", "Nombre del jugador para la lista blanca", "nombre")
	case "d":
		return s, s.startAccessPrompt("acl.remove", "Nombre del jugador a quitar de la lista blanca", "nombre")
	case "o":
		return s, s.startAccessPrompt("acl.op", "Nombre del jugador a hacer operador", "nombre")
	case "O":
		return s, s.startAccessPrompt("acl.deop", "Nombre del jugador a quitar como operador", "nombre")
	case "w":
		if !s.accessLoaded {
			return s, nil
		}
		return s, s.setWhitelistCmd(!s.access.WhitelistEnabled)
	case "R":
		return s, s.accessCmd()
	}
	return s, nil
}

// startAccessPrompt abre el formulario para una operación de acceso.
func (s *instanceScreen) startAccessPrompt(act, label, placeholder string) tea.Cmd {
	s.prompt = true
	s.promptAct = act
	s.promptLabel = label
	s.input.Placeholder = placeholder
	s.input.SetValue("")
	return s.input.Focus()
}

// accessCmd lee el estado de acceso (whitelist/operadores) con su fuente.
func (s *instanceScreen) accessCmd() tea.Cmd {
	ref, slug := s.svc, s.slug
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		st, err := ref.Access.Read(ctx, slug)
		return accessMsg{slug: slug, state: st, err: err}
	}
}

// accessMutateCmd valida localmente y ejecuta una mutación con read-back. Si el
// nombre no es válido no se emite ningún comando al servidor (ACL-10).
func (s *instanceScreen) accessMutateCmd(act, raw string) tea.Cmd {
	name, err := svc.ValidatePlayerName(raw)
	if err != nil {
		return func() tea.Msg {
			return toastMsg{Text: "✖ " + err.Error(), Kind: components.Destructive}
		}
	}
	s.accessBusy = true
	s.accessStatus = ""
	ref, slug := s.svc, s.slug
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		var (
			st   svc.AccessState
			merr error
		)
		switch act {
		case "acl.add":
			st, merr = ref.Access.AddWhitelist(ctx, slug, name)
		case "acl.remove":
			st, merr = ref.Access.RemoveWhitelist(ctx, slug, name)
		case "acl.op":
			st, merr = ref.Access.AddOperator(ctx, slug, name)
		case "acl.deop":
			st, merr = ref.Access.RemoveOperator(ctx, slug, name)
		}
		return accessMsg{slug: slug, state: st, err: merr, op: act, player: name}
	}
}

// setWhitelistCmd activa o desactiva la whitelist con read-back.
func (s *instanceScreen) setWhitelistCmd(enabled bool) tea.Cmd {
	s.accessBusy = true
	s.accessStatus = ""
	ref, slug := s.svc, s.slug
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		st, err := ref.Access.SetWhitelistEnabled(ctx, slug, enabled)
		return accessMsg{slug: slug, state: st, err: err, op: "acl.toggle"}
	}
}

// handleAccessMsg aplica el resultado de una lectura o mutación de acceso.
func (s *instanceScreen) handleAccessMsg(msg accessMsg) (Screen, tea.Cmd) {
	if msg.slug != s.slug {
		return s, nil // respuesta obsoleta de otra instancia o pestaña
	}
	s.accessBusy = false
	if msg.err != nil {
		s.access = msg.state
		s.accessLoaded = true
		s.accessStatus = "✖ Error al consultar el acceso: " + msg.err.Error()
		return s, nil
	}
	s.access = msg.state
	s.accessLoaded = true
	if msg.op == "" {
		// Una lectura solo "verifica" si hubo estado en vivo. Con configuración
		// guardada o consulta no disponible no se afirma una verificación que no
		// ocurrió: el panel y la barra de estado ya explican la fuente (§6/§8).
		if msg.state.Source == svc.AccessLive {
			s.accessStatus = "✓ Estado de acceso verificado"
		} else {
			s.accessStatus = ""
		}
		return s, nil
	}
	switch msg.state.Source {
	case svc.AccessLive:
		s.accessStatus = "✓ Verificado: " + accessOpLabel(msg.op, msg.player)
	case svc.AccessSavedConfig:
		s.accessStatus = "⚠ Comando enviado · resultado pendiente de verificar (sin lectura en vivo)"
	default:
		s.accessStatus = "⚠ No se pudo verificar el resultado"
	}
	return s, nil
}

// accessOpLabel redacta la acción de acceso aplicada.
func accessOpLabel(op, player string) string {
	switch op {
	case "acl.add":
		return "«" + player + "» añadido a la lista blanca"
	case "acl.remove":
		return "«" + player + "» quitado de la lista blanca"
	case "acl.op":
		return "«" + player + "» ahora es operador"
	case "acl.deop":
		return "«" + player + "» ya no es operador"
	case "acl.toggle":
		return "lista blanca actualizada"
	}
	return "acceso actualizado"
}

// --- Backups ---

func (s *instanceScreen) backupsKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if s.bkCursor < len(s.backups)-1 {
			s.bkCursor++
		}
	case "k", "up":
		if s.bkCursor > 0 {
			s.bkCursor--
		}
	case "b":
		return s, s.createBackup()
	case "x":
		return s, s.restoreBackup()
	case "X":
		return s, s.pruneBackups()
	}
	return s, nil
}

// createBackup lanza una copia en segundo plano. El aviso de operación en curso
// vive en el panel (bkBusy) y el resultado llega por instanceOpMsg.
func (s *instanceScreen) createBackup() tea.Cmd {
	s.bkBusy = "creando backup… (puede tardar unos minutos)"
	ref, slug := s.svc, s.slug
	return tea.Batch(busyCmd(true), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		return instanceOpMsg{what: "backup", slug: slug, err: ref.Backups.Create(ctx, slug)}
	})
}

// restoreBackup restaura el backup seleccionado (el servicio hace antes una
// copia de seguridad del estado actual).
func (s *instanceScreen) restoreBackup() tea.Cmd {
	if len(s.backups) == 0 || s.bkCursor >= len(s.backups) {
		return nil
	}
	name := s.backups[s.bkCursor].Name
	s.bkBusy = "restaurando «" + name + "»… (se crea un backup previo)"
	ref, slug := s.svc, s.slug
	return tea.Batch(busyCmd(true), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()
		return instanceOpMsg{what: "restaurar", slug: slug, detail: name, err: ref.Backups.Restore(ctx, slug, name)}
	})
}

func (s *instanceScreen) pruneBackups() tea.Cmd {
	s.bkBusy = "podando backups antiguos…"
	ref, slug := s.svc, s.slug
	return tea.Batch(busyCmd(true), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		removed, err := ref.Backups.Prune(ctx, slug, 5)
		if err != nil {
			return instanceOpMsg{what: "podar", slug: slug, err: err}
		}
		return instanceOpMsg{what: "podar", slug: slug, detail: itoa(len(removed))}
	})
}

// --- Auto-apagado ---

func (s *instanceScreen) idleKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if s.idleCursor < len(idlePresets) {
			s.idleCursor++
		}
	case "k", "up":
		if s.idleCursor > 0 {
			s.idleCursor--
		}
	case "enter":
		cfg := s.idleCfg
		if s.idleCursor >= len(idlePresets) {
			cfg.Enabled = false
		} else if p := idlePresets[s.idleCursor]; p.Timeout <= 0 {
			// El preset «Nunca» equivale a desactivar.
			cfg.Enabled = false
		} else {
			cfg.Enabled = true
			cfg.Timeout = p.Timeout
		}
		return s, s.idleSetCmd(cfg.Normalized())
	case "b":
		cfg := s.idleCfg
		cfg.BackupBeforeStop = !cfg.BackupBeforeStop
		return s, s.idleSetCmd(cfg.Normalized())
	case "t":
		return s, s.playersCmd()
	}
	return s, nil
}

// idleSetCmd guarda la configuración de auto-apagado: el resultado llega por
// instanceIdleSetMsg y la pantalla solo adopta el cambio confirmado (§8).
func (s *instanceScreen) idleSetCmd(cfg idle.Config) tea.Cmd {
	ref, slug := s.svc, s.slug
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return instanceIdleSetMsg{cfg: cfg, err: ref.Idle.Set(ctx, slug, cfg)}
	}
}

// --- Ajustes ---

type settingField struct {
	key      string
	label    string
	value    string
	kind     string // text | cycle
	options  []string
	requires bool
}

// settingsSpec es el Spec visible en Ajustes: la copia preparada mientras hay un
// cambio sin confirmar (pendiente o en vuelo) o, si no, el Spec ya persistido.
// Así el valor mostrado sigue al usuario sin mutar lo guardado (§5.11).
func (s *instanceScreen) settingsSpec() *instance.Spec {
	if s.pendingSave != nil {
		return s.pendingSave
	}
	if s.detail == nil {
		return nil
	}
	return s.detail.Spec
}

func (s *instanceScreen) settingFields() []settingField {
	sp := s.settingsSpec()
	if sp == nil {
		return nil
	}
	return []settingField{
		{key: "max_players", label: "Jugadores máximos", value: itoa(sp.MaxPlayers), kind: "text"},
		{key: "difficulty", label: "Dificultad", value: sp.Difficulty, kind: "cycle", options: []string{"peaceful", "easy", "normal", "hard"}, requires: true},
		{key: "gamemode", label: "Modo de juego", value: sp.Gamemode, kind: "cycle", options: []string{"survival", "creative", "adventure", "spectator"}, requires: true},
		{key: "memory", label: "Memoria", value: sp.Memory, kind: "text", requires: true},
		{key: "motd", label: "Mensaje (MOTD)", value: sp.Motd, kind: "text"},
	}
}

func (s *instanceScreen) settingsKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	fields := s.settingFields()
	if s.editing {
		if msg.String() == "enter" {
			s.commitSetting(strings.TrimSpace(s.input.Value()))
			return s, s.saveCmd()
		}
		if msg.String() == "esc" {
			s.editing = false
			s.input.Blur()
			return s, nil
		}
		var c tea.Cmd
		s.input, c = s.input.Update(msg)
		return s, c
	}
	switch msg.String() {
	case "j", "down":
		if s.setCursor < len(fields)-1 {
			s.setCursor++
		}
	case "k", "up":
		if s.setCursor > 0 {
			s.setCursor--
		}
	case "enter", "right":
		if s.setCursor < len(fields) {
			f := fields[s.setCursor]
			if f.kind == "cycle" {
				s.cycleSetting(f)
				return s, s.saveCmd()
			}
			s.editing = true
			s.editKey = f.key
			s.input.SetValue(f.value)
			return s, s.input.Focus()
		}
	case "left":
		if s.setCursor < len(fields) && fields[s.setCursor].kind == "cycle" {
			s.cycleSettingBack(fields[s.setCursor])
			return s, s.saveCmd()
		}
	}
	return s, nil
}

func (s *instanceScreen) cycleSetting(f settingField) {
	idx := 0
	for i, o := range f.options {
		if o == f.value {
			idx = i
			break
		}
	}
	idx = (idx + 1) % len(f.options)
	s.applySetting(f.key, f.options[idx])
}

func (s *instanceScreen) cycleSettingBack(f settingField) {
	idx := 0
	for i, o := range f.options {
		if o == f.value {
			idx = i
			break
		}
	}
	idx = (idx + len(f.options) - 1) % len(f.options)
	s.applySetting(f.key, f.options[idx])
}

func (s *instanceScreen) commitSetting(v string) {
	s.editing = false
	s.input.Blur()
	s.applySetting(s.editKey, v)
}

// applySetting prepara un cambio sobre una COPIA del Spec: el Spec persistido no
// se muta hasta que el guardado se confirme (§6/§5.11). La copia viaja en
// pendingSave y solo se adopta al recibir instanceSavedMsg sin error. La copia es
// superficial y basta porque Ajustes solo edita campos escalares (las listas
// compartidas no se tocan aquí).
func (s *instanceScreen) applySetting(key, val string) {
	base := s.settingsSpec()
	if base == nil {
		return
	}
	next := *base
	switch key {
	case "max_players":
		if n, ok := intValue(val); ok {
			next.MaxPlayers = n
		}
	case "difficulty":
		next.Difficulty = val
	case "gamemode":
		next.Gamemode = val
	case "memory":
		next.Memory = val
	case "motd":
		next.Motd = val
	}
	s.status = "guardando…"
	s.setCursor = 0
	s.pendingSave = &next
}

// saveCmd lanza el guardado del Spec preparado. pendingSave se conserva hasta que
// llega el resultado: identifica la escritura en vuelo y permite descartar
// respuestas de un guardado anterior.
func (s *instanceScreen) saveCmd() tea.Cmd {
	if s.pendingSave == nil {
		return nil
	}
	spec := s.pendingSave
	ref := s.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return instanceSavedMsg{spec: spec, err: ref.Instances.Save(ctx, spec)}
	}
}

// View pinta el contenido de la pestaña activa. El título y la fila de pestañas
// los dibuja Chrome y el App los reserva como franja fija (§4).
func (s *instanceScreen) View(w, h int) string {
	// El ancho del campo sigue al terminal (nada de anchos fijos desconectados).
	if w > 4 {
		s.input.Width = w - 2
	}
	var b strings.Builder
	switch s.tab {
	case 0:
		b.WriteString(s.summaryView(w, h-s.reservedBodyLines()))
	case 1:
		b.WriteString(s.consoleView(w, h))
	case 2:
		b.WriteString(s.playersView(w, h-s.reservedBodyLines()))
	case 3:
		b.WriteString(s.backupsView(w, h-s.reservedBodyLines()))
	case 4:
		b.WriteString(s.worldsView(w, h-s.reservedBodyLines()))
	case 5:
		b.WriteString(s.pluginsView(w, h-s.reservedBodyLines()))
	case 6:
		b.WriteString(s.netView(w, h-s.reservedBodyLines()))
	case 7:
		b.WriteString(s.idleView(w, h-s.reservedBodyLines()))
	case 8:
		b.WriteString(s.settingsView(w, h-s.reservedBodyLines()))
	}
	if s.prompt {
		b.WriteString("\n\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Accent)).Render(s.promptLabel+": ") + s.input.View())
	}
	if s.pending != "" && s.tab != 1 { // en la pestaña Consola el aviso va dentro del panel
		b.WriteString("\n\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Warn)).Render("⚠ Comando peligroso: «"+s.pending+"». Pulsa Enter otra vez para confirmar, o Esc para cancelar."))
	}
	if s.confirmKind == "version" {
		b.WriteString("\n\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Warn)).Render(
			"⚠ Se hará un backup y se cambiará la versión a «"+s.confirmValue+"». Bajar de versión puede corromper el mundo. [Enter] Confirmar · [Esc] Cancelar"))
	}
	return b.String()
}

// Chrome devuelve el nombre de la instancia y la fila de pestañas. En terminales
// compactas muestra el nombre completo de la pestaña activa con un indicador
// «n/9» y navegación izquierda/derecha (§3), sin truncar todo hasta lo ilegible.
// En medio y ancho la pestaña activa también queda siempre completa: si no caben
// todas, se muestra una ventana a su alrededor con marcadores «…».
func (s *instanceScreen) Chrome(w int) string {
	name := s.slug
	if s.detail != nil && s.detail.Spec != nil {
		name = s.detail.Spec.Name
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(s.pal.Accent)).Render(name)
	if layout.ClassifyWidth(w) == layout.Compact {
		ind := " " + itoa(s.tab+1) + "/" + itoa(len(instanceTabs))
		label := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(s.pal.AccentText)).Background(lipgloss.Color(s.pal.Accent)).Render(" [" + instanceTabs[s.tab] + "] ")
		muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render(ind + "  ←/→")
		return layout.Truncate(title+" "+label+muted, w)
	}
	avail := w - layout.Width(title) - 2
	if avail < 8 {
		avail = 8
	}
	return title + "  " + s.tabsLine(avail)
}

// tabsLine pinta la fila de pestañas en `w` celdas garantizando que la activa se
// vea siempre completa (UI-02): si no caben todas, muestra una ventana a su
// alrededor con marcadores «…» en los extremos ocultos, en vez de recortar a
// ciegas y dejar la pestaña activa fuera de la vista.
func (s *instanceScreen) tabsLine(w int) string {
	n := len(instanceTabs)
	item := func(i int) string {
		style := lipgloss.NewStyle()
		label := " " + instanceTabs[i] + " "
		if i == s.tab {
			// La activa se marca con corchetes además del color: el foco debe
			// verse sin depender exclusivamente del color (§9 del rediseño).
			style = style.Bold(true).Foreground(lipgloss.Color(s.pal.AccentText)).Background(lipgloss.Color(s.pal.Accent))
			label = " [" + instanceTabs[i] + "] "
		} else {
			style = style.Foreground(lipgloss.Color(s.pal.Muted))
		}
		return style.Render(label)
	}
	widths := make([]int, n)
	for i := range widths {
		widths[i] = layout.Width(instanceTabs[i]) + 2
		if i == s.tab {
			widths[i] += 2 // corchetes de la pestaña activa
		}
	}
	lo, hi := s.tab, s.tab
	used := widths[s.tab]
	for {
		grew := false
		if hi+1 < n && used+1+widths[hi+1] <= w {
			used += 1 + widths[hi+1]
			hi++
			grew = true
		}
		if lo-1 >= 0 && used+1+widths[lo-1] <= w {
			used += 1 + widths[lo-1]
			lo--
			grew = true
		}
		if !grew {
			break
		}
	}
	leftMore, rightMore := lo > 0, hi < n-1
	for leftMore || rightMore {
		need := 0
		if leftMore {
			need += 2 // «…» más su separador
		}
		if rightMore {
			need += 2
		}
		if used+need <= w {
			break
		}
		// Se recorta el extremo más alejado de la pestaña activa.
		switch {
		case hi > lo && hi-s.tab >= s.tab-lo:
			used -= 1 + widths[hi]
			hi--
			rightMore = hi < n-1
		case hi > lo:
			used -= 1 + widths[lo]
			lo++
			leftMore = lo > 0
		default:
			// Solo queda la activa: se pinta igualmente y se recorta al final.
			leftMore, rightMore = false, false
		}
	}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted))
	parts := make([]string, 0, hi-lo+3)
	if leftMore {
		parts = append(parts, muted.Render("…"))
	}
	for i := lo; i <= hi; i++ {
		parts = append(parts, item(i))
	}
	if rightMore {
		parts = append(parts, muted.Render("…"))
	}
	return layout.Truncate(strings.Join(parts, " "), w)
}

// --- Resumen (§5.3) ---

// noteMetrics guarda las métricas del contenedor que llegan con el sondeo: el
// Resumen las muestra sin consultar nada en View (§5.3/§7). El histórico de
// memoria se acota a maxMemHist muestras.
func (s *instanceScreen) noteMetrics(r svc.Row) {
	if r.PlayersOnline >= 0 {
		s.playersOnline, s.playersKnown = r.PlayersOnline, true
	} else {
		s.playersKnown = false
	}
	s.memUsedMB, s.memLimitMB = r.MemUsedMB, r.MemLimitMB
	if r.State == "running" && r.MemUsedMB > 0 {
		s.memHist = append(s.memHist, r.MemUsedMB)
		if len(s.memHist) > maxMemHist {
			s.memHist = s.memHist[len(s.memHist)-maxMemHist:]
		}
	}
}

// summaryView dibuja el Resumen dentro de paneles. En medio y ancho reparte el
// espacio en dos paneles contiguos cuyos anchos suman exactamente w: RESUMEN
// (datos en dos columnas etiqueta/valor) y CÓMO CONECTARSE. En compacto usa un
// solo panel a ancho completo y deja las líneas de conexión debajo. Solo
// representa estado: las métricas llegan por pollRowsMsg y la conexión se deriva
// del Spec, así que aquí no se hace ninguna E/S.
func (s *instanceScreen) summaryView(w, h int) string {
	if s.detail == nil || s.detail.Spec == nil {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("cargando…")
	}
	if w < 2 {
		w = 2
	}
	if h < 2 {
		h = 2
	}
	spec := s.detail.Spec
	conn := connectionFromSpecWithHost(spec, s.detail.Host)
	if layout.ClassifyWidth(w) == layout.Compact {
		return s.compactSummary(w, h, spec, conn)
	}
	rightW := connectionPanelWidth(w)
	leftW := w - rightW
	left := components.Panel("RESUMEN", "", s.summaryBody(leftW-2, spec), leftW, h, false, s.pal, s.ascii)
	right := components.Panel("CÓMO CONECTARSE", "", s.connectionBody(rightW-2, conn), rightW, h, false, s.pal, s.ascii)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

// connectionPanelWidth reserva el ancho del panel de conexión: deja el resto al
// Resumen y nunca deja a ninguno de los dos sin espacio útil.
func connectionPanelWidth(w int) int {
	rw := w * 36 / 100
	if rw < 28 {
		rw = 28
	}
	if rw > 56 {
		rw = 56
	}
	if w-rw < 34 {
		rw = w - 34
	}
	if rw < 18 {
		rw = 18
	}
	return rw
}

// summaryBody compone el cuerpo del panel RESUMEN: el aviso de caída (si la hubo)
// y, debajo, las dos columnas de datos.
func (s *instanceScreen) summaryBody(inner int, spec *instance.Spec) string {
	if inner < 8 {
		inner = 8
	}
	var lines []string
	if warn := s.crashLines(inner); len(warn) > 0 {
		lines = append(lines, warn...)
		lines = append(lines, "")
	}
	lines = append(lines, twoColumns(s.summaryLeftRows(spec), s.summaryRightRows(spec), inner)...)
	return strings.Join(lines, "\n")
}

// compactSummary usa un solo panel a ancho completo, con los datos en una columna
// ordenados por importancia (el estado primero) y las líneas de conexión debajo
// cuando el alto deja sitio para ambas cosas.
func (s *instanceScreen) compactSummary(w, h int, spec *instance.Spec, conn connection.Info) string {
	inner := w - 2
	if inner < 8 {
		inner = 8
	}
	var body []string
	body = append(body, s.crashLines(inner)...)
	if len(body) > 0 {
		body = append(body, "")
	}
	rows := [][2]string{
		{"Estado", theme.Icon(stateKey(s.detail.State), s.ascii)},
		{"Motor", spec.Engine + " " + spec.Version},
		{"Puerto", itoa(spec.Port)},
		{"Jugadores", s.playersText(spec)},
		{"Memoria", orDash(spec.Memory)},
		{"Dificultad", s.difficultyText(spec)},
		{"Auto-apagado", s.idleSummaryText()},
	}
	if cpu := s.cpuRAMText(); cpu != "" {
		rows = append(rows, [2]string{"CPU/RAM", cpu})
	}
	body = append(body, columnLines(rows, inner, columnAligned(rows, inner))...)

	// El panel ocupa todo el alto salvo que, además, quepan dos líneas de conexión.
	panelH, connBudget := h, 0
	if h-(len(body)+2)-1 >= 2 {
		panelH = len(body) + 2
		connBudget = h - len(body) - 3
	}
	out := components.Panel("RESUMEN", "", strings.Join(body, "\n"), w, panelH, false, s.pal, s.ascii)
	// Las líneas de conexión se añaden por bloques completos: con poco alto se
	// omite un aviso entero antes que mostrar media frase.
	if connBudget > 0 {
		var cl []string
		for _, b := range s.connectionBlocks(inner, conn) {
			if len(cl)+len(b) > connBudget {
				break
			}
			cl = append(cl, b...)
		}
		if len(cl) > 0 {
			out += "\n" + strings.Join(cl, "\n")
		}
	}
	return out
}

// summaryLeftRows son las filas de la columna izquierda del Resumen.
func (s *instanceScreen) summaryLeftRows(spec *instance.Spec) [][2]string {
	return [][2]string{
		{"Motor", spec.Engine + " " + spec.Version},
		{"Memoria", orDash(spec.Memory)},
		{"Puerto", itoa(spec.Port)},
		{"Jugadores", s.playersText(spec)},
	}
}

// summaryRightRows son las filas de la columna derecha: dificultad/modo, estado,
// auto-apagado y CPU/RAM con la minigráfica del histórico.
func (s *instanceScreen) summaryRightRows(spec *instance.Spec) [][2]string {
	rows := [][2]string{
		{"Dificultad", s.difficultyText(spec)},
		{"Estado", theme.Icon(stateKey(s.detail.State), s.ascii)},
		{"Auto-apagado", s.idleSummaryText()},
	}
	if cpu := s.cpuRAMText(); cpu != "" {
		rows = append(rows, [2]string{"CPU/RAM", cpu})
	}
	return rows
}

// playersText resume los jugadores conectados: «3/20» o «—» si no se pudo medir.
func (s *instanceScreen) playersText(spec *instance.Spec) string {
	if !s.playersKnown {
		return "—"
	}
	return itoa(s.playersOnline) + "/" + itoa(spec.MaxPlayers)
}

// difficultyText resume dificultad y modo de juego.
func (s *instanceScreen) difficultyText(spec *instance.Spec) string {
	return orDash(spec.Difficulty) + " · " + orDash(spec.Gamemode)
}

// idleSummaryText resume el auto-apagado configurado (llega por instanceIdleMsg).
func (s *instanceScreen) idleSummaryText() string {
	if !s.idleLoaded {
		return "cargando…"
	}
	if !s.idleCfg.Enabled {
		return "no"
	}
	return "sí · " + idle.HumanDuration(s.idleCfg.Timeout)
}

// cpuRAMText resume el consumo de memoria y, si hay histórico y no estamos en
// modo ASCII, la minigráfica (§5.3). Vacío si el contenedor no corre: sin
// métricas no hay nada que mostrar.
func (s *instanceScreen) cpuRAMText() string {
	if s.detail == nil || s.detail.State != "running" {
		return ""
	}
	if s.memUsedMB <= 0 && s.memLimitMB <= 0 {
		return "(sin métricas)"
	}
	spark := ""
	if !s.ascii && len(s.memHist) > 0 {
		spark = components.Sparkline(s.memHist, 8) + " "
	}
	return spark + itoa(int(s.memUsedMB)) + "/" + itoa(int(s.memLimitMB)) + " MB"
}

// crashLines redacta el aviso de caída detectada (P-08): cabecera con el icono
// de aviso, causa probable y, si se cayó tres veces en diez minutos, el aviso de
// bucle. Se envuelve por palabras para que quepa en paneles estrechos.
func (s *instanceScreen) crashLines(w int) []string {
	if !s.crash {
		return nil
	}
	bad := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Bad))
	head := bad.Bold(true).Render(layout.Truncate(components.Warn(s.ascii)+" El servidor se cayó inesperadamente", w))
	out := []string{head}
	out = append(out, wrapWords("Causa probable: "+orDash(s.crashCause), w)...)
	if s.crashLoop {
		for _, l := range wrapWords("Se cayó 3 veces en 10 min: revisa la causa antes de reiniciar.", w) {
			out = append(out, bad.Render(l))
		}
	} else {
		out = append(out, "[R] Reiniciar")
	}
	return out
}

// connectionLines son las líneas de conexión sin el encabezado «Cómo conectarse»:
// en medio y ancho ese texto es el título del panel, así que no se repite dentro.
func connectionLines(conn connection.Info, pal theme.Palette, ascii bool) []string {
	lines := components.ConnectLines(conn, pal, ascii)
	if len(lines) > 0 {
		return lines[1:]
	}
	return nil
}

// connectionBody envuelve las líneas de conexión al ancho interior disponible.
func (s *instanceScreen) connectionBody(inner int, conn connection.Info) string {
	var out []string
	for _, b := range s.connectionBlocks(inner, conn) {
		out = append(out, b...)
	}
	return strings.Join(out, "\n")
}

// copyHint es la pista de copiado que añade components.ConnectLines.
const copyHint = "(c = copiar)"

// connectionBlocks ajusta las líneas de conexión al ancho disponible y las
// agrupa por bloques: la dirección con su pista de copiado y cada aviso (que
// puede ocupar varias líneas). Así el Resumen compacto puede omitir un bloque
// entero en vez de cortar una frase por la mitad.
func (s *instanceScreen) connectionBlocks(inner int, conn connection.Info) [][]string {
	if inner < 8 {
		inner = 8
	}
	lines := connectionLines(conn, s.pal, s.ascii)
	out := make([][]string, 0, len(lines))
	for _, ln := range lines {
		if body, ok := splitCopyHint(ln, inner); ok {
			out = append(out, []string{body, copyHint})
			continue
		}
		out = append(out, wrapLines([]string{ln}, inner))
	}
	return out
}

// splitCopyHint separa la pista de copiado cuando la línea no cabe completa.
func splitCopyHint(line string, w int) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if layout.Width(line) <= w || !strings.HasSuffix(trimmed, copyHint) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimSuffix(trimmed, copyHint)), true
}

// wrapLines envuelve por palabras solo las líneas que no caben en w: las que
// caben se conservan tal cual (con su espaciado y color originales).
func wrapLines(lines []string, w int) []string {
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if layout.Width(ln) <= w {
			out = append(out, ln)
			continue
		}
		out = append(out, wrapWords(ln, w)...)
	}
	return out
}

// twoColumns reparte dos columnas de pares etiqueta/valor en `inner` celdas. Los
// valores se alinean cuando hay sitio para las dos columnas; si no, cada par se
// separa con un solo espacio y se recorta con «…» (nunca se desborda).
func twoColumns(left, right [][2]string, inner int) []string {
	const gap = 2
	if inner < gap+2 {
		inner = gap + 2
	}
	avail := inner - gap
	colL := avail * 48 / 100
	colR := avail - colL
	aligned := columnAligned(left, colL) && columnAligned(right, colR)
	l := columnLines(left, colL, aligned)
	r := columnLines(right, colR, aligned)
	n := len(l)
	if len(r) > n {
		n = len(r)
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		ll, rr := "", ""
		if i < len(l) {
			ll = l[i]
		}
		if i < len(r) {
			rr = r[i]
		}
		out = append(out, strings.TrimRight(padLine(ll, colL)+strings.Repeat(" ", gap)+rr, " "))
	}
	return out
}

// columnAligned indica si la columna puede alinear sus valores (etiqueta con
// relleno) sin recortar el valor más largo.
func columnAligned(rows [][2]string, colW int) bool {
	pad, maxVal := 0, 0
	for _, r := range rows {
		if w := layout.Width(r[0]) + 2; w > pad {
			pad = w
		}
		if w := layout.Width(r[1]); w > maxVal {
			maxVal = w
		}
	}
	return pad+maxVal <= colW
}

// columnLines pinta una columna de pares «etiqueta: valor» en colW celdas.
func columnLines(rows [][2]string, colW int, aligned bool) []string {
	if colW < 1 {
		colW = 1
	}
	pad := 0
	for _, r := range rows {
		if w := layout.Width(r[0]) + 2; w > pad {
			pad = w
		}
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		label := r[0] + ":"
		line := label + " " + r[1]
		if aligned && pad > layout.Width(label) {
			line = label + strings.Repeat(" ", pad-layout.Width(label)) + r[1]
		}
		out = append(out, padLine(line, colW))
	}
	return out
}

// consoleView dibuja la Consola dentro de un panel (Fase C): el log vive en el
// cuerpo del panel (el único viewport) y la línea de entrada queda fuera del
// marco, siempre visible. Aquí no se hace I/O: los logs llegan por mensajes.
func (s *instanceScreen) consoleView(w, h int) string {
	// Reserva una línea para la entrada; si hay un comando peligroso pendiente se
	// muestra su aviso en el cuerpo y se le resta otra línea al log.
	logH := h - 1
	warn := ""
	if s.pending != "" {
		logH = h - 2
		warn = layout.Truncate(lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Warn)).Render(
			"⚠ Comando peligroso: «"+s.pending+"». Pulsa Enter otra vez para confirmar, o Esc para cancelar."), w)
	}
	if logH < 2 {
		logH = 2 // el marco del panel necesita al menos dos líneas
	}
	// El cuerpo del panel son logH-2 líneas: los bordes no dibujan cuerpo.
	max := logH - 2
	// Error recuperable: se conserva el buffer y se reserva la primera línea del
	// cuerpo para el aviso, que así no se pierde aunque el viewport siga la cola.
	errLine := ""
	if s.logsErr != "" {
		// Mensaje breve y accionable (§8): el volcado técnico del error no cabe ni
		// ayuda; la causa real (Docker caído, contenedor reiniciando) se ve en el
		// banner y en Salud (F2), y el refresco del sondeo reintenta solo.
		errLine = layout.Truncate(components.Cross(s.ascii)+" no se pudieron leer los logs · se reintentará al refrescar (F2 revisa el sistema)", w-2)
		max--
	}
	if max < 0 {
		max = 0
	}
	var disp []string
	if strings.TrimSpace(s.logs) != "" {
		disp = wrapWidth(strings.Join(s.filteredLogs(), "\n"), w-2)
		if len(disp) > max {
			if s.follow {
				disp = disp[len(disp)-max:]
			} else {
				disp = disp[:max]
			}
		}
	}
	var body strings.Builder
	written := 0
	if errLine != "" {
		body.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Bad)).Render(errLine))
		written++
	}
	if len(disp) == 0 {
		if written == 0 && max > 0 {
			body.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("(vacío)"))
		}
	} else {
		for _, l := range disp {
			if written > 0 {
				body.WriteString("\n")
			}
			body.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(logColor(l, s.pal))).Render(l))
			written++
		}
	}
	out := components.Panel("CONSOLA", s.consoleFlags(w), body.String(), w, logH, false, s.pal, s.ascii)
	if warn != "" {
		out += "\n" + warn
	}
	return out + "\n> " + s.input.View()
}

// consoleFlags compone el contador derecho del panel de consola con la misma
// regla que los demás paneles (§5.2): se elige la forma más larga que quepa
// junto al título («CONSOLA») y, si ninguna cabe, se omite antes que recortar el
// título. Nunca se envía al panel una forma que este vaya a descartar: así el
// estado de la consola no desaparece en anchos medios.
func (s *instanceScreen) consoleFlags(w int) string {
	long := "seguir=" + onOff(s.follow) + " pausa=" + onOff(s.paused) + " filtro=" + orDash(s.filter)
	short := "seg=" + onOff(s.follow) + " pau=" + onOff(s.paused) + " filtro=" + orDash(s.filter)
	mini := "seg=" + onOff(s.follow) + " pau=" + onOff(s.paused)
	need := 3 + layout.Width("CONSOLA") + 3
	for _, form := range []string{long, short, mini} {
		if need+layout.Width(form) <= w-2 {
			return form
		}
	}
	return ""
}

// wrapWidth envuelve el texto de log (sin ANSI) en líneas de ancho visual `w`,
// de modo que ninguna línea desborde ni desplace el footer (UI-04). Se recorre
// runa a runa con el ancho de celda incremental para ser O(n) (PERF-05).
func wrapWidth(text string, w int) []string {
	if w < 8 {
		w = 8
	}
	raw := strings.Split(stripANSI(text), "\n")
	out := make([]string, 0, len(raw))
	buf := make([]rune, 0, w)
	width := 0
	flush := func() {
		out = append(out, string(buf))
		buf = buf[:0]
		width = 0
	}
	for _, ln := range raw {
		for _, r := range ln {
			rw := runewidth.RuneWidth(r)
			if rw < 0 {
				rw = 0
			}
			if width > 0 && width+rw > w {
				flush()
			}
			buf = append(buf, r)
			width += rw
		}
		flush()
	}
	return out
}

// trimLogs recorta el texto de log a sus últimas max líneas (buffer acotado):
// así la pestaña Consola nunca crece sin límite aunque lleguen mensajes grandes.
func trimLogs(text string, max int) string {
	if max <= 0 {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= max {
		return text
	}
	return strings.Join(lines[len(lines)-max:], "\n")
}

func (s *instanceScreen) filteredLogs() []string {
	lines := strings.Split(s.logs, "\n")
	if s.filter == "" {
		return lines
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.Contains(strings.ToUpper(l), strings.ToUpper(s.filter)) {
			out = append(out, l)
		}
	}
	return out
}

// --- Jugadores y acceso (§5.5) ---

// playersView dibuja la pestaña «Jugadores y acceso» por regiones. En anchos
// medios y anchos reparte el contenido en dos paneles contiguos cuyos anchos
// suman exactamente w: ACCESO (estado de la lista blanca, jugadores en línea,
// permitidos y atajos) y OPERADORES. En compacto usa un solo panel a ancho
// completo con las dos secciones. Solo representa estado: aquí no se hace I/O
// ni se recalcula la lista; el read-back llega por accessMsg.
func (s *instanceScreen) playersView(w, h int) string {
	if s.detail == nil || s.detail.Spec == nil {
		return "cargando…"
	}
	if w < 2 {
		w = 2
	}
	if h < 4 {
		h = 4 // título, una línea de cuerpo y el borde inferior
	}
	if c := layout.ClassifyWidth(w); c == layout.Wide || c == layout.Normal {
		// La clase garantiza w ≥ 70, así que el 60 % deja los dos paneles por
		// encima del ancho útil mínimo (42 y 28 celdas).
		leftW := w * 60 / 100
		return lipgloss.JoinHorizontal(lipgloss.Top,
			s.accessPanel(leftW, h), s.operatorsPanel(w-leftW, h))
	}
	return s.compactAccessPanel(w, h)
}

// accessPanel compone el panel ACCESO de dos columnas. El pie de atajos se ancla
// al fondo del panel cuando hay sitio (§4); si la lista crece y no lo hay, pasa
// junto al estado para no perderse.
func (s *instanceScreen) accessPanel(w, h int) string {
	max := bodyLines(h)
	head := s.accessHead(w)
	list := s.accessListSection()
	body := append(append([]string{}, head...), "")
	body = append(body, list...)
	foot := s.shortcutLines(w, false)
	if len(body)+len(foot) <= max {
		if pad := max - len(body) - len(foot); pad > 0 {
			body = append(body, make([]string, pad)...)
		}
		body = append(body, foot...)
	} else {
		body = append(append(append([]string{}, head...), foot...), list...)
	}
	return components.Panel("ACCESO", s.accessSourceTag(), strings.Join(body, "\n"), w, h, false, s.pal, s.ascii)
}

// compactAccessPanel usa una sola columna con las secciones de permitidos y
// operadores. Si el alto no da, se sacrifica primero el pie de atajos y después
// la sección de operadores: el estado y los permitidos nunca desaparecen.
func (s *instanceScreen) compactAccessPanel(w, h int) string {
	max := bodyLines(h)
	body := append(s.accessHead(w), s.accessListSection()...)
	ops := s.accessOpsSection()
	foot := s.shortcutLines(w, true)
	switch {
	case len(body)+1+len(ops)+len(foot) <= max:
		body = append(body, "")
		body = append(body, ops...)
		body = append(body, foot...)
	case len(body)+1+len(ops) <= max:
		body = append(body, "")
		body = append(body, ops...)
	case len(body)+len(foot) <= max:
		body = append(body, foot...)
	}
	return components.Panel("ACCESO", s.accessSourceTag(), strings.Join(body, "\n"), w, h, false, s.pal, s.ascii)
}

// operatorsPanel lista los operadores en su propio panel.
func (s *instanceScreen) operatorsPanel(w, h int) string {
	ops := s.accessOperators()
	count := ""
	if len(ops) > 0 {
		count = itoa(len(ops))
	}
	return components.Panel("OPERADORES", count, strings.Join(s.playerLines(ops, "[O] Operador"), "\n"), w, h, false, s.pal, s.ascii)
}

// bodyLines es el alto útil del cuerpo de un panel (sin bordes).
func bodyLines(h int) int {
	if n := h - 2; n > 1 {
		return n
	}
	return 1
}

// accessHead es el bloque superior: estado de la lista blanca con su fuente,
// mensajes de la última operación y jugadores conectados ahora. El texto se
// envuelve por palabras para que ni el estado ni la fuente se recorten en
// paneles estrechos (UI-06).
func (s *instanceScreen) accessHead(w int) []string {
	out := s.accessStatusLines(w)
	if s.accessBusy {
		out = append(out, lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Warn)).Render("⟳ aplicando…"))
	}
	if s.accessStatus != "" {
		out = append(out, wrapWords(s.accessStatus, w-2)...)
	}
	if !s.accessLoaded {
		out = append(out, lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("verificando estado de acceso…"))
	}
	return append(out, s.onlineLine())
}

// accessListSection es la cabecera «Jugadores permitidos» y sus entradas.
func (s *instanceScreen) accessListSection() []string {
	out := []string{lipgloss.NewStyle().Bold(true).Render("Jugadores permitidos")}
	return append(out, s.playerLines(s.accessWhitelist(), "[A] Añadir")...)
}

// accessOpsSection es la cabecera «Operadores» y sus entradas.
func (s *instanceScreen) accessOpsSection() []string {
	out := []string{lipgloss.NewStyle().Bold(true).Render("Operadores")}
	return append(out, s.playerLines(s.accessOperators(), "[O] Operador")...)
}

// playerLines pinta una lista de nombres. Si está vacía, dice qué falta y con qué
// tecla se resuelve (§8: estado vacío con acción).
func (s *instanceScreen) playerLines(names []string, action string) []string {
	if len(names) == 0 {
		return []string{lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("(ninguno) · " + action)}
	}
	out := make([]string, 0, len(names))
	for _, p := range names {
		out = append(out, "  "+components.Pointer(s.ascii)+" "+p)
	}
	return out
}

// onlineLine informa de los jugadores conectados ahora mismo. El dato llega por
// mensaje (playersCmd): aquí no se consulta nada.
func (s *instanceScreen) onlineLine() string {
	if s.playersKnown {
		return "En línea: " + itoa(s.playersOnline) + " conectados ahora"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("En línea: no se pudo medir (¿servidor apagado?)")
}

// accessSourceTag etiqueta la fuente del dato en el borde del panel (§3): estado
// en vivo, configuración guardada o consulta no disponible.
func (s *instanceScreen) accessSourceTag() string {
	if !s.accessLoaded {
		return ""
	}
	switch s.access.Source {
	case svc.AccessLive:
		return "en vivo"
	case svc.AccessSavedConfig:
		return "guardado"
	case svc.AccessUnavailable:
		return "error"
	}
	return ""
}

// playersShortcutFull y playersShortcutBrief son la línea de atajos de la
// pestaña; la forma corta se usa en compacto.
const (
	playersShortcutFull  = "[A] Añadir · [D] Quitar · [W] Activar/desactivar · [O] Operador · [Shift+O] Quitar operador · [R] Verificar de nuevo"
	playersShortcutBrief = "[A] Añadir · [D] Quitar · [W] On/Off · [O] Operador · [Shift+O] Quitar operador · [R] Verificar"
)

// shortcutLines reparte la línea de atajos en líneas que quepan en w-2 celdas,
// sin partir una tecla por la mitad.
func (s *instanceScreen) shortcutLines(w int, compact bool) []string {
	text := playersShortcutFull
	if compact {
		text = playersShortcutBrief
	}
	packed := packTokens(text, w-2)
	out := make([]string, 0, len(packed))
	for _, l := range packed {
		out = append(out, lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render(l))
	}
	return out
}

// packTokens reparte un texto separado por « · » en líneas de ancho `w` como
// máximo, cortando siempre por el separador para no partir un atajo.
func packTokens(text string, w int) []string {
	if w < 8 {
		w = 8
	}
	var out []string
	cur := ""
	for _, part := range strings.Split(text, " · ") {
		switch {
		case cur == "":
			cur = part
		case layout.Width(cur)+3+layout.Width(part) <= w:
			cur += " · " + part
		default:
			out = append(out, cur)
			cur = part
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// wrapWords envuelve un texto por palabras en líneas de ancho `max` como máximo:
// así un estado largo no se recorta ni desplaza el panel.
func wrapWords(text string, max int) []string {
	if max < 8 {
		max = 8
	}
	var out []string
	cur := ""
	for _, word := range strings.Fields(text) {
		switch {
		case cur == "":
			cur = word
		case layout.Width(cur)+1+layout.Width(word) <= max:
			cur += " " + word
		default:
			out = append(out, cur)
			cur = word
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// reservedBodyLines cuenta las líneas que View añade DESPUÉS del cuerpo de la
// pestaña (prompt, aviso de comando peligroso, confirmación) para que un panel de
// alto completo no las deje fuera del cuerpo.
func (s *instanceScreen) reservedBodyLines() int {
	n := 0
	if s.prompt {
		n += 3 // dos de separación y la línea del formulario
	}
	if s.pending != "" && s.tab != 1 {
		n += 3
	}
	if s.confirmKind == "version" {
		n += 3
	}
	return n
}

// accessStatusLine resume el estado de la whitelist y su fuente en una línea.
func (s *instanceScreen) accessStatusLine() string {
	text, color := s.accessStatusText()
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(strings.ReplaceAll(text, "\n", " · "))
}

// accessStatusLines envuelve el estado por palabras y lo pinta del color del
// estado: en paneles estrechos ni «no verificable» ni «ERROR DE CONSULTA» se
// recortan.
func (s *instanceScreen) accessStatusLines(w int) []string {
	text, color := s.accessStatusText()
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	out := make([]string, 0, 2)
	// Cada segmento (separado por «\n») se envuelve por su cuenta: así «no
	// verificable» o «ERROR DE CONSULTA» nunca se parten por la mitad.
	for _, segment := range strings.Split(text, "\n") {
		for _, l := range wrapWords(segment, w-2) {
			out = append(out, style.Render(l))
		}
	}
	return out
}

// accessStatusText redacta el estado de la lista blanca y su fuente (§6) con
// icono y texto (nunca solo color): distingue estado en vivo verificado de
// configuración guardada no verificable, consulta fallida y estado sin verificar.
// El salto de línea separa fragmentos que deben conservarse juntos al envolver.
func (s *instanceScreen) accessStatusText() (string, string) {
	if !s.accessLoaded {
		return "Estado: " + components.Warn(s.ascii) + " NO VERIFICADA", s.pal.Muted
	}
	switch s.access.Source {
	case svc.AccessLive:
		icon, state, color := components.Cross(s.ascii), "DESACTIVADA", s.pal.Warn
		if s.access.WhitelistEnabled {
			icon, state, color = components.Check(s.ascii), "ACTIVADA", s.pal.Good
		}
		when := ""
		if !s.access.VerifiedAt.IsZero() {
			when = " · verificado " + humanAgo(s.access.VerifiedAt)
		}
		return "Whitelist: " + icon + " " + state + when, color
	case svc.AccessSavedConfig:
		saved := "desactivada"
		if s.access.WhitelistEnabled {
			saved = "activada"
		}
		return components.Warn(s.ascii) + " Configuración guardada: " + saved + "\nEstado en vivo: no verificable (servidor apagado)", s.pal.Warn
	case svc.AccessUnavailable:
		return components.Cross(s.ascii) + " Lista blanca: ERROR DE CONSULTA", s.pal.Bad
	}
	return "Estado: " + components.Warn(s.ascii) + " NO VERIFICADA", s.pal.Muted
}

// accessWhitelist devuelve la lista mostrada: el estado de acceso si se cargó y
// es fiable, o la configuración guardada como respaldo.
func (s *instanceScreen) accessWhitelist() []string {
	if s.accessLoaded && s.access.Source != svc.AccessUnavailable {
		return s.access.Whitelist
	}
	if s.detail != nil && s.detail.Spec != nil {
		return s.detail.Spec.Whitelist
	}
	return nil
}

func (s *instanceScreen) accessOperators() []string {
	if s.accessLoaded && s.access.Source != svc.AccessUnavailable {
		return s.access.Operators
	}
	if s.detail != nil && s.detail.Spec != nil {
		return s.detail.Spec.Ops
	}
	return nil
}

// humanAgo redacta el tiempo transcurrido desde t.
func humanAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Second:
		return "ahora"
	case d < time.Minute:
		return "hace " + itoa(int(d.Seconds())) + " s"
	case d < time.Hour:
		return "hace " + itoa(int(d.Minutes())) + " min"
	default:
		return "hace " + itoa(int(d.Hours())) + " h"
	}
}

// backupsShortcut es la línea de atajos de la pestaña Backups.
const backupsShortcut = "[B] Crear backup ahora · [X] Restaurar (crea backup previo) · [Shift+X] Podar antiguos"

// backupsView dibuja la pestaña Backups dentro de un panel (§5.6): contador a la
// derecha del título, filas con nombre, tamaño y checksum corto (la seleccionada
// con banda y puntero) y los atajos anclados al pie. Distingue cargando, vacío,
// operación en curso y error, y no hace ninguna E/S: la lista llega por
// instanceBackupsMsg.
func (s *instanceScreen) backupsView(w, h int) string {
	if w < 2 {
		w = 2
	}
	if h < 4 {
		h = 4
	}
	inner := w - 2
	max := bodyLines(h)
	head := s.backupsHead(inner)
	foot := s.backupsFoot(inner)
	blank := 0
	if len(head) > 0 {
		blank = 1
	}
	// La lista cede espacio primero: los avisos y los atajos nunca desaparecen.
	slot := max - len(head) - blank - len(foot)
	list := windowRows(s.backupsRows(inner), slot, s.bkCursor)
	body := append([]string{}, head...)
	if blank > 0 {
		body = append(body, "")
	}
	body = append(body, list...)
	if pad := max - len(body) - len(foot); pad > 0 {
		body = append(body, make([]string, pad)...)
	}
	body = append(body, foot...)
	return components.Panel("BACKUPS", s.backupsTag(), strings.Join(body, "\n"), w, h, false, s.pal, s.ascii)
}

// backupsHead son los avisos del panel: la operación en curso y el error de
// lectura de la lista, con texto e icono (nunca solo color, §8).
func (s *instanceScreen) backupsHead(inner int) []string {
	var out []string
	if s.bkBusy != "" {
		out = append(out, lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Warn)).
			Render(layout.Truncate("⟳ "+s.bkBusy, inner)))
	}
	if s.bkErr != "" {
		out = append(out, lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Bad)).
			Render(layout.Truncate(components.Cross(s.ascii)+" no se pudieron leer los backups: "+s.bkErr, inner)))
	}
	return out
}

// backupsRows pinta las filas de la lista o su estado vacío/cargando.
func (s *instanceScreen) backupsRows(inner int) []string {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted))
	switch {
	case !s.backupsLoaded:
		return []string{muted.Render("cargando…")}
	case len(s.backups) == 0:
		// Vacío con la acción que lo resuelve (§8).
		return []string{muted.Render("○ (sin backups todavía) · [B] Crear ahora")}
	}
	// El ancho se reparte por prioridad: nombre, tamaño y, si sobra sitio, el
	// checksum corto. Nunca se desborda ni se deja el nombre ilegible.
	const shaW = 12 // shortSha: 12 caracteres
	nameW, sizeW := 0, 0
	for _, bk := range s.backups {
		if w := layout.Width(bk.Name); w > nameW {
			nameW = w
		}
		if w := layout.Width(humanBytes(bk.Size)); w > sizeW {
			sizeW = w
		}
	}
	if nameW > 32 {
		nameW = 32
	}
	showSha := nameW+sizeW+shaW+6 <= inner
	if !showSha {
		if room := inner - sizeW - 4; nameW > room {
			nameW = room
		}
	}
	if nameW < 8 {
		nameW = 8
	}
	band := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.AccentText)).Background(lipgloss.Color(s.pal.Accent))
	out := make([]string, 0, len(s.backups))
	for i, bk := range s.backups {
		tail := padLine(humanBytes(bk.Size), sizeW)
		if showSha {
			tail += "  " + shortSha(bk.Sha256)
		}
		row := padLine(bk.Name, nameW) + "  " + tail
		if i == s.bkCursor {
			row = components.Pointer(s.ascii) + " " + row
			out = append(out, band.Render(padLine(row, inner)))
			continue
		}
		out = append(out, "  "+row)
	}
	return out
}

// backupsFoot son los atajos del pie, repartidos en líneas que quepan.
func (s *instanceScreen) backupsFoot(inner int) []string {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted))
	out := make([]string, 0, 3)
	for _, l := range packTokens(backupsShortcut, inner) {
		out = append(out, muted.Render(l))
	}
	return out
}

// backupsTag es el contador derecho del panel: cuántas copias hay. Vacío
// mientras no haya una lectura confirmada.
func (s *instanceScreen) backupsTag() string {
	if !s.backupsLoaded || len(s.backups) == 0 {
		return ""
	}
	if len(s.backups) == 1 {
		return "1 copia"
	}
	return itoa(len(s.backups)) + " copias"
}

// windowRows recorta una lista a `keep` filas manteniendo visible la fila
// seleccionada (centrada cuando se puede): la selección nunca se pierde de vista.
func windowRows(rows []string, keep, cursor int) []string {
	if keep >= len(rows) {
		return rows
	}
	if keep < 1 {
		keep = 1
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(rows)-1 {
		cursor = len(rows) - 1
	}
	start := cursor - keep/2
	if start < 0 {
		start = 0
	}
	if start+keep > len(rows) {
		start = len(rows) - keep
	}
	return rows[start : start+keep]
}

// --- Mundos (§5.7) ---

// worldsView dibuja la pestaña Mundos dentro de un panel (§5.7). Solo muestra el
// dato del Spec (la semilla) y las instrucciones del CLI para importar/exportar:
// la TUI no calcula el tamaño del mundo ni hace E/S en View. Estados: cargando
// (detail==nil) y el panel con la semilla y la ayuda (vacío cuando no hay semilla
// definida).
func (s *instanceScreen) worldsView(w, h int) string {
	if w < 2 {
		w = 2
	}
	if h < 4 {
		h = 4
	}
	inner := w - 2
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted))
	if s.detail == nil || s.detail.Spec == nil {
		return components.Panel("MUNDO", "", muted.Render("cargando…"), w, h, false, s.pal, s.ascii)
	}
	spec := s.detail.Spec
	var body []string
	body = append(body, "Semilla: "+orDash(spec.Seed))
	if strings.TrimSpace(spec.Seed) == "" {
		body = append(body, muted.Render("(sin semilla; el mundo se genera aleatorio)"))
	}
	body = append(body, "")
	body = append(body, lipgloss.NewStyle().Bold(true).Render("Biblioteca de mundos (CLI)"))
	body = append(body, wrapWords("Guardar, importar o cambiar el mundo se hace desde el CLI:", inner)...)
	for _, cmd := range []string{
		"badomens world list",
		"badomens -i " + spec.Slug + " world save <nombre>",
		"badomens -i " + spec.Slug + " world use <nombre>",
		"badomens -i " + spec.Slug + " world export [nombre]",
		"badomens world import <zip> <nombre>",
	} {
		for _, ln := range wrapWords("  "+cmd, inner) {
			body = append(body, ln)
		}
	}
	return components.Panel("MUNDO", "", strings.Join(body, "\n"), w, h, false, s.pal, s.ascii)
}

// --- Plugins/Mods (§5.8) ---

// pluginsView dibuja la pestaña Plugins/Mods dentro de un panel (§5.8): la lista
// de spec.Plugins con el contador a la derecha del título y, si está vacía, qué
// falta y con qué comando se resuelve. Los plugins ya vienen del detalle: aquí no
// se hace ninguna E/S nueva ni se listan por pulsación.
func (s *instanceScreen) pluginsView(w, h int) string {
	if w < 2 {
		w = 2
	}
	if h < 4 {
		h = 4
	}
	inner := w - 2
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted))
	if s.detail == nil {
		return components.Panel("PLUGINS/MODS", "", muted.Render("cargando…"), w, h, false, s.pal, s.ascii)
	}
	if s.detail.Spec == nil {
		return components.Panel("PLUGINS/MODS", "", lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Bad)).
			Render(components.Cross(s.ascii)+" no se pudo leer la definición de la instancia"), w, h, false, s.pal, s.ascii)
	}
	spec := s.detail.Spec
	tag := ""
	if n := len(spec.Plugins); n == 1 {
		tag = "1 instalado"
	} else if n > 1 {
		tag = itoa(n) + " instalados"
	}
	var body []string
	if len(spec.Plugins) == 0 {
		body = append(body, muted.Render("○ (ninguno instalado)"))
	} else {
		for _, p := range spec.Plugins {
			body = append(body, "  "+components.Pointer(s.ascii)+" "+p)
		}
	}
	body = append(body, "")
	body = append(body, lipgloss.NewStyle().Bold(true).Render("Gestionar (CLI)"))
	for _, cmd := range []string{
		"badomens -i " + spec.Slug + " plugin list",
		"badomens -i " + spec.Slug + " plugin add <archivo-o-url>",
		"badomens -i " + spec.Slug + " plugin remove <nombre>",
	} {
		for _, ln := range wrapWords("  "+cmd, inner) {
			body = append(body, ln)
		}
	}
	return components.Panel("PLUGINS/MODS", tag, strings.Join(body, "\n"), w, h, false, s.pal, s.ascii)
}

// --- Red (§5.9) ---

// netShortcut es la línea de atajos de la pestaña Red.
const netShortcut = "[R] Comprobar de nuevo · [C] Copiar dirección"

// netView dibuja la pestaña Red dentro de paneles (§5.9). En medio y ancho reparte
// dos paneles contiguos cuyos anchos suman exactamente w: CONEXIÓN (con las líneas
// de components.ConnectLines) y PUERTO (el resultado de la comprobación). En
// compacto apila los mismos paneles en una sola columna: conexión y luego puerto.
// Solo representa estado: la comprobación llega por instancePortMsg y la conexión
// se deriva del Spec, sin E/S en View.
func (s *instanceScreen) netView(w, h int) string {
	if w < 2 {
		w = 2
	}
	if h < 4 {
		h = 4
	}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted))
	if s.detail == nil || s.detail.Spec == nil {
		return components.Panel("CONEXIÓN", "", muted.Render("cargando…"), w, h, false, s.pal, s.ascii)
	}
	spec := s.detail.Spec
	conn := connectionFromSpecWithHost(spec, s.detail.Host)
	if layout.ClassifyWidth(w) == layout.Compact {
		connH := h / 2
		if connH < 2 {
			connH = 2
		}
		portH := h - connH
		if portH < 2 {
			portH = 2
			connH = h - portH
		}
		if connH < 2 {
			connH = 2
		}
		return lipgloss.JoinVertical(lipgloss.Left,
			s.connectionPanel(w, connH, conn), s.portPanel(w, portH))
	}
	connW := w * 52 / 100
	if connW < 24 {
		connW = 24
	}
	if w-connW < 20 {
		connW = w - 20
	}
	if connW < 12 {
		connW = 12
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		s.connectionPanel(connW, h, conn), s.portPanel(w-connW, h))
}

// connectionPanel dibuja el panel CONEXIÓN con las líneas de ConnectLines (sin
// repetir el encabezado, que ya es el título del panel) ajustadas al ancho.
func (s *instanceScreen) connectionPanel(w, h int, conn connection.Info) string {
	return components.Panel("CONEXIÓN", "", s.connectionBody(w-2, conn), w, h, false, s.pal, s.ascii)
}

// portPanel dibuja el panel PUERTO con el estado de la comprobación y sus atajos.
func (s *instanceScreen) portPanel(w, h int) string {
	return components.Panel("PUERTO", s.portTag(), strings.Join(s.portBody(w-2), "\n"), w, h, false, s.pal, s.ascii)
}

// portTag es el contador derecho del panel PUERTO: el desenlace en una palabra.
func (s *instanceScreen) portTag() string {
	switch {
	case s.portErr != "":
		return "error"
	case s.portRes == nil:
		return ""
	case s.portRes.Free:
		return "libre"
	default:
		return "ocupado"
	}
}

// portBody compone el cuerpo del panel PUERTO. Distingue comprobando (sin
// resultado), libre, ocupado (con sus conflictos y el puerto sugerido) y error de
// comprobación (§8), con icono y texto, nunca solo color.
func (s *instanceScreen) portBody(inner int) []string {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted))
	var out []string
	switch {
	case s.portErr != "":
		out = append(out, lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Bad)).
			Render(components.Cross(s.ascii)+" No se pudo comprobar el puerto"))
		out = append(out, wrapWords(s.portErr, inner)...)
	case s.portRes == nil:
		out = append(out, muted.Render("Comprobando el puerto…"))
		out = append(out, muted.Render("[R] comprobar de nuevo"))
	default:
		r := s.portRes
		if r.Free {
			out = append(out, lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Good)).
				Render(components.Check(s.ascii)+" Puerto "+itoa(r.Port)+" · libre"))
			for _, ln := range wrapWords("Nadie lo está usando: la instancia puede publicarlo.", inner) {
				out = append(out, muted.Render(ln))
			}
		} else {
			out = append(out, lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Bad)).
				Render(components.Cross(s.ascii)+" Puerto "+itoa(r.Port)+" · ocupado"))
			for _, cf := range r.Conflicts {
				for _, ln := range wrapWords("  "+conflictText(cf), inner) {
					out = append(out, ln)
				}
			}
			if r.Suggested > 0 {
				out = append(out, lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Warn)).
					Render("Puerto sugerido: "+itoa(r.Suggested)))
			}
		}
	}
	out = append(out, "")
	for _, ln := range packTokens(netShortcut, inner) {
		out = append(out, muted.Render(ln))
	}
	return out
}

// conflictText redacta un conflicto de puerto con su severidad y su ocupante.
func conflictText(cf ports.Conflict) string {
	owner := cf.Owner.Detail
	if owner == "" {
		owner = cf.Owner.Name
	}
	if owner == "" {
		owner = cf.Owner.Kind.String()
	}
	return cf.Severity.String() + ": " + owner
}

// netKey atiende los atajos de la pestaña Red: [R] vuelve a comprobar el puerto y
// [C] copia la dirección de conexión.
func (s *instanceScreen) netKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	switch msg.String() {
	case "R":
		return s, s.portCmd()
	case "C":
		return s, s.Invoke("instance.copy")
	}
	return s, nil
}

// --- Auto-apagado (§5.10) ---

// idleView dibuja la pestaña Auto-apagado dentro de un panel (§5.10): el estado
// (activado con su tiempo / desactivado), si se hará backup antes de apagar, la
// lista de presets con puntero (más «Desactivar») y los atajos. Solo representa
// estado: la configuración llega por instanceIdleMsg y se aplica por idleKey, sin
// E/S en View. La cuenta atrás llega por el bus de eventos (idleEventMsg, que el
// App reenvía a la pantalla activa) y se muestra mientras el apagado esté armado.
func (s *instanceScreen) idleView(w, h int) string {
	if w < 2 {
		w = 2
	}
	if h < 4 {
		h = 4
	}
	inner := w - 2
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted))
	good := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Good))
	var body []string
	switch {
	case !s.idleLoaded:
		body = append(body, muted.Render("cargando…"))
	case s.idleCfg.Enabled:
		state := "Estado: activado · se apaga tras " + idle.HumanDuration(s.idleCfg.Timeout) + " sin jugadores"
		for _, ln := range wrapWords(state, inner) {
			body = append(body, good.Render(ln))
		}
	default:
		body = append(body, "Estado: "+muted.Render("desactivado"))
	}
	if s.idleNote != "" {
		body = append(body, lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Warn)).Render(s.idleNote))
	}
	body = append(body, "Backup antes de apagar: "+onOff(s.idleCfg.BackupBeforeStop))
	body = append(body, "")
	body = append(body, lipgloss.NewStyle().Bold(true).Render("Apagar tras inactividad"))
	for i, p := range idlePresets {
		marker := "  "
		if i == s.idleCursor {
			marker = components.Pointer(s.ascii) + " "
		}
		line := marker + p.Label
		if s.idleLoaded && s.idleCfg.Enabled && p.Timeout > 0 && p.Timeout == s.idleCfg.Timeout {
			line += "  " + good.Render("(actual)")
		}
		body = append(body, line)
	}
	marker := "  "
	if s.idleCursor == len(idlePresets) {
		marker = components.Pointer(s.ascii) + " "
	}
	body = append(body, marker+"Desactivar")
	body = append(body, "")
	for _, ln := range wrapWords("El tiempo no cuenta mientras haya alguien dentro.", inner) {
		body = append(body, muted.Render(ln))
	}
	for _, ln := range packTokens("[Enter] Aplicar · [B] Alternar backup · [T] Probar detección ahora", inner) {
		body = append(body, muted.Render(ln))
	}
	return components.Panel("AUTO-APAGADO", s.idleTag(), strings.Join(body, "\n"), w, h, false, s.pal, s.ascii)
}

// idleTag es el contador derecho del panel de Auto-apagado.
func (s *instanceScreen) idleTag() string {
	if !s.idleLoaded {
		return ""
	}
	if s.idleCfg.Enabled {
		return "activado"
	}
	return "desactivado"
}

// settingsView dibuja la pestaña Ajustes dentro de un panel (§5.11): el estado
// del guardado, los campos con su valor (el que se edita muestra el input con
// foco) y el aviso «Requiere reiniciar». Solo representa estado: el guardado
// llega por instanceSavedMsg y el Spec no se muta antes de persistir.
func (s *instanceScreen) settingsView(w, h int) string {
	if w < 2 {
		w = 2
	}
	if h < 4 {
		h = 4
	}
	inner := w - 2
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted))
	fields := s.settingFields()
	if len(fields) == 0 {
		return components.Panel("AJUSTES", "", muted.Render("cargando…"), w, h, false, s.pal, s.ascii)
	}
	var body []string
	if line := s.settingsStateLine(inner); line != "" {
		body = append(body, line, "")
	}
	if s.editing {
		body = append(body, muted.Render("Enter: guardar · Esc: cancelar"), "")
	}
	// La ayuda de cada campo solo se dibuja cuando hay alto para todos: en
	// compacto se sacrifica antes que dejar campos fuera del panel.
	showHelp := bodyLines(h) >= len(fields)*2
	for i, f := range fields {
		marker := "  "
		if i == s.setCursor {
			marker = components.Pointer(s.ascii) + " "
		}
		prefix := marker + f.label + ": "
		val := f.value
		if s.editing && s.editKey == f.key {
			// El campo activo usa el ancho que queda tras la etiqueta para que el
			// cursor siga visible (§9).
			if bw := inner - layout.Width(prefix) - 1; bw > 8 {
				s.input.Width = bw
			}
			val = s.input.View()
		}
		line := prefix + val
		if f.requires {
			// El aviso se acorta solo si no cabe: primero el texto completo y, en
			// último término, el icono con la acción imprescindible.
			warn := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Warn))
			for _, text := range []string{" Requiere reiniciar", " reiniciar", ""} {
				mark := components.Warn(s.ascii) + text
				if layout.Width(line)+2+layout.Width(mark) <= inner || text == "" {
					line += "  " + warn.Render(mark)
					break
				}
			}
		}
		body = append(body, padLine(line, inner))
		if showHelp {
			if hlp, ok := help.Get(f.key); ok {
				body = append(body, padLine("    "+muted.Render(hlp.Short), inner))
			}
		}
	}
	return components.Panel("AJUSTES", s.settingsTag(), strings.Join(body, "\n"), w, h, false, s.pal, s.ascii)
}

// settingsStateLine describe el estado del formulario (guardando, guardado o
// error) con icono y texto: el estado nunca se comunica solo con color (§8).
func (s *instanceScreen) settingsStateLine(inner int) string {
	text := strings.TrimSpace(s.status)
	if text == "" {
		return ""
	}
	color := s.pal.Muted
	switch {
	case hasAnyPrefix(text, "✖"):
		color = s.pal.Bad
	case hasAnyPrefix(text, "⟳", "⚠"):
		color = s.pal.Warn
	case hasAnyPrefix(text, "✓"):
		color = s.pal.Good
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(layout.Truncate(text, inner))
}

// settingsTag es el contador derecho del panel de Ajustes: el estado del
// formulario mientras se edita o se guarda (el foco nunca depende solo del color).
func (s *instanceScreen) settingsTag() string {
	switch {
	case s.editing:
		return "editando"
	case strings.HasPrefix(s.status, "guardando"):
		return "guardando…"
	case hasAnyPrefix(s.status, "✓"):
		return "guardado"
	}
	return ""
}

func (s *instanceScreen) Actions() []components.Action {
	name := "«" + s.slug + "»"
	running := s.detail != nil && s.detail.State == "running"
	restartDisabled := !running
	restartWhy := "No está en marcha"
	if s.crashLoop {
		restartDisabled = true
		restartWhy = "Se cayó 3 veces en 10 min — revisa la causa en Resumen antes de reiniciar"
	}
	return []components.Action{
		{ID: "instance.start", Verb: "Iniciar", Object: name, Key: "s", Hint: "Arranca el servidor.", Kind: components.Primary, Disabled: running, DisabledReason: "Ya está en marcha"},
		{ID: "instance.stop", Verb: "Detener", Object: name, Key: "p", Hint: "Apaga guardando el mundo.", Kind: components.Secondary, Disabled: !running, DisabledReason: "No está en marcha"},
		{ID: "instance.restart", Verb: "Reiniciar", Object: name, Key: "r", Hint: "Recrea el contenedor aplicando cambios.", Kind: components.Secondary, Disabled: restartDisabled, DisabledReason: restartWhy},
		{ID: "instance.console", Verb: "Escribir", Object: "en la consola", Key: "e", Hint: "Envía un comando al servidor.", Kind: components.Secondary},
		{ID: "instance.copy", Verb: "Copiar", Object: "dirección de conexión", Key: "c", Hint: "Copia ip:puerto al portapapeles.", Kind: components.Secondary},
		{ID: "instance.version", Verb: "Cambiar", Object: "la versión de " + name, Key: "v", Hint: "Hace un backup y cambia la versión (reinicia para aplicar).", Kind: components.Secondary},
		{ID: "instance.delete", Verb: "Eliminar", Object: name + " para siempre", Key: "D", Hint: "Borra el contenedor y sus datos; pide escribir el nombre.", Kind: components.Destructive},
		{ID: "instance.back", Verb: "Volver", Object: "a la lista", Key: "esc", Hint: "Cierra el detalle.", Kind: components.Navigation},
	}
}

// Invoke ejecuta una acción por su ID.
func (s *instanceScreen) Invoke(id string) tea.Cmd {
	switch id {
	case "instance.start":
		return s.lifecycle("start")
	case "instance.stop":
		return s.lifecycle("stop")
	case "instance.restart":
		return s.lifecycle("restart")
	case "instance.console":
		s.tab = 1
		s.focus = true
		return s.input.Focus()
	case "instance.version":
		return s.startVersionPrompt()
	case "instance.delete":
		return s.startDeletePrompt()
	case "instance.back":
		return func() tea.Msg { return popScreenMsg{} }
	case "instance.copy":
		if s.detail == nil {
			return nil
		}
		addr := connectionFromSpecWithHost(s.detail.Spec, s.detail.Host).Preferred()
		if addr == "" {
			return func() tea.Msg { return toastMsg{Text: "⚠ No hay dirección todavía", Kind: components.Destructive} }
		}
		if err := copyToClipboard(addr); err != nil {
			return func() tea.Msg {
				return toastMsg{Text: "✖ No se pudo copiar: " + err.Error(), Kind: components.Destructive}
			}
		}
		return func() tea.Msg { return toastMsg{Text: "✓ Copiado: " + addr} }
	}
	return nil
}

func (s *instanceScreen) Title() string { return "Instancia " + s.slug }
func (s *instanceScreen) ID() string    { return "instance" }

// NavSection indica la sección de primer nivel activa.
func (s *instanceScreen) NavSection() string { return "instances" }

// Status aporta el mensaje de la barra de estado: en la pestaña de jugadores, el
// resultado de la última operación de acceso o, si no hay ninguna, el resumen de
// una línea del estado de la lista blanca (§4). Fuera de esa pestaña, el mensaje
// general de la pantalla.
func (s *instanceScreen) Status() string {
	if s.tab == 2 {
		if s.accessStatus != "" {
			return s.accessStatus
		}
		return s.accessStatusLine()
	}
	if s.status != "" {
		return s.status
	}
	return s.accessStatus
}

// CapturesInput: el detalle captura las teclas cuando hay un campo de texto, un
// prompt o una confirmación con foco, para no disparar atajos globales (§5).
func (s *instanceScreen) CapturesInput() bool {
	return s.prompt || s.confirmKind != "" || s.focus || s.editing
}

func logColor(line string, pal theme.Palette) string {
	switch {
	case strings.Contains(line, "/ERROR") || strings.Contains(line, "ERROR"):
		return pal.Bad
	case strings.Contains(line, "/WARN") || strings.Contains(line, "WARN"):
		return pal.Warn
	}
	return pal.Foreground
}

func onOff(b bool) string {
	if b {
		return "sí"
	}
	return "no"
}

func yesNo(b bool) string {
	if b {
		return "activado"
	}
	return "desactivado"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// sortedStrings ordena una lista (helper determinista).
func sortedStrings(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}
