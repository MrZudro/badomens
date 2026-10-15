package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	gdisk "github.com/shirou/gopsutil/v4/disk"

	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/host"
	"github.com/MrZudro/badomens/internal/tailscale"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
	updx "github.com/MrZudro/badomens/internal/update"
)

type (
	capacityMsg  struct{ cap host.Capacity }
	tailscaleMsg struct {
		rep tailscale.Report
	}
)

type healthDiskMsg struct {
	freeGB  float64
	totalGB float64
	err     error
}
type healthPortsMsg struct {
	conflicts []string
}
type healthUpdateMsg struct {
	latest string
	has    bool
}
type healthReexpMsg struct{}

// healthRAMMsg trae la RAM asignada a las instancias, medida en un tea.Cmd (la
// vista nunca vuelve a medir, §7).
type healthRAMMsg struct {
	assignedGB float64
	err        error
}

// healthLevel clasifica el estado de una comprobación (§8): el estado nunca se
// comunica solo con color; cada nivel lleva su icono y su texto.
type healthLevel int

const (
	// healthUnknown: todavía sin comprobar.
	healthUnknown healthLevel = iota
	// healthOK: comprobado y correcto.
	healthOK
	// healthWarn: comprobado con aviso (degradado, no bloqueante).
	healthWarn
	// healthBad: comprobado y crítico.
	healthBad
)

// healthItem es una fila de estado dentro de un panel de Salud.
type healthItem struct {
	name   string
	status string
	detail string
	level  healthLevel
}

// healthScreen es "Salud del sistema" (H): Docker, Compose, Tailscale, puertos,
// disco, RAM y actualización (P-01/RF-6).
type healthScreen struct {
	svc      *svc.Services
	pal      theme.Palette
	ascii    bool
	docker   dockerx.Diagnosis
	capacity host.Capacity
	// ts es el estado EN VIVO de Tailscale (incluye MagicDNS tipado). Una única
	// fuente de verdad: la pantalla no vuelve a consultar tailscale.
	ts     tailscale.Report
	loaded bool
	// magicDismissed oculta la advertencia legítima de MagicDNS (opción de descartar).
	magicDismissed bool

	freeGB    float64
	totalGB   float64
	diskOK    bool
	portsOK   bool
	portsNote string
	latest    string
	hasUpdate bool
	checkedUp bool
	busy      string

	// RAM asignada a las instancias: la mide el comando de refresco, no View.
	assignedGB    float64
	assignedKnown bool

	// zone es el foco entre los dos paneles de Salud (ENTORNO · RECURSOS): Tab
	// avanza y Shift+Tab retrocede (§1.2 «Desplazarse»).
	zone zoneFocus
}

func newHealthScreen(sv *svc.Services, pal theme.Palette, ascii bool) *healthScreen {
	return &healthScreen{svc: sv, pal: pal, ascii: ascii, portsOK: true}
}

func (h *healthScreen) Init() tea.Cmd {
	return tea.Batch(h.refreshCmd())
}

func (h *healthScreen) refreshCmd() tea.Cmd {
	ref := h.svc
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			diag, _ := ref.DockerState.Status(ctx)
			return pollDockerMsg{diag: diag}
		},
		func() tea.Msg { return capacityMsg{cap: host.Detect(context.Background())} },
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			// Estado tipado con motivo: permite distinguir «no instalado», «parado»
			// y «MagicDNS desactivado» en vez de un aviso genérico (§5).
			return tailscaleMsg{rep: tailscale.Probe(ctx)}
		},
		func() tea.Msg {
			u, err := gdisk.Usage(ref.DataDir)
			if err != nil || u == nil {
				return healthDiskMsg{err: err}
			}
			return healthDiskMsg{freeGB: float64(u.Free) / (1 << 30), totalGB: float64(u.Total) / (1 << 30)}
		},
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			return healthPortsMsg{conflicts: h.scanPorts(ctx)}
		},
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			info, err := updx.Check(ctx, ref.Repo)
			if err != nil {
				return healthUpdateMsg{}
			}
			return healthUpdateMsg{latest: info.Latest, has: info.UpdateAvailable}
		},
		func() tea.Msg { return healthAssignedRAM(ref) },
	)
}

