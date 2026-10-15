package tui

import (
	"context"
	"os"
	"path/filepath"
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

// instancePlayersMsg cierra una consulta de jugadores. Lleva slug y seq para
// DESCARTAR una respuesta tardía de otra instancia o de una consulta anterior
// (§7), y la CAUSA cuando no se pudo medir (mensajes diferenciados, §7).
type instancePlayersMsg struct {
	slug   string
	seq    int
	online int
	max    int
	known  bool
	method string
	reason svc.PlayerReason
	detail string
	err    error
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

// lifecycleDoneMsg indica que una operación de ciclo de vida terminó: el
// semáforo deja de estar en «Cargando» y vuelve al estado sondeado (§8).
type lifecycleDoneMsg struct {
	slug string
	op   string
}

// worldsLibMsg trae la biblioteca de mundos compartida (svc.Worlds.Library). La
// lectura vive en un tea.Cmd: View nunca toca el disco (§7).
type worldsLibMsg struct {
	names []string
	err   error
}

// browseEntry es una entrada del explorador de archivos de la pestaña Mundos: una
// subcarpeta o un archivo .zip.
type browseEntry struct {
	Name  string
	Path  string
	IsDir bool
}

// worldsBrowseMsg trae el contenido de un directorio del explorador (la lectura se
// hace en un tea.Cmd, nunca en View).
type worldsBrowseMsg struct {
	dir     string
	entries []browseEntry
	err     error
}

// worldsOpMsg cierra una operación de la pestaña Mundos (regenerar o importar):
// la pantalla publica el desenlace como toast y, si fue bien, recarga el detalle
// y la biblioteca. `verify` explica que el resultado NO se pudo verificar ("" =
// verificado): nunca se confirma un éxito sin comprobarlo (T-08).
type worldsOpMsg struct {
	what   string // "regenerar" | "importar"
	slug   string
	seed   string // semilla aplicada al regenerar (vacío = aleatoria)
	name   string // nombre del mundo importado
	verify string // motivo por el que no se pudo verificar ("" = verificado)
	backup string // aviso del backup previo, si se pidió
	err    error
}

// text redacta el desenlace de una operación de Mundos con su icono.
func (m worldsOpMsg) text() string {
	if m.err != nil {
		return "✖ «" + m.slug + "»: no se pudo " + m.what + " el mundo: " + m.err.Error()
	}
	if m.verify != "" {
		return "⚠ «" + m.slug + "»: el mundo se regeneró, pero no se pudo verificar (" + m.verify + ")"
	}
	if m.what == "importar" {
		return "✓ «" + m.slug + "»: mundo «" + m.name + "» importado y activo"
	}
	seed := m.seed
	if seed == "" {
		seed = "aleatoria"
	}
	out := "✓ «" + m.slug + "»: mundo regenerado (semilla " + seed + ")"
	if m.backup != "" {
		out += " · " + m.backup
	}
	return out
}

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
	// input es el campo de la Consola y de los formularios (prompt). Ajustes usa
	// su propio modelo (settingsInput): compartirlo hacía que el texto de un
	// ajuste («4G») acabara pintándose en la línea de la Consola.
	input         textinput.Model
	settingsInput textinput.Model
	focus         bool
	status        string

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
	// settingsSel es el selector ÚNICO de Ajustes (P-03): el mismo componente
	// (components.Selector) que usan los flujos de creación y la regeneración.
	// Se guarda la selección por VALOR (T-04), no por índice.
	settingsSel *components.Selector
	// selectKey es la clave del campo cuyo selector está abierto.
	selectKey string

	playersOnline int
	playersKnown  bool
	playersMax    int
	// playersSeq numera las consultas de jugadores: una respuesta tardía de otra
	// instancia o de una consulta anterior se descarta (§7).
	playersSeq int
	// playersMethod es el método que respondió ("slp"/"rcon") y playersReason /
	// playersDetail explican por qué NO se pudo medir (mensajes diferenciados).
	playersMethod string
	playersReason svc.PlayerReason
	playersDetail string
	// localIP es la IPv4 local (LAN), resuelta en un Cmd: permite ofrecer una
	// dirección copiable antes del primer arranque (§10).
	localIP string
	// regenPending indica que hay una regeneración de mundo EN CURSO: se comunica
	// como indicador persistente en la pestaña Mundos (no solo como notificación).
	regenPending bool
	// Estado de acceso (whitelist/operadores) con fuente y verificación (§6).
	access       svc.AccessState
	accessLoaded bool
	accessStatus string
	accessBusy   bool

	prompt      bool
	promptLabel string
	promptAct   string

	pendingSave *instance.Spec

	// P-03/§5.15: confirmación modal (borrado destructivo, cambio de versión y
	// las operaciones de la pestaña Mundos).
	modal      *components.Modal
	modalKind  string // "", "delete", "version", "world_regen", "world_import"
	modalValue string // versión pendiente, semilla del mundo o archivo .zip
	modalName  string // nombre del mundo a importar (solo "world_import")

	// WS-4: biblioteca de mundos (llega por worldsLibMsg) y explorador de archivos
	// interno de la pestaña Mundos. Todo lo que toca el disco se lanza con un tea.Cmd:
	// View solo representa el estado que ya llegó.
	worldsLib       []string
	worldsLibLoaded bool
	worldsLibErr    string
	browsing        bool
	browseDir       string
	browseEntries   []browseEntry
	browseCursor    int
	browseErr       string
	browseLoading   bool

	// magic es el estado EN VIVO de MagicDNS (Fase 8 §5), que manda sobre el valor
	// persistido en spec.Connection.MagicDNS al pintar la conexión.
	magic magicState

	// opPending nombra la operación de ciclo de vida en vuelo ("start", "stop",
	// "restart"): mientras exista, el semáforo único muestra 🟡 Cargando (§8).
	opPending string

	// zone es el foco entre las zonas/paneles de la pestaña activa: Tab avanza y
	// Shift+Tab retrocede (§1.2 «Desplazarse»).
	zone zoneFocus

	// P-08: recuperación ante caídas.
	lastState    string
	userStopped  bool
	crash        bool
	crashPending bool
	crashCause   string
	crashHistory []time.Time
	crashLoop    bool
}

// instanceTabs son las secciones del detalle. Plugins/Mods se anuncia con
// «(SOON)» porque la TUI todavía no consume los marketplaces (SPEC 2 §14): la
// etiqueta viaja con el nombre, de modo que se ve en la fila de pestañas, en el
// indicador «n/9» del terminal compacto y en la barra de acciones.
var instanceTabs = []string{"Resumen", "Consola", "Jugadores", "Backups", "Mundos", "Plugins/Mods (SOON)", "Red", "Auto-apagado", "Ajustes"}

var idlePresets = idle.Presets()

var consoleCommands = []string{
	"say ", "list", "save-all", "save-all flush", "whitelist add ", "whitelist remove ",
	"op ", "deop ", "kick ", "ban ", "pardon ", "time set day", "weather clear", "stop",
}

var dangerousPrefixes = []string{"stop", "op ", "deop ", "ban ", "ban-ip ", "kick ", "whitelist off", "pardon "}

func newInstanceScreen(sv *svc.Services, pal theme.Palette, ascii bool, slug string) *instanceScreen {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = consolePlaceholder
	in.Width = 40
	// Ajustes usa su propio campo de texto: así lo que se escribe allí nunca
	// aparece en la línea de la Consola (bug «4G»).
	set := textinput.New()
	set.Prompt = ""
	set.Placeholder = "nuevo valor"
	set.Width = 40
	return &instanceScreen{svc: sv, pal: pal, ascii: ascii, slug: slug, input: in, settingsInput: set, status: "cargando…", follow: true}
}

// consolePlaceholder es la pista del campo de la consola.
const consolePlaceholder = "comando para la consola (ej. say hola)"

// resetConsoleInput deja el campo de la consola vacío, sin foco y con su pista
// por defecto: no arrastra ni el texto ni el placeholder de Ajustes ni el de un
// formulario anterior (bug «4G»).
func (s *instanceScreen) resetConsoleInput() {
	s.input.SetValue("")
	s.input.Placeholder = consolePlaceholder
	s.input.Blur()
}

func (s *instanceScreen) Init() tea.Cmd {
	// Nueva generación del buffer: lo que llegue de una vista anterior se descarta.
	s.logsGen++
	// El estado en vivo de MagicDNS se consulta al abrir el detalle: así el aviso
	// de la tarjeta de conexión se reevalúa en vez de arrastrar el dato persistido.
	return tea.Batch(s.load(), s.loadIdle(), magicProbeCmd(), localIPCmd())
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

// playersCmd consulta los jugadores por la fachada de servicios (Server List Ping
// como método principal y RCON como respaldo). NO consulta mientras el servidor
// esté 🟡 Cargando ni apagado: en esos casos devuelve la causa sin gastar una
// conexión (§7). Cada consulta lleva su secuencia y su slug.
func (s *instanceScreen) playersCmd() tea.Cmd {
	s.playersSeq++
	seq := s.playersSeq
	switch s.serverState() {
	case components.ServerStarting:
		return func() tea.Msg {
			return instancePlayersMsg{slug: s.slug, seq: seq, reason: svc.PlayerStarting, detail: "el servidor está arrancando"}
		}
	case components.ServerStopped, components.ServerUnknown:
		return func() tea.Msg {
			return instancePlayersMsg{slug: s.slug, seq: seq, reason: svc.PlayerStopped, detail: "el servidor no está en marcha"}
		}
	}
	ref, slug := s.svc, s.slug
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		rep, err := ref.Players.Report(ctx, slug)
		return instancePlayersMsg{
			slug: slug, seq: seq,
			online: rep.Online, max: rep.Max, known: rep.Known, method: rep.Method,
			reason: rep.Reason, detail: rep.Detail,
			err: err,
		}
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
		// Respuesta obsoleta: de otra instancia o de una consulta anterior. No pisa
		// el estado de la instancia vigente (§7).
		if msg.slug != s.slug || msg.seq != s.playersSeq {
			return s, nil
		}
		s.playersOnline, s.playersKnown = msg.online, msg.known
		s.playersMethod, s.playersReason, s.playersDetail = msg.method, msg.reason, msg.detail
		if msg.max > 0 {
			s.playersMax = msg.max
		}
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
	case worldsLibMsg:
		// Una respuesta (aunque venga vacía o con error) cierra la carga: así el
		// panel distingue el vacío real de la lectura en curso.
		s.worldsLibLoaded = true
		if msg.err != nil {
			s.worldsLibErr = msg.err.Error()
			s.worldsLib = nil
			return s, nil
		}
		s.worldsLibErr = ""
		s.worldsLib = msg.names
		return s, nil
	case worldsBrowseMsg:
		// Con el explorador cerrado se descarta la respuesta: no se reabre solo.
		if !s.browsing {
			return s, nil
		}
		s.browseLoading = false
		s.browseDir = msg.dir
		s.browseEntries = msg.entries
		s.browseErr = ""
		if msg.err != nil {
			s.browseEntries = nil
			s.browseErr = msg.err.Error()
		}
		if s.browseCursor >= len(s.browseEntries) {
			s.browseCursor = 0
		}
		return s, nil
	case magicProbeMsg:
		s.magic = magicStateFrom(msg.rep)
		return s, nil
	case localIPMsg:
		// IPv4 local para poder copiar una dirección antes del primer arranque (§10).
		s.localIP = msg.ip
		return s, nil
	case lifecycleDoneMsg:
		if msg.slug != s.slug {
			return s, nil
		}
		s.opPending = ""
		return s, s.applyState(s.detailState())
	case worldsOpMsg:
		return s, s.handleWorldsOp(msg)
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
			s.resetConsoleInput()
			// Un campo vacío cancela el formulario salvo en la semilla del mundo:
			// ahí vacío significa «aleatoria» y sí se aplica.
			if val == "" && act != "__regen__" {
				return s, nil
			}
			return s, s.runPrompt(act, val)
		}
		if msg.String() == "esc" {
			s.prompt = false
			s.resetConsoleInput()
			return s, nil
		}
		var c tea.Cmd
		s.input, c = s.input.Update(msg)
		return s, c
	}
	if s.modal != nil {
		return s, s.handleModalKey(msg)
	}
	// El explorador de archivos de Mundos captura sus teclas (mover, entrar o
	// cargar, subir y cerrar) antes de los atajos del detalle.
	if s.browsing {
		return s.browseKey(msg)
	}
	if s.focus {
		return s.handleConsoleKey(msg)
	}
	// Editar un ajuste o elegir en un selector captura las teclas: Esc cierra el
	// formulario (no abandona el detalle) y las letras se escriben, no son atajos.
	if s.editing || s.selOpen() {
		return s.settingsKey(msg)
	}
	switch msg.String() {
	case "esc":
		return s, func() tea.Msg { return popScreenMsg{} }
	case "D":
		return s, s.startDeletePrompt()
	case "v":
		return s, s.startVersionPrompt()
	case "tab":
		// «Desplazarse» (SPEC 2 §1.2): mueve el foco entre las zonas/paneles de
		// la pestaña activa. Cambiar de pestaña es cosa de ←/→.
		s.zone.move(1)
		return s, nil
	case "shift+tab":
		s.zone.move(-1)
		return s, nil
	case "right", "l":
		// Dentro de una instancia, ←/→ cambian de pestaña en cualquier pestaña
		// (incluida Ajustes). En Ajustes nunca actúan "como Enter": no cambian el
		// valor ni guardan (eso lo hace Enter, que abre el selector). Mientras se
		// edita un campo o hay un selector abierto, la captura de foco (§5) las
		// entrega a settingsKey antes de llegar aquí.
		// Excepción: en Mundos, [L] carga un mundo desde un archivo; → sigue
		// cambiando de pestaña (se distingue la tecla «l» de la flecha).
		if msg.String() == "l" && s.tab == 4 {
			return s, s.Invoke("instance.world.load")
		}
		return s.switchTab(1)
	case "left", "h":
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
	case "c":
		// [c] copia la dirección de conexión en cualquier pestaña del detalle
		// (en Red, [C] ofrece ELEGIR entre las direcciones disponibles).
		return s, s.Invoke("instance.copy")
	case "b":
		// [B] Backup siempre disponible en el detalle; en Auto-apagado, [B] sigue
		// alternando «backup antes de apagar», que es la acción de esa pestaña.
		if s.tab != 7 {
			return s, s.createBackup()
		}
	case "f":
		if s.tab == 1 {
			s.follow = !s.follow
			return s, nil
		}
	case "ctrl+f":
		// El buscador de logs vive en una combinación no imprimible: «/» queda
		// libre para escribir en la consola.
		if s.tab == 1 {
			s.prompt = true
			s.promptAct = "__search__"
			s.promptLabel = "Buscar en los logs"
			s.input.Placeholder = "texto"
			return s, s.input.Focus()
		}
	case "/":
		// El campo de la consola se activa solo con «/».
		if s.tab == 1 {
			s.focus = true
			s.resetConsoleInput()
			return s, s.input.Focus()
		}
	}
	// Teclas específicas de la pestaña activa.
	switch s.tab {
	case 2:
		return s.playersKey(msg)
	case 4:
		return s.worldsKey(msg)
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
	// Cambiar de pestaña cierra lo que estuviera a medias: los campos de la
	// Consola y de Ajustes se limpian (no arrastran texto de una pestaña a otra,
	// bug «4G») y la edición o el selector abiertos se descartan.
	s.resetConsoleInput()
	s.cancelEditing()
	s.closeSelector()
	// Al abandonar la pestaña Consola se invalida el buffer en vuelo: la respuesta
	// que llegue más tarde se descarta por generación.
	if prev == 1 && s.tab != 1 {
		s.logsGen++
	}
	switch s.tab {
	case 1:
		return s, s.logsCmd()
	case 2:
		// Al abrir Jugadores se consultan el acceso Y los jugadores: el segundo no
		// gasta conexión si el servidor está apagado o cargando (playersCmd lo
		// resuelve y lo explica).
		return s, tea.Batch(s.accessCmd(), s.playersCmd())
	case 3:
		return s, s.backupsCmd()
	case 4:
		return s, s.worldsLibCmd()
	case 6:
		return s, s.portCmd()
	}
	return s, nil
}

// handleConsoleKey gestiona la línea de consola (RF-1/P-06).
func (s *instanceScreen) handleConsoleKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	switch msg.String() {
	case "enter":
		// Se admite el comando con o sin la barra inicial («/say hola» = «say hola»).
		cmd := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s.input.Value()), "/"))
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
		return s.deleteCmd(false)
	case "__version__":
		s.openVersionModal(val)
		return nil
	case "__regen__":
		s.openRegenModal(val)
		return nil
	case "acl.add", "acl.remove", "acl.op", "acl.deop":
		return s.accessMutateCmd(act, val)
	}
	return s.consoleCmd(act + " " + val)
}

