package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"github.com/MrZudro/badomens/internal/connection"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tailscale"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

type homeDetailMsg struct {
	d    *svc.Detail
	err  error
	slug string
	gen  int
}

type homeLogsMsg struct {
	slug string
	text string
	err  error
}

type openInstanceMsg struct{ slug string }

// homeDetailDebounceMsg dispara la carga del detalle tras el debounce que evita
// consultas repetidas al recorrer instancias rápido (§7).
type homeDetailDebounceMsg struct{ gen int }

// homeNavDebounce es la espera antes de cargar el detalle de la selección.
const homeNavDebounce = 150 * time.Millisecond

// homeDetailTTL es la vigencia del detalle cacheado por slug: evita repetir la
// consulta al volver a una fila ya vista (§5.2/§6.2).
const homeDetailTTL = 3 * time.Second

// detailCacheEntry guarda una ficha con el instante en que se obtuvo.
type detailCacheEntry struct {
	d  *svc.Detail
	at time.Time
}

type homeReqMsg struct {
	dockerOK bool
	tsIP     string
	tsOK     bool
	// localIP es la IPv4 local del equipo (LAN); "" si no se pudo resolver.
	localIP string
}

// homeScreen es el dashboard: lista de instancias + detalle + tarjeta de conexión.
type homeScreen struct {
	svc      *svc.Services
	pal      theme.Palette
	ascii    bool
	rows     []svc.Row
	cursor   int
	detail   *svc.Detail
	status   string
	running  int
	dockerOK bool
	tsIP     string
	tsOK     bool
	reqs     bool
	// idleSince marca desde cuándo una instancia corre sin jugadores.
	idleSince map[string]time.Time
	memHist   map[string][]float64
	logs      string
	logSlug   string
	// detailGen identifica la selección vigente para descartar respuestas tardías.
	detailGen int
	// loaded indica que ya hubo un listado (aunque sea vacío) y loadErr el
	// último error de listado: distinguen vacío real de carga y de error (§5.2).
	loaded  bool
	loadErr error
	// magic es el estado EN VIVO de MagicDNS (Fase 8 §5): alimenta la tarjeta de
	// conexión en lugar del valor persistido y obsoleto del Spec.
	magic magicState
	// modal es la confirmación explícita del borrado (§11): nunca se borra sin
	// pasar por él, ni desde la lista ni desde el detalle.
	modal     *components.Modal
	modalSlug string
	// localIP es la IPv4 local (LAN), resuelta en un Cmd: permite ofrecer una
	// dirección copiable antes del primer arranque (§10).
	localIP string
	// cache guarda el detalle por slug con TTL corto (§5.2).
	cache map[string]detailCacheEntry
	// zone es el foco entre las zonas/paneles de Inicio (lista · detalle ·
	// actividad): Tab avanza y Shift+Tab retrocede (§1.2 «Desplazarse»).
	zone zoneFocus
}

func newHomeScreen(sv *svc.Services, pal theme.Palette, ascii bool) *homeScreen {
	return &homeScreen{
		svc: sv, pal: pal, ascii: ascii, status: "cargando instancias…",
		idleSince: map[string]time.Time{}, memHist: map[string][]float64{},
		cache: map[string]detailCacheEntry{},
	}
}

func (s *homeScreen) Init() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		diag, _ := s.svc.DockerState.Status(ctx)
		ip, _ := tailscale.ResolveIPv4(ctx)
		return homeReqMsg{dockerOK: diag.State.Ready(), tsIP: ip, tsOK: ip != "", localIP: localIPv4()}
	}
}

