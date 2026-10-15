package tui

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/host"
	"github.com/MrZudro/badomens/internal/idle"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/ports"
	"github.com/MrZudro/badomens/internal/provision"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/help"
	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

type wizardCreatedMsg struct {
	slug string
	// outcome dice si la creación terminó, falló o quedó parcialmente aplicada
	// (AC-10): la configuración puede estar escrita aunque falle el arranque.
	outcome result.Outcome
	err     error
	// mismatch explica que la configuración PERSISTIDA no coincide con el resumen
	// revisado ("" = coincide): nunca se informa de un éxito silencioso sobre una
	// configuración distinta de la revisada (AD-02/T-08).
	mismatch string
}

type wizardVersionsMsg struct {
	engine string
	snaps  bool
	// seq es el número de secuencia de la petición: permite DESCARTAR una
	// respuesta tardía de una consulta que ya quedó obsoleta (§7).
	seq      int
	versions []engines.Version
	// stale etiqueta una lista que procede de caché vencida o del catálogo
	// embebido; status y warning explican su antigüedad y su incertidumbre.
	stale   bool
	status  engines.CatalogStatus
	warning string
	err     error
}

// wizardResolvedMsg trae la resolución (pins finales) del servicio para el motor
// y la versión pedidos. Lleva motor, versión y secuencia para poder descartar una
// respuesta que ya no corresponde a la selección vigente (§7/AD-02).
type wizardResolvedMsg struct {
	engine  string
	version string
	seq     int
	res     engines.Resolved
	err     error
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
	kind    string // text | cycle | bool | version | port
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
	verErr       string
	showSNAPS    bool
	pickingVer   bool
	versionsSeen bool
	// verStale, verWarning y verAge describen la frescura de la lista mostrada:
	// una caché vencida se muestra ETIQUETADA, nunca como lo más reciente
	// confirmado.
	verStale   bool
	verWarning string
	verAge     string
	// pickerW/pickerCols recuerdan el ancho y las columnas del último render del
	// panel de versiones: la navegación en rejilla (4 flechas) los necesita.
	pickerW    int
	pickerCols int
	// verCache guarda las listas ya pedidas, por motor + snapshots: volver a un
	// motor ya visto no repite la petición.
	verCache map[string][]engines.Version
	// verSeq numera las peticiones de catálogo: las respuestas de una petición
	// anterior se descartan (§7).
	verSeq int

	// Resolución VIGENTE del servicio (motor + versión): el resumen y el payload
	// de creación se construyen SOLO desde aquí, nunca desde índices ni valores
	// por defecto (AD-02). Se descarta al cambiar de motor o de versión.
	resolution *engines.Resolved
	resEngine  string
	// resVersion es la versión PEDIDA con la que se obtuvo la resolución (puede
	// ser "" si el servicio eligió la última estable): dice si sigue vigente.
	resVersion string
	resSeq     int
	resolving  bool
	resErr     string

	// Ayuda larga del campo (F1/?).
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
	return &wizardScreen{
		svc: sv, pal: pal, ascii: ascii, mode: -1, input: in,
		engine: engines.Order[0], verCache: map[string][]engines.Version{},
	}
}

func (w *wizardScreen) Init() tea.Cmd {
	return func() tea.Msg { return wizardHostMsg{cap: host.Detect(context.Background())} }
}

// start prepara los pasos del modo elegido y devuelve el Cmd que carga el
// catálogo de versiones (nil si el motor ya estaba en la caché, RF-5).
func (w *wizardScreen) start(mode int) tea.Cmd {
	w.mode = mode
	w.step, w.cur = 0, 0
	w.err = ""
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
	// Cada paso termina en la celda «Continuar a X →»: SOLO Enter sobre ella (o
	// Tab) avanza de paso; ↓ nunca cambia de paso (P-02/UX-02/UX-03).
	w.steps = withContinueCells(w.steps)
	w.clearResolution()
	w.syncInput()
	return w.loadVersions()
}

// continueKey identifica la celda que cierra la lista de campos de un paso.
const continueKey = "__continue__"

// withContinueCells añade la celda «Continuar a X →» al final de cada paso salvo
// el último (el resumen se confirma creando).
func withContinueCells(steps []wizStep) []wizStep {
	for i := range steps {
		if i == len(steps)-1 {
			continue
		}
		steps[i].fields = append(steps[i].fields, wizField{key: continueKey, kind: "continue"})
	}
	return steps
}

// continueLabel es el texto de la celda de continuación del paso activo.
func (w *wizardScreen) continueLabel() string {
	if w.step < 0 || w.step >= len(w.steps)-1 {
		return "Continuar →"
	}
	return "Continuar a " + w.steps[w.step+1].title + " →"
}

// clearResolution descarta la resolución vigente: cualquier cambio de motor o de
// versión la invalida (AD-02).
func (w *wizardScreen) clearResolution() {
	w.resolution, w.resEngine, w.resErr, w.resVersion = nil, "", "", ""
	w.resolving = false
	w.resSeq++
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

// --- Versiones con caché por motor y snapshots (RF-5) ---

// verKeyOf identifica una lista de versiones: motor + modo snapshots.
func verKeyOf(engine string, snaps bool) string {
	return engine + "|" + strconv.FormatBool(snaps)
}

// verKey es la clave de caché de la selección actual.
func (w *wizardScreen) verKey() string { return verKeyOf(w.engine.Name, w.showSNAPS) }

// loadVersions sirve las versiones desde la caché del motor (y del modo
// snapshots) y, si no están, devuelve el Cmd que las pide. Nunca se pide dos
// veces la misma lista: cambiar de motor y volver no repite la consulta.
func (w *wizardScreen) loadVersions() tea.Cmd {
	w.verEngine = w.engine.Name
	if list, ok := w.verCache[w.verKey()]; ok {
		w.versions, w.verLoading, w.versionsSeen = list, false, true
		w.verCursor, w.verErr = 0, ""
		w.pickRecommended()
		return nil
	}
	w.versions, w.verLoading, w.verCursor, w.verErr = nil, true, 0, ""
	w.verStale, w.verWarning, w.verAge = false, "", ""
	return w.versionsCmd()
}

// versionsCmd pide el catálogo del motor actual con contexto y timeout. Cada
// petición lleva su número de secuencia: al recibirla, la pantalla descarta las
// respuestas que ya no corresponden a la selección vigente, de modo que una
// respuesta TARDÍA no sustituye el estado de otra instancia/motor elegido
// después (§7). El servicio, además, cancela la consulta con el contexto.
func (w *wizardScreen) versionsCmd() tea.Cmd {
	ref, engine, all := w.svc, w.engine.Name, w.showSNAPS
	w.verSeq++
	seq := w.verSeq
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		// La lista viaja CON su estado (frescura y aviso): la UI puede etiquetar
		// una caché vencida sin inventarse el dato, y nunca muestra una lista
		// vacía silenciosa (Fase 5/§2.2).
		list, err := ref.Catalog.Versions(ctx, engine, all)
		return wizardVersionsMsg{
			engine: engine, snaps: all, seq: seq,
			versions: list.Versions, stale: list.Stale,
			status: list.Status, warning: list.Warning,
			err: err,
		}
	}
}