// deleteCmd elimina el contenedor y los datos de la instancia (§11/P-03). Si el
// servidor está EN MARCHA se detiene antes (el diálogo lo avisa), opcionalmente se
// hace un backup previo y, al terminar, se VERIFICA que la instancia ya no existe:
// el resultado se notifica y un fallo deja un estado recuperable y explicado
// (T-08/AC-10).
func (s *instanceScreen) deleteCmd(withBackup bool) tea.Cmd {
	ref, slug := s.svc, s.slug
	running := s.detail != nil && s.detail.State == "running"
	return tea.Batch(busyCmd(true), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		if withBackup {
			if err := ref.Backups.Create(ctx, slug); err != nil {
				return toastMsg{Text: "✖ Backup previo falló; no se borró «" + slug + "»: " + err.Error(), Kind: components.Destructive}
			}
		}
		if running {
			if err := ref.Lifecycle.Stop(ctx, slug); err != nil {
				return toastMsg{Text: "✖ No se pudo detener «" + slug + "» antes de borrar; no se borró nada: " + err.Error(), Kind: components.Destructive}
			}
		}
		if err := ref.Lifecycle.Delete(ctx, slug); err != nil {
			return toastMsg{Text: "✖ No se pudo borrar «" + slug + "»: " + err.Error() + " · puede haber quedado detenida; revisa el estado", Kind: components.Destructive}
		}
		// Verificación (T-08): si la instancia sigue registrada, no se informa de
		// éxito; se avisa de que el borrado quedó a medias.
		if _, err := ref.Instances.Detail(ctx, slug); err == nil {
			return toastMsg{Text: "⚠ «" + slug + "»: el borrado no se pudo verificar (la instancia sigue en el registro)", Kind: components.Destructive}
		}
		return instanceDeletedMsg{slug: slug}
	})
}

// changeVersionCmd hace un backup previo obligatorio y cambia la versión (P-03).
// La versión y sus PINS los resuelve el SERVICIO de catálogo (svc.Catalog): la
// pantalla no reconstruye reglas de resolución y nunca guarda media resolución
// (AC-06). Primero se resuelve, después se hace el backup y por último se
// guarda: un fallo deja intactos la versión y los pins anteriores.
func (s *instanceScreen) changeVersionCmd(v string) tea.Cmd {
	if s.detail == nil || s.detail.Spec == nil || strings.TrimSpace(v) == "" {
		return nil
	}
	ref, spec := s.svc, s.detail.Spec
	requested := strings.TrimSpace(v)
	return tea.Batch(busyCmd(true), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()
		resolved, err := ref.Catalog.Resolve(ctx, spec.Engine, requested)
		if err != nil {
			return toastMsg{Text: "✖ No se pudo resolver " + requested + ": " + err.Error(), Kind: components.Destructive}
		}
		if err := ref.Backups.Create(ctx, spec.Slug); err != nil {
			return toastMsg{Text: "✖ Backup previo falló: " + err.Error(), Kind: components.Destructive}
		}
		spec.Version = resolved.Version
		spec.Pins = resolved.Pins
		if err := ref.Instances.Save(ctx, spec); err != nil {
			return toastMsg{Text: "✖ No se pudo cambiar la versión: " + err.Error(), Kind: components.Destructive}
		}
		text := "✓ «" + spec.Slug + "»: backup hecho · versión " + spec.Version + " (reinicia para aplicar)"
		if resolved.Warning != "" {
			text += " · aviso: " + resolved.Warning
		}
		return toastMsg{Text: text}
	})
}