func (s *homeScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case homeReqMsg:
		s.dockerOK, s.tsIP, s.tsOK, s.reqs = msg.dockerOK, msg.tsIP, msg.tsOK, true
		if msg.localIP != "" {
			s.localIP = msg.localIP
		}
		return s, nil
	case localIPMsg:
		s.localIP = msg.ip
		return s, nil
	case instanceDeletedMsg:
		// El borrado ya se verificó en el servicio de la pantalla (deleteCmd): la
		// fila desaparece de la lista y se confirma con una notificación.
		out := s.rows[:0]
		for _, r := range s.rows {
			if r.Slug != msg.slug {
				out = append(out, r)
			}
		}
		s.rows = out
		if s.cursor >= len(s.rows) && s.cursor > 0 {
			s.cursor--
		}
		delete(s.cache, msg.slug)
		delete(s.memHist, msg.slug)
		delete(s.idleSince, msg.slug)
		if s.detail != nil && s.detail.Spec != nil && s.detail.Spec.Slug == msg.slug {
			s.detail = nil
		}
		if len(s.rows) == 0 {
			s.status = "no hay instancias todavía"
		}
		return s, func() tea.Msg {
			return toastMsg{Text: "✓ Instancia «" + msg.slug + "» eliminada (contenedor y datos)"}
		}
	case magicProbeMsg:
		// El estado en vivo reevalúa MagicDNS: si se resuelve, el aviso desaparece.
		s.magic = magicStateFrom(msg.rep)
		return s, nil
	case pollRowsMsg:
		if msg.err != nil {
			s.loadErr = msg.err
			s.status = components.Cross(s.ascii) + " error al listar: " + msg.err.Error()
			return s, nil
		}
		s.loadErr = nil
		s.loaded = true
		prev := s.selected()
		s.rows = msg.rows
		s.running = 0
		now := nowUTC()
		seen := map[string]bool{}
		for _, r := range s.rows {
			if r.State == "running" {
				s.running++
			}
			seen[r.Slug] = true
			if r.State == "running" && r.PlayersOnline == 0 && r.IdleEnabled {
				if _, ok := s.idleSince[r.Slug]; !ok {
					s.idleSince[r.Slug] = now
				}
			} else {
				delete(s.idleSince, r.Slug)
			}
			if r.State == "running" && r.MemUsedMB > 0 {
				h := append(s.memHist[r.Slug], r.MemUsedMB)
				if len(h) > 40 {
					h = h[len(h)-40:]
				}
				s.memHist[r.Slug] = h
			}
		}
		for slug := range s.memHist {
			if !seen[slug] {
				delete(s.memHist, slug)
			}
		}
		for slug := range s.cache {
			if !seen[slug] {
				delete(s.cache, slug)
			}
		}
		s.cursor = 0
		if prev != "" {
			for i, r := range s.rows {
				if r.Slug == prev {
					s.cursor = i
				}
			}
		}
		if len(s.rows) == 0 {
			s.status = "no hay instancias todavía"
			return s, nil
		}
		s.status = ""
		s.detailGen++
		return s, s.refreshDetailCmd()
	case homeDetailDebounceMsg:
		if msg.gen != s.detailGen {
			return s, nil // la selección cambió: tick obsoleto
		}
		return s, s.detailCmd()
	case homeDetailMsg:
		// Cachea por slug aunque la selección ya haya cambiado (TTL corto): así
		// volver a una fila reciente no repite la consulta (§5.2).
		if msg.err == nil && msg.d != nil && msg.slug != "" {
			s.cache[msg.slug] = detailCacheEntry{d: msg.d, at: nowUTC()}
		}
		// Descarta respuestas de otra instancia o de una selección ya cambiada.
		if msg.slug != "" && (msg.slug != s.selected() || msg.gen != s.detailGen) {
			return s, nil
		}
		if msg.err == nil {
			s.detail = msg.d
		}
		return s, nil
	case homeLogsMsg:
		if msg.slug == s.selected() {
			s.logs = msg.text
			s.logSlug = msg.slug
		}
		return s, nil
	case tea.KeyMsg:
		return s.handleKey(msg)
	}
	return s, nil
}

func (s *homeScreen) handleKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	// Una confirmación abierta captura las teclas: el borrado nunca se ejecuta sin
	// pasar por el diálogo (§11).
	if s.modal != nil {
		return s, s.handleModalKey(msg)
	}
	switch msg.String() {
	case "j", "down":
		return s.move(1)
	case "k", "up":
		return s.move(-1)
	case "enter":
		if slug, ok := s.selectedSlug(); ok {
			slugCopy := slug
			return s, func() tea.Msg { return openInstanceMsg{slug: slugCopy} }
		}
	case "s":
		return s, s.invokeOp("instance.start")
	case "p":
		return s, s.invokeOp("instance.stop")
	case "r":
		return s, s.invokeOp("instance.restart")
	case "b":
		return s, s.invokeOp("instance.backup")
	case "c":
		return s, s.copyAddress()
	case "D":
		return s, s.startDeletePrompt()
	case "tab":
		// «Desplazarse»: mueve el foco entre las zonas/paneles de Inicio sin
		// salir de la pantalla (SPEC 2 §1.2/§15).
		s.zone.move(1)
		return s, nil
	case "shift+tab":
		s.zone.move(-1)
		return s, nil
	}
	return s, nil
}

// Invoke ejecuta una acción de la pantalla por su ID (paleta y botones).
func (s *homeScreen) Invoke(id string) tea.Cmd {
	switch id {
	case "instance.start", "instance.stop", "instance.restart":
		return s.invokeOp(id)
	case "instance.backup":
		return s.invokeOp(id)
	case "instance.open":
		if slug, ok := s.selectedSlug(); ok {
			return func() tea.Msg { return openInstanceMsg{slug: slug} }
		}
	case "instance.copy":
		return s.copyAddress()

	case "instance.delete":
		return s.startDeletePrompt()
	}
	return nil
}

