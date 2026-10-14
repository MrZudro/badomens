package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/host"
	"github.com/MrZudro/badomens/internal/tailscale"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

type capacityMsg struct{ cap host.Capacity }
type tailscaleMsg struct {
	ip    string
	magic string
	err   error
}

// healthScreen es "Salud del sistema" (H): Docker, Tailscale, capacidad y RAM.
type healthScreen struct {
	svc      *svc.Services
	pal      theme.Palette
	ascii    bool
	docker   dockerx.Diagnosis
	capacity host.Capacity
	ip       string
	magic    string
	loaded   bool
}

func newHealthScreen(sv *svc.Services, pal theme.Palette, ascii bool) *healthScreen {
	return &healthScreen{svc: sv, pal: pal, ascii: ascii}
}

func (h *healthScreen) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return capacityMsg{cap: host.Detect(context.Background())} },
		func() tea.Msg {
			ip, err := tailscale.ResolveIPv4(context.Background())
			magic, _ := tailscale.MagicDNS(context.Background())
			return tailscaleMsg{ip: ip, magic: magic, err: err}
		},
	)
}

func (h *healthScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case capacityMsg:
		h.capacity = msg.cap
		h.loaded = true
	case tailscaleMsg:
		h.ip, h.magic = msg.ip, msg.magic
	case pollDockerMsg:
		h.docker = msg.diag
	}
	return h, nil
}

func (h *healthScreen) View(w, hgt int) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(h.pal.Accent)).Render("Salud del sistema"))
	b.WriteString("\n\n")
	row := func(name, status, detail string, ok bool) {
		color := h.pal.Good
		icon := "✓"
		if !ok {
			color = h.pal.Bad
			icon = "✖"
		}
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(icon) + " " +
			lipgloss.NewStyle().Bold(true).Render(name) + "  " + detail + "  " +
			lipgloss.NewStyle().Foreground(lipgloss.Color(h.pal.Muted)).Render(status) + "\n")
	}
	dockerOK := h.docker.State.Ready()
	dockerDetail := h.docker.Message
	if h.docker.Detail != "" {
		dockerDetail += " — " + h.docker.Detail
	}
	row("Docker", string(h.docker.State), dockerDetail, dockerOK)
	tsOK := h.ip != ""
	tsDetail := h.ip
	if h.magic != "" {
		tsDetail += " · " + h.magic
	}
	if !tsOK {
		tsDetail = "sin IPv4 del tailnet (ejecuta 'tailscale up')"
	}
	row("Tailscale", "IP/MagicDNS", tsDetail, tsOK)
	capDetail := itoa(h.capacity.NCPU) + " CPU · " + trimFloat(h.capacity.MemTotalGiB()) + " GiB RAM (" + h.capacity.Source + ")"
	row("Capacidad", "host", capDetail, h.capacity.NCPU > 0)
	row("Plan", "límites", itoa(h.svc.MaxParallel)+" en paralelo · "+itoa(h.svc.MaxInstances)+" registradas", true)
	b.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(h.pal.Muted)).Render("Reparar Docker: 'badomens docker ensure'; comprobar de nuevo: R"))
	return b.String()
}

func (h *healthScreen) Actions() []components.Action {
	return []components.Action{
		{ID: "health.retry", Verb: "Comprobar", Object: "de nuevo", Key: "R", Hint: "Repite el diagnóstico de Docker y Tailscale.", Kind: components.Secondary},
		{ID: "health.repair", Verb: "Reparar", Object: "Docker", Key: "d", Hint: "Intenta arrancar Docker y espera a que responda.", Kind: components.Primary, Disabled: h.docker.State.Ready(), DisabledReason: "Docker ya está activo"},
	}
}

func (h *healthScreen) Title() string { return "Salud del sistema" }
func (h *healthScreen) ID() string    { return "health" }

// trimFloat redacta un float con un decimal sin ceros de más.
func trimFloat(f float64) string {
	whole := int64(f)
	frac := int64((f - float64(whole)) * 10)
	if frac == 0 {
		return itoa(int(whole))
	}
	return itoa(int(whole)) + "." + itoa(int(frac))
}