// startDeletePrompt abre la confirmación reforzada del borrado (§5.15/§11) como un
// modal con borde: detalla QUÉ se elimina, avisa si el servidor está en marcha
// (se detendrá antes), ofrece un backup previo con Ctrl+B y exige escribir el
// nombre exacto. El foco arranca en el botón seguro, nunca en el destructivo.
func (s *instanceScreen) startDeletePrompt() tea.Cmd {
	running := s.detail != nil && s.detail.State == "running"
	body := "Se eliminarán el contenedor, los datos del servidor, el mundo (Overworld, Nether y End), " +
		"los backups y la configuración de «" + s.slug + "»."
	if running {
		body += " El servidor está EN MARCHA: se detendrá antes de borrar."
	}
	body += " Pulsa [Ctrl+B] para hacer un backup antes (recomendado)."
	m := components.DestroyConfirm("eliminar", s.slug)
	// Motivo corto para que el botón destructivo quepa en una línea junto a su
	// etiqueta (el detalle de qué escribir ya está en el cuerpo del modal).
	m.Actions[0].DisabledReason = "escribe el nombre"
	m.Body = body
	s.modal = &m
	s.modalKind, s.modalValue = "delete", ""
	return nil
}

// openVersionModal confirma el cambio de versión con un modal que avisa del
// backup previo. Enter confirma y Esc cancela (§5.15).
func (s *instanceScreen) openVersionModal(v string) {
	m := components.Modal{
		Title: "Cambiar la versión",
		Body: "Se hará un backup de «" + s.slug + "» antes de cambiar a " + v +
			". Bajar de versión puede corromper el mundo.",
		Actions: []components.Action{
			{ID: "confirm", Verb: "Cambiar", Object: "a " + v, Key: "y", Hint: "Hace un backup y aplica la nueva versión.", Kind: components.Primary},
			{ID: "cancel", Verb: "No cambiar", Object: "la versión", Key: "n", Hint: "Cierra sin tocar la versión.", Kind: components.Secondary},
		},
		Focused: 0,
	}
	s.modal = &m
	s.modalKind, s.modalValue = "version", v
}

// handleModalKey atiende el modal activo: captura todas las teclas, Esc cancela
// sin mutar nada y Enter solo actúa sobre el botón enfocado (o sobre el destructivo
// cuando el texto exigido ya coincide). Ctrl+B (backup previo) se atiende ANTES de
// que los caracteres imprimibles se escriban en la confirmación reforzada, y los
// atajos de las acciones ([B], [N], [Y]) funcionan en los modales sin texto exigido.
func (s *instanceScreen) handleModalKey(msg tea.KeyMsg) tea.Cmd {
	m := *s.modal
	key := msg.String()
	if key == "ctrl+b" {
		return s.modalBackupAction()
	}
	// Con texto exigido, cualquier carácter imprimible se escribe: una letra del
	// nombre (p. ej. «h» o «l») nunca debe interpretarse como navegación (§5).
	if m.RequireText != "" {
		if r := []rune(key); len(r) == 1 && r[0] >= 32 {
			m.Typed += string(r[0])
			s.modal = &m
			return nil
		}
	} else if act, ok := modalActionByKey(m, key); ok {
		switch act.ID {
		case "backup":
			return s.modalBackupAction()
		case "cancel":
			s.clearModal()
			return nil
		case "confirm":
			return s.confirmModal()
		}
	}
	switch key {
	case "esc":
		s.clearModal()
		return nil
	case "left", "h", "shift+tab", "up":
		// ←/→ y ↑/↓ mueven el foco entre las acciones del diálogo (una fila de
		// botones se recorre en cualquier dirección sin sorpresas).
		if m.Focused > 0 {
			m.Focused--
		}
		s.modal = &m
		return nil
	case "right", "l", "tab", "down":
		if m.Focused < len(m.Actions)-1 {
			m.Focused++
		}
		s.modal = &m
		return nil
	case "backspace":
		if m.RequireText != "" {
			if r := []rune(m.Typed); len(r) > 0 {
				m.Typed = string(r[:len(r)-1])
			}
			s.modal = &m
		}
		return nil
	case "enter":
		act := m.Actions[m.Focused]
		if m.RequireText != "" {
			// Escribir el nombre exacto habilita confirmar: si no coincide no se
			// borra nada y el modal sigue abierto.
			if !m.Enabled() {
				return nil
			}
			act = m.Actions[0]
		}
		switch act.ID {
		case "confirm":
			if !m.Enabled() {
				return nil
			}
			return s.confirmModal()
		case "cancel":
			s.clearModal()
			return nil
		case "backup":
			return s.modalBackupAction()
		}
		return nil
	}
	return nil
}

// modalActionByKey busca la acción de un modal cuya tecla coincida con una pulsación
// imprimible de una sola letra (los modales sin texto exigido pueden anunciar [B],
// [N], [Y] y estas deben funcionar de verdad).
func modalActionByKey(m components.Modal, key string) (components.Action, bool) {
	r := []rune(key)
	if len(r) != 1 {
		return components.Action{}, false
	}
	for _, act := range m.Actions {
		if strings.EqualFold(strings.TrimSpace(act.Key), key) {
			return act, true
		}
	}
	return components.Action{}, false
}

// confirmModal aplica la acción del modal ya aceptado. El borrado conserva la
// validación del nombre (runPrompt) aunque el botón ya estuviera habilitado, y las
// acciones con clave propia ([B] backup previo, Ctrl+B en el borrado) entran por
// modalBackupAction.
func (s *instanceScreen) confirmModal() tea.Cmd {
	kind, val, name := s.modalKind, s.modalValue, s.modalName
	typed := ""
	if s.modal != nil {
		typed = s.modal.Typed
	}
	s.clearModal()
	switch kind {
	case "delete":
		return s.runPrompt("__delete__", typed)
	case "version":
		return s.changeVersionCmd(val)
	case "world_regen":
		return s.worldsRegenCmd(val, false)
	case "restore":
		return s.restoreBackupCmd(val)
	case "world_import":
		return s.worldsImportCmd(val, name)
	}
	return nil
}

// modalBackupAction ejecuta la acción «backup previo» del modal activo ([B] o
// Ctrl+B): hace el backup y continúa con la operación destructiva que el usuario
// ya eligió por esa vía. En el borrado, la confirmación reforzada sigue siendo
// obligatoria (el backup no sustituye escribir el nombre).
func (s *instanceScreen) modalBackupAction() tea.Cmd {
	kind, val, name := s.modalKind, s.modalValue, s.modalName
	enabled := true
	if s.modal != nil {
		enabled = s.modal.Enabled()
	}
	s.clearModal()
	switch kind {
	case "delete":
		if !enabled {
			return func() tea.Msg {
				return toastMsg{Text: "✖ Escribe el nombre exacto para confirmar; el backup no sustituye la confirmación", Kind: components.Destructive}
			}
		}
		return s.deleteCmd(true)
	case "world_regen":
		return s.worldsRegenCmd(val, true)
	case "version":
		// El cambio de versión ya hace su backup obligatorio antes de aplicar.
		return s.changeVersionCmd(val)
	case "world_import":
		return s.worldsImportCmd(val, name)
	}
	return nil
}

// clearModal cierra el modal activo sin ejecutar nada.
func (s *instanceScreen) clearModal() {
	s.modal = nil
	s.modalKind, s.modalValue, s.modalName = "", "", ""
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

// serverState es la ÚNICA fuente de verdad del estado del servidor (§8): la usan
// el semáforo, la pestaña Jugadores, la regeneración, el borrado y el backup.
// Mientras hay una operación de ciclo de vida en vuelo devuelve «Cargando».
func (s *instanceScreen) serverState() components.ServerState {
	if s.opPending != "" {
		return components.ServerStarting
	}
	if s.detail == nil {
		return components.ServerUnknown
	}
	return components.ServerStateOf(s.detail.State)
}

// InputFocused indica que el campo de comandos de la consola tiene el foco (§6):
// la barra inferior resalta entonces «[Esc] Atrás».
func (s *instanceScreen) InputFocused() bool { return s.focus }

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
	// El estado pasó a «en línea»: si la pestaña Jugadores está abierta, se vuelve a
	// consultar la lista (mientras carga no se consulta, §7).
	var playersRefresh tea.Cmd
	if state == "running" && s.tab == 2 {
		playersRefresh = s.playersCmd()
	}
	// Gate de I/O: fuera de la pestaña Consola no se piden logs (tampoco en View).
	// Excepción única: una caída recién detectada, que carga los logs una vez para
	// deducir la causa aunque el usuario no esté en la consola.
	if crashDetected || s.tab == 1 {
		return tea.Batch(s.logsCmd(), playersRefresh)
	}
	return playersRefresh
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
	// El semáforo único pasa a 🟡 Cargando mientras la operación está en vuelo
	// (§8.2): arrancar, detener y reiniciar comparten el estado «Cargando».
	s.opPending = op
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
	}, func() tea.Msg { return lifecycleDoneMsg{slug: slug, op: op} })
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
		// Verificar de nuevo refresca el acceso Y la lista de jugadores: son las dos
		// caras de la misma pestaña (lista blanca/operadores + quién está dentro).
		return s, tea.Batch(s.accessCmd(), s.playersCmd())
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
		return s, s.startRestorePrompt()
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

