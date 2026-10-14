package tui

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/host"
	"github.com/MrZudro/badomens/internal/idle"
	"github.com/MrZudro/badomens/internal/ports"
	"github.com/MrZudro/badomens/internal/provision"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/help"
	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

type wizardCreatedMsg struct {
	slug string
	err  error
}

type wizardVersionsMsg struct {
	engine   string
	versions []engines.Version
	err      error
}

type wizardHostMsg struct{ cap host.Capacity }

// wizardPortDebounceMsg dispara la comprobación del puerto tras el debounce.
type wizardPortDebounceMsg struct{ seq int }

// wizardPortMsg trae el resultado de comprobar un puerto (RF-3).
type wizardPortMsg struct {
	seq  int
	port int
	res  ports.Result
	err  error
}

// wizField es un campo del asistente con su ayuda (RF-4).
type wizField struct {
	key     string
	kind    string // text | cycle | bool | version
	value   string
	boolVal bool
	options []string
}

// wizStep agrupa los campos de un paso del asistente.
type wizStep struct {
	title  string
	fields []wizField
}

// wizardScreen es el asistente de nueva instancia (rápido/avanzado, RF-4/RF-5).
type wizardScreen struct {
	svc   *svc.Services
	pal   theme.Palette
	ascii bool
	mode  int // -1 = elegir modo, 0 = rápido, 1 = avanzado
	step  int
	steps []wizStep
	cur   int
	edit  bool
	err   string
	input textinput.Model

	engineIdx int
	engine    engines.Engine

	// Versiones (RF-5).
	versions     []engines.Version
	verEngine    string
	verCursor    int
	verLoading   bool
	showSNAPS    bool
	pickingVer   bool
	versionsSeen bool

	// Ayuda larga.
	helpOpen bool

	// Validación en vivo del puerto con debounce (RF-3).
	portSeq     int
	portStatus  string // "", checking, free, busy, warn, invalid, error
	portDetail  string
	portSuggest int

	// Auto-apagado elegido en el asistente.
	cap host.Capacity
}

func newWizardScreen(sv *svc.Services, pal theme.Palette, ascii bool) *wizardScreen {
	in := textinput.New()
	in.Width = 40
	return &wizardScreen{svc: sv, pal: pal, ascii: ascii, mode: -1, input: in, engine: engines.Order[0]}
}

func (w *wizardScreen) Init() tea.Cmd {
	return func() tea.Msg { return wizardHostMsg{cap: host.Detect(context.Background())} }
}

func (w *wizardScreen) start(mode int) {
	w.mode = mode
	w.step, w.cur = 0, 0
	def := func(key, val string) wizField {
		return wizField{key: key, kind: "text", value: val}
	}
	basico := wizStep{title: "Básico", fields: []wizField{
		def("name", ""),
		{key: "engine", kind: "cycle", value: w.engine.Name, options: engineNames()},
		{key: "version", kind: "version", value: ""},
	}}
	if mode == 0 {
		w.steps = []wizStep{
			basico,
			{title: "Recursos", fields: []wizField{def("memory", "4G")}},
			{title: "Revisar", fields: nil},
		}
	} else {
		w.steps = []wizStep{
			basico,
			{title: "Mundo", fields: []wizField{def("seed", "")}},
			{title: "Jugadores", fields: []wizField{
				def("max_players", "20"),
				{key: "difficulty", kind: "cycle", value: "normal", options: []string{"peaceful", "easy", "normal", "hard"}},
				{key: "gamemode", kind: "cycle", value: "survival", options: []string{"survival", "creative", "adventure", "spectator"}},
				{key: "online_mode", kind: "bool", boolVal: false},
				{key: "whitelist", kind: "bool", boolVal: false},
				def("ops", ""),
			}},
			{title: "Recursos", fields: []wizField{def("memory", "4G")}},
			{title: "Red", fields: []wizField{
				{key: "port", kind: "port", value: ""},
				def("motd", ""),
			}},
			{title: "Automatización", fields: []wizField{
				{key: "idle_enabled", kind: "bool", boolVal: false},
				{key: "idle_timeout", kind: "cycle", value: "15m", options: []string{"5m", "10m", "15m", "30m", "1h", "2h"}},
				{key: "start", kind: "bool", boolVal: true},
			}},
			{title: "Revisar", fields: nil},
		}
	}
	w.syncInput()
	w.ensureVersions()
}

