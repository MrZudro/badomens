package infra

import (
	"context"

	"github.com/MrZudro/badomens/internal/tailscale"
)

// Tailscale adapta la consulta de estado de tailscale al mecanismo común. La
// detección usa `tailscale.Probe`, que devuelve el motivo tipado (incluido el de
// MagicDNS) para que la UI pueda reevaluar y explicar la causa.
type Tailscale struct {
	// Env es el entorno inyectable de tailscale (vacío = real).
	Env tailscale.Env
}

// NewTailscale construye el componente Tailscale con el entorno dado.
func NewTailscale(env tailscale.Env) Tailscale { return Tailscale{Env: env} }

// Domain identifica el componente.
func (Tailscale) Domain() Domain { return DomainTailscale }

// Detect traduce el estado de tailscale a un estado tipado.
func (t Tailscale) Detect(ctx context.Context) State {
	return tailscaleState(tailscale.ProbeWith(ctx, t.Env))
}

// StartPlan devuelve los pasos de arranque por SO. Requiere autenticación aparte
// cuando el backend pide login.
func (Tailscale) StartPlan(st State, platform string) []Step {
	// Sin sesión iniciada, arrancar el servicio no basta: se pide autenticar.
	if st.Status == StatusNeedsLogin {
		return []Step{{Description: "Iniciar sesión en Tailscale", Instruction: "Ejecuta 'tailscale up' y autentícate en el navegador que se abra."}}
	}
	switch platform {
	case "windows":
		return []Step{
			{Description: "Iniciando el servicio Tailscale…", Command: []string{"sc", "start", "Tailscale"}, RequiresElevation: true},
			{Description: "Iniciando el servicio TailscaleIPN…", Command: []string{"sc", "start", "TailscaleIPN"}, RequiresElevation: true},
			{Description: "Abrir Tailscale manualmente", Instruction: "Abre la app Tailscale desde el menú Inicio e inicia sesión."},
		}
	case "darwin":
		return []Step{
			{Description: "Abriendo la app Tailscale…", Command: []string{"open", "-a", "Tailscale"}},
			{Description: "Iniciando tailscaled…", Command: []string{"sudo", "-n", "launchctl", "load", "-w", "/Library/LaunchDaemons/com.tailscale.tailscaled.plist"}, RequiresElevation: true},
			{Description: "Iniciar sesión si hace falta", Instruction: "Si Tailscale pide sesión, ejecuta 'tailscale up'."},
		}
	default:
		return []Step{
			{Description: "Iniciando tailscaled…", Command: []string{"systemctl", "start", "tailscaled"}},
			{Description: "Iniciando tailscaled (usuario)…", Command: []string{"systemctl", "--user", "start", "tailscaled"}},
			{Description: "Iniciando tailscaled con permisos…", Command: []string{"sudo", "-n", "systemctl", "start", "tailscaled"}, RequiresElevation: true},
			{Description: "Iniciar sesión si hace falta", Instruction: "Si Tailscale pide sesión, ejecuta 'tailscale up'."},
		}
	}
}

// InstallHint devuelve la instrucción de instalación por SO (nunca se instala solo).
func (Tailscale) InstallHint(platform string) string {
	switch platform {
	case "windows":
		return "Descarga el instalador para Windows desde https://tailscale.com/download/windows y ejecútalo."
	case "darwin":
		return "Instala Tailscale con 'brew install --cask tailscale' o descárgalo de https://tailscale.com/download/mac."
	}
	return "Instala Tailscale (revisa el script antes de ejecutarlo): https://tailscale.com/download/linux"
}

// ManualGuide devuelve los pasos manuales por SO según la causa.
func (t Tailscale) ManualGuide(st State, platform string) []string {
	switch st.Status {
	case StatusNotInstalled:
		return []string{t.InstallHint(platform)}
	case StatusNeedsLogin:
		return []string{"Ejecuta 'tailscale up' y autentícate en el navegador.", "Comprueba el estado con 'tailscale status'."}
	}
	switch platform {
	case "windows":
		return []string{"Abre Tailscale desde el menú Inicio y asegúrate de que está conectado.", "Si el servicio no arranca, ejecútalo como administrador."}
	case "darwin":
		return []string{"Abre la app Tailscale desde Aplicaciones y comprueba que está conectada.", "Alternativa por terminal: 'sudo launchctl start com.tailscale.tailscaled'."}
	}
	return []string{"Ejecuta 'sudo systemctl start tailscaled' y comprueba con 'tailscale status'.", "Si sigue apagado: 'sudo tailscale up' para autenticarte."}
}

// tailscaleState traduce un tailscale.Report a un infra.State.
func tailscaleState(rep tailscale.Report) State {
	st := State{
		Domain:  DomainTailscale,
		Reason:  rep.Reason,
		Message: rep.Message,
		Hint:    rep.Hint,
		Extra: map[string]any{
			"tailscale_state":   string(rep.State),
			"backend_state":     rep.BackendState,
			"ip":                rep.IP,
			"hostname":          rep.Hostname,
			"magic_dns":         rep.MagicDNS,
			"magic_dns_enabled": rep.MagicDNSEnabled,
			"magic_dns_reason":  rep.MagicDNSReason,
			"peers_online":      rep.PeersOnline,
		},
		raw: rep,
	}
	switch rep.State {
	case tailscale.StateReady:
		st.Status = StatusReady
	case tailscale.StateNotInstalled:
		st.Status = StatusNotInstalled
	case tailscale.StateNotRunning, tailscale.StateStopped:
		st.Status = StatusDown
	case tailscale.StateNeedsLogin, tailscale.StateNeedsMachineAuth:
		st.Status = StatusNeedsLogin
	case tailscale.StateStarting:
		st.Status = StatusStarting
	case tailscale.StateNoIP:
		st.Status = StatusDegraded
	default:
		st.Status = StatusUnavailable
	}
	return st
}