// startRestorePrompt confirma la restauración: se sustituyen el mundo y la
// configuración actuales por el backup elegido. El servicio VALIDA el checksum
// antes de tocar los datos y crea un backup previo (§13/6.3), y el diálogo lo dice
// antes de confirmar.
func (s *instanceScreen) startRestorePrompt() tea.Cmd {
	if len(s.backups) == 0 || s.bkCursor >= len(s.backups) {
		return func() tea.Msg {
			return toastMsg{Text: "⚠ no hay backups que restaurar: crea uno con [B]", Kind: components.Destructive}
		}
	}
	info := s.backups[s.bkCursor]
	body := "Se sustituirán el mundo y la configuración de «" + s.slug + "» por el backup «" + info.Name + "». " +
		"Se validará su checksum ANTES de tocar nada y se creará un backup del estado actual."
	if strings.TrimSpace(info.Sha256) == "" {
		body += " Este backup no tiene checksum: no se puede verificar y no se restaurará."
	}
	if s.detail != nil && s.detail.State == "running" {
		body += " El servidor está en marcha: se detendrá y se reiniciará después."
	}
	m := components.Modal{
		Title: "Restaurar el backup «" + info.Name + "»",
		Body:  body,
		Actions: []components.Action{
			{ID: "confirm", Verb: "Sí, restaurar", Object: "«" + info.Name + "»", Key: "y", Hint: "Valida el checksum, crea un backup previo y sustituye los datos.", Kind: components.Destructive},
			{ID: "cancel", Verb: "No, conservar", Object: "lo actual", Key: "n", Hint: "Cierra sin tocar nada.", Kind: components.Secondary},
		},
		Focused: 1,
	}
	s.modal = &m
	s.modalKind, s.modalValue = "restore", info.Name
	return nil
}

// restoreBackupCmd restaura el backup confirmado: el servicio valida su checksum,
// detiene la instancia si corría, hace un backup previo y sustituye los datos.
func (s *instanceScreen) restoreBackupCmd(name string) tea.Cmd {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	s.bkBusy = "restaurando «" + name + "»… (valida checksum y crea backup previo)"
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

// settingField describe un campo editable de Ajustes. kind «text» se edita con el
// campo de texto propio de la pestaña; kind «select» abre una lista de opciones
// con Enter (nada de ciclar con ←/→, que no cambia el valor ni guarda nada).
type settingField struct {
	key      string
	label    string
	value    string
	kind     string   // text | select
	options  []string // valores válidos de un select
	labels   []string // etiquetas en español de options (paralelas; opcional)
	requires bool
}

// optionLabel es la etiqueta visible de la opción i (en español si la hay).
func (f settingField) optionLabel(i int) string {
	switch {
	case i < 0:
		return ""
	case i < len(f.labels):
		return f.labels[i]
	case i < len(f.options):
		return f.options[i]
	}
	return ""
}

// optionIndex devuelve el índice de val entre opts (sin distinguir mayúsculas) o 0
// si no está: el selector siempre tiene una opción señalada.
func optionIndex(opts []string, val string) int {
	for i, o := range opts {
		if strings.EqualFold(o, val) {
			return i
		}
	}
	return 0
}

// display es el valor que se pinta del campo: para un select, la etiqueta en
// español de la opción activa; si el valor guardado no está en la lista se muestra
// tal cual (nunca se pierde).
func (f settingField) display() string {
	if strings.TrimSpace(f.value) == "" {
		return "—"
	}
	if f.kind == "select" {
		for i, o := range f.options {
			if strings.EqualFold(o, f.value) {
				return f.optionLabel(i)
			}
		}
	}
	return f.value
}

// memoryChoices son los tamaños de RAM que ofrece el selector.
var memoryChoices = []string{"1G", "2G", "3G", "4G", "6G", "8G", "12G", "16G"}

// memoryOptions añade el valor actual a la lista de tamaños si no está: un valor
// fuera de la lista no se pierde al abrir el selector. Se ordena por tamaño para
// que el añadido quede en su sitio; lo que no se puede interpretar va al final.
func memoryOptions(cur string) []string {
	out := append([]string{}, memoryChoices...)
	cur = strings.TrimSpace(cur)
	if cur == "" {
		return out
	}
	for _, o := range out {
		if strings.EqualFold(o, cur) {
			return out
		}
	}
	out = append(out, cur)
	sort.SliceStable(out, func(i, j int) bool { return memMB(out[i]) < memMB(out[j]) })
	return out
}

// memMB interpreta «4G», «512M» o un número de MiB para ordenar los tamaños.
func memMB(v string) int {
	s := strings.ToUpper(strings.TrimSpace(v))
	if s == "" {
		return 1 << 20
	}
	unit := 1024
	switch s[len(s)-1] {
	case 'M':
		unit = 1
		s = s[:len(s)-1]
	case 'G':
		s = s[:len(s)-1]
	}
	n, ok := intValue(s)
	if !ok {
		return 1 << 20
	}
	return n * unit
}

// onlineModeChoice traduce el modo online del Spec a la opción del selector.
func onlineModeChoice(online bool) string {
	if online {
		return "premium"
	}
	return "crackeado"
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

// settingFields describe los campos de Ajustes. Dificultad, modo de juego,
// memoria y cuentas son selectores: no admiten texto libre.
func (s *instanceScreen) settingFields() []settingField {
	sp := s.settingsSpec()
	if sp == nil {
		return nil
	}
	return []settingField{
		{key: "max_players", label: "Jugadores máximos", value: itoa(sp.MaxPlayers), kind: "text"},
		{
			key: "difficulty", label: "Dificultad", value: sp.Difficulty, kind: "select",
			options:  []string{"peaceful", "easy", "normal", "hard"},
			labels:   []string{"Pacífico", "Fácil", "Normal", "Difícil"},
			requires: true,
		},
		{
			key: "gamemode", label: "Modo de juego", value: sp.Gamemode, kind: "select",
			options:  []string{"survival", "creative", "adventure", "spectator"},
			labels:   []string{"Supervivencia", "Creativo", "Aventura", "Espectador"},
			requires: true,
		},
		{key: "memory", label: "Memoria", value: sp.Memory, kind: "select", options: memoryOptions(sp.Memory), requires: true},
		{
			key: "online_mode", label: "Cuentas", value: onlineModeChoice(sp.OnlineMode), kind: "select",
			options:  []string{"crackeado", "premium"},
			labels:   []string{"Crackeado", "Premium"},
			requires: true,
		},
		{key: "motd", label: "Mensaje (MOTD)", value: sp.Motd, kind: "text"},
	}
}

// settingsKey atiende la pestaña Ajustes. Con un selector abierto, ↑/↓ (o j/k)
// eligen, Enter aplica la opción y guarda y Esc cierra sin guardar. Sin selector:
// ↑/↓ mueven el cursor y Enter edita el campo de texto o abre el selector. ←/→ no
// hacen nada aquí: no cambian el valor ni guardan.
func (s *instanceScreen) settingsKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	if s.selOpen() {
		return s.settingsSelectKey(msg)
	}
	if s.editing {
		switch msg.String() {
		case "enter":
			s.commitSetting(strings.TrimSpace(s.settingsInput.Value()))
			return s, s.saveCmd()
		case "esc":
			s.cancelEditing()
			return s, nil
		}
		var c tea.Cmd
		s.settingsInput, c = s.settingsInput.Update(msg)
		return s, c
	}
	fields := s.settingFields()
	switch msg.String() {
	case "j", "down":
		if s.setCursor < len(fields)-1 {
			s.setCursor++
		}
	case "k", "up":
		if s.setCursor > 0 {
			s.setCursor--
		}
	case "enter":
		if s.setCursor < len(fields) {
			return s, s.openSetting(fields[s.setCursor])
		}
	}
	return s, nil
}

// openSetting activa el campo enfocado: un campo de texto se edita con el campo
// propio de Ajustes y un select abre su lista de opciones.
func (s *instanceScreen) openSetting(f settingField) tea.Cmd {
	if f.kind == "select" {
		s.selectKey = f.key
		s.settingsSel = settingSelector(f)
		return nil
	}
	s.editing, s.editKey = true, f.key
	s.settingsInput.SetValue(f.value)
	s.settingsInput.CursorEnd()
	return s.settingsInput.Focus()
}

// settingSelector construye el selector único (components.Selector) a partir de
// un campo de Ajustes, con la selección guardada por valor.
func settingSelector(f settingField) *components.Selector {
	items := make([]components.SelectItem, 0, len(f.options))
	for i := range f.options {
		items = append(items, components.SelectItem{Value: f.options[i], Label: f.optionLabel(i)})
	}
	sel := components.NewSelector(f.label, items)
	sel.Pad = "  "        // alinea las opciones con la etiqueta del campo
	sel.SetValue(f.value) // selección por VALOR (0 si el valor no está)
	return sel
}

// selOpen indica si hay un selector de Ajustes abierto.
func (s *instanceScreen) selOpen() bool { return s.settingsSel != nil }

// selKey devuelve la clave del campo cuyo selector está abierto.
func (s *instanceScreen) selKey() string { return s.selectKey }

// selCursor devuelve la posición del cursor del selector abierto.
func (s *instanceScreen) selCursor() int {
	if s.settingsSel == nil {
		return 0
	}
	return s.settingsSel.Cursor()
}

// settingsSelectKey atiende el selector abierto: ↑/↓ mueven, Enter aplica y
// guarda, y Esc cierra sin guardar. La navegación la resuelve el componente único.
func (s *instanceScreen) settingsSelectKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	if s.settingsSel == nil {
		return s, nil
	}
	switch msg.String() {
	case "j", "down":
		s.settingsSel.Move(1)
	case "k", "up":
		s.settingsSel.Move(-1)
	case "enter":
		key, val := s.selectKey, s.settingsSel.Value()
		s.closeSelector()
		if val == "" {
			return s, nil
		}
		s.applySetting(key, val)
		return s, s.saveCmd()
	case "esc":
		s.closeSelector()
	}
	return s, nil
}

