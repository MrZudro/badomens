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

type instancePlayersMsg struct {
	online int
	known  bool
}

type instanceSavedMsg struct{ err error }

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

	backups  []backup.Info
	bkCursor int

	portRes *ports.Result

	idleCfg    idle.Config
	idleCursor int
	idleLoaded bool

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
		if msg.err == nil {
			s.backups = msg.items
		}
	case instancePortMsg:
		if msg.err == nil {
			r := msg.res
			s.portRes = &r
		}
	case instanceIdleMsg:
		if msg.err == nil {
			s.idleCfg, s.idleLoaded = msg.cfg, true
		}
	case instancePlayersMsg:
		s.playersOnline, s.playersKnown = msg.online, msg.known
	case accessMsg:
		return s.handleAccessMsg(msg)
	case instanceSavedMsg:
		if msg.err != nil {
			return s, func() tea.Msg {
				return toastMsg{Text: "✖ No se pudo guardar: " + msg.err.Error(), Kind: components.Destructive}
			}
		}
		return s, func() tea.Msg { return toastMsg{Text: "✓ Cambios guardados"} }
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
	case "tab", "right", "l":
		return s.switchTab(1)
	case "shift+tab", "left", "h":
		return s.switchTab(-1)
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

func (s *instanceScreen) createBackup() tea.Cmd {
	ref, slug := s.svc, s.slug
	return tea.Batch(busyCmd(true), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		if err := ref.Backups.Create(ctx, slug); err != nil {
			return toastMsg{Text: "✖ backup: " + err.Error(), Kind: components.Destructive}
		}
		return toastMsg{Text: "✓ «" + slug + "»: backup creado"}
	})
}

func (s *instanceScreen) restoreBackup() tea.Cmd {
	if len(s.backups) == 0 || s.bkCursor >= len(s.backups) {
		return nil
	}
	ref, slug := s.svc, s.slug
	name := s.backups[s.bkCursor].Name
	return tea.Batch(busyCmd(true), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()
		if err := ref.Backups.Restore(ctx, slug, name); err != nil {
			return toastMsg{Text: "✖ restaurar: " + err.Error(), Kind: components.Destructive}
		}
		return toastMsg{Text: "✓ «" + slug + "»: restaurado " + name + " (con backup previo)"}
	})
}

func (s *instanceScreen) pruneBackups() tea.Cmd {
	ref, slug := s.svc, s.slug
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		removed, err := ref.Backups.Prune(ctx, slug, 5)
		if err != nil {
			return toastMsg{Text: "✖ podar: " + err.Error(), Kind: components.Destructive}
		}
		return toastMsg{Text: "✓ «" + slug + "»: podados " + itoa(len(removed)) + " backups (se conservan 5)"}
	}
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
		if s.idleCursor == len(idlePresets) {
			cfg.Enabled = false
		} else {
			cfg.Enabled = true
			cfg.Timeout = idlePresets[s.idleCursor].Timeout
		}
		cfg = cfg.Normalized()
		ref, slug := s.svc, s.slug
		return s, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := ref.Idle.Set(ctx, slug, cfg); err != nil {
				return toastMsg{Text: "✖ auto-apagado: " + err.Error(), Kind: components.Destructive}
			}
			return toastMsg{Text: "✓ Auto-apagado actualizado"}
		}
	case "b":
		cfg := s.idleCfg
		cfg.BackupBeforeStop = !cfg.BackupBeforeStop
		ref, slug := s.svc, s.slug
		return s, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = ref.Idle.Set(ctx, slug, cfg)
			return toastMsg{Text: "✓ Backup antes de apagar: " + yesNo(cfg.BackupBeforeStop)}
		}
	case "t":
		return s, s.playersCmd()
	}
	return s, nil
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