// refreshVersions fuerza el refresco del catálogo (el equivalente a
// `catalog refresh`, por la MISMA ruta del servicio) y vuelve a pedir la lista:
// es el atajo de actualización del panel y el reintento tras un fallo (§2.2).
func (w *wizardScreen) refreshVersions() tea.Cmd {
	ref, engine, all := w.svc, w.engine.Name, w.showSNAPS
	w.verSeq++
	seq := w.verSeq
	w.verLoading, w.verErr = true, ""
	delete(w.verCache, verKeyOf(engine, all))
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		_, rerr := ref.Catalog.Refresh(ctx, engine)
		list, lerr := ref.Catalog.Versions(ctx, engine, all)
		err := lerr
		if err == nil {
			err = rerr
		}
		return wizardVersionsMsg{
			engine: engine, snaps: all, seq: seq,
			versions: list.Versions, stale: list.Stale,
			status: list.Status, warning: list.Warning,
			err: err,
		}
	}
}

// noteVersions cachea el catálogo recibido y lo aplica solo si sigue
// correspondiendo a la ÚLTIMA petición (secuencia) y al motor + snapshots
// vigentes: las respuestas tardías se descartan en silencio (§7). La lista sí se
// guarda en la caché por motor, porque es válida para la clave que la pidió.
func (w *wizardScreen) noteVersions(msg wizardVersionsMsg) {
	if msg.err == nil {
		w.verCache[verKeyOf(msg.engine, msg.snaps)] = msg.versions
	}
	if msg.seq != w.verSeq || msg.engine != w.engine.Name || msg.snaps != w.showSNAPS {
		return
	}
	w.versions, w.verLoading, w.versionsSeen = msg.versions, false, true
	w.verEngine = msg.engine
	w.verStale, w.verWarning = msg.stale, msg.warning
	w.verAge = msg.status.Age
	if msg.err != nil {
		// El fallo se queda VISIBLE en el panel de versiones, con reintento: nunca
		// una lista vacía silenciosa (§2.2). Si además hay una lista utilizable
		// (caché previa válida), se conserva y el error se muestra como aviso.
		w.verErr = "no se pudieron cargar las versiones: " + msg.err.Error()
		if len(msg.versions) == 0 {
			w.versions = nil
		}
		return
	}
	w.verErr = ""
	w.pickRecommended()
} // toggleHelp abre o cierra la ayuda larga del campo activo (F1/?).
func (w *wizardScreen) toggleHelp() {
	if w.helpOpen {
		w.helpOpen = false
		return
	}
	if w.mode < 0 || w.currentStep() == nil {
		return
	}
	// Abrir la ayuda no debe perder lo escrito: se confirma el campo y el foco
	// vuelve al paso.
	w.commitTextField()
	w.edit = false
	w.input.Blur()
	w.helpOpen = true
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
		w.noteVersions(msg)
		return w, nil
	case wizardResolvedMsg:
		w.noteResolved(msg)
		return w, nil
	case wizardCreatedMsg:
		if msg.err != nil {
			// El error de creación se queda visible en el paso Revisar (§8),
			// diciendo si la instancia quedó APLICADA en parte (AC-10).
			w.err = createErrorText(msg)
			w.step = len(w.steps) - 1
			w.cur = 0
			w.syncInput()
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
			return w, w.start(0)
		case "a":
			return w, w.start(1)
		case "esc":
			return w, func() tea.Msg { return popScreenMsg{} }
		}
		return w, nil
	}
	// F1 abre y cierra la ayuda del campo, también mientras se escribe: no es una
	// tecla imprimible y no choca con el texto.
	if km.String() == "f1" {
		w.toggleHelp()
		return w, nil
	}
	if w.helpOpen {
		// La ayuda larga es modal: solo Esc o Enter la cierran (§5).
		switch km.String() {
		case "esc", "enter":
			w.helpOpen = false
		}
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
		// Esc cierra el panel SIN cambiar la versión elegida (UX-03).
		w.pickingVer = false
		w.verErr = ""
		w.syncInput()
	case "j", "down":
		w.moveVerCursor(0, 1)
	case "k", "up":
		w.moveVerCursor(0, -1)
	case "h", "left":
		w.moveVerCursor(-1, 0)
	case "l", "right":
		w.moveVerCursor(1, 0)
	case "s":
		// Cada variante (releases/snapshots) tiene su propia caché: separar los
		// snapshots/betas es cosa del canal, no de la pantalla (T-01).
		w.showSNAPS = !w.showSNAPS
		w.verErr = ""
		return w, w.loadVersions()
	case "R":
		// Atajo de refresco del catálogo (catalog refresh) y reintento tras un
		// fallo (§2.2).
		return w, w.refreshVersions()
	case "enter":
		sel := w.verSelect(w.pickerInner())
		it, ok := sel.Selected()
		if !ok {
			return w, nil
		}
		if it.Disabled {
			w.verErr = "esa versión no se puede usar: " + it.DisabledWhy
			return w, nil
		}
		f, _ := w.currentField()
		f.value = it.Value
		w.setField(f)
		// La versión cambia: la resolución anterior deja de valer (AD-02) y, si ya
		// estamos en el resumen, se pide la nueva.
		w.clearResolution()
		w.pickingVer, w.verErr, w.err = false, "", ""
		return w, w.afterStepChange()
	}
	return w, nil
}

// moveVerCursor mueve el cursor del selector de versiones con el componente
// único: dy en lista/grid y dx solo en modo rejilla (4 flechas).
func (w *wizardScreen) moveVerCursor(dx, dy int) {
	sel := w.verSelect(w.pickerInner())
	sel.SetCursor(w.verCursor)
	cols := max(1, w.pickerCols)
	if sel.Mode == components.SelectGrid && dx != 0 {
		sel.MoveGrid(dx, 0, cols)
	} else {
		sel.MoveGrid(0, dy, cols)
	}
	w.verCursor = sel.Cursor()
	w.verErr = ""
}