// closeSelector cierra el selector abierto sin aplicar nada.
func (s *instanceScreen) closeSelector() {
	s.settingsSel, s.selectKey = nil, ""
}

// cancelEditing descarta la edición de texto en curso y limpia su campo.
func (s *instanceScreen) cancelEditing() {
	s.editing, s.editKey = false, ""
	s.settingsInput.SetValue("")
	s.settingsInput.Blur()
}

// commitSetting aplica el valor tecleado y deja el campo listo para la próxima
// edición (vacío y sin foco).
func (s *instanceScreen) commitSetting(v string) {
	key := s.editKey
	s.editing, s.editKey = false, ""
	s.settingsInput.SetValue("")
	s.settingsInput.Blur()
	s.applySetting(key, v)
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
	case "online_mode":
		// El selector muestra «crackeado»/«premium»; el Spec guarda el booleano.
		next.OnlineMode = val == "premium"
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
	// Zonas/paneles de la pestaña activa para este ancho (§1.2 «Desplazarse»).
	s.zone.set(s.zoneCountFor(w))
	// Una confirmación modal (§5.15) ocupa la vista: se pinta sola para que no
	// la recorte el alto reservado ni quede tapada por el cuerpo de la pestaña.
	if s.modal != nil {
		return s.modal.Render(w, s.pal, s.ascii)
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
	// Semáforo en ubicación FIJA de la franja: visible en TODAS las sub-pantallas
	// de la instancia, con luz + texto (nunca solo color) y actualizado por el
	// sondeo sin recarga manual (§8/UX-09).
	state := s.serverState()
	pill := state.Pill(s.ascii)
	pillStyled := lipgloss.NewStyle().Foreground(lipgloss.Color(state.Color(s.pal))).Render(pill)
	head := title + "  " + pillStyled
	if layout.ClassifyWidth(w) == layout.Compact {
		// En compacto no caben nombre + semáforo + pestaña + contador en una sola
		// línea: el semáforo va con el nombre y la fila de pestañas debajo, así
		// nada esencial se pierde (UX-09 / UI-02).
		ind := itoa(s.tab+1) + "/" + itoa(len(instanceTabs))
		label := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(s.pal.AccentText)).Background(lipgloss.Color(s.pal.Accent)).Render(" [" + instanceTabs[s.tab] + "] ")
		muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render(" " + ind)
		return layout.Truncate(head, w) + "\n" + layout.Truncate(label+muted, w)
	}
	avail := w - layout.Width(title) - layout.Width(pill) - 4
	if avail < 8 {
		avail = 8
	}
	return head + "  " + s.tabsLine(avail)
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
	conn := connectionInfo(spec, s.detail.Host, s.magic)
	if layout.ClassifyWidth(w) == layout.Compact {
		return s.compactSummary(w, h, spec, conn)
	}
	rightW := connectionPanelWidth(w)
	leftW := w - rightW
	left := components.Panel(components.ZoneTitle("RESUMEN", s.zone.active(0), s.ascii), "",
		s.summaryBody(leftW-2, spec), leftW, h, s.zone.active(0), s.pal, s.ascii)
	right := components.Panel(components.ZoneTitle("CÓMO CONECTARSE", s.zone.active(1), s.ascii), "",
		s.connectionBody(rightW-2, conn), rightW, h, s.zone.active(1), s.pal, s.ascii)
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
		{"Estado", s.serverState().Pill(s.ascii)},
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
	out := components.Panel(components.ZoneTitle("RESUMEN", s.zone.active(0), s.ascii), "",
		strings.Join(body, "\n"), w, panelH, s.zone.active(0), s.pal, s.ascii)
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
			if s.zone.active(1) {
				// La conexión no forma panel en compacto: la zona con foco se
				// marca con el puntero en su primera línea.
				cl[0] = components.ZoneMarker(s.ascii) + " " + cl[0]
			}
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
		{"Estado", s.serverState().Pill(s.ascii)},
		{"Auto-apagado", s.idleSummaryText()},
	}
	if cpu := s.cpuRAMText(); cpu != "" {
		rows = append(rows, [2]string{"CPU/RAM", cpu})
	}
	return rows
}

// playersText resume los jugadores conectados: «3/20» o «—» si no se pudo medir.
// playersText resume los jugadores conectados: «3/20» o «-» si no se pudo medir.
func (s *instanceScreen) playersText(spec *instance.Spec) string {
	if !s.playersKnown {
		return "—"
	}
	return itoa(s.playersOnline) + "/" + itoa(s.playerMax())
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
	return components.Panel(components.ZoneTitle("ACCESO", s.zone.active(0), s.ascii), s.accessSourceTag(),
		strings.Join(body, "\n"), w, h, s.zone.active(0), s.pal, s.ascii)
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
	return components.Panel(components.ZoneTitle("OPERADORES", s.zone.active(1), s.ascii), count,
		strings.Join(s.playerLines(ops, "[O] Operador"), "\n"), w, h, s.zone.active(1), s.pal, s.ascii)
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
// onlineLine informa de los jugadores conectados ahora mismo con un mensaje
// DISTINTO por causa (SPEC 2 §7): servidor apagado, iniciando, consulta no
// disponible (RCON/SLP deshabilitado), sin dirección o error de conexión. El dato
// llega por mensaje (playersCmd): aquí no se consulta nada.
func (s *instanceScreen) onlineLine() string {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Warn))
	bad := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Bad))
	if s.playersKnown {
		line := "En línea: " + itoa(s.playersOnline) + " de " + itoa(s.playerMax()) + " conectados ahora"
		switch s.playersMethod {
		case "rcon":
			line += " (RCON)"
		case "slp":
			line += " (Server List Ping)"
		}
		return line
	}
	// Mientras hay una operación de ciclo de vida en vuelo, el estado manda: no se
	// culpa al servidor de algo que aún se está resolviendo (§8).
	switch s.serverState() {
	case components.ServerStarting:
		return warn.Render(components.Warn(s.ascii) + " Servidor iniciando: la lista de jugadores se consultará cuando acepte conexiones.")
	case components.ServerStopped:
		return muted.Render("Servidor apagado: no hay a quién preguntar · [S] Iniciar para verlo")
	case components.ServerUnknown:
		return muted.Render("Estado del servidor desconocido: no se puede consultar la lista de jugadores")
	}
	switch s.playersReason {
	case svc.PlayerNoAddress:
		return warn.Render(components.Warn(s.ascii) + " Sin dirección para consultar: falta la IP del tailnet y el servidor no ha arrancado · [S] Iniciar o ejecuta 'tailscale up'")
	case svc.PlayerQueryDisabled:
		return warn.Render(components.Warn(s.ascii) + " Consulta no disponible: el Server List Ping no respondió y RCON está deshabilitado en este servidor · habilita RCON para el respaldo")
	case svc.PlayerUnreachable:
		line := components.Cross(s.ascii) + " Error de conexión al consultar la lista"
		if s.playersDetail != "" {
			line += ": " + s.playersDetail
		}
		return bad.Render(line + " · [R] Reintentar")
	case svc.PlayerStopped, svc.PlayerStarting:
		// El servidor dejó de estar en línea entre la comprobación y la consulta.
		return muted.Render("Servidor no disponible en el momento de la consulta · [R] Reintentar")
	}
	return muted.Render("En línea: consultando la lista de jugadores… · [R] Reintentar")
}

// playerMax es el cupo conocido (del Spec o del informe del servidor).
func (s *instanceScreen) playerMax() int {
	if s.playersMax > 0 {
		return s.playersMax
	}
	if s.detail != nil && s.detail.Spec != nil {
		return s.detail.Spec.MaxPlayers
	}
	return 0
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
// pestaña (prompt y aviso de comando peligroso) para que un panel de alto
// completo no las deje fuera del cuerpo. Las confirmaciones modales (§5.15)
// ocupan la vista entera y no reservan nada aquí.
func (s *instanceScreen) reservedBodyLines() int {
	n := 0
	if s.prompt {
		n += 3 // dos de separación y la línea del formulario
	}
	if s.pending != "" && s.tab != 1 {
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
const backupsShortcut = "[B] Crear backup ahora · [X] Restaurar (valida checksum) · [Shift+X] Podar antiguos"

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

// worldsShortcut es la línea de atajos de la pestaña Mundos.
const worldsShortcut = "[G] Regenerar el mundo (se apaga y reinicia) · [L] Cargar desde un archivo (.zip)"

// browseShortcut es la línea de atajos del explorador de archivos.
const browseShortcut = "[J/K] mover · [Enter] abrir carpeta o cargar el .zip · [←/H/Retroceso] subir · [Esc] cerrar"

// worldsView dibuja la pestaña Mundos dentro de un panel (§5.7): la semilla del
// Spec, el aviso de pérdida de datos de [G] y la biblioteca de mundos compartida
// (que llega por worldsLibMsg). Con el explorador abierto muestra sus archivos.
// Solo representa estado: aquí no se hace ninguna E/S.
func (s *instanceScreen) worldsView(w, h int) string {
	if w < 2 {
		w = 2
	}
	if h < 4 {
		h = 4
	}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted))
	if s.detail == nil || s.detail.Spec == nil {
		return components.Panel("MUNDO", "", muted.Render("cargando…"), w, h, false, s.pal, s.ascii)
	}
	if s.browsing {
		return s.browseView(w, h)
	}
	return components.Panel("MUNDO", "", s.worldsBody(w-2, h), w, h, false, s.pal, s.ascii)
}

// worldsBody compone el cuerpo del panel de Mundos. El espacio se reparte por
// prioridad: primero la semilla y el aviso de pérdida de datos, después la
// biblioteca (que cede líneas) y por último el pie de teclas.
func (s *instanceScreen) worldsBody(inner, h int) string {
	spec := s.detail.Spec
	max := bodyLines(h)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Warn))
	seed := []string{"Semilla: " + orDash(spec.Seed)}
	if strings.TrimSpace(spec.Seed) == "" {
		seed = append(seed, muted.Render("(sin semilla; el mundo se genera aleatorio)"))
	}
	var avisos []string
	for _, ln := range wrapWords(components.Warn(s.ascii)+" [G] Regenerar borra el mundo actual: se pierden Overworld, Nether y End.", inner) {
		avisos = append(avisos, warn.Render(ln))
	}
	if s.regenPending {
		// Indicador PERSISTENTE (no una notificación fugaz) mientras la
		// regeneración está en curso: la política dice que se apaga y se reinicia.
		avisos = append(avisos, warn.Render("⟳ Regeneración en curso: el servidor se apaga y se reinicia con el mundo nuevo."))
	}
	foot := s.worldsFoot(inner)
	lib := append([]string{lipgloss.NewStyle().Bold(true).Render("Biblioteca de mundos")}, s.worldsLibraryLines(inner)...)
	slot := max - len(seed) - 2 - len(avisos) - len(foot)
	if slot < 1 {
		// Sin sitio para todo, el pie de teclas es lo primero que se sacrifica:
		// el aviso de pérdida de datos nunca se omite.
		foot = nil
		slot = max - len(seed) - 2 - len(avisos)
	}
	if slot < 1 {
		slot = 1
	}
	if len(lib) > slot {
		lib = windowRows(lib, slot, 0)
	}
	body := append([]string{}, seed...)
	body = append(body, "")
	body = append(body, avisos...)
	body = append(body, "")
	body = append(body, lib...)
	if pad := max - len(body) - len(foot); pad > 0 {
		body = append(body, make([]string, pad)...)
	}
	body = append(body, foot...)
	return strings.Join(body, "\n")
}