func (w *wizardScreen) currentStep() *wizStep {
	if w.step < 0 || w.step >= len(w.steps) {
		return nil
	}
	return &w.steps[w.step]
}

func (w *wizardScreen) currentField() (wizField, bool) {
	st := w.currentStep()
	if st == nil || len(st.fields) == 0 || w.cur < 0 || w.cur >= len(st.fields) {
		return wizField{}, false
	}
	return st.fields[w.cur], true
}

func (w *wizardScreen) setField(f wizField) {
	st := w.currentStep()
	if st == nil || w.cur < 0 || w.cur >= len(st.fields) {
		return
	}
	st.fields[w.cur] = f
}

func (w *wizardScreen) ensureVersions() {
	w.verEngine = w.engine.Name
	w.verLoading = true
	w.versions = nil
	w.verCursor = 0
}

func (w *wizardScreen) versionsCmd() tea.Cmd {
	ref, engine, all := w.svc, w.engine.Name, w.showSNAPS
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		list, err := ref.Catalog.Versions(ctx, engine, all)
		return wizardVersionsMsg{engine: engine, versions: list, err: err}
	}
}

func (w *wizardScreen) syncInput() {
	if w.pickingVer {
		return
	}
	f, ok := w.currentField()
	if !ok || f.kind != "text" {
		w.edit = false
		w.input.Blur()
		return
	}
	w.edit = true
	w.input.SetValue(f.value)
	w.input.Placeholder = fieldExample(f.key)
	_ = w.input.Focus()
}

func (w *wizardScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case wizardHostMsg:
		w.cap = msg.cap
		return w, nil
	case wizardVersionsMsg:
		if msg.engine == w.engine.Name {
			w.versions, w.verLoading = msg.versions, false
			w.versionsSeen = true
			if w.verEngine != msg.engine {
				w.verEngine = msg.engine
			}
			w.pickRecommended()
		}
		return w, nil
	case wizardPortDebounceMsg:
		if msg.seq != w.portSeq {
			return w, nil
		}
		return w, w.checkPortCmd(msg.seq)
	case wizardPortMsg:
		if msg.seq != w.portSeq {
			return w, nil
		}
		w.applyPortResult(msg)
		return w, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		if w.edit {
			var c tea.Cmd
			w.input, c = w.input.Update(msg)
			return w, c
		}
		return w, nil
	}
	if w.mode < 0 {
		switch km.String() {
		case "r":
			w.start(0)
			return w, w.versionsCmd()
		case "a":
			w.start(1)
			return w, w.versionsCmd()
		case "esc":
			return w, func() tea.Msg { return popScreenMsg{} }
		}
		return w, nil
	}
	if w.helpOpen {
		w.helpOpen = false
		return w, nil
	}
	if w.pickingVer {
		return w.handleVersionPicker(km)
	}
	return w.handleStepKey(km)
}

func (w *wizardScreen) handleVersionPicker(km tea.KeyMsg) (Screen, tea.Cmd) {
	switch km.String() {
	case "esc":
		w.pickingVer = false
		w.syncInput()
	case "j", "down":
		if w.verCursor < len(w.versions)-1 {
			w.verCursor++
		}
	case "k", "up":
		if w.verCursor > 0 {
			w.verCursor--
		}
	case "s":
		w.showSNAPS = !w.showSNAPS
		w.verLoading = true
		return w, w.versionsCmd()
	case "enter":
		if w.verCursor >= 0 && w.verCursor < len(w.versions) {
			v := w.versions[w.verCursor]
			if !v.Compatible {
				w.err = "esa versión no se puede usar: " + v.IncompatibleWhy
				return w, nil
			}
			f, _ := w.currentField()
			f.value = v.ID()
			w.setField(f)
			w.pickingVer = false
			return w, nil
		}
	}
	return w, nil
}

