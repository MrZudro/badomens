package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/connection"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tailscale"
)

// magicState es el estado EN VIVO de MagicDNS (causa raíz de la Fase 8 §5): la
// TUI deja de fiarse del valor persistido en spec.Connection.MagicDNS, que nadie
// reevalúa y por eso dejaba un aviso permanente aunque MagicDNS ya estuviera activo.
type magicState struct {
	Known   bool   // ya se consultó el estado en vivo
	Enabled bool   // MagicDNS activo en el tailnet
	Name    string // nombre del tailnet (maquina.tail1234.ts.net)
	Reason  string // motivo tipado (magicdns_ok, magicdns_disabled, …)
}

// magicProbeMsg trae el estado en vivo de Tailscale/MagicDNS.
type magicProbeMsg struct{ rep tailscale.Report }

// magicProbeCmd consulta el estado en vivo sin bloquear el bucle de Bubble Tea.
func magicProbeCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return magicProbeMsg{rep: tailscale.Probe(ctx)}
	}
}

// magicStateFrom traduce el Report tipado de tailscale al estado que usa la UI.
func magicStateFrom(rep tailscale.Report) magicState {
	return magicState{
		Known:   true,
		Enabled: rep.MagicDNSEnabled && rep.MagicDNS != "",
		Name:    rep.MagicDNS,
		Reason:  rep.MagicDNSReason,
	}
}

// warning devuelve el texto de la advertencia legítima de MagicDNS y si procede
// mostrarla. Vive en Salud (lugar fijo, con opción de descartar), nunca como
// notificación flotante permanente (SPEC §5/§12).
func (m magicState) warning() (string, bool) {
	if !m.Known || m.Enabled {
		return "", false
	}
	switch m.Reason {
	case tailscale.ReasonMagicDNSDisabled:
		return "MagicDNS está desactivado en el tailnet: los jugadores deben usar la dirección IP.", true
	case tailscale.ReasonMagicDNSNoName:
		return "Tailscale no tiene nombre MagicDNS para este equipo: usa la dirección IP.", true
	case tailscale.ReasonMagicDNSNoSelf:
		return "No se pudo leer el nombre de MagicDNS de este equipo: usa la dirección IP.", true
	}
	return "MagicDNS no está activo: usa la dirección IP del tailnet.", true
}

// connectionInfo deriva la conexión de una instancia con el estado EN VIVO de
// MagicDNS: si se conoce, manda sobre el valor persistido (obsoleto) y el aviso
// de MagicDNS deja de pintarse en la tarjeta de conexión.
func connectionInfo(spec *instance.Spec, host string, m magicState) connection.Info {
	magic := ""
	if spec.Connection != nil {
		magic = spec.Connection.MagicDNS
		if host == "" {
			host = spec.Connection.Host
		}
	}
	if m.Known {
		magic = m.Name
	}
	info := connection.FromData(spec.Slug, spec.Name, spec.Engine, spec.Version, spec.Port, spec.Pins, spec.OnlineMode, spec.WhitelistEnabled, host, magic, time.Now())
	if m.Known {
		info.Warnings = dropMagicWarning(info.Warnings)
	}
	return info
}

// dropMagicWarning quita el aviso de MagicDNS de la tarjeta de conexión cuando el
// estado en vivo ya lo explica.
func dropMagicWarning(warns []string) []string {
	out := make([]string, 0, len(warns))
	for _, w := range warns {
		if strings.Contains(w, "MagicDNS") {
			continue
		}
		out = append(out, w)
	}
	return out
}