// healthAssignedRAM suma la memoria reservada por las instancias registradas.
func healthAssignedRAM(ref *svc.Services) tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := ref.Instances.Rows(ctx)
	if err != nil {
		return healthRAMMsg{err: err}
	}
	var total float64
	for _, r := range rows {
		if mem := host.ParseMemory(r.Memory); mem > 0 {
			total += float64(mem) / (1 << 30)
		}
	}
	return healthRAMMsg{assignedGB: total}
}

func (h *healthScreen) scanPorts(ctx context.Context) []string {
	rows, err := h.svc.Instances.Rows(ctx)
	if err != nil {
		return nil
	}
	var out []string
	for _, r := range rows {
		res, cerr := h.svc.Ports.Check(ctx, r.Port)
		if cerr != nil {
			continue
		}
		for _, cf := range res.Conflicts {
			out = append(out, "puerto "+itoa(r.Port)+": "+cf.Owner.Detail)
		}
	}
	return out
}

func (h *healthScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case capacityMsg:
		h.capacity = msg.cap
		h.loaded = true
	case tailscaleMsg:
		h.ts = msg.rep
	case healthDiskMsg:
		h.freeGB, h.totalGB, h.diskOK = msg.freeGB, msg.totalGB, msg.err == nil
	case healthPortsMsg:
		h.portsOK = len(msg.conflicts) == 0
		h.portsNote = strings.Join(msg.conflicts, "; ")
	case healthUpdateMsg:
		h.checkedUp = true
		h.latest, h.hasUpdate = msg.latest, msg.has
	case healthRAMMsg:
		h.assignedGB, h.assignedKnown = msg.assignedGB, msg.err == nil
	case healthReexpMsg:
		h.busy = ""
		return h, h.refreshCmd()
	case infraReadyMsg:
		// Terminó una reparación lanzada desde Salud (mecanismo común).
		h.busy = ""
		return h, h.refreshCmd()
	case pollDockerMsg:
		h.docker = msg.diag
	case tea.KeyMsg:
		if cmd := h.handleKey(msg); cmd != nil {
			return h, cmd
		}
	}
	return h, nil
}

// handleKey atiende los atajos propios de la pantalla de Salud (no globales).
func (h *healthScreen) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		// Salud se apila sobre Inicio: Esc vuelve. Su salida ya no depende de la
		// fila de pestañas superior, que se retiró (SPEC 2 §15).
		return func() tea.Msg { return popScreenMsg{} }
	case "tab":
		// «Desplazarse» entre los paneles de Salud (§1.2).
		h.zone.move(1)
		return nil
	case "shift+tab":
		h.zone.move(-1)
		return nil
	case "R":
		return h.Invoke("health.retry")
	case "D":
		return h.Invoke("health.repair")
	case "T":
		return h.Invoke("health.tailscale")
	case "M":
		return h.Invoke("health.dismissmagic")
	}
	return nil
}

// magicWarning describe la advertencia legítima de MagicDNS y si procede mostrarla.
func (h *healthScreen) magicWarning() (string, bool) {
	if h.magicDismissed {
		return "", false
	}
	return magicStateFrom(h.ts).warning()
}

// tsHint redacta la causa tipada de Tailscale cuando no hay IPv4 del tailnet.
func tsHint(rep tailscale.Report) string {
	switch rep.State {
	case tailscale.StateNotInstalled:
		return "Tailscale no está instalado; pulsa [T] o instálalo desde tailscale.com"
	case tailscale.StateNeedsLogin, tailscale.StateNeedsMachineAuth:
		return rep.Message + " — pulsa [T]"
	case tailscale.StateNotRunning, tailscale.StateStopped:
		return "Tailscale está apagado; pulsa [T] para levantarlo"
	case tailscale.StateStarting:
		return "Tailscale está arrancando; espera unos segundos"
	}
	return "pulsa [T] para recuperar la red"
}