// verSelect construye el selector ÚNICO (components.Selector) desde las versiones
// cargadas: en rejilla alineada cuando hay ancho y agrupado por versión mayor
// (con los snapshots/betas aparte). NO filtra nada por su cuenta: lista TODO lo
// que devuelve el servicio, que ya garantiza que cada versión tiene artefacto
// descargable (QC-04); solo marca como no elegible lo que el servicio declara
// incompatible, explicando el motivo.
func (w *wizardScreen) verSelect(inner int) *components.Selector {
	items := make([]components.SelectItem, 0, len(w.versions))
	for _, v := range w.versions {
		it := components.SelectItem{
			Value: v.ID(), Label: v.ID(), Detail: versionDetail(v),
			Recommended: v.Recommended, Group: verGroup(v),
		}
		if v.Recommended {
			it.Detail = joinDetail(it.Detail, "última estable")
		}
		switch {
		case v.Compatibility == engines.CompatibilityIncompatible:
			it.Disabled = true
			it.DisabledWhy = versionIncompatibleWhy(v)
			it.Detail = joinDetail(it.Detail, "incompatible")
		case !v.Compatible && v.Compatibility != engines.CompatibilityCompatible:
			it.Detail = joinDetail(it.Detail, "sin verificar")
		}
		items = append(items, it)
	}
	mode := components.SelectList
	if inner >= 60 {
		mode = components.SelectGrid
	}
	sel := components.NewSelectorMode("VERSIONES", items, mode)
	sel.SetCursor(w.verCursor)
	return sel
}

// verGroup agrupa las versiones por versión mayor (1.21.x) y aparte los
// snapshots y las betas, que no son estables (QC-05). El orden de los grupos es
// el de la fuente: la pantalla no reordena nada (T-01).
func verGroup(v engines.Version) string {
	switch v.Channel {
	case engines.ChannelRelease, "":
		if major := twoSegments(v.Version); major != "" {
			return "Minecraft " + major + ".x"
		}
		return "Otras versiones"
	case engines.ChannelSnapshot:
		return "Snapshots"
	}
	return "Betas"
}

// twoSegments devuelve los dos primeros segmentos numéricos de una versión
// («1.21.4» → «1.21»), o "" si no tiene esa forma.
func twoSegments(version string) string {
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) < 2 {
		return ""
	}
	for _, p := range parts[:2] {
		if p == "" {
			return ""
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return ""
			}
		}
	}
	return parts[0] + "." + parts[1]
}

// joinDetail añade una nota a un detalle existente sin dejar separadores sueltos.
func joinDetail(detail, note string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return note
	}
	return detail + " · " + note
}

// versionIncompatibleWhy explica por qué el servicio marca una versión como no
// utilizable (con respaldo si no dio motivo).
func versionIncompatibleWhy(v engines.Version) string {
	if strings.TrimSpace(v.IncompatibleWhy) != "" {
		return v.IncompatibleWhy
	}
	return "el catálogo la marca como incompatible con este motor"
}

// pickerInner es el ancho interior del panel de versiones en el último render.
func (w *wizardScreen) pickerInner() int {
	if w.pickerW > 0 {
		return w.pickerW
	}
	return 40
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
			// Enter confirma el campo y mueve el foco al siguiente (incluida la
			// celda «Continuar»): cambiar de PASO es cosa de Enter sobre esa celda
			// o de Tab (P-02).
			return w, tea.Batch(c, w.advance())
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
			w.err = ""
			w.syncInput()
			return w, nil
		}
		return w, func() tea.Msg { return popScreenMsg{} }
	case "tab":
		return w, w.advance()
	case "shift+tab":
		if w.step > 0 {
			w.step--
			w.cur = 0
			w.err = ""
			w.syncInput()
		}
	case "down", "j":
		// ↓ mueve la selección y NUNCA cambia de paso (P-02/UX-02): al llegar a la
		// celda «Continuar a X →» se detiene ahí.
		if st := w.currentStep(); st != nil && w.cur < len(st.fields)-1 {
			w.cur++
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
		return w, w.cycle(-1)
	case "right":
		return w, w.cycle(1)
	case " ":
		w.toggle()
	case "?":
		w.toggleHelp()
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
// advance pasa al siguiente campo o, si ya está en la celda de continuación, al
// siguiente paso. Es la ruta de Tab y del final de un campo editado: NUNCA la de
// ↓ (P-02).
func (w *wizardScreen) advance() tea.Cmd {
	st := w.currentStep()
	if st != nil && w.cur < len(st.fields)-1 {
		w.cur++
		w.syncInput()
		return nil
	}
	return w.nextStep()
}

// enterField activa la edición del campo enfocado, avanza de paso si es la celda
// «Continuar a X →» o crea la instancia si es el resumen.
func (w *wizardScreen) enterField() (Screen, tea.Cmd) {
	if w.step == len(w.steps)-1 {
		// El resumen solo crea con la resolución del servicio ya en la mano: así lo
		// que se muestra es lo que se guarda (AD-02). Si falta, se pide y se avisa.
		if cmd := w.ensureResolution(); cmd != nil {
			w.err = "resolviendo motor, versión y pines con el catálogo…"
			return w, cmd
		}
		if w.resolution == nil && w.resErr != "" {
			w.err = w.resErr + " · vuelve a pulsar [Enter] para reintentar"
			return w, nil
		}
		return w, w.create()
	}
	f, ok := w.currentField()
	if !ok {
		return w, w.nextStep()
	}
	switch f.kind {
	case "continue":
		return w, w.nextStep()
	case "version":
		w.pickingVer = true
		w.verErr = ""
		if len(w.versions) == 0 {
			return w, w.loadVersions()
		}
	case "cycle":
		return w, w.cycle(1)
	case "bool":
		w.toggle()
	default:
		w.edit = true
		w.input.SetValue(f.value)
		return w, w.input.Focus()
	}
	return w, nil
}

// readyToCreate indica que el resumen está completo: nombre, sin error y con una
// resolución (pins finales) vigente del servicio.
func (w *wizardScreen) readyToCreate() bool {
	return strings.TrimSpace(w.nameValue()) != "" && w.resolution != nil && w.resErr == "" && !w.resolving
}

// nameValue devuelve el nombre escrito en el asistente.
func (w *wizardScreen) nameValue() string {
	for _, st := range w.steps {
		if f, ok := st.findField("name"); ok {
			return f.value
		}
	}
	return ""
}

// ensureResolution pide la resolución del servicio si la vigente no corresponde
// ya al motor y a la versión elegidos (AD-02).
func (w *wizardScreen) ensureResolution() tea.Cmd {
	if w.resolving {
		return nil
	}
	version := strings.TrimSpace(w.versionValue())
	if w.resolution != nil && w.resEngine == w.engine.Name && w.resVersion == version && w.resErr == "" {
		return nil
	}
	return w.resolveCmd()
}

// resolveCmd pide al servicio los pins finales de motor + versión.
func (w *wizardScreen) resolveCmd() tea.Cmd {
	ref, engine, version := w.svc, w.engine.Name, strings.TrimSpace(w.versionValue())
	w.resSeq++
	seq := w.resSeq
	w.resolving, w.resErr = true, ""
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := ref.Catalog.Resolve(ctx, engine, version)
		return wizardResolvedMsg{engine: engine, version: version, seq: seq, res: res, err: err}
	}
}

