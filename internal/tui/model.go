package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/backup"
	"github.com/MrZudro/badomens/internal/doctor"
	"github.com/MrZudro/badomens/internal/provision"
)

// modos de la TUI correspondientes a una pantalla.
type mode int

const (
	modeList mode = iota
	modeDetail
	modeLogs
	modeConsole
	modeBackups
	modeHelp
	modeCreate
)

// mensajes internos del programa bubbletea.
type rowsMsg struct {
	rows    []Row
	running int
	err     error
}
type detailMsg struct {
	detail *Detail
	err    error
}
type logsMsg struct {
	slug string
	text string
	err  error
}
type backupsMsg struct {
	slug  string
	items []backup.Info
	err   error
}
type opDoneMsg struct {
	o    op
	slug string
	err  error
}
type healthMsg struct {
	health map[string]string
	err    error
}
type requirementsMsg struct {
	checks []doctor.Check
	err    error
}
type createdMsg struct {
	slug string
	err  error
}
type refreshTickMsg struct{}

// Model es el modelo de la vista: guarda estado, responde a mensajes y pinta.
type Model struct {
	backend Backend
	ctx     context.Context
	cancel  context.CancelFunc

	width  int
	height int
	mode   mode

	rows         []Row
	cursor       int
	running      int
	maxParallel  int
	maxInstances int
	health       map[string]string
	querying     bool

	requirements  []doctor.Check
	noPreflight   bool
	pendingSelect string
	creating      bool
	create        *createForm

	loading bool
	busy    map[string]string // slug -> operación en curso
	status  string
	errText string

	detail *Detail

	logSlug   string
	logLines  int
	logsView  viewport.Model
	stickLogs bool

	backupsSlug string
	backups     []backup.Info

	input textinput.Model
}

// NewModel construye el modelo sobre un backend, listo para Init + Run.
func NewModel(backend Backend) (Model, error) {
	maxParallel, maxInstances := backend.Config()
	ctx, cancel := context.WithCancel(context.Background())
	in := textinput.New()
	in.Placeholder = "comando para la consola (ej. say hola)"
	in.Width = 60
	return Model{
		backend:      backend,
		ctx:          ctx,
		cancel:       cancel,
		mode:         modeList,
		health:       map[string]string{},
		busy:         map[string]string{},
		maxParallel:  maxParallel,
		maxInstances: maxInstances,
		logLines:     200,
		logsView:     viewport.New(80, 16),
		input:        in,
		status:       "cargando instancias…",
	}, nil
}

// Init arranca con el primer refresco de la lista y, si procede, el preflight
// de requisitos.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{refreshCmd(m.ctx, m.backend)}
	if !m.noPreflight {
		cmds = append(cmds, requirementsCmd(m.ctx, m.backend))
	}
	return tea.Batch(cmds...)
}

// ---------------------------------------------------------------------------
// comandos asíncronos

func refreshCmd(ctx context.Context, b Backend) tea.Cmd {
	return func() tea.Msg {
		rows, running, err := b.ListRows(ctx)
		return rowsMsg{rows: rows, running: running, err: err}
	}
}

func requirementsCmd(ctx context.Context, b Backend) tea.Cmd {
	return func() tea.Msg {
		checks, err := b.Preflight(ctx)
		return requirementsMsg{checks: checks, err: err}
	}
}

func createCmd(ctx context.Context, b Backend, req provision.Request) tea.Cmd {
	return func() tea.Msg {
		slug, err := b.Create(ctx, req)
		return createdMsg{slug: slug, err: err}
	}
}

func detailCmd(ctx context.Context, b Backend, slug string) tea.Cmd {
	return func() tea.Msg {
		d, err := b.Detail(ctx, slug)
		return detailMsg{detail: d, err: err}
	}
}

func logsCmd(ctx context.Context, b Backend, slug string, lines int) tea.Cmd {
	return func() tea.Msg {
		text, err := b.Logs(ctx, slug, lines)
		return logsMsg{slug: slug, text: text, err: err}
	}
}

func backupsCmd(ctx context.Context, b Backend, slug string) tea.Cmd {
	return func() tea.Msg {
		items, err := b.Backups(ctx, slug)
		return backupsMsg{slug: slug, items: items, err: err}
	}
}

func healthCmd(ctx context.Context, b Backend) tea.Cmd {
	return func() tea.Msg {
		h, err := b.Health(ctx)
		return healthMsg{health: h, err: err}
	}
}

