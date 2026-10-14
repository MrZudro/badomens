package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	gdisk "github.com/shirou/gopsutil/v4/disk"

	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/execx"
	"github.com/MrZudro/badomens/internal/host"
	"github.com/MrZudro/badomens/internal/ports"
	"github.com/MrZudro/badomens/internal/tailscale"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
	updx "github.com/MrZudro/badomens/internal/update"
)

type capacityMsg struct{ cap host.Capacity }
type tailscaleMsg struct {
	ip    string
	magic string
	err   error
}
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
	err    error
}
type healthReexpMsg struct{ err error }

// healthScreen es "Salud del sistema" (H): Docker, Compose, Tailscale, puertos,
// disco, RAM y actualización (P-01/RF-6).
type healthScreen struct {
	svc      *svc.Services
	pal      theme.Palette
	ascii    bool
	docker   dockerx.Diagnosis
	capacity host.Capacity
	ip       string
	magic    string
	loaded   bool

	freeGB    float64
	totalGB   float64
	diskOK    bool
	portsOK   bool
	portsNote string
	latest    string
	hasUpdate bool
	checkedUp bool
	busy      string
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
			ip, err := tailscale.ResolveIPv4(ctx)
			magic, _ := tailscale.MagicDNS(ctx)
			return tailscaleMsg{ip: ip, magic: magic, err: err}
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
				return healthUpdateMsg{err: err}
			}
			return healthUpdateMsg{latest: info.Latest, has: info.UpdateAvailable}
		},
	)
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
		h.ip, h.magic = msg.ip, msg.magic
	case healthDiskMsg:
		h.freeGB, h.totalGB, h.diskOK = msg.freeGB, msg.totalGB, msg.err == nil
	case healthPortsMsg:
		h.portsOK = len(msg.conflicts) == 0
		h.portsNote = strings.Join(msg.conflicts, "; ")
	case healthUpdateMsg:
		h.checkedUp = true
		h.latest, h.hasUpdate = msg.latest, msg.has
	case healthReexpMsg:
		h.busy = ""
		return h, h.refreshCmd()
	case pollDockerMsg:
		h.docker = msg.diag
	}
	return h, nil
}

func (h *healthScreen) View(w, hgt int) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(h.pal.Accent)).Render("Salud del sistema"))
	if h.busy != "" {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(h.pal.Muted)).Render("⟳ "+h.busy))
	}
	b.WriteString("\n\n")
	row := func(name, status, detail string, ok, known bool) {
		color := h.pal.Good
		icon := components.Check(h.ascii)
		switch {
		case !known:
			color = h.pal.Muted
			icon = "·"
			if h.ascii {
				icon = "."
			}
		case !ok:
			color = h.pal.Bad
			icon = components.Cross(h.ascii)
		}
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(icon) + " " +
			lipgloss.NewStyle().Bold(true).Render(name) + "  " + detail + "  " +
			lipgloss.NewStyle().Foreground(lipgloss.Color(h.pal.Muted)).Render(status) + "\n")
	}

	dockerDetail := h.docker.Message
	if h.docker.Detail != "" {
		dockerDetail += " — " + h.docker.Detail
	}
	dockerKnown := h.docker.State != ""
	dockerState := string(h.docker.State)
	if !dockerKnown {
		dockerState = "sin comprobar"
		dockerDetail = "pulsa [R] para comprobar"
	}
	row("Docker", dockerState, dockerDetail, h.docker.State.Ready(), dockerKnown)

	compose := "no comprobado"
	if h.docker.ComposeVersion != "" {
		compose = h.docker.ComposeVersion
	}
	row("Docker Compose", compose, "", h.docker.ComposeVersion != "", dockerKnown)

	tsOK := h.ip != ""
	tsDetail := h.ip
	if h.magic != "" {
		tsDetail += " · " + h.magic
	}
	if !tsOK {
		tsDetail = "sin IPv4 del tailnet"
	}
	row("Tailscale", "IP/MagicDNS", tsDetail, tsOK, true)

	portsDetail := "sin conflictos"
	if !h.portsOK {
		portsDetail = h.portsNote
	}
	row("Puertos", "rango libre", portsDetail, h.portsOK, true)

	if h.diskOK {
		diskStatus := "ok"
		diskHealthy := true
		if h.freeGB < 2 {
			diskStatus, diskHealthy = "crítico (<2 GB)", false
		} else if h.freeGB < 10 {
			diskStatus = "aviso (<10 GB)"
		}
		row("Disco", diskStatus, trimFloat(h.freeGB)+" GB libres de "+trimFloat(h.totalGB)+" GB", diskHealthy, true)
	} else {
		row("Disco", "no medido", "", true, false)
	}

	assigned := h.assignedRAM()
	capDetail := itoa(h.capacity.NCPU) + " CPU · " + trimFloat(h.capacity.MemTotalGiB()) + " GiB RAM · " + trimFloat(assigned) + " GiB asignados"
	row("RAM", h.capacity.Source, capDetail, true, h.capacity.NCPU > 0)

	upStatus, upOK := "no comprobado", true
	if h.checkedUp {
		upStatus = "al día"
		if h.hasUpdate {
			upStatus, upOK = "disponible "+h.latest, false
		}
	}
	row("Actualización", upStatus, "repositorio "+h.svc.Repo, upOK, h.checkedUp)

	row("Plan", "límites", itoa(h.svc.MaxParallel)+" en paralelo · "+itoa(h.svc.MaxInstances)+" registradas", true, true)

	b.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(h.pal.Muted)).Render("[D] Reparar Docker · [T] Reparar Tailscale · [R] Comprobar de nuevo"))
	return b.String()
}