// worldsLibraryLines es la sección de la biblioteca: cargando, error, vacío con la
// acción que lo resuelve (§8) o la lista de mundos guardados.
func (s *instanceScreen) worldsLibraryLines(inner int) []string {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted))
	switch {
	case s.worldsLibErr != "":
		bad := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Bad))
		out := []string{bad.Render(components.Cross(s.ascii) + " no se pudo leer la biblioteca")}
		for _, ln := range wrapWords(s.worldsLibErr, inner) {
			out = append(out, muted.Render(ln))
		}
		return out
	case !s.worldsLibLoaded:
		return []string{muted.Render("cargando…")}
	case len(s.worldsLib) == 0:
		return []string{muted.Render("○ (biblioteca vacía) · [L] Cargar un mundo desde un .zip")}
	}
	out := make([]string, 0, len(s.worldsLib))
	for _, name := range s.worldsLib {
		out = append(out, "  "+components.Pointer(s.ascii)+" "+name)
	}
	return out
}

// worldsFoot son los atajos del pie de la pestaña Mundos.
func (s *instanceScreen) worldsFoot(inner int) []string {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted))
	out := make([]string, 0, 3)
	for _, l := range packTokens(worldsShortcut, inner) {
		out = append(out, muted.Render(l))
	}
	return out
}

// worldsKey atiende los atajos de la pestaña Mundos: [G] regenerar el mundo y [L]
// cargar uno desde un archivo.
func (s *instanceScreen) worldsKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	switch msg.String() {
	case "g":
		return s, s.Invoke("instance.world.regen")
	case "l":
		return s, s.Invoke("instance.world.load")
	}
	return s, nil
}

// worldsLibCmd lee la biblioteca de mundos compartida fuera de View.
func (s *instanceScreen) worldsLibCmd() tea.Cmd {
	ref := s.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		names, err := ref.Worlds.Library(ctx)
		return worldsLibMsg{names: names, err: err}
	}
}

// startRegenPrompt pide la semilla del mundo regenerado (vacío = aleatoria).
func (s *instanceScreen) startRegenPrompt() tea.Cmd {
	s.prompt = true
	s.promptAct = "__regen__"
	s.promptLabel = "Nueva semilla del mundo (vacío = aleatoria)"
	s.input.Placeholder = "p. ej. 42"
	s.input.SetValue("")
	return s.input.Focus()
}

// regenPolicy es la ÚNICA política de regeneración (RG-03/SEC-09): si el servidor
// está en marcha se APAGA, se borra el mundo y se vuelve a ARRANCAR con la nueva
// semilla. El diálogo la explica ANTES de confirmar y la pantalla la comunica
// mientras está en curso; no hay una segunda política («programada») que el
// usuario no pueda predecir.
const regenPolicy = "Política: apagar y regenerar. Si el servidor está en marcha, se DETENDRÁ ahora, se borrará el mundo y se volverá a ARRANCAR con la nueva semilla."

// openRegenModal advierte de la pérdida de datos ANTES de regenerar y declara la
// política explícitamente, ofreciendo además un backup previo con [B]. El foco
// arranca en el botón seguro, nunca en el destructivo (§5.15).
func (s *instanceScreen) openRegenModal(seed string) {
	seedText := "una semilla aleatoria"
	if seed != "" {
		seedText = "la semilla «" + seed + "»"
	}
	running := s.detail != nil && s.detail.State == "running"
	body := "Se BORRARÁ el mundo actual de «" + s.slug + "» (Overworld, Nether y End) y se generará otro con " +
		seedText + ". Esta acción no se puede deshacer.\n\n" + regenPolicy
	if !running {
		body += " Ahora mismo el servidor está apagado, así que solo se borrará y se regenerará el mundo."
	}
	body += " Recomendado: pulsa [B] para hacer un backup antes."
	m := components.Modal{
		Title: "Regenerar el mundo (se pierde el actual)",
		Body:  body,
		Actions: []components.Action{
			{ID: "confirm", Verb: "Sí, apagar y", Object: "regenerar", Key: "y", Hint: "Detiene el servidor si hace falta, borra el mundo y lo regenera.", Kind: components.Destructive},
			{ID: "backup", Verb: "Backup antes y", Object: "regenerar", Key: "b", Hint: "Crea un backup del mundo actual y después regenera.", Kind: components.Secondary},
			{ID: "cancel", Verb: "No, conservar", Object: "el mundo actual", Key: "n", Hint: "Cierra sin tocar el mundo.", Kind: components.Secondary},
		},
		Focused: 2,
	}
	s.modal = &m
	s.modalKind, s.modalValue, s.modalName = "world_regen", seed, ""
}

// worldsRegenCmd regenera el mundo activo con la semilla indicada (vacío =
// aleatoria). El SERVICIO cumple la política declarada: detiene el servidor si
// estaba en marcha, borra el mundo, guarda la semilla y lo vuelve a arrancar.
// Después se VERIFICA el resultado (semilla persistida y servidor de nuevo en
// marcha) antes de informar de éxito (T-08): si no se puede verificar, se dice.
func (s *instanceScreen) worldsRegenCmd(seed string, backupFirst bool) tea.Cmd {
	ref, slug := s.svc, s.slug
	wasRunning := s.detail != nil && s.detail.State == "running"
	// Progreso visible: el semáforo pasa a 🟡 Cargando (la política apaga y
	// reinicia) y el panel muestra el indicador persistente hasta el desenlace.
	s.opPending = "regenerar"
	s.regenPending = true
	return tea.Batch(busyCmd(true), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()
		msg := worldsOpMsg{what: "regenerar", slug: slug, seed: seed}
		if backupFirst {
			if err := ref.Backups.Create(ctx, slug); err != nil {
				msg.err = err
				msg.verify = "el backup previo falló; el mundo no se tocó"
				return msg
			}
			msg.backup = "con backup previo"
		}
		if err := ref.Worlds.Regenerate(ctx, slug, seed); err != nil {
			msg.err = err
			return msg
		}
		// Verificación: la semilla quedó persistida y, si estaba corriendo, el
		// servidor está otra vez en marcha. Sin esto no se afirma el éxito.
		detail, derr := ref.Instances.Detail(ctx, slug)
		switch {
		case derr != nil || detail == nil || detail.Spec == nil:
			msg.verify = "no se pudo releer la instancia"
		case strings.TrimSpace(detail.Spec.Seed) != strings.TrimSpace(seed):
			msg.verify = "la semilla guardada no coincide con la pedida"
		case wasRunning && detail.State == "exited":
			// La política declara que se reinicia: si quedó detenido, no se confirma
			// como éxito y se dice (T-08), para poder arrancarlo de nuevo con [S].
			msg.verify = "el servidor quedó detenido después de regenerar"
		}
		return msg
	})
}

// handleWorldsOp publica el desenlace de una operación de Mundos y, si fue bien,
// recarga el detalle y la biblioteca (el mundo activo cambió). Importar cierra
// además el explorador: ya no hay nada que elegir. Limpia el indicador persistente
// de regeneración en curso.
func (s *instanceScreen) handleWorldsOp(msg worldsOpMsg) tea.Cmd {
	if msg.what == "regenerar" {
		s.regenPending = false
		s.opPending = ""
	}
	if msg.err != nil {
		// Estado recuperable (AC-10): se recarga el detalle para que el semáforo y
		// los datos reflejen lo que quedó de verdad (p. ej. el servidor detenido).
		text := "✖ no se pudo " + msg.what + " el mundo de «" + msg.slug + "»: " + msg.err.Error()
		if msg.what == "regenerar" {
			text += " · revisa el estado: si quedó detenido, arráncalo con [S]"
		}
		return tea.Batch(
			func() tea.Msg { return toastMsg{Text: text, Kind: components.Destructive} },
			s.load(),
		)
	}
	if msg.what == "importar" {
		s.closeBrowse()
	}
	cmds := []tea.Cmd{
		func() tea.Msg { return toastMsg{Text: msg.text()} },
		s.load(),
		s.worldsLibCmd(),
	}
	if msg.verify != "" {
		// No se confirma un éxito que no se pudo comprobar (T-08): el aviso lo dice.
		cmds = append(cmds, func() tea.Msg {
			return toastMsg{Text: msg.text(), Kind: components.Destructive}
		})
	}
	return tea.Batch(cmds...)
}