func opCmd(ctx context.Context, b Backend, o op, slug string, force bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		switch o {
		case opStart:
			err = b.Start(ctx, slug, force)
		case opStop:
			err = b.Stop(ctx, slug)
		case opClose:
			err = b.Close(ctx, slug)
		case opRestart:
			err = b.Restart(ctx, slug)
		case opBackupCreate:
			err = b.CreateBackup(ctx, slug)
		case opPrune:
			_, err = b.PruneBackups(ctx, slug, 5)
		case opConsole:
			// o enviado vía consoleCmd() (necesita el texto)
			err = nil
		}
		return opDoneMsg{o: o, slug: slug, err: err}
	}
}

// consoleCmd envía un comando de consola y reporta como opDoneMsg.
func consoleCmd(ctx context.Context, b Backend, slug, command string) tea.Cmd {
	return func() tea.Msg {
		err := b.Console(ctx, slug, command)
		return opDoneMsg{o: opConsole, slug: slug, err: err}
	}
}

// nextRefresh agenda el siguiente refresco automático de la pantalla actual.
func (m Model) nextRefresh() tea.Cmd {
	interval := 5 * time.Second
	if m.mode == modeLogs {
		interval = 3 * time.Second
	}
	return tea.Tick(interval, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

// ---------------------------------------------------------------------------
// Update

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.logsView.Width = msg.Width
		m.logsView.Height = bodyHeight(msg.Height)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case rowsMsg:
		return m.onRows(msg)

	case detailMsg:
		return m.onDetail(msg)

	case logsMsg:
		return m.onLogs(msg)

	case backupsMsg:
		return m.onBackups(msg)

	case opDoneMsg:
		return m.onOpDone(msg)

	case healthMsg:
		m.querying = false
		if msg.err != nil {
			m.errText = "salud: " + msg.err.Error()
		} else {
			m.health = msg.health
			m.status = "salud actualizada (d para repetir)"
		}
		return m, m.nextRefresh()

	case requirementsMsg:
		if msg.err != nil {
			m.errText = "requisitos: " + msg.err.Error()
		} else {
			m.requirements = msg.checks
		}
		return m, nil

	case createdMsg:
		return m.onCreated(msg)

	case refreshTickMsg:
		return m.onRefreshTick()
	}
	return m, nil
}

// refreshTick refresca la pantalla actual según su modo.
func (m Model) onRefreshTick() (tea.Model, tea.Cmd) {
	if m.loading {
		return m, m.nextRefresh()
	}
	switch m.mode {
	case modeList:
		m.loading = true
		return m, refreshCmd(m.ctx, m.backend)
	case modeDetail:
		if m.detail != nil && m.detail.Spec != nil {
			m.loading = true
			return m, detailCmd(m.ctx, m.backend, m.detail.Spec.Slug)
		}
	case modeLogs:
		return m, logsCmd(m.ctx, m.backend, m.logSlug, m.logLines)
	case modeBackups:
		return m, backupsCmd(m.ctx, m.backend, m.backupsSlug)
	}
	return m, m.nextRefresh()
}

func (m Model) onRows(msg rowsMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	// conserva la selección por slug si sigue existiendo
	prev := ""
	if m.cursor < len(m.rows) {
		prev = m.rows[m.cursor].Slug
	}
	m.rows, m.running = msg.rows, msg.running
	m.cursor = 0
	if prev != "" {
		for i, r := range m.rows {
			if r.Slug == prev {
				m.cursor = i
				break
			}
		}
	}
	if m.pendingSelect != "" {
		for i, r := range m.rows {
			if r.Slug == m.pendingSelect {
				m.cursor = i
				break
			}
		}
		m.pendingSelect = ""
	}
	if msg.err != nil {
		m.errText = "lista: " + msg.err.Error()
	} else {
		m.status = ""
	}
	return m, m.nextRefresh()
}

func (m Model) onDetail(msg detailMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	if msg.err != nil {
		m.errText = "detalle: " + msg.err.Error()
		if m.mode == modeDetail {
			m.mode = modeList
		}
		return m, m.nextRefresh()
	}
	m.detail = msg.detail
	m.status = "detalle de " + msg.detail.Spec.Slug
	return m, m.nextRefresh()
}

func (m Model) onLogs(msg logsMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeLogs || msg.slug != m.logSlug {
		return m, m.nextRefresh()
	}
	m.status = ""
	m.errText = ""
	if msg.err != nil {
		m.errText = "logs: " + msg.err.Error()
	} else {
		// Sincroniza el viewport con el tamaño del panel para que el truncado
		// de líneas coincida con las columnas realmente visibles.
		m.logsView.Width = m.width
		if h := m.height; h > 0 {
			m.logsView.Height = bodyHeight(h)
		}
		atBottom := m.logsView.ScrollPercent() >= 0.99
		m.logsView.SetContent(wrapLogs(msg.text, m.width-2))
		if m.stickLogs || atBottom {
			m.logsView.GotoBottom()
		}
	}
	return m, m.nextRefresh()
}