// noteResolved adopta la resolución SOLO si sigue siendo la vigente (ni motor ni
// versión han cambiado desde que se pidió): las respuestas tardías se descartan
// sin tocar el estado (§7/AD-02).
func (w *wizardScreen) noteResolved(msg wizardResolvedMsg) {
	if msg.seq != w.resSeq || msg.engine != w.engine.Name || msg.version != strings.TrimSpace(w.versionValue()) {
		return
	}
	w.resolving = false
	if msg.err != nil {
		w.resolution = nil
		w.resErr = "no se pudo resolver la versión: " + msg.err.Error()
		return
	}
	res := msg.res
	w.resolution, w.resEngine, w.resErr, w.resVersion = &res, msg.engine, "", msg.version
}

// afterStepChange prepara el paso recién abierto: al entrar en el resumen se pide
// la resolución vigente para poder mostrar los valores FINALES antes de crear.
func (w *wizardScreen) afterStepChange() tea.Cmd {
	if w.step == len(w.steps)-1 {
		return w.ensureResolution()
	}
	return nil
}

// cycle cambia el valor del campo de ciclo enfocado. Al cambiar de motor
// devuelve el Cmd de versiones (desde la caché si ya se pidió, RF-5).
// cycle cambia el valor del campo de ciclo enfocado. Al cambiar de motor se
// RESETEA la versión, se descarta la resolución anterior y se recarga la lista
// válida para el nuevo motor (AD-02/§3.2).
func (w *wizardScreen) cycle(d int) tea.Cmd {
	f, ok := w.currentField()
	if !ok || f.kind != "cycle" {
		return nil
	}
	if f.key == "engine" {
		w.engineIdx = (w.engineIdx + len(engines.Order) + d) % len(engines.Order)
		w.engine = engines.Order[w.engineIdx]
		f.value = w.engine.Name
		w.setField(f)
		// El motor cambia el catálogo de versiones: se descarta la versión anterior
		// y se elige la recomendada del nuevo motor (RF-5); la resolución anterior
		// deja de valer (AD-02).
		if v, ok := w.currentStep().findField("version"); ok {
			v.value = ""
			w.steps[w.step].fields = replaceField(w.steps[w.step].fields, v)
		}
		w.clearResolution()
		return w.loadVersions()
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
	return nil
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

// nextStep abre el paso siguiente (y, si es el resumen, pide la resolución).
func (w *wizardScreen) nextStep() tea.Cmd {
	if w.step >= len(w.steps)-1 {
		return nil
	}
	w.step++
	w.cur = 0
	w.err = ""
	w.syncInput()
	return w.afterStepChange()
}

// pickRecommended marca la versión recomendada del motor como elección por
// defecto. Si cambia la versión elegida, la resolución anterior deja de valer.
func (w *wizardScreen) pickRecommended() {
	for _, v := range w.versions {
		if v.Recommended && v.Compatible {
			for i, f := range w.steps[0].fields {
				if f.key == "version" && f.value == "" {
					f.value = v.ID()
					w.steps[0].fields[i] = f
					w.clearResolution()
				}
			}
			return
		}
	}
}

// buildRequest reúne los valores del asistente en una petición de creación. El
// motor, la versión y los PINS salen del objeto Resolved del SERVICIO (nunca de
// índices ni de valores por defecto): es lo que garantiza que se cree EXACTAMENTE
// lo que se revisó en el resumen, aunque el catálogo publique un build nuevo
// entre la resolución y la creación (AD-02/T-08).
func (w *wizardScreen) buildRequest() provision.Request {
	req := provision.Request{Engine: w.engine.Name, Start: true}
	if w.resolution != nil {
		if w.resEngine != "" {
			req.Engine = w.resEngine
		}
		req.Version = w.resolution.Version
		// Los pins EXACTOS revisados viajan con la petición: provision los usa
		// tal cual (validados) en lugar de re-resolver.
		req.Pins = w.resolution.Pins
	} else {
		req.Version = strings.TrimSpace(w.versionValue())
	}
	for _, st := range w.steps {
		for _, f := range st.fields {
			switch f.key {
			case "name":
				req.Name = f.value
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

// create crea la instancia con la resolución VIGENTE ya mostrada en el resumen y
// VERIFICA después que la configuración persistida coincide con ella: si no
// coincide, no se informa de un éxito silencioso (AD-02/T-08).
func (w *wizardScreen) create() tea.Cmd {
	req := w.buildRequest()
	if strings.TrimSpace(req.Name) == "" {
		w.err = "el nombre es obligatorio"
		w.step = 0
		w.cur = 0
		w.syncInput()
		return nil
	}
	if !w.readyToCreate() {
		w.err = "falta la resolución del catálogo (motor, versión y pines): vuelve a pulsar [Enter]"
		return nil
	}
	w.err = ""
	ref := w.svc
	cfg, wantIdle := w.idleConfig()
	// La resolución viaja por copia: el payload y la verificación usan los MISMOS
	// valores que vio el usuario en el resumen.
	res := *w.resolution
	engine := req.Engine
	return tea.Batch(busyCmd(true), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		out, err := ref.Creator.Create(ctx, req)
		mismatch := ""
		if err == nil && out.Spec != nil {
			mismatch = createdMismatch(engine, res, out.Spec)
		}
		if err == nil && wantIdle {
			_ = ref.Idle.Set(ctx, out.Slug, cfg)
		}
		return wizardCreatedMsg{slug: out.Slug, outcome: out.Outcome, err: err, mismatch: mismatch}
	})
}

// createdMismatch compara la configuración PERSISTIDA con el resumen revisado:
// motor, versión y pines obligatorios. Devuelve el texto de la discrepancia ("" si
// coinciden). Se comparan VALORES, nunca índices (AD-02).
func createdMismatch(engine string, res engines.Resolved, spec *instance.Spec) string {
	if spec == nil {
		return "no se pudo leer la configuración creada para verificarla"
	}
	eng := engines.Paper
	if e, ok := engines.ByName(engine); ok {
		eng = e
	}
	var diffs []string
	if spec.Engine != eng.Name {
		diffs = append(diffs, "motor guardado «"+spec.Engine+"» ≠ «"+eng.Name+"»")
	}
	if res.Version != "" && spec.Version != res.Version {
		diffs = append(diffs, "versión guardada «"+spec.Version+"» ≠ «"+res.Version+"»")
	}
	for _, dep := range engines.RequiredPins(eng) {
		want := strings.TrimSpace(res.Pins[dep])
		got := ""
		if spec.Pins != nil {
			got = strings.TrimSpace(spec.Pins[dep])
		}
		if want != got {
			diffs = append(diffs, "pin "+dep+": guardado «"+got+"» ≠ resuelto «"+want+"»")
		}
	}
	if len(diffs) == 0 {
		return ""
	}
	return strings.Join(diffs, " · ")
}

// createErrorText redacta el fallo de creación distinguiendo un fallo total de uno
// PARCIAL (la instancia ya quedó escrita) (AC-10).
func createErrorText(msg wizardCreatedMsg) string {
	if msg.outcome == result.OutcomePartial {
		return "no se pudo terminar de crear (quedó parcialmente aplicada; revisa «" + msg.slug + "»): " + msg.err.Error()
	}
	return "no se pudo crear: " + msg.err.Error()
}

// --- Vista (§5.12): todo dentro de paneles y ajustado a width×height ---

// View pinta el asistente: el modo, el paso actual o la ayuda larga, siempre
// dentro de paneles. El resultado ocupa exactamente height líneas de width
// celdas como máximo (medido en celdas visuales, nunca con len()).
func (w *wizardScreen) View(width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	var out string
	switch {
	case w.mode < 0:
		out = w.modeView(width, height)
	case w.helpOpen:
		out = w.helpView(width, height)
	default:
		out = w.stepView(width, height)
	}
	return fitBlock(out, width, height)
}

// fitBlock ajusta un bloque a exactamente h líneas de w celdas: recorta con «…»
// lo que sobra y rellena con líneas vacías lo que falta.
func fitBlock(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for i := range lines {
		lines[i] = truncCells(lines[i], w)
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// truncCells recorta una línea a w celdas conservando las secuencias ANSI.
func truncCells(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if layout.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// modeView elige entre configuración rápida y avanzada dentro de un panel, con
// las dos opciones como botones (RF-4).
func (w *wizardScreen) modeView(width, height int) string {
	quick := components.Action{ID: "wizard.quick", Verb: "Configuración", Object: "rápida (3 preguntas)", Key: "r", Hint: "Nombre, motor y memoria.", Kind: components.Primary}
	adv := components.Action{ID: "wizard.advanced", Verb: "Configuración", Object: "avanzada (todas las opciones)", Key: "a", Hint: "Los 7 pasos, con ayuda por campo.", Kind: components.Secondary}
	body := []string{
		" Elige cómo configurarla:",
		"",
		" " + components.Button(quick, true, w.pal),
		" " + components.Button(adv, false, w.pal),
		"",
		" Los valores recomendados aparecen en el resumen antes de crear.",
		"",
		" Pulsa [R] o [A] para empezar · [Esc] vuelve sin crear.",
	}
	return components.Panel("NUEVA INSTANCIA", "", strings.Join(body, "\n"), width, height, true, w.pal, w.ascii)
}

// stepView pinta el Stepper en una línea y, debajo, el panel del paso (o el
// selector de versiones) con el panel AYUDA lateral cuando hay ancho (§5.12).
// stepView pinta el Stepper en una línea y, debajo, el panel del paso con su panel
// lateral: las VERSIONES cuando el selector está abierto (UX-03) o la AYUDA del
// campo. En terminales estrechas el selector ocupa el cuerpo entero.
func (w *wizardScreen) stepView(width, height int) string {
	stepper := truncCells(components.Stepper(w.stepTitles(), w.step, w.pal), width)
	panelH := height - 1
	if panelH < 2 {
		return stepper
	}
	formW, sideW, split := wizardSplit(width, w.pickingVer)
	title, right := w.panelHead(split)
	inner := width - 2
	if split {
		inner = formW - 2
	}
	panel := components.Panel(title, right, strings.Join(w.panelBody(inner, panelH-2, split), "\n"), formW, panelH, true, w.pal, w.ascii)
	if !split {
		return stepper + "\n" + panel
	}
	sideTitle, sideRight := "AYUDA", ""
	sideBody := w.helpBody(sideW-2, panelH-2)
	if w.pickingVer {
		// Panel LATERAL de versiones: rejilla alineada, agrupada por versión mayor
		// y con el pie de teclas (Enter elige, Esc cierra, R actualiza).
		sideTitle, sideRight = "VERSIONES", w.engine.Label
		sideBody = w.pickerBody(sideW-2, panelH-2)
	}
	side := components.Panel(sideTitle, sideRight, strings.Join(sideBody, "\n"), sideW, panelH, true, w.pal, w.ascii)
	return stepper + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, panel, side)
}

// panelHead devuelve el título y el contador del panel activo: el selector de
// versiones y el paso Revisar tienen su propio título (§5.12).
// panelHead devuelve el título y el contador del panel activo. Con el selector de
// versiones abierto y sin panel lateral (terminal estrecha), el cuerpo ES el panel
// de versiones; el resumen tiene su propio título (§5.12).
func (w *wizardScreen) panelHead(split bool) (title, right string) {
	switch {
	case w.pickingVer && !split:
		return "VERSIONES", w.engine.Label
	case w.step == len(w.steps)-1:
		return "REVISAR", w.stepCounter()
	}
	title = "Instancia"
	if st := w.currentStep(); st != nil {
		title = st.title
	}
	return title, w.stepCounter()
}

// stepCounter es el contador «n/N» de pasos del borde del panel.
func (w *wizardScreen) stepCounter() string {
	return itoa(w.step+1) + "/" + itoa(len(w.steps))
}

// stepTitles son los títulos de los pasos para el Stepper.
func (w *wizardScreen) stepTitles() []string {
	titles := make([]string, len(w.steps))
	for i, st := range w.steps {
		titles[i] = st.title
	}
	return titles
}

// panelBody compone el cuerpo del panel activo: formulario, versiones o resumen.
func (w *wizardScreen) panelBody(inner, budget int, sideHelp bool) []string {
	switch {
	case w.pickingVer && !sideHelp:
		// Sin sitio para el panel lateral, el selector de versiones ocupa el
		// cuerpo entero del panel.
		return w.pickerBody(inner, budget)
	case w.step == len(w.steps)-1:
		return w.reviewBody(inner, budget)
	}
	return w.formBody(inner, budget, sideHelp)
}

// wizardSplit reparte el ancho entre el panel del formulario y el de ayuda: en
// terminales anchas hay panel lateral; en las estrechas el formulario ocupa todo
// el ancho y la ayuda corta va bajo cada campo (§4.1).
// wizardSplit reparte el ancho entre el panel del formulario y el lateral. El
// selector de versiones necesita más sitio que la ayuda: sus columnas deben verse
// alineadas y con separación visible (UX-03/QC-05).
func wizardSplit(w int, versions bool) (formW, sideW int, ok bool) {
	if w < 100 {
		return w, 0, false
	}
	if versions {
		sideW = w * 55 / 100
		if sideW < 30 {
			sideW = 30
		}
		if sideW > 70 {
			sideW = 70
		}
		formW = w - sideW
		if formW < 48 {
			return w, 0, false
		}
		return formW, sideW, true
	}
	sideW = w * 34 / 100
	if sideW < 30 {
		sideW = 30
	}
	if sideW > 56 {
		sideW = 56
	}
	formW = w - sideW
	if formW < 52 {
		return w, 0, false
	}
	return formW, sideW, true
}

// formBody compone el formulario del paso en `budget` líneas: mantiene visible
// el campo enfocado (etiqueta, valor y validación) y deja sitio, si el aviso
// cabe entero, a los estados de error y riesgo (§8/UI-05).
func (w *wizardScreen) formBody(inner, budget int, sideHelp bool) []string {
	st := w.currentStep()
	if st == nil || budget < 1 {
		return nil
	}
	if inner < 4 {
		inner = 4
	}
	lines := make([]string, 0, len(st.fields)*2)
	focus := -1
	for i, f := range st.fields {
		focused := i == w.cur
		if focused {
			focus = len(lines)
			if w.edit {
				// El campo enfocado ajusta su ancho al del panel: nada de anchos
				// fijos desconectados del terminal. Se descuentan el prompt del
				// input y una celda de holgura para el cursor.
				avail := inner - layout.Width(w.fieldPrefix(f, true)) - layout.Width(w.input.Prompt) - 1
				w.input.Width = max(1, avail)
			}
		}
		lines = append(lines, truncCells(w.fieldPrefix(f, focused)+w.fieldValue(f), inner))
		if h, ok := help.Get(f.key); ok && h.Short != "" {
			lines = append(lines, truncCells(w.fieldHelpLine(h, sideHelp), inner))
		}
		if f.key == "port" {
			if line := w.portStatusLine(); line != "" {
				lines = append(lines, truncCells(lipgloss.NewStyle().Foreground(lipgloss.Color(w.portColor())).Render(line), inner))
			}
		}
	}
	// El foco manda: los avisos se llevan como mucho la mitad del cuerpo.
	maxTail := budget / 2
	var tail []string
	for _, blk := range w.statusBlocks(inner) {
		if len(tail)+len(blk) > maxTail {
			continue
		}
		tail = append(tail, blk...)
	}
	out := windowLines(lines, focus, budget-len(tail))
	out = append(out, tail...)
	if len(out) > budget {
		out = out[:budget]
	}
	return out
}

// fieldPrefix compone el prefijo de un campo (puntero + etiqueta + «: »).
// fieldPrefix compone el prefijo de un campo (puntero + etiqueta + «: »). La celda
// de continuación no es un campo: se pinta como el botón que es.
func (w *wizardScreen) fieldPrefix(f wizField, focused bool) string {
	if f.kind == "continue" {
		label := w.continueLabel()
		if focused {
			return components.Pointer(w.ascii) + " " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(w.pal.Accent)).Render(label)
		}
		return "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Muted)).Render(label)
	}
	label := f.key
	if h, ok := help.Get(f.key); ok && h.Title != "" {
		label = h.Title
	}
	marker := "  "
	style := lipgloss.NewStyle()
	if focused {
		marker = components.Pointer(w.ascii) + " "
		style = style.Bold(true).Foreground(lipgloss.Color(w.pal.Accent))
	}
	return marker + style.Render(label) + ": "
}

// fieldValue pinta el valor de un campo: el texto en edición, el valor guardado
// o el estado explícito cuando aún no hay valor (nunca un hueco en blanco).
// fieldValue pinta el valor de un campo: el texto en edición, el valor guardado
// o el estado explícito cuando aún no hay valor (nunca un hueco en blanco).
func (w *wizardScreen) fieldValue(f wizField) string {
	switch f.kind {
	case "continue":
		// La celda no tiene valor: el prefijo ya lleva el texto completo.
		return ""
	case "bool":
		return onOff(f.boolVal)
	case "version":
		if w.verErr != "" && len(w.versions) == 0 {
			return "no disponible: Enter reintenta la carga"
		}
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

// fieldHelpLine es la ayuda corta del campo. En terminal estrecha (sin panel de
// ayuda lateral) añade el impacto, que si no quedaría oculto.
func (w *wizardScreen) fieldHelpLine(h help.Help, sideHelp bool) string {
	short := h.Short
	if !sideHelp && len(h.Impact) > 0 {
		short += " · " + impactText(h.Impact)
	}
	return "    " + lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Muted)).Render(short)
}

// statusBlocks son los avisos de estado que acompañan al formulario, en orden de
// prioridad y como bloques completos: error, versión incompatible y riesgo
// offline (§8). Nunca se muestra media frase.
func (w *wizardScreen) statusBlocks(inner int) [][]string {
	var blocks [][]string
	if w.err != "" {
		blocks = append(blocks, blockLines(components.Cross(w.ascii), w.err, w.pal.Bad, inner))
	}
	if issue := w.versionIssue(); issue != "" {
		blocks = append(blocks, blockLines(components.Cross(w.ascii), issue, w.pal.Bad, inner))
	}
	if w.offlineRisk() {
		blocks = append(blocks, blockLines(components.Warn(w.ascii),
			"Riesgo de suplantación: sin modo online ni lista blanca, cualquiera entra. Pulsa [W] para activar la lista blanca.",
			w.pal.Warn, inner))
	}
	return blocks
}

// versionIssue avisa si la versión elegida está marcada como no utilizable.
// versionIssue avisa si la versión elegida está marcada como no utilizable por el
// SERVICIO (nunca por un filtro propio de la UI).
func (w *wizardScreen) versionIssue() string {
	v := w.versionValue()
	if v == "" {
		return ""
	}
	for _, ver := range w.versions {
		if ver.ID() == v && ver.Compatibility == engines.CompatibilityIncompatible {
			return "esa versión no se puede usar: " + versionIncompatibleWhy(ver)
		}
	}
	return ""
}

// versionValue es la versión elegida (en cualquier paso del asistente).
func (w *wizardScreen) versionValue() string {
	for _, st := range w.steps {
		if f, ok := st.findField("version"); ok {
			return strings.TrimSpace(f.value)
		}
	}
	return ""
}

// pickerBody lista las versiones dentro del panel VERSIONES, con la ventana
// centrada en el cursor y el pie de teclas siempre visible (RF-5).
// pickerBody lista las versiones dentro del panel VERSIONES, con la ventana
// centrada en el cursor y el pie de teclas siempre visible (RF-5). Ante un fallo
// o una lista vacía muestra un error CLARO con reintento y, si la lista procede de
// caché vencida, lo dice (nunca una lista vacía silenciosa, §2.2).
func (w *wizardScreen) pickerBody(inner, budget int) []string {
	if budget < 1 {
		return nil
	}
	if inner < 8 {
		inner = 8
	}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Muted))
	bad := lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Bad))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Warn))
	hint := truncCells(muted.Render("[R] Actualizar · [S] "+snapToggle(w.showSNAPS)+" · [Enter] Elegir · [Esc] Cerrar"), inner)
	if w.verLoading {
		return []string{muted.Render("⟳ cargando el catálogo…"), hint}
	}
	if len(w.versions) == 0 {
		msg := "no hay versiones que mostrar para " + w.engine.Label
		if w.verErr != "" {
			msg = w.verErr
		}
		out := []string{bad.Render(components.Cross(w.ascii) + " " + truncCells(msg, inner-2))}
		if note := w.verNote(); note != "" {
			out = append(out, warn.Render(truncCells(note, inner)))
		}
		out = append(out, muted.Render("[R] Reintentar la carga · [Esc] Cerrar"))
		return out[:min(len(out), budget)]
	}
	// El selector único decide modo (lista/rejilla), ancho de columna fijo,
	// columnas adaptables al ancho de terminal y los grupos por versión mayor
	// (QC-05).
	w.pickerW = inner
	sel := w.verSelect(inner)
	cols := sel.GridColumns(inner, w.versionValue())
	if sel.Mode == components.SelectList {
		cols = 1
	}
	w.pickerCols = cols
	lines := sel.Render(inner, w.versionValue(), w.pal, w.ascii)
	focus := sel.FocusLine(cols)
	reserved := 1 // el pie de teclas
	notes := 0
	if w.verErr != "" {
		notes++
	}
	if w.verNote() != "" {
		notes++
	}
	reserved += notes
	if reserved > budget {
		reserved = budget
	}
	out := windowLines(lines, focus, budget-reserved)
	if w.verErr != "" {
		out = append(out, warn.Render(truncCells("⚠ "+w.verErr+" · [R] Reintentar", inner)))
	}
	if note := w.verNote(); note != "" {
		out = append(out, warn.Render(truncCells(note, inner)))
	}
	out = append(out, hint)
	if len(out) > budget {
		out = out[:budget]
	}
	return out
}