func (w *wizardScreen) handleStepKey(km tea.KeyMsg) (Screen, tea.Cmd) {
	if w.edit {
		switch km.String() {
		case "enter", "tab":
			isPort := w.editingPort()
			w.commitTextField()
			var c tea.Cmd
			if isPort {
				c = w.schedulePort()
			}
			w.advance()
			return w, c
		case "esc":
			w.commitTextField()
			w.edit = false
			w.input.Blur()
			return w, nil
		}
		var c tea.Cmd
		w.input, c = w.input.Update(km)
		if w.editingPort() {
			c = tea.Batch(c, w.schedulePort())
		}
		return w, c
	}
	switch km.String() {
	case "esc":
		if w.step > 0 {
			w.step--
			w.cur = 0
			w.syncInput()
			return w, nil
		}
		return w, func() tea.Msg { return popScreenMsg{} }
	case "tab":
		w.advance()
	case "shift+tab":
		if w.step > 0 {
			w.step--
			w.cur = 0
			w.syncInput()
		}
	case "down", "j":
		if st := w.currentStep(); st != nil && len(st.fields) > 0 {
			w.cur++
			if w.cur >= len(st.fields) {
				w.advance()
				return w, nil
			}
			w.syncInput()
		}
	case "up", "k":
		if st := w.currentStep(); st != nil && len(st.fields) > 0 {
			w.cur--
			if w.cur < 0 {
				w.cur = 0
			}
			w.syncInput()
		}
	case "left":
		w.cycle(-1)
	case "right":
		w.cycle(1)
	case " ":
		w.toggle()
	case "?":
		w.helpOpen = true
	case "w":
		w.enableWhitelist()
	case "p":
		if f, ok := w.currentField(); ok && f.kind == "port" && w.portSuggest > 0 {
			w.setPortField(strconv.Itoa(w.portSuggest))
			return w, w.schedulePort()
		}
	case "enter":
		return w.enterField()
	}
	return w, nil
}

// commitTextField vuelca el valor escrito al campo activo.
func (w *wizardScreen) commitTextField() {
	f, ok := w.currentField()
	if !ok || f.kind != "text" {
		return
	}
	val := strings.TrimSpace(w.input.Value())
	switch f.key {
	case "max_players":
		if n, ok := intValue(val); ok {
			val = strconv.Itoa(n)
		}
	case "port":
		if n, ok := intValue(val); ok && n >= 1 && n <= 65535 {
			val = strconv.Itoa(n)
		}
	}
	f.value = val
	w.setField(f)
}

// --- Validación en vivo del puerto con debounce (RF-3) ---

// schedulePort arranca la comprobación del puerto 300 ms después del último
// cambio, para no lanzar un sondeo por pulsación.
func (w *wizardScreen) schedulePort() tea.Cmd {
	w.portSeq++
	seq := w.portSeq
	w.portStatus, w.portDetail, w.portSuggest = "checking", "", 0
	return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg { return wizardPortDebounceMsg{seq: seq} })
}

// editingPort indica si el campo activo es el puerto en edición.
func (w *wizardScreen) editingPort() bool {
	f, ok := w.currentField()
	return ok && f.kind == "port" && w.edit
}

// portFieldValue devuelve el valor actual del puerto (editándose o guardado).
func (w *wizardScreen) portFieldValue() string {
	if w.editingPort() {
		return strings.TrimSpace(w.input.Value())
	}
	for _, st := range w.steps {
		for _, f := range st.fields {
			if f.key == "port" {
				return f.value
			}
		}
	}
	return ""
}

// setPortField fija el valor del puerto en el campo (si existe).
func (w *wizardScreen) setPortField(v string) {
	for si := range w.steps {
		for fi := range w.steps[si].fields {
			if w.steps[si].fields[fi].key == "port" {
				w.steps[si].fields[fi].value = v
				if w.editingPort() {
					w.input.SetValue(v)
				}
				return
			}
		}
	}
}

// checkPortCmd comprueba el puerto actual contra el Checker (RF-3).
func (w *wizardScreen) checkPortCmd(seq int) tea.Cmd {
	raw := w.portFieldValue()
	if raw == "" {
		w.portStatus = ""
		return nil
	}
	port, ok := intValue(raw)
	if !ok || port < 1 || port > 65535 {
		w.portStatus, w.portDetail = "invalid", "el puerto debe ser un número entre 1 y 65535"
		return nil
	}
	ref := w.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		res, err := ref.Ports.Check(ctx, port)
		return wizardPortMsg{seq: seq, port: port, res: res, err: err}
	}
}

