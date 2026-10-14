package tui

import (
	"context"
	"sort"
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

type homeReqMsg struct {
	dockerOK bool
	tsIP     string
	tsOK     bool
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
}

func newHomeScreen(sv *svc.Services, pal theme.Palette, ascii bool) *homeScreen {
	return &homeScreen{
		svc: sv, pal: pal, ascii: ascii, status: "cargando instancias…",
		idleSince: map[string]time.Time{}, memHist: map[string][]float64{},
	}
}

func (s *homeScreen) Init() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		diag, _ := s.svc.DockerState.Status(ctx)
		ip, _ := tailscale.ResolveIPv4(ctx)
		return homeReqMsg{dockerOK: diag.State.Ready(), tsIP: ip, tsOK: ip != ""}
	}
}

func (s *homeScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case homeReqMsg:
		s.dockerOK, s.tsIP, s.tsOK, s.reqs = msg.dockerOK, msg.tsIP, msg.tsOK, true
		return s, nil
	case pollRowsMsg:
		if msg.err != nil {
			s.status = "error al listar: " + msg.err.Error()
			return s, nil
		}
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
		return s, s.detailCmd()
	case homeDetailMsg:
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
	}
	return nil
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
	s.detailGen++
	return s, s.detailCmd()
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

func (s *homeScreen) detailCmd() tea.Cmd {
	slug := s.selected()
	if slug == "" {
		return nil
	}
	gen := s.detailGen
	ref := s.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		d, err := ref.Instances.Detail(ctx, slug)
		return homeDetailMsg{d: d, err: err, slug: slug, gen: gen}
	}
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
	if s.detail == nil || s.detail.Spec == nil {
		return nil
	}
	addr := connectionFromSpec(s.detail.Spec).Preferred()
	if addr == "" {
		return func() tea.Msg {
			return toastMsg{Text: "⚠ No hay dirección de conexión todavía", Kind: components.Destructive}
		}
	}
	if err := copyToClipboard(addr); err != nil {
		return func() tea.Msg {
			return toastMsg{Text: "✖ No se pudo copiar: " + err.Error(), Kind: components.Destructive}
		}
	}
	return func() tea.Msg { return toastMsg{Text: "✓ Copiado: " + addr} }
}

// View pinta el dashboard según la clase de tamaño.
func (s *homeScreen) View(w, h int) string {
	class := layout.ClassifyWidth(w)
	if len(s.rows) == 0 {
		return s.emptyView(w, class)
	}
	list := s.listView(w, h, class)
	if class == layout.Compact {
		bar := components.ActionBar(s.Actions(), w, 0, s.pal)
		return list + "\n" + bar
	}
	// Wide: tres zonas (lista · detalle · actividad en vivo, §4.1).
	if class == layout.Wide {
		activityW := 28
		if w/4 > activityW {
			activityW = w / 4
		}
		activity := s.activityView(activityW, h)
		detailW := w - lipgloss.Width(list) - activityW - 4
		if detailW >= 30 {
			detail := s.detailView(detailW, h)
			return lipgloss.JoinHorizontal(lipgloss.Top, list, "  ", detail, "  ", activity)
		}
	}
	detailW := w - lipgloss.Width(list) - 2
	if detailW < 20 {
		detailW = 20
	}
	detail := s.detailView(detailW, h)
	return lipgloss.JoinHorizontal(lipgloss.Top, list, "  ", detail)
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

// activityView muestra el resumen operativo derivado (no logs crudos, §UI-09).
func (s *homeScreen) activityView(w, h int) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(s.pal.Accent)).Render("ACTIVIDAD"))
	b.WriteString("\n")
	lines := s.activityLines()
	max := h - 1
	if max < 1 {
		max = 1
	}
	if len(lines) > max {
		lines = lines[:max]
	}
	for _, l := range lines {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(l.pal)).Render(layout.Truncate(l.text, w)))
		b.WriteString("\n")
	}
	// Ancho fijo para que la zona reserve su sitio aunque no haya líneas.
	return lipgloss.NewStyle().Width(w).Render(strings.TrimRight(b.String(), "\n"))
}

func (s *homeScreen) emptyView(w int, class layout.Class) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(s.pal.Accent)).Render("Todavía no hay instancias"))
	b.WriteString("\n\n")
	b.WriteString("Crea tu primer servidor y badomens te dirá cómo conectarte.\n\n")
	b.WriteString(components.Button(components.Action{ID: "global.new", Verb: "Crear", Object: "mi primer servidor", Key: "n", Kind: components.Primary, Hint: "Abre el asistente."}, true, s.pal))
	b.WriteString("\n\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Requisitos"))
	b.WriteString("\n")
	if s.reqs {
		s.reqRow(&b, "Docker", s.dockerOK, "instalado y en marcha", "instálalo o arráncalo", "Ctrl+R")
		s.reqRow(&b, "Tailscale", s.tsOK, "IP "+s.tsIP, "sin IP del tailnet ('tailscale up')", "H")
	} else {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("comprobando Docker y Tailscale…"))
	}
	return b.String()
}