// View compone la pantalla en dos paneles con borde (§5.13): ENTORNO (Docker,
// Compose, Tailscale, puertos) y RECURSOS (disco, RAM, actualización, plan). En
// terminal compacto se apilan en una sola columna. Solo representa el estado ya
// medido: aquí no se vuelve a sondear nada.
func (h *healthScreen) View(w, hgt int) string {
	if w < 1 {
		w = 1
	}
	env := h.envItems()
	res := h.resItems()

	var head strings.Builder
	head.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(h.pal.Accent)).Render("Salud del sistema"))
	if h.busy != "" {
		head.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(h.pal.Muted)).Render("⟳ "+h.busy))
	}

	// Alto de los paneles: lo que queda tras el título y la pista de atajos.
	panelH := hgt - 2
	if panelH < 3 {
		panelH = 3
	}

	// Zonas/paneles para Tab/Shift+Tab (§1.2 «Desplazarse»): ENTORNO y RECURSOS,
	// en columna o apilados.
	h.zone.set(2)
	var panels string
	if layout.ClassifyWidth(w) == layout.Compact {
		panels = h.stackedPanels(env, res, w, panelH)
	} else {
		leftW := (w - 1) / 2
		rightW := w - 1 - leftW
		panels = lipgloss.JoinHorizontal(lipgloss.Top,
			h.envPanel(env, leftW, panelH),
			" ",
			h.resPanel(res, rightW, panelH))
	}

	hintText := "[D] Reparar Docker · [T] Reparar Tailscale · [R] Comprobar de nuevo"
	if _, ok := h.magicWarning(); ok {
		hintText += " · [M] Descartar MagicDNS"
	}
	hint := ansi.Truncate(hintText, w, "…")

	out := ansi.Truncate(head.String(), w, "…") + "\n" + panels + "\n" +
		lipgloss.NewStyle().Foreground(lipgloss.Color(h.pal.Muted)).Render(hint)
	return clampHeight(out, hgt)
}

// stackedPanels apila ENTORNO y RECURSOS en una sola columna (terminal compacto).
// Reparte el alto según las filas de cada panel; si no cabe ni lo mínimo, deja
// solo ENTORNO para que el estado crítico (Docker) nunca desaparezca.
func (h *healthScreen) stackedPanels(env, res []healthItem, w, panelH int) string {
	if panelH < 9 {
		return h.envPanel(env, w, panelH)
	}
	topH := len(env) + 2
	botH := len(res) + 2
	if topH+botH+1 > panelH {
		body := panelH - 5 // dos bordes por panel y un hueco entre ellos
		if body < 2 {
			body = 2
		}
		topH = 2 + body*len(env)/(len(env)+len(res))
		botH = panelH - 1 - topH
		if topH < 3 {
			topH = 3
		}
		if botH < 3 {
			botH = 3
		}
	}
	return h.envPanel(env, w, topH) + "\n" + h.resPanel(res, w, botH)
}

// envPanel dibuja el panel ENTORNO marcando el foco de zona (§1.2).
func (h *healthScreen) envPanel(env []healthItem, w, height int) string {
	active := h.zone.active(0)
	return components.Panel(components.ZoneTitle("ENTORNO", active, h.ascii), h.tag(env),
		h.rows(env), w, height, active, h.pal, h.ascii)
}

// resPanel dibuja el panel RECURSOS marcando el foco de zona (§1.2).
func (h *healthScreen) resPanel(res []healthItem, w, height int) string {
	active := h.zone.active(1)
	return components.Panel(components.ZoneTitle("RECURSOS", active, h.ascii), h.tag(res),
		h.rows(res), w, height, active, h.pal, h.ascii)
}