// handleModalKey atiende la confirmación del borrado desde la lista: Esc cancela
// sin tocar nada, Ctrl+B hace un backup previo y Enter solo borra cuando el
// nombre exacto ya está escrito (confirmación reforzada, §11).
func (s *homeScreen) handleModalKey(msg tea.KeyMsg) tea.Cmd {
	m := *s.modal
	key := msg.String()
	if key == "ctrl+b" {
		// La confirmación reforzada sigue siendo obligatoria: sin el nombre exacto
		// no se borra nada (el backup no sustituye la confirmación).
		if !m.Enabled() {
			return func() tea.Msg {
				return toastMsg{Text: "✖ Escribe el nombre exacto para confirmar; el backup no sustituye la confirmación", Kind: components.Destructive}
			}
		}
		return s.confirmDelete(true)
	}
	if m.RequireText != "" {
		if r := []rune(key); len(r) == 1 && r[0] >= 32 {
			m.Typed += string(r[0])
			s.modal = &m
			return nil
		}
	}
	switch key {
	case "esc":
		s.clearModal()
		return nil
	case "left", "h", "shift+tab", "up":
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
		if r := []rune(m.Typed); len(r) > 0 {
			m.Typed = string(r[:len(r)-1])
			s.modal = &m
		}
		return nil
	case "enter":
		if !m.Enabled() {
			return nil
		}
		return s.confirmDelete(false)
	}
	return nil
}

// confirmDelete ejecuta el borrado ya confirmado desde la lista, con backup previo
// opcional (Ctrl+B).
func (s *homeScreen) confirmDelete(withBackup bool) tea.Cmd {
	slug := s.modalSlug
	s.clearModal()
	if strings.TrimSpace(slug) == "" {
		return nil
	}
	return s.deleteCmd(slug, withBackup)
}

// clearModal cierra la confirmación sin ejecutar nada.
func (s *homeScreen) clearModal() {
	s.modal, s.modalSlug = nil, ""
}

// startDeletePrompt abre la confirmación reforzada del borrado con el foco en el
// botón seguro: detalla qué se elimina, avisa si el servidor está en marcha y
// ofrece un backup previo con Ctrl+B (§11).
func (s *homeScreen) startDeletePrompt() tea.Cmd {
	slug, ok := s.selectedSlug()
	if !ok {
		return func() tea.Msg {
			return toastMsg{Text: "⚠ selecciona una instancia para borrarla", Kind: components.Destructive}
		}
	}
	running := false
	for _, r := range s.rows {
		if r.Slug == slug {
			running = r.State == "running"
		}
	}
	body := "Se eliminarán el contenedor, los datos del servidor, el mundo (Overworld, Nether y End), " +
		"los backups y la configuración de «" + slug + "»."
	if running {
		body += " El servidor está EN MARCHA: se detendrá antes de borrar."
	}
	body += " Pulsa [Ctrl+B] para hacer un backup antes (recomendado)."
	m := components.DestroyConfirm("eliminar", slug)
	m.Actions[0].DisabledReason = "escribe el nombre"
	m.Body = body
	s.modal, s.modalSlug = &m, slug
	return nil
}

// deleteCmd elimina la instancia verificando el resultado: si el servidor está en
// marcha se detiene antes, se puede hacer un backup previo y, si el borrado no se
// puede comprobar, no se informa de éxito (T-08).
func (s *homeScreen) deleteCmd(slug string, withBackup bool) tea.Cmd {
	ref := s.svc
	running := false
	for _, r := range s.rows {
		if r.Slug == slug {
			running = r.State == "running"
		}
	}
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
		if _, err := ref.Instances.Detail(ctx, slug); err == nil {
			return toastMsg{Text: "⚠ «" + slug + "»: el borrado no se pudo verificar (la instancia sigue en el registro)", Kind: components.Destructive}
		}
		return instanceDeletedMsg{slug: slug}
	})
}

func (s *homeScreen) invokeOp(id string) tea.Cmd {
	slug := s.selected()
	if slug == "" {
		return nil
	}
	switch id {
	case "instance.start":
		return s.lifecycle("start", slug)
	case "instance.stop":
		return s.lifecycle("stop", slug)
	case "instance.restart":
		return s.lifecycle("restart", slug)
	case "instance.backup":
		return s.backup(slug)
	}
	return nil
}