// applyPortResult traduce el resultado de la comprobación a un estado visible.
func (w *wizardScreen) applyPortResult(msg wizardPortMsg) {
	if msg.err != nil {
		w.portStatus, w.portDetail, w.portSuggest = "error", msg.err.Error(), 0
		return
	}
	w.portSuggest = msg.res.Suggested
	if msg.res.Free {
		w.portStatus, w.portDetail = "free", "libre"
		return
	}
	w.portStatus, w.portDetail = "busy", "ocupado"
	for _, cf := range msg.res.Conflicts {
		if cf.Severity == ports.SeverityBlock {
			w.portStatus = "busy"
			if cf.Owner.Detail != "" {
				w.portDetail = "ocupado por " + cf.Owner.Detail
			}
			return
		}
		if cf.Severity == ports.SeverityWarn {
			w.portStatus = "warn"
			w.portDetail = "responde en la tailnet: " + cf.Owner.Detail
		}
	}
	if w.portStatus == "busy" && w.portSuggest > 0 {
		w.portDetail += " — sugerimos " + itoa(w.portSuggest)
	}
}

// portColor elige el color de la línea de validación del puerto.
func (w *wizardScreen) portColor() string {
	switch w.portStatus {
	case "free":
		return w.pal.Good
	case "busy", "invalid", "error":
		return w.pal.Bad
	case "warn":
		return w.pal.Warn
	}
	return w.pal.Muted
}

// portStatusLine compone la línea de validación del puerto (RF-3).
func (w *wizardScreen) portStatusLine() string {
	switch w.portStatus {
	case "":
		return ""
	case "checking":
		return "    ⟳ Comprobando…"
	case "free":
		return "    " + components.Check(w.ascii) + " Libre"
	case "invalid":
		return "    " + components.Cross(w.ascii) + " " + w.portDetail
	case "error":
		return "    " + components.Cross(w.ascii) + " No se pudo comprobar: " + w.portDetail
	case "warn":
		return "    " + components.Warn(w.ascii) + " " + w.portDetail + " (aviso)"
	case "busy":
		line := "    " + components.Cross(w.ascii) + " " + w.portDetail
		if w.portSuggest > 0 {
			line += " · [P] usar " + itoa(w.portSuggest)
		}
		return line
	}
	return ""
}

// advance pasa al siguiente campo o, si es el último, al siguiente paso.
func (w *wizardScreen) advance() {
	st := w.currentStep()
	if st != nil && w.cur < len(st.fields)-1 {
		w.cur++
		w.syncInput()
		return
	}
	w.nextStep()
}

// enterField activa edición o avanza de paso si es "Revisar".
func (w *wizardScreen) enterField() (Screen, tea.Cmd) {
	if w.step == len(w.steps)-1 {
		return w, w.create()
	}
	f, ok := w.currentField()
	if !ok {
		if w.nextStep() {
			return w, nil
		}
		return w, nil
	}
	switch f.kind {
	case "version":
		w.pickingVer = true
		if len(w.versions) == 0 {
			w.verLoading = true
			return w, w.versionsCmd()
		}
	case "cycle":
		w.cycle(1)
	case "bool":
		w.toggle()
	default:
		w.edit = true
		w.input.SetValue(f.value)
		return w, w.input.Focus()
	}
	return w, nil
}

func (w *wizardScreen) cycle(d int) {
	f, ok := w.currentField()
	if !ok || f.kind != "cycle" {
		return
	}
	if f.key == "engine" {
		w.engineIdx = (w.engineIdx + len(engines.Order) + d) % len(engines.Order)
		w.engine = engines.Order[w.engineIdx]
		f.value = w.engine.Name
		// Cambio de motor: se recarga la lista de versiones y se elige la
		// recomendada del nuevo motor (RF-5).
		w.ensureVersions()
		w.setField(f)
		if v, ok := w.currentStep().findField("version"); ok {
			v.value = ""
			w.steps[w.step].fields = replaceField(w.steps[w.step].fields, v)
		}
		w.versionsCmd()
		return
	}
	idx := 0
	for i, o := range f.options {
		if o == f.value {
			idx = i
			break
		}
	}
	idx = (idx + len(f.options) + d) % len(f.options)
	f.value = f.options[idx]
	w.setField(f)
}

func (w *wizardScreen) toggle() {
	f, ok := w.currentField()
	if !ok || f.kind != "bool" {
		return
	}
	f.boolVal = !f.boolVal
	w.setField(f)
}