// verNote etiqueta la frescura del catálogo mostrado: una cache vencida (stale) o
// el catálogo embebido se dicen tal cual, con su antigüedad legible (§2.2/Fase 4).
func (w *wizardScreen) verNote() string {
	var parts []string
	if w.verStale {
		note := "⚠ caché vencida (stale)"
		if w.verAge != "" {
			note += " · " + w.verAge
		}
		parts = append(parts, note)
	}
	if strings.TrimSpace(w.verWarning) != "" {
		parts = append(parts, "⚠ "+w.verWarning)
	}
	return strings.Join(parts, " · ")
}

// reviewBody resume la instancia a crear dentro del panel REVISAR, dejando
// visibles el error (si lo hubo) y la tecla de creación (§8).
// reviewBody resume la instancia a crear dentro del panel REVISAR: primero la
// RESOLUCIÓN del servicio (motor, versión, loader, build, Java, imagen, pines,
// compatibilidad y aviso) y después el resto de valores del asistente. Lo que se
// ve aquí es exactamente lo que se guarda (AD-02/UX-05), y la creación se verifica
// contra ello.
func (w *wizardScreen) reviewBody(inner, budget int) []string {
	if budget < 1 {
		return nil
	}
	if inner < 8 {
		inner = 8
	}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Muted))
	lines := append([]string{}, w.resolutionLines(inner)...)
	lines = append(lines, "")
	for _, st := range w.steps {
		if st.title == "Revisar" {
			continue
		}
		for _, f := range st.fields {
			if f.kind == "continue" {
				continue
			}
			label := f.key
			if h, ok := help.Get(f.key); ok && h.Title != "" {
				label = h.Title
			}
			lines = append(lines, truncCells("  "+label+": "+w.reviewValue(f), inner))
		}
	}
	if !w.hasPortField() {
		lines = append(lines, truncCells("  Puerto: automático (el primero libre)", inner))
	}
	if w.cap.NCPU > 0 {
		lines = append(lines, truncCells(muted.Render("Capacidad: "+strconv.Itoa(w.cap.NCPU)+" CPU · "+trimFloat(w.cap.MemTotalGiB())+" GiB RAM"), inner))
	}
	var tail []string
	if w.err != "" {
		tail = append(tail, blockLines(components.Cross(w.ascii), w.err, w.pal.Bad, inner)...)
	}
	if w.offlineRisk() {
		// El resumen repite el riesgo antes de crear, como bloque completo (sin
		// truncar), para que no se pierda el aviso visto en Jugadores (§8/P-05).
		tail = append(tail, blockLines(components.Warn(w.ascii),
			"Riesgo de suplantación: sin modo online ni lista blanca, cualquiera entra. [W] activa la lista blanca.",
			w.pal.Warn, inner)...)
	}
	tail = append(tail, truncCells(muted.Render("[Enter] Crear y arrancar · [Esc] Volver a editar"), inner))
	avail := budget - len(tail)
	if avail < 0 {
		avail = 0
	}
	// La resolución manda: si no cabe todo, se recorta la lista de valores del
	// asistente, pero se dice CUÁNTOS quedan fuera (nunca un recorte silencioso).
	out := append([]string{}, lines[:min(len(lines), avail)]...)
	if ocultos := len(lines) - len(out); ocultos > 0 && len(out) > 0 {
		out[len(out)-1] = muted.Render(truncCells("  … ("+itoa(ocultos)+" valores más en los pasos · [Esc] para revisar)", inner))
	}
	out = append(out, tail...)
	if len(out) > budget {
		out = out[:budget]
	}
	return out
}

