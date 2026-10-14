package tui

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

type instanceLogsMsg struct {
	slug string
	text string
	err  error
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

// instanceScreen es el detalle de una instancia con pestañas (RF-1/§5.2).
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

	prompt      bool
	promptLabel string
	promptAct   string

	pendingSave *instance.Spec
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
	in.Placeholder = "comando para la consola (ej. say hola)"
	in.Width = 50
	return &instanceScreen{svc: sv, pal: pal, ascii: ascii, slug: slug, input: in, status: "cargando…", follow: true}
}

func (s *instanceScreen) Init() tea.Cmd {
	return tea.Batch(s.load(), s.loadIdle())
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
	ref, slug := s.svc, s.slug
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		text, err := ref.Instances.Logs(ctx, slug, 200)
		return instanceLogsMsg{slug: slug, text: text, err: err}
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
		s.detail = msg.d
		if msg.err != nil {
			s.status = msg.err.Error()
		} else {
			s.status = ""
			if s.tab == 1 {
				return s, s.logsCmd()
			}
		}
	case instanceLogsMsg:
		if msg.slug == s.slug {
			s.logs = msg.text
		}
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
	if s.focus {
		return s.handleConsoleKey(msg)
	}
	switch msg.String() {
	case "esc":
		return s, func() tea.Msg { return popScreenMsg{} }
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
	s.tab = (s.tab + len(instanceTabs) + d) % len(instanceTabs)
	switch s.tab {
	case 1:
		return s, s.logsCmd()
	case 2:
		return s, s.playersCmd()
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
	if act == "__search__" {
		s.filter = val
		return nil
	}
	return s.consoleCmd(act + " " + val)
}

func (s *instanceScreen) lifecycle(op string) tea.Cmd {
	ref, slug := s.svc, s.slug
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
		s.prompt = true
		s.promptAct = "whitelist add"
		s.promptLabel = "Nombre del jugador para la lista blanca"
		s.input.Placeholder = "nombre"
		return s, s.input.Focus()
	case "o":
		s.prompt = true
		s.promptAct = "op"
		s.promptLabel = "Nombre del jugador a hacer operador"
		s.input.Placeholder = "nombre"
		return s, s.input.Focus()
	case "R":
		return s, s.playersCmd()
	}
	return s, nil
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

// View pinta el detalle con pestañas.
func (s *instanceScreen) View(w, h int) string {
	var b strings.Builder
	name := s.slug
	if s.detail != nil && s.detail.Spec != nil {
		name = s.detail.Spec.Name
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(s.pal.Accent)).Render(name))
	b.WriteString("  ")
	tabLine := ""
	for i, tab := range instanceTabs {
		style := lipgloss.NewStyle()
		if i == s.tab {
			style = style.Bold(true).Foreground(lipgloss.Color(s.pal.AccentText)).Background(lipgloss.Color(s.pal.Accent))
		} else {
			style = style.Foreground(lipgloss.Color(s.pal.Muted))
		}
		tabLine += style.Render(" "+tab+" ") + " "
	}
	b.WriteString(layout.Truncate(tabLine, w-lipgloss.Width(name)-2))
	b.WriteString("\n\n")
	switch s.tab {
	case 0:
		b.WriteString(s.summaryView())
	case 1:
		b.WriteString(s.consoleView(w, h))
	case 2:
		b.WriteString(s.playersView())
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
	if s.pending != "" {
		b.WriteString("\n\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Warn)).Render("⚠ Comando peligroso: «"+s.pending+"». Pulsa Enter otra vez para confirmar, o Esc para cancelar."))
	}
	return b.String()
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
	return b.String()
}

func (s *instanceScreen) consoleView(w, h int) string {
	var b strings.Builder
	lines := s.filteredLogs()
	max := h - 7
	if max < 1 {
		max = 1
	}
	if s.follow && len(lines) > max {
		lines = lines[len(lines)-max:]
	}
	for _, l := range lines {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(logColor(l, s.pal))).Render(l))
		b.WriteString("\n")
	}
	flags := "seguir=" + onOff(s.follow) + "  pausa=" + onOff(s.paused) + "  filtro=" + orDash(s.filter)
	b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render(flags))
	if s.pending == "" {
		b.WriteString("\n> " + s.input.View())
	}
	return b.String()
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

func (s *instanceScreen) playersView() string {
	if s.detail == nil || s.detail.Spec == nil {
		return "cargando…"
	}
	spec := s.detail.Spec
	var b strings.Builder
	online := "no se pudo medir (¿servidor apagado?)"
	if s.playersKnown {
		online = itoa(s.playersOnline) + " conectados ahora"
	}
	b.WriteString("En línea: " + online + "\n\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Lista blanca") + " (" + onOff(spec.WhitelistEnabled) + ")\n")
	if len(spec.Whitelist) == 0 {
		b.WriteString("  (vacía)\n")
	}
	for _, p := range spec.Whitelist {
		b.WriteString("  • " + p + "\n")
	}
	b.WriteString("\n" + lipgloss.NewStyle().Bold(true).Render("Operadores") + "\n")
	if len(spec.Ops) == 0 {
		b.WriteString("  (ninguno)\n")
	}
	for _, p := range spec.Ops {
		b.WriteString("  • " + p + "\n")
	}
	b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("[A] Añadir a lista blanca · [O] Hacer operador · [R] Actualizar"))
	return b.String()
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
	return []components.Action{
		{ID: "instance.start", Verb: "Iniciar", Object: name, Key: "s", Hint: "Arranca el servidor.", Kind: components.Primary, Disabled: running, DisabledReason: "Ya está en marcha"},
		{ID: "instance.stop", Verb: "Detener", Object: name, Key: "p", Hint: "Apaga guardando el mundo.", Kind: components.Secondary, Disabled: !running, DisabledReason: "No está en marcha"},
		{ID: "instance.restart", Verb: "Reiniciar", Object: name, Key: "r", Hint: "Recrea el contenedor aplicando cambios.", Kind: components.Secondary, Disabled: !running, DisabledReason: "No está en marcha"},
		{ID: "instance.console", Verb: "Escribir", Object: "en la consola", Key: "e", Hint: "Envía un comando al servidor.", Kind: components.Secondary},
		{ID: "instance.copy", Verb: "Copiar", Object: "dirección de conexión", Key: "c", Hint: "Copia ip:puerto al portapapeles.", Kind: components.Secondary},
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