func (s *homeScreen) move(d int) (Screen, tea.Cmd) {
	if len(s.rows) == 0 {
		return s, nil
	}
	s.cursor += d
	if s.cursor < 0 {
		s.cursor = 0
	}
	if s.cursor >= len(s.rows) {
		s.cursor = len(s.rows) - 1
	}
	// Debounce: no se lanza la carga de detalle en cada pulsación (§7).
	s.detailGen++
	gen := s.detailGen
	return s, tea.Tick(homeNavDebounce, func(time.Time) tea.Msg {
		return homeDetailDebounceMsg{gen: gen}
	})
}

func (s *homeScreen) selectedSlug() (string, bool) {
	if len(s.rows) == 0 || s.cursor < 0 || s.cursor >= len(s.rows) {
		return "", false
	}
	return s.rows[s.cursor].Slug, true
}

func (s *homeScreen) selected() string {
	slug, _ := s.selectedSlug()
	return slug
}

// detailCmd carga el detalle de la selección; si hay una ficha reciente en cache
// (TTL corto) la sirve sin tocar el servicio (§5.2).
func (s *homeScreen) detailCmd() tea.Cmd {
	slug := s.selected()
	if slug == "" {
		return nil
	}
	gen := s.detailGen
	if d := s.cachedDetail(slug); d != nil {
		return func() tea.Msg { return homeDetailMsg{d: d, slug: slug, gen: gen} }
	}
	return s.fetchDetailCmd(slug, gen)
}

// refreshDetailCmd fuerza una consulta nueva (tras el sondeo, el estado de la
// instancia puede haber cambiado).
func (s *homeScreen) refreshDetailCmd() tea.Cmd {
	slug := s.selected()
	if slug == "" {
		return nil
	}
	return s.fetchDetailCmd(slug, s.detailGen)
}

func (s *homeScreen) fetchDetailCmd(slug string, gen int) tea.Cmd {
	ref := s.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		d, err := ref.Instances.Detail(ctx, slug)
		return homeDetailMsg{d: d, err: err, slug: slug, gen: gen}
	}
}

// cachedDetail devuelve la ficha cacheada de un slug si sigue vigente.
func (s *homeScreen) cachedDetail(slug string) *svc.Detail {
	e, ok := s.cache[slug]
	if !ok || e.d == nil {
		return nil
	}
	if nowUTC().Sub(e.at) > homeDetailTTL {
		delete(s.cache, slug)
		return nil
	}
	return e.d
}

// logsCmd carga las últimas líneas de la instancia seleccionada para la zona de
// actividad en modo Wide (§4.1).
func (s *homeScreen) logsCmd() tea.Cmd {
	slug := s.selected()
	if slug == "" {
		return nil
	}
	ref := s.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		text, err := ref.Instances.Logs(ctx, slug, 40)
		return homeLogsMsg{slug: slug, text: text, err: err}
	}
}

func (s *homeScreen) lifecycle(op, slug string) tea.Cmd {
	ref := s.svc
	delete(s.cache, slug) // el estado cambia: invalida la ficha cacheada
	return tea.Batch(busyCmd(true), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		started := time.Now()
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
			return toastMsg{Text: "✖ No se pudo " + op + " «" + slug + "»: " + err.Error() + " — revisa Docker con H", Kind: components.Destructive}
		}
		secs := int(time.Since(started).Seconds())
		switch op {
		case "start":
			return toastMsg{Text: "✓ «" + slug + "» iniciado en " + itoa(secs) + " s"}
		case "stop":
			return toastMsg{Text: "✓ «" + slug + "» detenido (mundo guardado)"}
		default:
			return toastMsg{Text: "✓ «" + slug + "» reiniciado"}
		}
	})
}

func (s *homeScreen) backup(slug string) tea.Cmd {
	ref := s.svc
	delete(s.cache, slug)
	return tea.Batch(busyCmd(true), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		if err := ref.Backups.Create(ctx, slug); err != nil {
			return toastMsg{Text: "✖ No se pudo crear el backup de «" + slug + "»: " + err.Error(), Kind: components.Destructive}
		}
		return toastMsg{Text: "✓ «" + slug + "»: backup creado con checksum"}
	})
}

func (s *homeScreen) copyAddress() tea.Cmd {
	return copyAddressCmd(s.copyCandidates(), s.selected())
}

// (Elegir entre varias direcciones vive en el detalle, pestaña Red: [C]. En la
// lista, [c] copia la preferida con una sola pulsación.)