func (w *wizardScreen) enableWhitelist() {
	st := w.currentStep()
	if st == nil {
		return
	}
	for i, f := range st.fields {
		if f.key == "whitelist" {
			f.boolVal = true
			st.fields[i] = f
		}
	}
}

func (w *wizardScreen) nextStep() bool {
	if w.step < len(w.steps)-1 {
		w.step++
		w.cur = 0
		w.syncInput()
		return true
	}
	return false
}

func (w *wizardScreen) pickRecommended() {
	// Marca la versión recomendada del motor como elección por defecto.
	for _, v := range w.versions {
		if v.Recommended && v.Compatible {
			st := w.currentStep()
			if st == nil {
				return
			}
			for i, f := range st.fields {
				if f.key == "version" && f.value == "" {
					f.value = v.ID()
					st.fields[i] = f
				}
			}
			return
		}
	}
}

// buildRequest reúne los valores del asistente en una petición de creación.
func (w *wizardScreen) buildRequest() provision.Request {
	req := provision.Request{Engine: w.engine.Name, Start: true}
	for _, st := range w.steps {
		for _, f := range st.fields {
			switch f.key {
			case "name":
				req.Name = f.value
			case "version":
				req.Version = f.value
			case "seed":
				req.Seed = f.value
			case "memory":
				req.Memory = f.value
			case "motd":
				req.Motd = f.value
			case "difficulty":
				req.Difficulty = f.value
			case "gamemode":
				req.Gamemode = f.value
			case "max_players":
				if n, ok := intValue(f.value); ok {
					req.MaxPlayers = n
				}
			case "port":
				if n, ok := intValue(f.value); ok {
					req.Port = n
				}
			case "online_mode":
				req.Online = f.boolVal
			case "whitelist":
				req.WhitelistEnabled = f.boolVal
			case "ops":
				for _, o := range strings.Split(f.value, ",") {
					if t := strings.TrimSpace(o); t != "" {
						req.Ops = append(req.Ops, t)
					}
				}
			case "start":
				req.Start = f.boolVal
			}
		}
	}
	return req
}

func (w *wizardScreen) idleConfig() (idle.Config, bool) {
	for _, st := range w.steps {
		for i, f := range st.fields {
			if f.key == "idle_enabled" && f.boolVal {
				cfg := idle.DefaultConfig()
				cfg.Enabled = true
				for _, g := range st.fields {
					if g.key == "idle_timeout" {
						if d, err := idle.ParseDuration(g.value); err == nil {
							cfg.Timeout = d
						}
					}
				}
				_ = i
				return cfg.Normalized(), true
			}
		}
	}
	return idle.Config{}, false
}

func (w *wizardScreen) create() tea.Cmd {
	req := w.buildRequest()
	if strings.TrimSpace(req.Name) == "" {
		w.err = "el nombre es obligatorio"
		w.step = 0
		w.cur = 0
		w.syncInput()
		return nil
	}
	ref := w.svc
	cfg, wantIdle := w.idleConfig()
	return tea.Batch(busyCmd(true), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		slug, err := ref.Creator.Create(ctx, req)
		if err != nil {
			return wizardCreatedMsg{slug: slug, err: err}
		}
		if wantIdle {
			_ = ref.Idle.Set(ctx, slug, cfg)
		}
		return wizardCreatedMsg{slug: slug}
	})
}

func (w *wizardScreen) View(width, height int) string {
	if w.mode < 0 {
		var b strings.Builder
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(w.pal.Accent)).Render("Nueva instancia"))
		b.WriteString("\n\nElige cómo configurarla:\n\n")
		b.WriteString(components.Button(components.Action{Verb: "Configuración", Object: "rápida (3 preguntas)", Key: "r", Hint: "Nombre, motor y memoria.", Kind: components.Primary}, true, w.pal))
		b.WriteString("\n")
		b.WriteString(components.Button(components.Action{Verb: "Configuración", Object: "avanzada (todas las opciones)", Key: "a", Hint: "Los 7 pasos, con ayuda por campo.", Kind: components.Secondary}, false, w.pal))
		b.WriteString("\n\nLos valores recomendados aparecen en el resumen antes de crear.")
		return b.String()
	}
	var b strings.Builder
	titles := make([]string, len(w.steps))
	for i, st := range w.steps {
		titles[i] = st.title
	}
	b.WriteString(components.Stepper(titles, w.step, w.pal))
	b.WriteString("\n\n")

	wide := width >= 100
	formW := width
	if wide {
		formW = width * 3 / 5
	}
	form := w.formView(formW)
	if wide {
		side := w.helpPanel(width - formW - 4)
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, form, "   ", side))
	} else {
		b.WriteString(form)
	}
	if w.helpOpen {
		b.WriteString("\n\n" + w.longHelp())
	}
	if w.err != "" {
		b.WriteString("\n\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Bad)).Render(components.Warn(w.ascii)+" "+w.err))
	}
	return b.String()
}