// envItems compone las filas del panel ENTORNO.
func (h *healthScreen) envItems() []healthItem {
	sep := " · "
	if h.ascii {
		sep = " | "
	}
	items := make([]healthItem, 0, 4)

	dockerDetail := h.docker.Message
	if h.docker.Detail != "" {
		dockerDetail += " — " + h.docker.Detail
	}
	switch {
	case h.docker.State == "":
		items = append(items, healthItem{name: "Docker", status: "sin comprobar", detail: "pulsa [R] para comprobar", level: healthUnknown})
	case h.docker.State.Ready():
		items = append(items, healthItem{name: "Docker", status: "OK", detail: dockerDetail, level: healthOK})
	default:
		items = append(items, healthItem{name: "Docker", status: string(h.docker.State), detail: dockerDetail, level: healthBad})
	}

	switch {
	case h.docker.State == "":
		items = append(items, healthItem{name: "Docker Compose", status: "sin comprobar", level: healthUnknown})
	case h.docker.ComposeVersion == "":
		items = append(items, healthItem{name: "Docker Compose", status: "no disponible", detail: "instala el plugin compose", level: healthBad})
	default:
		items = append(items, healthItem{name: "Docker Compose", status: "ok", detail: h.docker.ComposeVersion, level: healthOK})
	}

	if txt, ok := h.magicWarning(); ok {
		// La advertencia legítima de MagicDNS vive AQUÍ (lugar fijo, con opción de
		// descartar), nunca como notificación flotante permanente (SPEC §5/§12).
		items = append(items, healthItem{name: "MagicDNS", status: "aviso", detail: txt, level: healthWarn})
	}
	tsDetail := h.ts.IP
	if h.ts.MagicDNS != "" {
		tsDetail += sep + h.ts.MagicDNS
	}
	if h.ts.IP == "" {
		items = append(items, healthItem{name: "Tailscale", status: "sin IPv4 del tailnet", detail: tsHint(h.ts), level: healthWarn})
	} else {
		items = append(items, healthItem{name: "Tailscale", status: "IP/MagicDNS", detail: tsDetail, level: healthOK})
	}

	if h.portsOK {
		items = append(items, healthItem{name: "Puertos", status: "rango libre", detail: "sin conflictos", level: healthOK})
	} else {
		items = append(items, healthItem{name: "Puertos", status: "conflictos", detail: h.portsNote, level: healthWarn})
	}
	return items
}

// resItems compone las filas del panel RECURSOS.
func (h *healthScreen) resItems() []healthItem {
	sep := " · "
	if h.ascii {
		sep = " | "
	}
	items := make([]healthItem, 0, 4)

	disk := h.diskDetail()
	switch {
	case !h.diskOK:
		items = append(items, healthItem{name: "Disco", status: "sin medir", level: healthUnknown})
	case h.freeGB < 2:
		items = append(items, healthItem{name: "Disco", status: "crítico (<2 GB)", detail: disk, level: healthBad})
	case h.freeGB < 10:
		items = append(items, healthItem{name: "Disco", status: "aviso (<10 GB)", detail: disk, level: healthWarn})
	default:
		items = append(items, healthItem{name: "Disco", status: "ok", detail: disk, level: healthOK})
	}

	if h.capacity.NCPU <= 0 {
		items = append(items, healthItem{name: "RAM", status: "sin comprobar", level: healthUnknown})
	} else {
		detail := itoa(h.capacity.NCPU) + " CPU" + sep + trimFloat(h.capacity.MemTotalGiB()) + " GiB RAM" + sep + h.assignedText() + " asignados"
		items = append(items, healthItem{name: "RAM", status: h.capacity.Source, detail: detail, level: healthOK})
	}

	upDetail := "repositorio " + h.svc.Repo
	switch {
	case !h.checkedUp:
		items = append(items, healthItem{name: "Actualización", status: "no comprobado", detail: upDetail, level: healthUnknown})
	case h.hasUpdate:
		items = append(items, healthItem{name: "Actualización", status: "disponible " + h.latest, detail: upDetail, level: healthWarn})
	default:
		items = append(items, healthItem{name: "Actualización", status: "al día", detail: upDetail, level: healthOK})
	}

	items = append(items, healthItem{
		name:   "Plan",
		status: "límites",
		detail: itoa(h.svc.MaxParallel) + " en paralelo" + sep + itoa(h.svc.MaxInstances) + " registradas",
		level:  healthOK,
	})
	return items
}

// rows pinta las filas de un panel, una por línea.
func (h *healthScreen) rows(items []healthItem) string {
	var b strings.Builder
	for i, it := range items {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(h.itemLine(it))
	}
	return b.String()
}

// itemLine dibuja una fila: icono del nivel + nombre + detalle + estado.
func (h *healthScreen) itemLine(it healthItem) string {
	icon, color := h.levelIcon(it.level)
	line := " " + lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(icon) + " " +
		lipgloss.NewStyle().Bold(true).Render(it.name)
	if it.detail != "" {
		line += "  " + it.detail
	}
	if it.status != "" {
		line += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(h.pal.Muted)).Render(it.status)
	}
	return line
}