// copyCandidates reúne las direcciones copiables de la instancia seleccionada: IP
// del tailnet, nombre MagicDNS (estado EN VIVO si se conoce) y red local.
func (s *homeScreen) copyCandidates() []copyCandidate {
	spec := s.selectedSpec()
	if spec == nil {
		return nil
	}
	host := s.tsIP
	if s.detail != nil && s.detail.Spec != nil && s.detail.Spec.Slug == spec.Slug {
		if s.detail.Host != "" {
			host = s.detail.Host
		}
	}
	return addressCandidates(spec, host, s.magic, s.localIP)
}

// selectedSpec devuelve el Spec de la instancia seleccionada si ya se cargó.
func (s *homeScreen) selectedSpec() *instance.Spec {
	if s.detail != nil && s.detail.Spec != nil && s.detail.Spec.Slug == s.selected() {
		return s.detail.Spec
	}
	for _, r := range s.rows {
		if r.Slug != s.selected() {
			continue
		}
		// Sin detalle cargado se usa un Spec mínimo: el puerto es el dato que
		// permite derivar la dirección ANTES del primer arranque (§10).
		return &instance.Spec{Slug: r.Slug, Name: r.Name, Engine: r.Engine, Version: r.Version, Port: r.Port}
	}
	return nil
}

// View pinta el panel de Inicio por regiones (lista · detalle · actividad) según
// la clase de tamaño (§5.2). Todo el estado vive en paneles con borde; las
// acciones van en la barra inferior de la carcasa, no dentro del cuerpo.
func (s *homeScreen) View(w, h int) string {
	// Una confirmación abierta ocupa la vista: se pinta sola para que no la recorte
	// el alto reservado ni quede tapada por las columnas de Inicio.
	if s.modal != nil {
		return s.modal.RenderFit(w, h, s.pal, s.ascii)
	}
	if len(s.rows) == 0 {
		s.zone.set(1)
		switch {
		case s.loadErr != nil:
			return s.errorView(w, h)
		case !s.loaded:
			return s.loadingView(w, h)
		default:
			return s.emptyView(w, h)
		}
	}
	switch layout.ClassifyWidth(w) {
	case layout.Wide:
		return s.wideView(w, h)
	case layout.Normal:
		return s.normalView(w, h)
	}
	s.zone.set(1)
	return s.listPanel(w, h, s.zone.active(0))
}

// wideView compone lista · detalle · actividad (≥110 celdas interiores).
func (s *homeScreen) wideView(w, h int) string {
	listW, activityW := 44, 26
	if w >= 140 {
		activityW = 32
	}
	detailW := w - listW - activityW - 2
	if detailW < 30 {
		return s.normalView(w, h)
	}
	s.zone.set(3) // lista · detalle · actividad
	return lipgloss.JoinHorizontal(lipgloss.Top,
		s.listPanel(listW, h, true), " ",
		s.detailPanel(detailW, h), " ",
		s.activityPanel(activityW, h))
}

// normalView compone lista · detalle (70–109 celdas interiores).
func (s *homeScreen) normalView(w, h int) string {
	listW := w * 2 / 5
	if listW < 28 {
		listW = 28
	}
	if listW > 46 {
		listW = 46
	}
	detailW := w - listW - 1
	if detailW < 30 {
		listW = w - 31
		if listW < 24 {
			s.zone.set(1)
			return s.listPanel(w, h, s.zone.active(0))
		}
		detailW = w - listW - 1
	}
	s.zone.set(2) // lista · detalle
	return lipgloss.JoinHorizontal(lipgloss.Top,
		s.listPanel(listW, h, true), " ",
		s.detailPanel(detailW, h))
}

// homeActivity es una línea derivada del estado (nunca logs crudos) para la
// zona de actividad de Inicio (UI-09).
type homeActivity struct {
	text string
	pal  string
}

// activityLines resume el estado operativo sin mostrar el stream de consola.
func (s *homeScreen) activityLines() []homeActivity {
	var out []homeActivity
	for _, r := range s.rows {
		if r.State == "running" {
			line := "● " + r.Name
			if r.PlayersOnline >= 0 {
				line += "  " + itoa(r.PlayersOnline) + "/" + itoa(r.MaxPlayers) + " jug."
			}
			if r.MemLimitMB > 0 {
				line += "  " + itoa(int(r.MemUsedMB)) + "M"
			}
			out = append(out, homeActivity{text: line, pal: s.pal.Good})
		}
		if r.IdleEnabled {
			out = append(out, homeActivity{text: "☾ " + r.Name + " · " + s.idleLabel(r), pal: s.pal.Warn})
		} else if r.StoppedBy == "idle" {
			out = append(out, homeActivity{text: "☾ " + r.Name + " apagado por inactividad", pal: s.pal.Muted})
		}
	}
	if len(out) == 0 {
		out = append(out, homeActivity{text: "Sin novedades", pal: s.pal.Muted})
	}
	return out
}

