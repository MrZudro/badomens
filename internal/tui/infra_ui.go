package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/infra"
	"github.com/MrZudro/badomens/internal/tui/components"
)

// infraReadyMsg trae el resultado del mecanismo común de infraestructura (Fase 8).
type infraReadyMsg struct {
	what string // "daemon" | "daemon-install" | "docker" | "tailscale"
	rep  infra.Report
}

// ensureDaemonCmd comprueba y fuerza el arranque del daemon propio al iniciar la
// TUI (§8.2). No bloquea el bucle: se ejecuta dentro de un tea.Cmd.
func (a *App) ensureDaemonCmd() tea.Cmd {
	ref := a.svc
	if ref.Infra == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		return infraReadyMsg{what: "daemon", rep: ref.Infra.Daemon(ctx, false, nil)}
	}
}

// ensureDockerCmd detecta y levanta Docker con el MISMO mecanismo común (§8.3).
func (a *App) ensureDockerCmd() tea.Cmd {
	ref := a.svc
	if ref.Infra == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
		defer cancel()
		return infraReadyMsg{what: "docker", rep: ref.Infra.Docker(ctx, false, nil)}
	}
}

// installDaemonCmd instala el daemon como servicio persistente. El usuario ya lo
// confirmó, así que se autoriza la elevación; si se rechaza, se degrada a daemon
// de sesión con un aviso claro.
func (a *App) installDaemonCmd() tea.Cmd {
	ref := a.svc
	if ref.Infra == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return infraReadyMsg{what: "daemon-install", rep: ref.Infra.InstallDaemonService(ctx, true, nil)}
	}
}

// firstManual devuelve el primer paso manual por SO (o el hint) de un reporte.
func firstManual(rep infra.Report) string {
	if len(rep.Manual) > 0 {
		return rep.Manual[0]
	}
	return rep.Hint
}

// offerDaemonService ofrece (con confirmación la PRIMERA vez) instalar el daemon
// como servicio persistente. Nunca se instala en silencio.
func (a *App) offerDaemonService() {
	a.confirm = &components.Modal{
		Title: "Instalar el daemon",
		Body: "El daemon de badomens vigila el auto-apagado por inactividad. " +
			"¿Quieres instalarlo como servicio persistente para que arranque solo? " +
			"Se explicará el motivo antes de pedir permisos; si los rechazas, seguirá como daemon de sesión.",
		Actions: []components.Action{
			{ID: "daemon.install", Verb: "Instalar", Object: "el servicio del daemon", Key: "y", Hint: "Crea y habilita la unidad del sistema (systemd/launchd/tarea).", Kind: components.Primary},
			{ID: "cancel", Verb: "No,", Object: "usar daemon de sesión", Key: "n", Hint: "Se degrada a daemon de sesión sin servicio persistente.", Kind: components.Secondary},
		},
		Focused: 0,
	}
}

// noteInfra convierte el resultado del mecanismo común en avisos accionables y,
// en el primer arranque sin servicio, ofrece instalar el daemon como persistente.
func (a *App) noteInfra(what string, rep infra.Report) tea.Cmd {
	switch what {
	case "daemon":
		if rep.Ready {
			// El chequeo se RESUELVE con un mensaje final en vez de dejar el
			// «Comprobando…» sin desenlace (P-05/UX-11).
			return a.pushToast("✓ " + rep.Message)
		}
		if rep.NeedsElevation {
			return a.pushToast("⚠ El daemon necesita permisos de administrador: "+firstManual(rep), components.Destructive)
		}
		if !a.daemonPrompted && !a.svc.Infra.DaemonInstalled() {
			a.daemonPrompted = true
			a.offerDaemonService()
			return a.pushToast("⚠ El daemon no está en marcha: se usará un daemon de sesión mientras tanto")
		}
		return a.pushToast("⚠ " + rep.Message)
	case "daemon-install":
		if rep.Ready {
			return a.pushToast("✓ " + rep.Message)
		}
		return a.pushToast("⚠ no se instaló el servicio; el daemon queda como proceso de sesión · "+firstManual(rep), components.Destructive)
	}
	if rep.Ready {
		return nil
	}
	msg := "⚠ " + rep.Message
	if m := firstManual(rep); m != "" {
		msg += " · " + m
	}
	return a.pushToast(msg, components.Destructive)
}