// levelIcon devuelve el icono y el color del nivel: desconocido (·), correcto
// (✓), aviso (⚠) y crítico (✖). En ASCII todos tienen su equivalente plano.
func (h *healthScreen) levelIcon(level healthLevel) (string, string) {
	switch level {
	case healthOK:
		return components.Check(h.ascii), h.pal.Good
	case healthWarn:
		return components.Warn(h.ascii), h.pal.Warn
	case healthBad:
		return components.Cross(h.ascii), h.pal.Bad
	}
	icon := "·"
	if h.ascii {
		icon = "."
	}
	return icon, h.pal.Muted
}

// tag resume el estado de un panel: «ok», «N aviso(s)», «crítico», «sin
// comprobar» o «reparando…» mientras hay una operación en curso.
func (h *healthScreen) tag(items []healthItem) string {
	if h.busy != "" {
		return "reparando…"
	}
	bad, warn, unknown := 0, 0, 0
	for _, it := range items {
		switch it.level {
		case healthBad:
			bad++
		case healthWarn:
			warn++
		case healthUnknown:
			unknown++
		}
	}
	switch {
	case bad > 0:
		return "crítico"
	case warn == 1:
		return "1 aviso"
	case warn > 1:
		return itoa(warn) + " avisos"
	case unknown > 0:
		return "sin comprobar"
	}
	return "ok"
}

func (h *healthScreen) diskDetail() string {
	if h.totalGB <= 0 {
		return trimFloat(h.freeGB) + " GB libres"
	}
	return trimFloat(h.freeGB) + " GB libres de " + trimFloat(h.totalGB) + " GB"
}

func (h *healthScreen) assignedText() string {
	if !h.assignedKnown {
		return "—"
	}
	return trimFloat(h.assignedGB) + " GiB"
}

// clampHeight recorta la vista al alto disponible sin dejar líneas de más.
func clampHeight(s string, hgt int) string {
	if hgt <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) > hgt {
		lines = lines[:hgt]
	}
	return strings.Join(lines, "\n")
}

func (h *healthScreen) Actions() []components.Action {
	acts := []components.Action{
		{ID: "health.retry", Verb: "Comprobar", Object: "de nuevo", Key: "R", Hint: "Repite el diagnóstico de Docker, Tailscale, disco y puertos.", Kind: components.Secondary},
		{ID: "health.repair", Verb: "Reparar", Object: "Docker", Key: "D", Hint: "Intenta arrancar Docker y espera a que responda.", Kind: components.Primary, Disabled: h.docker.State.Ready(), DisabledReason: "Docker ya está activo"},
		{ID: "health.tailscale", Verb: "Reparar", Object: "Tailscale", Key: "T", Hint: "Levanta Tailscale con el mecanismo común (explica el motivo si pide permisos).", Kind: components.Secondary},
	}
	if _, ok := h.magicWarning(); ok {
		acts = append(acts, components.Action{ID: "health.dismissmagic", Verb: "Descartar", Object: "aviso de MagicDNS", Key: "M", Hint: "Oculta el aviso de MagicDNS de Salud.", Kind: components.Secondary})
	}
	return acts
}

func (h *healthScreen) Invoke(id string) tea.Cmd {
	switch id {
	case "health.retry":
		h.busy = "comprobando…"
		return func() tea.Msg { return healthReexpMsg{} }
	case "health.repair":
		// Mismo mecanismo común que el resto de la Fase 8: detectar → levantar →
		// elevar explicando el motivo → informar.
		h.busy = "reparando…"
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			return infraReadyMsg{what: "docker", rep: h.svc.Infra.Docker(ctx, false, nil)}
		}
	case "health.tailscale":
		h.busy = "reparando…"
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			return infraReadyMsg{what: "tailscale", rep: h.svc.Infra.Tailscale(ctx, false, nil)}
		}
	case "health.dismissmagic":
		h.magicDismissed = true
		return nil
	}
	return nil
}

func (h *healthScreen) Title() string { return "Salud del sistema" }
func (h *healthScreen) ID() string    { return "health" }

// Status aporta el mensaje de operaciones en curso (p. ej. reparar Docker).
func (h *healthScreen) Status() string { return h.busy }

// trimFloat redacta un float con un decimal sin ceros de más.
func trimFloat(f float64) string {
	whole := int64(f)
	frac := int64((f - float64(whole)) * 10)
	if frac == 0 {
		return itoa(int(whole))
	}
	return itoa(int(whole)) + "." + itoa(int(frac))
}