func (w *wizardScreen) formView(width int) string {
	var b strings.Builder
	if w.step == len(w.steps)-1 {
		b.WriteString(w.reviewView())
		return b.String()
	}
	st := w.currentStep()
	for i, f := range st.fields {
		h, _ := help.Get(f.key)
		label := h.Title
		if label == "" {
			label = f.key
		}
		marker, style := "  ", lipgloss.NewStyle()
		if i == w.cur && !w.pickingVer {
			marker = components.Pointer(w.ascii) + " "
			style = style.Bold(true).Foreground(lipgloss.Color(w.pal.Accent))
		}
		val := w.fieldValue(f)
		b.WriteString(marker + style.Render(label) + ": " + val + "\n")
		if h.Short != "" {
			b.WriteString("    " + lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Muted)).Render(layout.Truncate(h.Short, maxi(width-4, 20))) + "\n")
		}
		if f.key == "port" {
			if line := w.portStatusLine(); line != "" {
				b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(w.portColor())).Render(line) + "\n")
			}
		}
	}
	// Aviso P-05: modo offline sin whitelist.
	if w.offlineRisk() {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Warn)).Render(components.Warn(w.ascii)+" Riesgo de suplantación: sin modo online y sin lista blanca, cualquiera entra. Pulsa [W] para activar la lista blanca.") + "\n")
	}
	if w.pickingVer {
		b.WriteString("\n" + w.versionPicker(width))
	}
	return b.String()
}

func (w *wizardScreen) fieldValue(f wizField) string {
	switch f.kind {
	case "bool":
		return onOff(f.boolVal)
	case "version":
		if f.value == "" {
			if w.verLoading {
				return "cargando versiones…"
			}
			return "(elige una versión: Enter)"
		}
		return f.value
	case "port":
		if w.edit {
			if cf, ok := w.currentField(); ok && cf.key == f.key {
				return w.input.View()
			}
		}
		if strings.TrimSpace(f.value) == "" {
			return "(automático)"
		}
		return f.value
	}
	if w.edit {
		if cf, ok := w.currentField(); ok && cf.key == f.key {
			return w.input.View()
		}
	}
	return f.value
}

// versionPicker pinta la lista de versiones del motor (RF-5).
func (w *wizardScreen) versionPicker(width int) string {
	var b strings.Builder
	head := "Versiones de " + w.engine.Label
	if w.showSNAPS {
		head += " (con snapshots/beta)"
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(w.pal.Accent)).Render(head) + "\n")
	if w.verLoading {
		return b.String() + "  ⟳ cargando catálogo…\n"
	}
	if len(w.versions) == 0 {
		return b.String() + "  (sin versiones; se usará el catálogo embebido)\n"
	}
	shown := 0
	for i, v := range w.versions {
		if shown >= 8 {
			break
		}
		shown++
		star := "  "
		if v.Recommended {
			star = "★ "
		}
		detail := versionDetail(v)
		line := star + v.ID() + "  " + detail
		if i == w.verCursor {
			b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(w.pal.AccentText)).Background(lipgloss.Color(w.pal.Accent)).Render(components.Pointer(w.ascii)+" "+layout.Truncate(line, width-3)) + "\n")
		} else if !v.Compatible {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Muted)).Render("  "+layout.Truncate(line+"  ✖ "+v.IncompatibleWhy, width-3)) + "\n")
		} else {
			b.WriteString("  " + layout.Truncate(line, width-3) + "\n")
		}
	}
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Muted)).Render("  [S] " + snapToggle(w.showSNAPS) + " · [Enter] Elegir · [Esc] Cerrar"))
	return b.String()
}