func (m Model) onBackups(msg backupsMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeBackups || msg.slug != m.backupsSlug {
		return m, m.nextRefresh()
	}
	m.status = ""
	m.errText = ""
	if msg.err != nil {
		m.errText = "backups: " + msg.err.Error()
	} else {
		m.backups = msg.items
	}
	return m, m.nextRefresh()
}

func (m Model) onOpDone(msg opDoneMsg) (tea.Model, tea.Cmd) {
	delete(m.busy, msg.slug)
	if msg.err != nil {
		m.errText = fmt.Sprintf("%s %s: %s", msg.o, msg.slug, msg.err.Error())
		m.status = ""
	} else {
		m.status = fmt.Sprintf("%s %s: ok", msg.o, msg.slug)
	}
	if msg.err == nil && msg.o == opBackupCreate && m.mode == modeBackups && m.backupsSlug == msg.slug {
		return m, tea.Batch(refreshCmd(m.ctx, m.backend), backupsCmd(m.ctx, m.backend, msg.slug))
	}
	return m, refreshCmd(m.ctx, m.backend)
}

// onCreated cierra el asistente: con éxito vuelve a la lista y selecciona la
// instancia nueva; con error mantiene el formulario para corregir.
func (m Model) onCreated(msg createdMsg) (tea.Model, tea.Cmd) {
	m.creating = false
	if msg.err != nil {
		if m.create != nil {
			m.create.err = msg.err.Error()
		}
		m.errText = ""
		return m, nil
	}
	m.mode = modeList
	m.create = nil
	m.errText = ""
	m.status = "instancia " + msg.slug + " creada"
	m.pendingSelect = msg.slug
	return m, refreshCmd(m.ctx, m.backend)
}

// ---------------------------------------------------------------------------
// teclado

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.cancel()
		return m, tea.Quit
	}
	switch m.mode {
	case modeHelp:
		if msg.String() == "?" || msg.String() == "esc" || msg.String() == "q" {
			m.mode = modeList
			m.status = ""
		}
		return m, nil
	case modeList:
		return m.handleListKey(msg)
	case modeDetail:
		return m.handleDetailKey(msg)
	case modeLogs:
		return m.handleLogsKey(msg)
	case modeConsole:
		return m.handleConsoleKey(msg)
	case modeBackups:
		return m.handleBackupsKey(msg)
	case modeCreate:
		return m.handleCreateKey(msg)
	}
	return m, nil
}

func (m Model) quit() (tea.Model, tea.Cmd) {
	m.cancel()
	return m, tea.Quit
}

func (m Model) selectedSlug() (string, bool) {
	if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
		return "", false
	}
	return m.rows[m.cursor].Slug, true
}

// moveCursor desplaza la selección y devuelve el modelo actualizado.
func (m Model) moveCursor(delta int) Model {
	if len(m.rows) == 0 {
		return m
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor >= len(m.rows) {
		m.cursor = 0
	}
	return m
}

func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m.quit()
	case "j", "down":
		m = m.moveCursor(+1)
	case "k", "up":
		m = m.moveCursor(-1)
	case "enter":
		if _, ok := m.selectedSlug(); !ok {
			return m, nil
		}
		m.mode = modeDetail
		m.status = "cargando detalle…"
		return m, detailCmd(m.ctx, m.backend, m.rows[m.cursor].Slug)
	case "s":
		return m.runOp(opStart, true)
	case "p":
		return m.runOp(opStop, false)
	case "c":
		return m.runOp(opClose, false)
	case "r":
		return m.runOp(opRestart, false)
	case "l":
		return m.openLogs()
	case "b":
		return m.openBackups()
	case "d":
		if m.querying {
			return m, nil
		}
		m.querying = true
		m.status = "analizando salud TPS (tarda unos segundos)…"
		return m, healthCmd(m.ctx, m.backend)
	case "?":
		m.mode = modeHelp
		m.status = ""
	case "N":
		return m.openCreate()
	case "R":
		if m.noPreflight {
			return m, nil
		}
		m.status = "comprobando requisitos…"
		return m, requirementsCmd(m.ctx, m.backend)
	}
	return m, nil
}

// handleCreateKey delega en el formulario de creación.
func (m Model) handleCreateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.create == nil {
		m.mode = modeList
		return m, nil
	}
	if msg.String() == "esc" {
		m.mode = modeList
		m.create = nil
		m.status = ""
		m.errText = ""
		return m, nil
	}
	if m.creating {
		return m, nil // ya hay una creación en curso
	}
	cmd, submit := m.create.update(msg)
	if !submit {
		return m, cmd
	}
	req, ok := m.create.request()
	if !ok {
		return m, nil
	}
	m.creating = true
	m.status = "creando instancia…"
	m.errText = ""
	return m, tea.Batch(cmd, createCmd(m.ctx, m.backend, req))
}