// resolutionLines describe los valores FINALES que el servicio va a aplicar:
// son los del objeto Resolved (motor, versión, loader, build, Java, imagen y
// pines) más el veredicto de compatibilidad y su aviso.
func (w *wizardScreen) resolutionLines(inner int) []string {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Muted))
	head := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(w.pal.Accent)).Render("Resolución del servicio de catálogo")
	if w.resolving {
		return []string{head, muted.Render("  ⟳ resolviendo motor, versión y pines…")}
	}
	if w.resErr != "" {
		return append([]string{head},
			blockLines(components.Cross(w.ascii), w.resErr+" · vuelve a pulsar [Enter] para reintentar", w.pal.Bad, inner)...)
	}
	if w.resolution == nil {
		return []string{head, muted.Render("  (se resolverá antes de crear)")}
	}
	r := w.resolution
	// El orden es el de PRIORIDAD: si el cuerpo es corto, se recortan las últimas
	// filas y nunca desaparecen motor, versión, compatibilidad ni Java.
	rows := [][2]string{
		{"Motor", engineLabel(w.resEngine)},
		{"Versión", r.Version},
		{"Compatibilidad", w.compatibilityText(r.Compatibility)},
		{"Java", w.javaText(r)},
		{"Build", orDash(r.Build)},
		{"Loader", orDash(r.LoaderVersion)},
		{"Pines", pinsText(r.Pins)},
		{"Imagen", orDash(r.Image)},
	}
	out := []string{head}
	for _, row := range rows {
		out = append(out, truncCells("  "+row[0]+": "+row[1], inner))
	}
	if strings.TrimSpace(r.Warning) != "" {
		out = append(out, blockLines(components.Warn(w.ascii), "Aviso: "+r.Warning, w.pal.Warn, inner)...)
	}
	return out
}