func (h *healthScreen) assignedRAM() float64 {
	rows, err := h.svc.Instances.Rows(context.Background())
	if err != nil {
		return 0
	}
	var total float64
	for _, r := range rows {
		if mem := host.ParseMemory(r.Memory); mem > 0 {
			total += float64(mem) / (1 << 30)
		}
	}
	return total
}

func (h *healthScreen) Actions() []components.Action {
	return []components.Action{
		{ID: "health.retry", Verb: "Comprobar", Object: "de nuevo", Key: "R", Hint: "Repite el diagnóstico de Docker, Tailscale, disco y puertos.", Kind: components.Secondary},
		{ID: "health.repair", Verb: "Reparar", Object: "Docker", Key: "D", Hint: "Intenta arrancar Docker y espera a que responda.", Kind: components.Primary, Disabled: h.docker.State.Ready(), DisabledReason: "Docker ya está activo"},
		{ID: "health.tailscale", Verb: "Reparar", Object: "Tailscale", Key: "T", Hint: "Ejecuta 'tailscale up' para recuperar la red privada.", Kind: components.Secondary},
	}
}

func (h *healthScreen) Invoke(id string) tea.Cmd {
	switch id {
	case "health.retry":
		h.busy = "comprobando…"
		return func() tea.Msg { return healthReexpMsg{} }
	case "health.repair":
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			diag, err := h.svc.DockerState.Ensure(ctx)
			return dockerRepairMsg{diag: diag, err: err}
		}
	case "health.tailscale":
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			if _, err := execx.Run(ctx, "tailscale", "up"); err != nil {
				return toastMsg{Text: "✖ Tailscale: " + err.Error(), Kind: components.Destructive}
			}
			return toastMsg{Text: "✓ Tailscale activado"}
		}
	}
	return nil
}

func (h *healthScreen) Title() string { return "Salud del sistema" }
func (h *healthScreen) ID() string    { return "health" }

// healthPortsRow compone el detalle de un resultado de puerto (para tests).
func healthPortsRow(res ports.Result) string {
	if res.Free {
		return "libre"
	}
	return "ocupado"
}

// trimFloat redacta un float con un decimal sin ceros de más.
func trimFloat(f float64) string {
	whole := int64(f)
	frac := int64((f - float64(whole)) * 10)
	if frac == 0 {
		return itoa(int(whole))
	}
	return itoa(int(whole)) + "." + itoa(int(frac))
}