// openCreate abre el asistente de creación de instancias.
func (m Model) openCreate() (tea.Model, tea.Cmd) {
	m.mode = modeCreate
	m.create = newCreateForm(m.backend.Engines(), m.backend.DefaultMemory())
	m.status = ""
	m.errText = ""
	return m, textinput.Blink
}

func (m Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.status = ""
	case "q":
		return m.quit()
	case "s":
		return m.runOp(opStart, true)
	case "p":
		return m.runOp(opStop, false)
	case "c":
		return m.runOp(opClose, false)
	case "r":
		return m.runOp(opRestart, false)
	case "l":
		return m.openLogs()
	case "b":
		return m.openBackups()
	case "n":
		return m.runOp(opBackupCreate, false)
	case "e":
		m.mode = modeConsole
		m.status = ""
		m.input.Focus()
		m.input.SetValue("")
	case "?":
		m.mode = modeHelp
		m.status = ""
	}
	return m, nil
}

func (m Model) handleLogsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if isViewportKey(key) {
		var cmd tea.Cmd
		m.logsView, cmd = m.logsView.Update(msg)
		return m, cmd
	}
	switch key {
	case "esc":
		m.mode = modeDetail
		m.status = ""
	case "q":
		return m.quit()
	case "r":
		m.status = "recargando logs…"
		return m, logsCmd(m.ctx, m.backend, m.logSlug, m.logLines)
	}
	return m, nil
}

func (m Model) handleConsoleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "enter" {
		val := strings.TrimSpace(m.input.Value())
		var slug string
		if m.detail != nil && m.detail.Spec != nil {
			slug = m.detail.Spec.Slug
		}
		m.input.Blur()
		m.input.SetValue("")
		m.mode = modeDetail
		if slug == "" || val == "" {
			return m, nil
		}
		m.status = "comando enviado a " + slug + ": " + val
		return m, consoleCmd(m.ctx, m.backend, slug, val)
	}
	if msg.String() == "esc" {
		m.input.Blur()
		m.mode = modeDetail
		m.status = ""
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) handleBackupsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeDetail
		m.status = ""
	case "q":
		return m.quit()
	case "c":
		if _, ok := m.selectedSlug(); !ok {
			return m, nil
		}
		return m.runOp(opBackupCreate, false)
	case "p":
		if _, ok := m.selectedSlug(); !ok {
			return m, nil
		}
		return m.runOp(opPrune, false)
	}
	return m, nil
}

// runOp lanza una operación sobre la instancia seleccionada (asíncrona) y solo
// si esa instancia no está ya ocupada.
func (m Model) runOp(o op, force bool) (tea.Model, tea.Cmd) {
	slug, ok := m.selectedSlug()
	if !ok {
		return m, nil
	}
	if _, busy := m.busy[slug]; busy {
		return m, nil
	}
	m.busy[slug] = o.String()
	m.status = "ejecutando " + o.String() + " en " + slug + "…"
	m.errText = ""
	var cmd tea.Cmd
	if o == opBackupCreate && m.mode == modeBackups {
		cmd = opCmd(m.ctx, m.backend, opBackupCreate, slug, false)
	} else {
		cmd = opCmd(m.ctx, m.backend, o, slug, force)
	}
	return m, cmd
}

func (m Model) openLogs() (tea.Model, tea.Cmd) {
	slug, ok := m.selectedSlug()
	if !ok {
		if m.detail != nil && m.detail.Spec != nil {
			slug = m.detail.Spec.Slug
			ok = true
		}
	}
	if !ok {
		return m, nil
	}
	m.mode = modeLogs
	m.logSlug = slug
	m.status = "cargando logs de " + slug + "…"
	m.stickLogs = true
	m.logsView.GotoTop()
	return m, logsCmd(m.ctx, m.backend, slug, m.logLines)
}

func (m Model) openBackups() (tea.Model, tea.Cmd) {
	slug, ok := m.selectedSlug()
	if !ok {
		if m.detail != nil && m.detail.Spec != nil {
			slug = m.detail.Spec.Slug
			ok = true
		}
	}
	if !ok {
		return m, nil
	}
	m.mode = modeBackups
	m.backupsSlug = slug
	m.backups = nil
	m.status = "cargando backups de " + slug + "…"
	return m, backupsCmd(m.ctx, m.backend, slug)
}

// bodyHeight deja el alto disponible para el cuerpo.
func bodyHeight(h int) int {
	if h < 4 {
		return h
	}
	return h - 3 // cabecera + 2 líneas de pie
}

// isViewportKey indica si la tecla debe ir al viewport (scroll de logs).
func isViewportKey(key string) bool {
	switch key {
	case "up", "down", "pgup", "pgdown", "home", "end", " ":
		return true
	}
	return false
}