func (s *instanceScreen) settingFields() []settingField {
	if s.detail == nil || s.detail.Spec == nil {
		return nil
	}
	sp := s.detail.Spec
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

func (s *instanceScreen) applySetting(key, val string) {
	if s.detail == nil || s.detail.Spec == nil {
		return
	}
	sp := s.detail.Spec
	switch key {
	case "max_players":
		if n, ok := intValue(val); ok {
			sp.MaxPlayers = n
		}
	case "difficulty":
		sp.Difficulty = val
	case "gamemode":
		sp.Gamemode = val
	case "memory":
		sp.Memory = val
	case "motd":
		sp.Motd = val
	}
	s.status = "guardando…"
	s.setCursor = 0
	s.pendingSave = sp
}

// pendingSave se guarda en el Update para lanzar el Cmd una sola vez.
func (s *instanceScreen) saveCmd() tea.Cmd {
	if s.pendingSave == nil {
		return nil
	}
	spec := s.pendingSave
	s.pendingSave = nil
	ref := s.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return instanceSavedMsg{err: ref.Instances.Save(ctx, spec)}
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
		b.WriteString(s.summaryView())
	case 1:
		b.WriteString(s.consoleView(w, h))
	case 2:
		b.WriteString(s.playersView(w, h-s.reservedBodyLines()))
	case 3:
		b.WriteString(s.backupsView())
	case 4:
		b.WriteString(s.worldsView())
	case 5:
		b.WriteString(s.pluginsView())
	case 6:
		b.WriteString(s.netView(w))
	case 7:
		b.WriteString(s.idleView())
	case 8:
		b.WriteString(s.settingsView())
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
	tabLine := ""
	for i, tab := range instanceTabs {
		style := lipgloss.NewStyle()
		label := " " + tab + " "
		if i == s.tab {
			// La pestaña activa se marca con corchetes además del color: el foco
			// debe verse sin depender exclusivamente del color (§9 del rediseño).
			style = style.Bold(true).Foreground(lipgloss.Color(s.pal.AccentText)).Background(lipgloss.Color(s.pal.Accent))
			label = " [" + tab + "] "
		} else {
			style = style.Foreground(lipgloss.Color(s.pal.Muted))
		}
		tabLine += style.Render(label) + " "
	}
	return title + "  " + layout.Truncate(tabLine, w-lipgloss.Width(title)-2)
}

func (s *instanceScreen) summaryView() string {
	if s.detail == nil || s.detail.Spec == nil {
		return "cargando…"
	}
	spec := s.detail.Spec
	players := "—"
	if s.playersKnown {
		players = itoa(s.playersOnline) + "/" + itoa(spec.MaxPlayers)
	}
	var b strings.Builder
	b.WriteString(strings.Join([]string{
		"Motor:      " + spec.Engine + " " + spec.Version,
		"Memoria:    " + spec.Memory,
		"Puerto:     " + itoa(spec.Port),
		"Jugadores:  " + players,
		"Dificultad: " + spec.Difficulty + " · modo " + spec.Gamemode,
		"Estado:     " + theme.Icon(stateKey(s.detail.State), s.ascii),
	}, "\n"))
	b.WriteString("\n\n")
	conn := connectionFromSpecWithHost(spec, s.detail.Host)
	b.WriteString(components.ConnectCard(conn, 64, s.pal, s.ascii))
	if s.crash {
		b.WriteString("\n\n" + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(s.pal.Bad)).Render(components.Warn(s.ascii)+" El servidor se cayó inesperadamente"))
		b.WriteString("\n    Causa probable: " + orDash(s.crashCause))
		if s.crashLoop {
			b.WriteString("\n    " + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Bad)).Render("Se cayó 3 veces en 10 min: revisa la causa antes de reiniciar."))
		} else {
			b.WriteString("\n    [R] Reiniciar")
		}
	}
	return b.String()
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

func (s *instanceScreen) backupsView() string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Copias de seguridad") + "\n\n")
	if len(s.backups) == 0 {
		b.WriteString("  (sin backups todavía)\n")
	}
	for i, bk := range s.backups {
		line := "  " + bk.Name + "  " + humanBytes(bk.Size) + "  " + shortSha(bk.Sha256)
		if i == s.bkCursor {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.AccentText)).Background(lipgloss.Color(s.pal.Accent)).Render(components.Pointer(s.ascii)+line) + "\n")
		} else {
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("[B] Crear backup ahora · [X] Restaurar (crea backup previo) · [Shift+X] Podar antiguos"))
	return b.String()
}