func versionDetail(v engines.Version) string {
	parts := []string{}
	if v.Channel != "" && v.Channel != engines.ChannelRelease {
		parts = append(parts, string(v.Channel))
	}
	if v.JavaMajor > 0 {
		parts = append(parts, "Java "+strconv.Itoa(v.JavaMajor))
	}
	if v.Build != "" {
		parts = append(parts, "build "+v.Build)
	}
	if !v.ReleasedAt.IsZero() {
		parts = append(parts, sinceText(v.ReleasedAt))
	}
	return strings.Join(parts, " · ")
}

func snapToggle(on bool) string {
	if on {
		return "ocultar snapshots"
	}
	return "mostrar snapshots"
}

func (w *wizardScreen) reviewView() string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Revisar y crear") + "\n\n")
	for _, st := range w.steps {
		if st.title == "Revisar" {
			continue
		}
		for _, f := range st.fields {
			h, _ := help.Get(f.key)
			label := h.Title
			if label == "" {
				label = f.key
			}
			b.WriteString("  " + label + ": " + w.reviewValue(f) + "\n")
		}
	}
	if !w.hasPortField() {
		b.WriteString("  Puerto: automático (el primero libre)\n")
	}
	if w.cap.NCPU > 0 {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Muted)).Render("Capacidad: "+strconv.Itoa(w.cap.NCPU)+" CPU · "+trimFloat(w.cap.MemTotalGiB())+" GiB RAM") + "\n")
	}
	b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Muted)).Render("[Enter] Crear y arrancar · [Esc] Volver a editar"))
	return b.String()
}

func (w *wizardScreen) helpPanel(width int) string {
	str := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(w.pal.Accent)).Render("¿Qué es esto?") + "\n\n"
	f, ok := w.currentField()
	if !ok {
		return lipgloss.NewStyle().Width(maxi(width, 20)).Render(str)
	}
	h, _ := help.Get(f.key)
	if h.Long != "" {
		str += wrapText(h.Long, maxi(width-2, 20))
	}
	if h.Example != "" {
		str += "\n\nEjemplo: " + h.Example
	}
	if h.Default != "" {
		str += "\nPor defecto: " + h.Default
	}
	if len(h.Impact) > 0 {
		str += "\n\n" + impactText(h.Impact)
	}
	return lipgloss.NewStyle().Border(components.Border(w.ascii)).BorderForeground(lipgloss.Color(w.pal.Border)).Padding(0, 1).Width(maxi(width-2, 18)).Render(str)
}

func (w *wizardScreen) longHelp() string {
	f, ok := w.currentField()
	if !ok {
		return ""
	}
	h, _ := help.Get(f.key)
	box := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(w.pal.Accent)).Render(h.Title) + "\n\n" + h.Long
	return lipgloss.NewStyle().Border(components.Border(w.ascii)).BorderForeground(lipgloss.Color(w.pal.Border)).Padding(0, 1).Render(box)
}

// offlineRisk indica que se creará en modo offline sin lista blanca (P-05).
func (w *wizardScreen) offlineRisk() bool {
	var online, whitelist bool
	seen := false
	for _, st := range w.steps {
		for _, f := range st.fields {
			if f.key == "online_mode" {
				online, seen = f.boolVal, true
			}
			if f.key == "whitelist" {
				whitelist = f.boolVal
			}
		}
	}
	return seen && !online && !whitelist
}

// reviewValue pinta el valor de un campo en el resumen, con un marcador claro
// cuando está vacío (para que el estado no dependa de un hueco en blanco).
func (w *wizardScreen) reviewValue(f wizField) string {
	if f.kind == "bool" {
		return onOff(f.boolVal)
	}
	v := strings.TrimSpace(f.value)
	if v != "" {
		return v
	}
	switch f.key {
	case "ops":
		return "(ninguno)"
	case "port":
		return "(automático)"
	case "memory":
		return "(recomendada)"
	case "version":
		return "(recomendada)"
	}
	return "(auto)"
}

// hasPortField indica si el asistente incluye el campo de puerto.
func (w *wizardScreen) hasPortField() bool {
	for _, st := range w.steps {
		if _, ok := st.findField("port"); ok {
			return true
		}
	}
	return false
}