// startBrowse abre el explorador de archivos en la carpeta personal del usuario o,
// si no se puede resolver, en los datos de la app o en el directorio actual.
func (s *instanceScreen) startBrowse() tea.Cmd {
	s.browsing = true
	s.browseEntries = nil
	s.browseCursor = 0
	s.browseErr = ""
	s.browseLoading = true
	dir, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(dir) == "" {
		dir = s.svc.DataDir
	}
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	s.browseDir = dir
	return s.browseCmd(dir)
}

// browseCmd lee un directorio (subcarpetas y archivos .zip) fuera de View.
func (s *instanceScreen) browseCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		entries, err := listBrowseEntries(dir)
		return worldsBrowseMsg{dir: dir, entries: entries, err: err}
	}
}

// listBrowseEntries lista las subcarpetas y los archivos .zip de un directorio,
// con las carpetas primero y en orden alfabético estable (sin distinguir
// mayúsculas: el orden no debe depender del sistema).
func listBrowseEntries(dir string) ([]browseEntry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var dirs, zips []browseEntry
	for _, de := range des {
		name := de.Name()
		full := filepath.Join(dir, name)
		if de.IsDir() {
			dirs = append(dirs, browseEntry{Name: name, Path: full, IsDir: true})
			continue
		}
		if strings.EqualFold(filepath.Ext(name), ".zip") {
			zips = append(zips, browseEntry{Name: name, Path: full})
		}
	}
	byNameFold := func(list []browseEntry) {
		sort.SliceStable(list, func(i, j int) bool {
			return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
		})
	}
	byNameFold(dirs)
	byNameFold(zips)
	return append(dirs, zips...), nil
}

// browseKey atiende el explorador: moverse, entrar en una carpeta o cargar un
// .zip, subir al directorio padre y cerrar.
func (s *instanceScreen) browseKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if s.browseCursor < len(s.browseEntries)-1 {
			s.browseCursor++
		}
	case "k", "up":
		if s.browseCursor > 0 {
			s.browseCursor--
		}
	case "enter":
		return s, s.browseEnter()
	case "left", "h", "backspace":
		return s, s.browseUp()
	case "esc":
		s.closeBrowse()
	}
	return s, nil
}

// browseEnter entra en la carpeta señalada o, si la entrada es un .zip, abre la
// confirmación de importación: nunca se importa sin confirmar.
func (s *instanceScreen) browseEnter() tea.Cmd {
	if s.browseCursor < 0 || s.browseCursor >= len(s.browseEntries) {
		return nil
	}
	e := s.browseEntries[s.browseCursor]
	if e.IsDir {
		return s.browseOpen(e.Path)
	}
	s.openImportModal(e.Path, worldNameOf(e.Name))
	return nil
}

// browseUp sube al directorio padre. En la raíz del volumen no hay padre: se
// cierra el explorador.
func (s *instanceScreen) browseUp() tea.Cmd {
	parent := filepath.Dir(s.browseDir)
	if parent == s.browseDir {
		s.closeBrowse()
		return nil
	}
	return s.browseOpen(parent)
}

// browseOpen cambia de carpeta y lanza su lectura.
func (s *instanceScreen) browseOpen(dir string) tea.Cmd {
	s.browseDir = dir
	s.browseEntries = nil
	s.browseCursor = 0
	s.browseErr = ""
	s.browseLoading = true
	return s.browseCmd(dir)
}

// closeBrowse cierra el explorador y descarta su estado: una respuesta que llegue
// después no lo reabre.
func (s *instanceScreen) closeBrowse() {
	s.browsing = false
	s.browseEntries = nil
	s.browseCursor = 0
	s.browseErr = ""
	s.browseLoading = false
}

// worldNameOf deriva el nombre del mundo del nombre del archivo, sin extensión.
func worldNameOf(file string) string {
	base := filepath.Base(strings.TrimSpace(file))
	return strings.TrimSpace(strings.TrimSuffix(base, filepath.Ext(base)))
}

// openImportModal confirma la importación avisando de que el mundo actual se
// pierde. El foco arranca en el botón seguro.
func (s *instanceScreen) openImportModal(archive, name string) {
	if strings.TrimSpace(name) == "" {
		name = worldNameOf(archive)
	}
	body := "Se BORRARÁ el mundo actual de «" + s.slug + "» (Overworld, Nether y End) y se sustituirá por «" +
		name + "». Esta acción no se puede deshacer."
	if s.detail != nil && s.detail.State == "running" {
		body += " El servidor está en marcha: se detendrá y volverá a arrancar."
	}
	m := components.Modal{
		Title: "Cargar un mundo (se pierde el actual)",
		Body:  body,
		Actions: []components.Action{
			{ID: "confirm", Verb: "Sí, cargar", Object: "el mundo", Key: "y", Hint: "Importa el .zip y lo deja como mundo activo.", Kind: components.Destructive},
			{ID: "cancel", Verb: "No, conservar", Object: "el mundo actual", Key: "n", Hint: "Cierra sin tocar el mundo.", Kind: components.Secondary},
		},
		Focused: 1,
	}
	s.modal = &m
	s.modalKind, s.modalValue, s.modalName = "world_import", archive, name
}

// worldsImportCmd importa el .zip indicado como mundo activo de la instancia.
func (s *instanceScreen) worldsImportCmd(archive, name string) tea.Cmd {
	if strings.TrimSpace(archive) == "" {
		return nil
	}
	ref, slug := s.svc, s.slug
	return tea.Batch(busyCmd(true), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()
		return worldsOpMsg{what: "importar", slug: slug, name: name, err: ref.Worlds.ImportZip(ctx, slug, archive, name)}
	})
}

// browseView dibuja el explorador de archivos de Mundos: la carpeta actual, sus
// subcarpetas y .zip (con puntero y banda en la señalada) y las teclas. Solo
// representa estado: la lectura del directorio llega por worldsBrowseMsg.
func (s *instanceScreen) browseView(w, h int) string {
	if w < 2 {
		w = 2
	}
	if h < 4 {
		h = 4
	}
	inner := w - 2
	max := bodyLines(h)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted))
	head := []string{layout.Truncate("Carpeta: "+s.browseDir, inner)}
	switch {
	case s.browseErr != "":
		head = append(head, lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Bad)).
			Render(layout.Truncate(components.Cross(s.ascii)+" no se pudo abrir la carpeta: "+s.browseErr, inner)))
	case s.browseLoading:
		head = append(head, muted.Render("cargando…"))
	}
	rows := s.browseRows(inner)
	if len(rows) == 0 && s.browseErr == "" && !s.browseLoading {
		rows = []string{muted.Render("○ (sin subcarpetas ni archivos .zip aquí) · [←] subir")}
	}
	foot := s.browseFoot(inner)
	slot := max - len(head) - len(foot)
	if slot < 1 {
		slot = 1
	}
	if len(rows) > slot {
		rows = windowRows(rows, slot, s.browseCursor)
	}
	body := append([]string{}, head...)
	body = append(body, rows...)
	if pad := max - len(body) - len(foot); pad > 0 {
		body = append(body, make([]string, pad)...)
	}
	body = append(body, foot...)
	return components.Panel("CARGAR MUNDO", s.browseTag(), strings.Join(body, "\n"), w, h, false, s.pal, s.ascii)
}

// browseRows pinta las entradas del explorador: carpetas primero y luego .zip.
func (s *instanceScreen) browseRows(inner int) []string {
	if len(s.browseEntries) == 0 {
		return nil
	}
	band := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.AccentText)).Background(lipgloss.Color(s.pal.Accent))
	out := make([]string, 0, len(s.browseEntries))
	for i, e := range s.browseEntries {
		tag := "[zip] "
		if e.IsDir {
			tag = "[dir] "
		}
		row := tag + e.Name
		if i == s.browseCursor {
			row = components.Pointer(s.ascii) + " " + row
			out = append(out, band.Render(padLine(row, inner)))
			continue
		}
		out = append(out, "  "+row)
	}
	return out
}

// browseFoot son los atajos del explorador.
func (s *instanceScreen) browseFoot(inner int) []string {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted))
	out := make([]string, 0, 3)
	for _, l := range packTokens(browseShortcut, inner) {
		out = append(out, muted.Render(l))
	}
	return out
}

// browseTag es el contador derecho del explorador: cuántas entradas hay.
func (s *instanceScreen) browseTag() string {
	if s.browseLoading || len(s.browseEntries) == 0 {
		return ""
	}
	if len(s.browseEntries) == 1 {
		return "1 entrada"
	}
	return itoa(len(s.browseEntries)) + " entradas"
}

// --- Plugins/Mods (§5.8/§14) ---

// pluginsTitle es el título del panel de Plugins: lleva el aviso (SOON) para que
// la sección nunca sugiera que está operativa (SPEC 2 §14).
const pluginsTitle = "PLUGINS/MODS (SOON)"