func (s *homeScreen) reqRow(b *strings.Builder, name string, ok bool, detail, fix, key string) {
	icon, color := "✓", s.pal.Good
	if !ok {
		icon, color = "✖", s.pal.Bad
	}
	line := "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(icon+" ") +
		lipgloss.NewStyle().Bold(true).Render(name) + "  " + detail
	if !ok {
		line += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Warn)).Render("["+key+"] "+fix)
	}
	b.WriteString(line + "\n")
}

func (s *homeScreen) listView(w, h int, class layout.Class) string {
	width := 34
	if class == layout.Wide {
		width = 30
	}
	if class == layout.Compact {
		width = w
	}
	var b strings.Builder
	head := "INSTANCIAS (" + itoa(len(s.rows)) + "/" + itoa(s.svc.MaxInstances) + " · " +
		itoa(s.running) + "/" + itoa(s.svc.MaxParallel) + " en marcha)"
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(s.pal.Accent)).Render(layout.Truncate(head, width)))
	b.WriteString("\n")
	max := h - 2
	shown := 0
	for i, r := range s.rows {
		if max > 0 && shown >= max {
			break
		}
		shown++
		line := s.rowLine(r, width-3)
		var rendered string
		if i == s.cursor {
			rendered = lipgloss.NewStyle().Background(lipgloss.Color(s.pal.Accent)).Foreground(lipgloss.Color(s.pal.AccentText)).Render(components.Pointer(s.ascii) + " " + line)
		} else {
			rendered = "  " + line
		}
		b.WriteString(zone.Mark("row:"+r.Slug, rendered))
		b.WriteString("\n")
	}
	if max > 0 && len(s.rows) > shown {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("▲ " + itoa(len(s.rows)-shown) + " más"))
	}
	return strings.TrimRight(b.String(), "\n")
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

func (s *homeScreen) detailView(w, h int) string {
	if s.detail == nil || s.detail.Spec == nil {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(s.pal.Muted)).Render("Selecciona una instancia (↑/↓).")
	}
	spec := s.detail.Spec
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(spec.Name + " · " + spec.Engine + " " + spec.Version))
	b.WriteString("\n" + components.Badge(stateKey(s.detail.State), s.pal, s.ascii))
	running := s.detail.State == "running"
	b.WriteString("\n\n")
	if row, ok := s.rowFor(spec.Slug); ok && running {
		b.WriteString("CPU/RAM ")
		b.WriteString(components.Sparkline(s.memHist[spec.Slug], 16))
		b.WriteString("  " + itoa(int(row.MemUsedMB)) + "/" + itoa(int(row.MemLimitMB)) + " MB\n\n")
	}
	conn := connectionFromSpec(spec)
	if s.detail.Host != "" {
		conn = connectionFromSpecWithHost(spec, s.detail.Host)
	}
	b.WriteString(components.ConnectCard(conn, w, s.pal, s.ascii))
	b.WriteString("\n\n")
	b.WriteString(s.actionsBar(w))
	return b.String()
}

func (s *homeScreen) rowFor(slug string) (svc.Row, bool) {
	for _, r := range s.rows {
		if r.Slug == slug {
			return r, true
		}
	}
	return svc.Row{}, false
}

func (s *homeScreen) actionsBar(w int) string {
	return components.ActionBar(s.Actions(), w, 0, s.pal)
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
		{ID: "instance.open", Verb: "Ver", Object: "detalle de " + name, Key: "enter", Hint: "Abre consola, logs, jugadores, backups y ajustes.", Kind: components.Navigation},
	}
}

func (s *homeScreen) Title() string { return "Instancias" }
func (s *homeScreen) ID() string    { return "home" }

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

func stripBadge(s string) string {
	// Quita el posible prefijo de insignia para recalcular con color de selección.
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[i+1:]
	}
	return s
}

func connectionFromSpec(spec *instance.Spec) connection.Info {
	return connectionFromSpecWithHost(spec, "")
}

func connectionFromSpecWithHost(spec *instance.Spec, host string) connection.Info {
	magic := ""
	if spec.Connection != nil {
		magic = spec.Connection.MagicDNS
		if host == "" {
			host = spec.Connection.Host
		}
	}
	return connection.FromData(spec.Slug, spec.Name, spec.Engine, spec.Version, spec.Port, spec.Pins, spec.OnlineMode, spec.WhitelistEnabled, host, magic, time.Now())
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

// sortedSlugs ordena slugs para salidas estables (usado en tests).
func sortedSlugs(rows []svc.Row) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Slug)
	}
	sort.Strings(out)
	return out
}