// activityPanel muestra el resumen operativo derivado (sin logs crudos, UI-09)
// dentro de un panel con borde.
func (s *homeScreen) activityPanel(w, h int) string {
	lines := s.activityLines()
	max := h - 2
	if max < 1 {
		max = 1
	}
	if len(lines) > max {
		lines = lines[:max]
	}
	rendered := make([]string, 0, len(lines))
	for _, l := range lines {
		rendered = append(rendered, lipgloss.NewStyle().Foreground(lipgloss.Color(l.pal)).Render(layout.Truncate(l.text, w-2)))
	}
	return components.Panel(components.ZoneTitle("ACTIVIDAD", s.zone.active(2), s.ascii), "", strings.Join(rendered, "\n"), w, h, s.zone.active(2), s.pal, s.ascii)
}

// emptyView es el estado vacío: causa (CTA para crear) + requisitos, dentro de
// un panel con borde (§5.2).
func (s *homeScreen) emptyView(w, h int) string {
	inner := h - 2
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(s.pal.Accent)).Render("Todavía no hay instancias")}
	if inner >= 7 {
		lines = append(lines, "", "Crea tu primer servidor y badomens te dirá cómo conectarte.", "")
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, components.Button(components.Action{ID: "global.new", Verb: "Crear", Object: "mi primer servidor", Key: "n", Kind: components.Primary, Hint: "Abre el asistente."}, true, s.pal))
	if inner >= 10 {
		lines = append(lines, "", lipgloss.NewStyle().Bold(true).Render("Requisitos"))
		if s.reqs {
			lines = append(lines,
				s.reqLine("Docker", s.dockerOK, "instalado y en marcha", "instálalo o arráncalo", "Ctrl+R"),
				s.reqLine("Tailscale", s.tsOK, "IP "+s.tsIP, "sin IP del tailnet ('tailscale up')", "F2"))
		} else {
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("comprobando Docker y Tailscale…"))
		}
	}
	return components.Panel("INSTANCIAS", s.listCounters(w), strings.Join(lines, "\n"), w, h, false, s.pal, s.ascii)
}

// loadingView es el primer listado aún sin respuesta.
func (s *homeScreen) loadingView(w, h int) string {
	body := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("cargando instancias…")
	return components.Panel("INSTANCIAS", "", body, w, h, false, s.pal, s.ascii)
}

// errorView es el error recuperable de listado (con acción de reintento).
func (s *homeScreen) errorView(w, h int) string {
	line := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Bad)).Render(components.Cross(s.ascii) + " error al listar: " + s.loadErr.Error())
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("Se reintentará automáticamente; revisa Docker con F2.")
	return components.Panel("INSTANCIAS", "", line+"\n\n"+hint, w, h, false, s.pal, s.ascii)
}

func (s *homeScreen) reqLine(name string, ok bool, detail, fix, key string) string {
	icon, color := "✓", s.pal.Good
	if !ok {
		icon, color = "✖", s.pal.Bad
	}
	line := "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(icon+" ") +
		lipgloss.NewStyle().Bold(true).Render(name) + "  " + detail
	if !ok {
		line += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Warn)).Render("["+key+"] "+fix)
	}
	return line
}

// listPanel dibuja la lista de instancias en un panel con borde: el título lleva
// los contadores y la fila seleccionada una banda con puntero (§5.2).
func (s *homeScreen) listPanel(w, h int, focused bool) string {
	return components.Panel(components.ZoneTitle("INSTANCIAS", focused, s.ascii), s.listCounters(w), s.listBody(w-2, h-2), w, h, focused, s.pal, s.ascii)
}

// listCounters resume instancias y cupos. Usa la forma completa («… en marcha»)
// si cabe junto al título del panel y la corta en caso contrario, para no
// recortar nunca el título.
func (s *homeScreen) listCounters(w int) string {
	base := itoa(len(s.rows)) + "/" + itoa(s.svc.MaxInstances) + " · " +
		itoa(s.running) + "/" + itoa(s.svc.MaxParallel)
	inner := w - 2
	need := 3 + layout.Width("INSTANCIAS") + 3
	if need+layout.Width(base+" en marcha") <= inner {
		return base + " en marcha"
	}
	if need+layout.Width(base) <= inner {
		return base
	}
	return ""
}