// engineLabel describe el motor que aplicará el servicio (con respaldo).
func engineLabel(name string) string {
	if e, ok := engines.ByName(name); ok {
		return e.Label + " (" + e.Name + ")"
	}
	if strings.TrimSpace(name) == "" {
		return "(sin motor)"
	}
	return name
}

// javaText redacta el requisito de Java y su ORIGEN: verificado por el proveedor,
// inferido por regla o desconocido (nunca se asume Java 8, AC-05).
func (w *wizardScreen) javaText(r *engines.Resolved) string {
	if r.JavaMajor <= 0 {
		return components.Warn(w.ascii) + " desconocido (no se puede confirmar)"
	}
	origin := "sin origen declarado"
	switch r.JavaStatus {
	case engines.JavaStatusVerified:
		origin = "verificado por el proveedor"
	case engines.JavaStatusInferred:
		origin = "inferido por regla"
	case engines.JavaStatusUnknown:
		origin = "desconocido"
	}
	return strconv.Itoa(r.JavaMajor) + " (" + origin + ")"
}

// compatibilityText traduce el veredicto TRIVALENTE con icono y texto (nunca solo
// color): compatible, incompatible o sin verificar.
func (w *wizardScreen) compatibilityText(c engines.Compatibility) string {
	switch c {
	case engines.CompatibilityCompatible:
		return components.Check(w.ascii) + " compatible"
	case engines.CompatibilityIncompatible:
		return components.Cross(w.ascii) + " incompatible"
	case engines.CompatibilityUnknown:
		return components.Warn(w.ascii) + " sin verificar (unknown)"
	}
	if c == "" {
		return components.Warn(w.ascii) + " sin verificar"
	}
	return string(c)
}