// pluginsView dibuja la pestaña Plugins/Mods dentro de un panel (§5.8): el aviso
// (SOON) siempre visible (§14), la lista de spec.Plugins con el contador a la
// derecha del título y, si está vacía, qué falta y con qué comando se resuelve.
// Los plugins ya vienen del detalle: aquí no se hace ninguna E/S nueva ni se
// listan por pulsación.
func (s *instanceScreen) pluginsView(w, h int) string {
	if w < 2 {
		w = 2
	}
	if h < 4 {
		h = 4
	}
	inner := w - 2
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Warn))
	if s.detail == nil {
		return components.Panel(pluginsTitle, "", muted.Render("cargando…"), w, h, false, s.pal, s.ascii)
	}
	if s.detail.Spec == nil {
		return components.Panel(pluginsTitle, "", lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Bad)).
			Render(components.Cross(s.ascii)+" no se pudo leer la definición de la instancia"), w, h, false, s.pal, s.ascii)
	}
	spec := s.detail.Spec
	tag := ""
	if n := len(spec.Plugins); n == 1 {
		tag = "1 instalado"
	} else if n > 1 {
		tag = itoa(n) + " instalados"
	}
	// El aviso va en el título del panel y, además, en la primera línea del cuerpo:
	// así se lee aunque el borde tenga que abreviar el título.
	var body []string
	for _, l := range wrapWords(components.Warn(s.ascii)+" (SOON) Próximamente: la TUI todavía no busca ni descarga plugins.", inner) {
		body = append(body, warn.Render(l))
	}
	for _, l := range wrapWords("Lo de abajo es informativo; hoy se gestiona con la CLI.", inner) {
		body = append(body, warn.Render(l))
	}
	body = append(body, "")
	if len(spec.Plugins) == 0 {
		body = append(body, muted.Render("○ (ninguno instalado)"))
	} else {
		for _, pl := range spec.Plugins {
			body = append(body, "  "+components.Pointer(s.ascii)+" "+pl)
		}
	}
	body = append(body, "")
	body = append(body, lipgloss.NewStyle().Bold(true).Render("Gestionar (CLI)"))
	for _, cmd := range []string{
		"badomens -i " + spec.Slug + " plugin list",
		"badomens -i " + spec.Slug + " plugin add <archivo-o-url>",
		"badomens -i " + spec.Slug + " plugin remove <nombre>",
	} {
		body = append(body, wrapWords("  "+cmd, inner)...)
	}
	return components.Panel(pluginsTitle, tag, strings.Join(body, "\n"), w, h, false, s.pal, s.ascii)
}

// --- Red (§5.9) ---

// netShortcut es la línea de atajos de la pestaña Red.
const netShortcut = "[R] Comprobar de nuevo · [C] Elegir dirección a copiar"

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
	return components.Panel(components.ZoneTitle("CONEXIÓN", s.zone.active(0), s.ascii), "",
		s.connectionBody(w-2, conn), w, h, s.zone.active(0), s.pal, s.ascii)
}

// portPanel dibuja el panel PUERTO con el estado de la comprobación y sus atajos.
func (s *instanceScreen) portPanel(w, h int) string {
	return components.Panel(components.ZoneTitle("PUERTO", s.zone.active(1), s.ascii), s.portTag(),
		strings.Join(s.portBody(w-2), "\n"), w, h, s.zone.active(1), s.pal, s.ascii)
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
				out = append(out, wrapWords("  "+conflictText(cf), inner)...)
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
		// Elige entre las direcciones disponibles (si solo hay una, la copia).
		return s, s.Invoke("instance.choose-address")
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

// settingsView dibuja la pestaña Ajustes dentro de un panel (§5.11): el estado del
// guardado, los campos con su valor (el que se edita muestra el campo de texto con
// foco), el selector abierto como lista de opciones y el aviso «Requiere
// reiniciar». Solo representa estado: el guardado llega por instanceSavedMsg y el
// Spec no se muta antes de persistir.
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
	budget := bodyLines(h)
	// El estado del guardado va arriba; la ayuda de teclas va después de los
	// campos (abajo), para que en alturas pequeñas el foco y las opciones tengan
	// prioridad y no queden fuera del panel.
	var head []string
	if line := s.settingsStateLine(inner); line != "" {
		head = append(head, line)
	}
	var hint []string
	switch {
	case s.editing:
		hint = append(hint, muted.Render("Enter: guardar · Esc: cancelar"))
	case s.selOpen():
		hint = append(hint, muted.Render("↑/↓: elegir · Enter: guardar"), muted.Render("Esc: cancelar"))
	}
	// La ayuda de cada campo solo se dibuja cuando hay alto para todos: en
	// compacto se sacrifica antes que dejar campos fuera del panel.
	showHelp := budget >= len(fields)*2
	var rows []string
	focus := 0
	for i, f := range fields {
		marker := "  "
		if i == s.setCursor {
			marker = components.Pointer(s.ascii) + " "
		}
		prefix := marker + f.label + ": "
		val := f.display()
		if s.editing && s.editKey == f.key {
			// El campo activo usa el ancho que queda tras la etiqueta para que el
			// cursor siga visible (§9).
			if bw := inner - layout.Width(prefix) - 1; bw > 8 {
				s.settingsInput.Width = bw
			}
			val = s.settingsInput.View()
		}
		line := prefix + val
		// Un select enfocado y cerrado dice con qué tecla se cambia (Enter): el
		// panel no invita a usar ←/→, que aquí no hacen nada.
		if f.kind == "select" && i == s.setCursor && !s.selOpen() && !s.editing {
			mark := muted.Render("  (Enter para cambiar)")
			if layout.Width(line)+layout.Width(mark) <= inner {
				line += mark
			}
		}
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
		if i == s.setCursor {
			focus = len(rows)
		}
		rows = append(rows, padLine(line, inner))
		if s.selOpen() && s.selectKey == f.key {
			// El foco sigue a la opción elegida dentro del selector abierto; la
			// pintura la resuelve el componente único (puntero + banda + «(actual)»).
			opts := s.settingsSel.Render(inner, f.value, s.pal, s.ascii)
			focus = len(rows) + s.settingsSel.Cursor()
			rows = append(rows, opts...)
		}
		if showHelp {
			if hlp, ok := help.Get(f.key); ok {
				rows = append(rows, padLine("    "+muted.Render(hlp.Short), inner))
			}
		}
	}
	// La ayuda de teclas se reserva solo si, aun con ella, el foco queda visible;
	// si no, se sacrifica (Esc es un cancelar universal y el panel ya marca el
	// estado con «eligiendo»/«editando»).
	fieldBudget := budget - len(head) - len(hint)
	if fieldBudget < focus+1 {
		hint = nil
		fieldBudget = budget - len(head)
	}
	if fieldBudget < 1 {
		fieldBudget = 1
	}
	body := append([]string{}, head...)
	body = append(body, windowLines(rows, focus, fieldBudget)...)
	if len(hint) > 0 && len(body)+len(hint) <= budget {
		body = append(body, hint...)
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
// formulario mientras se elige, se edita o se guarda (el foco nunca depende solo
// del color).
func (s *instanceScreen) settingsTag() string {
	switch {
	case s.selOpen():
		return "eligiendo"
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
		{ID: "instance.console", Verb: "Escribir", Object: "en la consola", Key: "/", Hint: "Escribe un comando para el servidor.", Kind: components.Secondary},
		{ID: "instance.copy", Verb: "Copiar", Object: "dirección de conexión", Key: "c", Hint: "Copia ip:puerto al portapapeles.", Kind: components.Secondary},
		{ID: "instance.backup", Verb: "Crear", Object: "backup ahora", Key: "b", Hint: "Guarda una copia de seguridad con checksum.", Kind: components.Secondary, Disabled: s.tab == 7, DisabledReason: "En Auto-apagado, [B] alterna el backup antes de apagar"},
		{ID: "instance.version", Verb: "Cambiar", Object: "la versión de " + name, Key: "v", Hint: "Hace un backup y cambia la versión (reinicia para aplicar).", Kind: components.Secondary},
		{ID: "instance.world.regen", Verb: "Regenerar", Object: "el mundo", Key: "g", Hint: "Pide una semilla y borra el mundo actual (se pierde Overworld, Nether y End).", Kind: components.Secondary},
		{ID: "instance.world.load", Verb: "Cargar", Object: "un mundo desde .zip", Key: "l", Hint: "Explora tus archivos e importa un .zip como mundo activo.", Kind: components.Secondary},
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
		s.resetConsoleInput()
		return s.input.Focus()
	case "instance.version":
		return s.startVersionPrompt()
	case "instance.world.regen":
		s.tab = 4
		return s.startRegenPrompt()
	case "instance.world.load":
		s.tab = 4
		return s.startBrowse()
	case "instance.delete":
		return s.startDeletePrompt()
	case "instance.back":
		return func() tea.Msg { return popScreenMsg{} }
	case "instance.copy":
		// Copia la dirección preferida (tailnet → MagicDNS → red local); el dato
		// persistido permite derivarla ANTES del primer arranque y, si no hay
		// ninguna, se explica por qué y qué hacer (§10).
		return copyAddressCmd(s.copyCandidates(), s.slug)
	case "instance.choose-address":
		// Con varias direcciones válidas, se elige cuál copiar (§10).
		return chooseAddressCmd(s.copyCandidates(), s.slug)
	}
	return nil
}

// copyCandidates reúne las direcciones copiables de la instancia: IP del tailnet,
// nombre MagicDNS (estado EN VIVO si se conoce) y red local.
func (s *instanceScreen) copyCandidates() []copyCandidate {
	if s.detail == nil || s.detail.Spec == nil {
		return nil
	}
	host := s.detail.Host
	if host == "" && s.detail.Spec.Connection != nil {
		host = s.detail.Spec.Connection.Host
	}
	return addressCandidates(s.detail.Spec, host, s.magic, s.localIP)
}

// zoneCountFor declara cuántas zonas/paneles tiene la pestaña activa en un
// ancho dado: Tab/Shift+Tab se desplazan entre ellas (§1.2). Las pestañas con un
// solo panel no tienen a dónde desplazarse y Tab no hace nada en ellas.
func (s *instanceScreen) zoneCountFor(w int) int {
	compact := layout.ClassifyWidth(w) == layout.Compact
	switch s.tab {
	case 0: // Resumen: RESUMEN + CÓMO CONECTARSE
		return 2
	case 2: // Jugadores y acceso: ACCESO + OPERADORES (en compacto solo hay una columna)
		if compact {
			return 1
		}
		return 2
	case 6: // Red: CONEXIÓN + PUERTO
		return 2
	}
	return 1
}

func (s *instanceScreen) Title() string { return "Instancia " + s.slug }
func (s *instanceScreen) ID() string    { return "instance" }

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
// prompt, una confirmación con foco, un selector abierto o el explorador de
// archivos de Mundos, para no disparar atajos globales (§5).
func (s *instanceScreen) CapturesInput() bool {
	return s.prompt || s.modal != nil || s.browsing || s.focus || s.editing || s.selOpen()
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

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