// listBody compone las filas visibles (máx. rows líneas) con puntero y banda de
// selección; la banda se pinta al ancho interior para que no dependa del color.
func (s *homeScreen) listBody(innerW, rows int) string {
	if rows < 1 {
		rows = 1
	}
	var b strings.Builder
	shown := 0
	for i, r := range s.rows {
		if shown >= rows {
			break
		}
		shown++
		line := s.rowLine(r, innerW-2)
		var rendered string
		if i == s.cursor {
			band := lipgloss.NewStyle().Background(lipgloss.Color(s.pal.Accent)).Foreground(lipgloss.Color(s.pal.AccentText))
			rendered = band.Render(padPlain(components.Pointer(s.ascii)+" "+line, innerW))
		} else {
			rendered = "  " + line
		}
		b.WriteString(zone.Mark("row:"+r.Slug, rendered))
		b.WriteString("\n")
	}
	if len(s.rows) > shown {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("▲ " + itoa(len(s.rows)-shown) + " más"))
	}
	return strings.TrimRight(b.String(), "\n")
}

// padPlain ajusta texto sin ANSI al ancho visual exacto (corta con «…» o rellena
// con espacios). Se usa antes de aplicar la banda para que cubra toda la fila.
func padPlain(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := layout.Width(s)
	if sw > w {
		s = layout.Truncate(s, w)
		sw = layout.Width(s)
	}
	if sw < w {
		s += strings.Repeat(" ", w-sw)
	}
	return s
}

// rowLine compone la fila de una instancia: insignia, nombre, jugadores, RAM,
// ip:puerto y auto-apagado (RF-1/§5.1).
func (s *homeScreen) rowLine(r svc.Row, width int) string {
	parts := []string{r.Name}
	players := "-/" + itoa(r.MaxPlayers)
	if r.PlayersOnline >= 0 {
		players = itoa(r.PlayersOnline) + "/" + itoa(r.MaxPlayers)
	}
	parts = append(parts, players)
	if r.State == "running" && r.MemLimitMB > 0 {
		parts = append(parts, itoa(int(r.MemUsedMB))+"/"+itoa(int(r.MemLimitMB))+"M")
	} else {
		parts = append(parts, r.Memory)
	}
	if r.Host != "" {
		parts = append(parts, r.Host+":"+itoa(r.Port))
	} else {
		parts = append(parts, "pto "+itoa(r.Port))
	}
	if r.IdleEnabled {
		parts = append(parts, s.idleLabel(r))
	} else if r.StoppedBy == "idle" {
		parts = append(parts, "☾ idle")
	}
	return layout.Truncate(strings.Join(parts, "  "), width)
}

// idleLabel muestra la cuenta atrás del auto-apagado si está armado.
func (s *homeScreen) idleLabel(r svc.Row) string {
	since, ok := s.idleSince[r.Slug]
	if !ok || r.IdleTimeout <= 0 {
		return "idle " + shortDur(r.IdleTimeout)
	}
	left := r.IdleTimeout - nowUTC().Sub(since)
	if left < 0 {
		left = 0
	}
	return "Apagado en " + mmss(left)
}

// detailPanel resume la instancia seleccionada en un panel con borde, sin cajas
// anidadas (§5.2): identidad, estado, ocupación y tarjeta de conexión.
func (s *homeScreen) detailPanel(w, h int) string {
	if s.detail == nil || s.detail.Spec == nil {
		msg := "Selecciona una instancia (↑/↓)."
		if len(s.rows) > 0 && s.cachedDetail(s.selected()) == nil {
			msg = "cargando detalle…"
		}
		return components.Panel(components.ZoneTitle("DETALLE", s.zone.active(1), s.ascii), "", lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render(msg), w, h, s.zone.active(1), s.pal, s.ascii)
	}
	spec := s.detail.Spec
	var lines []string
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render(spec.Name+" · "+spec.Engine+" "+spec.Version))
	lines = append(lines, components.Badge(stateKey(s.detail.State), s.pal, s.ascii))
	if row, ok := s.rowFor(spec.Slug); ok {
		players := "-/" + itoa(row.MaxPlayers)
		if row.PlayersOnline >= 0 {
			players = itoa(row.PlayersOnline) + "/" + itoa(row.MaxPlayers)
		}
		lines = append(lines,
			"",
			"Jugadores  "+players,
			"Puerto     "+itoa(spec.Port))
		if spec.Memory != "" {
			lines = append(lines, "Memoria    "+spec.Memory)
		}
		if s.detail.State == "running" && row.MemLimitMB > 0 {
			lines = append(lines, "CPU/RAM    "+components.Sparkline(s.memHist[spec.Slug], 12)+"  "+
				itoa(int(row.MemUsedMB))+"/"+itoa(int(row.MemLimitMB))+" MB")
		}
		if row.IdleEnabled {
			lines = append(lines, "Auto-apagado  "+s.idleLabel(row))
		}
	}
	conn := connectionInfo(spec, s.detail.Host, s.magic)
	lines = append(lines, "")
	lines = append(lines, components.ConnectLines(conn, s.pal, s.ascii)...)
	return components.Panel(components.ZoneTitle("DETALLE", s.zone.active(1), s.ascii), "", strings.Join(lines, "\n"), w, h, s.zone.active(1), s.pal, s.ascii)
}