// pinsText lista los pines obligatorios resueltos, en orden estable.
func pinsText(pins map[string]string) string {
	if len(pins) == 0 {
		return "(ninguno)"
	}
	keys := make([]string, 0, len(pins))
	for k := range pins {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+pins[k])
	}
	return strings.Join(parts, " · ")
}

// helpBody compone el panel AYUDA con la explicación del campo enfocado.
func (w *wizardScreen) helpBody(inner, budget int) []string {
	if budget < 1 {
		return nil
	}
	if inner < 8 {
		inner = 8
	}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Muted))
	f, ok := w.currentField()
	if !ok {
		return []string{muted.Render("Sin campo activo en este paso.")}
	}
	h, _ := help.Get(f.key)
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Warn))
	var lines []string
	// Cada párrafo se envuelve por palabras: en paneles estrechos ni el ejemplo
	// ni el impacto se recortan a mitad de frase.
	para := func(text string, style lipgloss.Style) {
		for _, l := range wrapWords(text, inner) {
			lines = append(lines, style.Render(l))
		}
	}
	if h.Long != "" {
		para(h.Long, lipgloss.NewStyle())
	}
	if h.Example != "" {
		lines = append(lines, "")
		para("Ejemplo: "+h.Example, muted)
	}
	if h.Default != "" {
		para("Por defecto: "+h.Default, muted)
	}
	if len(h.Impact) > 0 {
		lines = append(lines, "")
		para(impactText(h.Impact), warn)
	}
	clipped := len(lines) > budget
	out := lines[:min(len(lines), budget)]
	for i := range out {
		if clipped && i == len(out)-1 {
			// Marca de recorte: se ve que hay más texto (F1 lo muestra entero).
			out[i] = truncCells(out[i]+" …", inner)
			continue
		}
		out[i] = truncCells(out[i], inner)
	}
	return out
}

// helpView es la ayuda larga (F1/?) en un panel modal: solo ella recibe teclas
// hasta que Esc o Enter la cierren (§5).
func (w *wizardScreen) helpView(width, height int) string {
	inner, budget := width-2, height-2
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(w.pal.Muted))
	f, ok := w.currentField()
	if !ok {
		return components.Panel("AYUDA", "Esc/F1", "Sin campo activo.", width, height, true, w.pal, w.ascii)
	}
	h, _ := help.Get(f.key)
	title := "AYUDA"
	if h.Title != "" {
		title = "AYUDA · " + h.Title
	}
	var body []string
	if budget >= 1 {
		body = w.helpBody(inner, budget-1)
		body = append(body, truncCells(muted.Render("Pulsa Esc o F1 para volver al formulario."), inner))
	}
	return components.Panel(title, "Esc/F1", strings.Join(body, "\n"), width, height, true, w.pal, w.ascii)
} // offlineRisk indica que se creará en modo offline sin lista blanca (P-05).

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
	out := []components.Action{}
	last := w.step == len(w.steps)-1
	if last {
		// El resumen no promete crear antes de tener la resolución del servicio: si
		// falta, la acción se muestra deshabilitada con el motivo (nunca un éxito
		// que no coincide con lo revisado, AD-02/T-08).
		act := components.Action{
			ID: "wizard.create", Verb: "Crear", Object: "la instancia", Key: "enter",
			Hint: "Crea la instancia con los valores resueltos del resumen.", Kind: components.Primary,
		}
		if !w.readyToCreate() {
			act.Disabled = true
			act.DisabledReason = "falta la resolución del catálogo"
		}
		out = append(out, act)
	} else {
		out = append(out, components.Action{
			ID: "wizard.next", Verb: "Siguiente", Object: "paso", Key: "tab",
			Hint: "Avanza al siguiente paso del asistente.", Kind: components.Primary,
		})
	}
	if w.offlineRisk() {
		out = append(out, components.Action{
			ID: "wizard.whitelist", Verb: "Activar", Object: "la lista blanca", Key: "w",
			Hint: "Impide que entren nombres suplantados.", Kind: components.Secondary,
		})
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
		return w.start(0)
	case "wizard.advanced":
		return w.start(1)
	case "wizard.next":
		return w.nextStep()
	case "wizard.whitelist":
		w.enableWhitelist()
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

// windowLines recorta `lines` a `budget` líneas manteniendo visible la línea
// enfocada, para que el campo activo nunca quede fuera del panel.
func windowLines(lines []string, focus, budget int) []string {
	if budget <= 0 {
		return nil
	}
	if len(lines) <= budget {
		return lines
	}
	if focus < 0 || focus < budget {
		return lines[:budget]
	}
	start := focus - budget + 1
	end := start + budget
	if end > len(lines) {
		end = len(lines)
		start = end - budget
	}
	if start < 0 {
		start = 0
	}
	return lines[start:end]
}

// blockLines compone un aviso como bloque completo: icono, texto envuelto por
// palabras y continuación alineada bajo el texto (nunca a ras del borde).
func blockLines(icon, text, color string, inner int) []string {
	if inner < 8 {
		inner = 8
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	prefix := ""
	if icon != "" {
		prefix = icon + " "
	}
	body := wrapWords(text, max(8, inner-layout.Width(prefix)))
	out := make([]string, 0, len(body))
	for i, l := range body {
		if i == 0 {
			out = append(out, style.Render(prefix+l))
			continue
		}
		out = append(out, style.Render("  "+l))
	}
	return out
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