func (s *instanceScreen) worldsView() string {
	if s.detail == nil || s.detail.Spec == nil {
		return "cargando…"
	}
	spec := s.detail.Spec
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Mundo") + "\n\n")
	b.WriteString("  Semilla: " + orDash(spec.Seed) + "\n")
	b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("Guardar/exportar/importar mundos desde el CLI: 'badomens world list|save|use|export|import'."))
	return b.String()
}

func (s *instanceScreen) pluginsView() string {
	if s.detail == nil || s.detail.Spec == nil {
		return "cargando…"
	}
	spec := s.detail.Spec
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Plugins/Mods") + "\n\n")
	if len(spec.Plugins) == 0 {
		b.WriteString("  (ninguno instalado)\n")
	}
	for _, p := range spec.Plugins {
		b.WriteString("  • " + p + "\n")
	}
	b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("Añadir/quitar desde el CLI: 'badomens -i "+spec.Slug+" plugin add|list|remove'."))
	return b.String()
}

func (s *instanceScreen) netView(w int) string {
	if s.detail == nil || s.detail.Spec == nil {
		return "cargando…"
	}
	spec := s.detail.Spec
	var b strings.Builder
	b.WriteString(components.ConnectCard(connectionFromSpecWithHost(spec, s.detail.Host), w, s.pal, s.ascii))
	b.WriteString("\n\n")
	if s.portRes == nil {
		b.WriteString("Comprobando el puerto… [R] comprobar de nuevo\n")
		return b.String()
	}
	icon, color := "✓", s.pal.Good
	if !s.portRes.Free {
		icon, color = "✖", s.pal.Bad
	}
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(icon + " Puerto " + itoa(s.portRes.Port)))
	for _, cf := range s.portRes.Conflicts {
		b.WriteString("\n  " + cf.Severity.String() + ": " + cf.Owner.Detail)
	}
	if s.portRes.Suggested > 0 {
		b.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Warn)).Render("Puerto sugerido: "+itoa(s.portRes.Suggested)))
	}
	b.WriteString("\n\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("[R] Comprobar de nuevo"))
	return b.String()
}

func (s *instanceScreen) idleView() string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Auto-apagado por inactividad") + "\n\n")
	state := "desactivado"
	if s.idleCfg.Enabled {
		state = "activado: se apaga tras " + idle.HumanDuration(s.idleCfg.Timeout) + " sin jugadores"
	}
	b.WriteString("  Estado: " + state + "\n")
	b.WriteString("  Backup antes de apagar: " + onOff(s.idleCfg.BackupBeforeStop) + "\n\n")
	for i, p := range idlePresets {
		marker := "  "
		if i == s.idleCursor {
			marker = components.Pointer(s.ascii) + " "
		}
		b.WriteString(marker + p.Label + "\n")
	}
	marker := "  "
	if s.idleCursor == len(idlePresets) {
		marker = components.Pointer(s.ascii) + " "
	}
	b.WriteString(marker + "Desactivar\n")
	b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("El tiempo no cuenta mientras haya alguien dentro. [Enter] Aplicar · [B] Alternar backup · [T] Probar detección ahora"))
	return b.String()
}

func (s *instanceScreen) settingsView() string {
	fields := s.settingFields()
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Ajustes (se aplican al reiniciar cuando se indica)") + "\n\n")
	for i, f := range fields {
		marker := "  "
		if i == s.setCursor {
			marker = components.Pointer(s.ascii) + " "
		}
		val := f.value
		if s.editing && s.editKey == f.key {
			val = s.input.View()
		}
		line := marker + f.label + ": " + val
		if f.requires {
			line += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Warn)).Render("Requiere reiniciar")
		}
		b.WriteString(line + "\n")
		if h, ok := help.Get(f.key); ok {
			b.WriteString("    " + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render(h.Short) + "\n")
		}
	}
	return b.String()
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