func (w *wizardScreen) Actions() []components.Action {
	if w.mode < 0 {
		return []components.Action{
			{ID: "wizard.quick", Verb: "Configuración", Object: "rápida", Key: "r", Hint: "Tres preguntas y listo.", Kind: components.Primary},
			{ID: "wizard.advanced", Verb: "Configuración", Object: "avanzada", Key: "a", Hint: "Todas las opciones con ayuda.", Kind: components.Secondary},
			{ID: "wizard.cancel", Verb: "Volver", Object: "sin crear", Key: "esc", Hint: "Cancela el asistente.", Kind: components.Navigation},
		}
	}
	out := []components.Action{
		{ID: "wizard.next", Verb: "Siguiente:", Object: "Revisar", Key: "tab", Hint: "Avanza al siguiente paso.", Kind: components.Primary},
	}
	if w.portSuggest > 0 {
		out = append(out, components.Action{
			ID: "wizard.useport", Verb: "Usar", Object: "el puerto sugerido " + itoa(w.portSuggest),
			Key: "p", Hint: "Asigna ese puerto libre en lugar del ocupado.", Kind: components.Secondary,
		})
	}
	out = append(out, components.Action{ID: "wizard.cancel", Verb: "Volver", Object: "sin crear", Key: "esc", Hint: "Cancela el asistente.", Kind: components.Navigation})
	return out
}

// Invoke ejecuta una acción del asistente por su ID.
func (w *wizardScreen) Invoke(id string) tea.Cmd {
	switch id {
	case "wizard.quick":
		w.start(0)
		return w.versionsCmd()
	case "wizard.advanced":
		w.start(1)
		return w.versionsCmd()
	case "wizard.next":
		w.nextStep()
	case "wizard.useport":
		if w.portSuggest > 0 {
			w.setPortField(itoa(w.portSuggest))
			return w.schedulePort()
		}
	case "wizard.cancel":
		return func() tea.Msg { return popScreenMsg{} }
	case "wizard.create":
		return w.create()
	}
	return nil
}

func (w *wizardScreen) Title() string { return "Nueva instancia" }
func (w *wizardScreen) ID() string    { return "wizard" }

// NavSection coloca el asistente en la sección de instancias.
func (w *wizardScreen) NavSection() string { return "instances" }

// CapturesInput: el asistente es un formulario; con un modo activo o con la
// ayuda abierta consume las teclas imprimibles y no deja pasar atajos globales.
func (w *wizardScreen) CapturesInput() bool { return w.helpOpen || w.pickingVer || w.mode >= 0 }

func engineNames() []string {
	out := make([]string, 0, len(engines.Order))
	for _, e := range engines.Order {
		out = append(out, e.Name)
	}
	return out
}

func fieldExample(key string) string {
	if h, ok := help.Get(key); ok {
		return h.Example
	}
	return ""
}

func impactText(tags []help.Tag) string {
	names := map[help.Tag]string{
		help.TagRequiresRestart:    "Requiere reiniciar",
		help.TagOnlyAtCreation:     "Solo al crear",
		help.TagAffectsPerformance: "Afecta al rendimiento",
		help.TagAffectsSecurity:    "Afecta a la seguridad",
		help.TagAffectsPlayers:     "Afecta a los jugadores",
	}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if n, ok := names[t]; ok {
			out = append(out, n)
		}
	}
	return strings.Join(out, " · ")
}

func sinceText(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < 24*time.Hour:
		return "hoy"
	case d < 30*24*time.Hour:
		return "hace " + strconv.Itoa(int(d.Hours()/24)) + " días"
	case d < 365*24*time.Hour:
		return "hace " + strconv.Itoa(int(d.Hours()/24/30)) + " meses"
	default:
		return "hace " + strconv.Itoa(int(d.Hours()/24/365)) + " años"
	}
}

func wrapText(s string, width int) string {
	if width < 10 {
		width = 10
	}
	words := strings.Fields(s)
	var b strings.Builder
	line := 0
	for i, word := range words {
		if line > 0 && line+1+len([]rune(word)) > width {
			b.WriteString("\n")
			line = 0
		} else if i > 0 {
			b.WriteString(" ")
			line++
		}
		b.WriteString(word)
		line += len([]rune(word))
	}
	return b.String()
}

func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// findField busca un campo por clave dentro del paso.
func (st wizStep) findField(key string) (wizField, bool) {
	for _, f := range st.fields {
		if f.key == key {
			return f, true
		}
	}
	return wizField{}, false
}

func replaceField(fields []wizField, f wizField) []wizField {
	for i := range fields {
		if fields[i].key == f.key {
			fields[i] = f
			return fields
		}
	}
	return fields
}