func (s *homeScreen) rowFor(slug string) (svc.Row, bool) {
	for _, r := range s.rows {
		if r.Slug == slug {
			return r, true
		}
	}
	return svc.Row{}, false
}

func (s *homeScreen) Actions() []components.Action {
	slug := s.selected()
	name := "«" + slug + "»"
	state := ""
	if len(s.rows) > 0 && s.cursor < len(s.rows) {
		state = s.rows[s.cursor].State
	}
	running := state == "running"
	atCap := !running && s.running >= s.svc.MaxParallel
	startReason := "Ya está en marcha"
	if atCap {
		startReason = "Ya hay " + itoa(s.svc.MaxParallel) + " servidores en marcha (límite); detén uno primero"
	}
	return []components.Action{
		{ID: "instance.start", Verb: "Iniciar", Object: name, Key: "s", Hint: "Arranca el servidor y recalcula cómo conectarse.", Kind: components.Primary, Disabled: running || atCap, DisabledReason: startReason},
		{ID: "instance.stop", Verb: "Detener", Object: name + " (guarda el mundo)", Key: "p", Hint: "Apaga el servidor guardando el mundo.", Kind: components.Secondary, Disabled: !running, DisabledReason: "El servidor no está en marcha"},
		{ID: "instance.restart", Verb: "Reiniciar", Object: name, Key: "r", Hint: "Recrea el contenedor aplicando cambios.", Kind: components.Secondary, Disabled: !running, DisabledReason: "El servidor no está en marcha"},
		{ID: "instance.backup", Verb: "Crear", Object: "backup ahora", Key: "b", Hint: "Guarda una copia de seguridad con checksum.", Kind: components.Secondary},
		{ID: "instance.copy", Verb: "Copiar", Object: "dirección de conexión", Key: "c", Hint: "Copia la dirección ip:puerto al portapapeles.", Kind: components.Secondary},

		{ID: "instance.delete", Verb: "Eliminar", Object: name + " para siempre", Key: "D", Hint: "Borra el contenedor y sus datos; pide escribir el nombre.", Kind: components.Destructive},
		{ID: "instance.open", Verb: "Ver", Object: "detalle de " + name, Key: "enter", Hint: "Abre consola, logs, jugadores, backups y ajustes.", Kind: components.Navigation},
	}
}

// CapturesInput: la lista captura las teclas mientras hay una confirmación
// abierta (las letras del nombre no son atajos globales).
func (s *homeScreen) CapturesInput() bool { return s.modal != nil }

func (s *homeScreen) Title() string { return "Instancias" }
func (s *homeScreen) ID() string    { return "home" }

// Status aporta el mensaje de la barra de estado (vacío mientras no haya nada).
func (s *homeScreen) Status() string { return s.status }

// Click abre el detalle de la fila pulsada con el ratón (§4.1).
func (s *homeScreen) Click(m tea.MouseMsg) tea.Cmd {
	for _, r := range s.rows {
		if z := zone.Get("row:" + r.Slug); z != nil && z.InBounds(m) {
			slug := r.Slug
			return func() tea.Msg { return openInstanceMsg{slug: slug} }
		}
	}
	return nil
}

// stateKey traduce el estado crudo de Docker a una clave de insignia.
func stateKey(state string) string {
	switch state {
	case "running":
		return "running"
	case "restarting":
		return "starting"
	case "exited", "created", "":
		return "stopped"
	case "paused":
		return "problems"
	}
	return state
}

// connectionFromSpecWithHost deriva la conexión con el estado de MagicDNS que
// tenga la pantalla; sin estado en vivo, cae al valor persistido del Spec.
func connectionFromSpecWithHost(spec *instance.Spec, host string) connection.Info {
	return connectionInfo(spec, host, magicState{})
}

// shortDur redacta una duración corta para la columna de auto-apagado.
func shortDur(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	if d < time.Hour {
		return itoa(int(d.Minutes())) + "m"
	}
	return itoa(int(d.Hours())) + "h"
}

// mmss formatea una cuenta atrás como MM:SS.
func mmss(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	return two(total/60) + ":" + two(total%60)
}

func two(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

// busyCmd avisa a la app de que hay una operación en curso.
func busyCmd(on bool) tea.Cmd {
	return func() tea.Msg { return busyMsg{on: on} }
}
