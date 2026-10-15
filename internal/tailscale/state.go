package tailscale

import (
	"context"
	"encoding/json"
	"time"

	"github.com/MrZudro/badomens/internal/execx"
)

// Env reúne las dependencias del paquete para poder inyectarlas en pruebas.
type Env struct {
	// Run ejecuta un proceso externo (sin shell). Por defecto execx.Run.
	Run func(ctx context.Context, name string, args ...string) (execx.Result, error)
	// LookPath indica si un binario está en el PATH. Por defecto execx.LookPath.
	LookPath func(name string) bool
}

// DefaultEnv devuelve el entorno real.
func DefaultEnv() Env {
	return Env{Run: execx.Run, LookPath: execx.LookPath}
}

func (e Env) run(ctx context.Context, name string, args ...string) (execx.Result, error) {
	if e.Run == nil {
		return execx.Run(ctx, name, args...)
	}
	return e.Run(ctx, name, args...)
}

func (e Env) lookPath(name string) bool {
	if e.LookPath == nil {
		return execx.LookPath(name)
	}
	return e.LookPath(name)
}

// State es el estado tipado de Tailscale. Distingue «no instalado», «apagado»,
// «sin autenticar»… para que la UI pueda reaccionar y explicar la causa.
type State string

// Estados posibles.
const (
	StateReady            State = "ready"
	StateNotInstalled     State = "not_installed"
	StateNotRunning       State = "not_running"
	StateNeedsLogin       State = "needs_login"
	StateNeedsMachineAuth State = "needs_machine_auth"
	StateStopped          State = "stopped"
	StateStarting         State = "starting"
	StateNoIP             State = "no_ip"
	StateInvalidOutput    State = "invalid_output"
	StateUnknown          State = "unknown"
)

// Ready indica que Tailscale está operativo (backend Running con IPv4).
func (s State) Ready() bool { return s == StateReady }

// Motivos estables (para error.details.reason y para la UI).
const (
	ReasonOK               = "ok"
	ReasonNotInstalled     = "tailscale_not_installed"
	ReasonNotRunning       = "tailscale_not_running"
	ReasonNeedsLogin       = "tailscale_needs_login"
	ReasonNeedsMachineAuth = "tailscale_needs_machine_auth"
	ReasonStopped          = "tailscale_stopped"
	ReasonStarting         = "tailscale_starting"
	ReasonNoIP             = "tailscale_no_ip"
	ReasonInvalidOutput    = "tailscale_invalid_output"
	ReasonUnknown          = "tailscale_unknown"
	ReasonMagicDNSOK       = "magicdns_ok"
	ReasonMagicDNSDisabled = "magicdns_disabled"
	ReasonMagicDNSNoSelf   = "magicdns_no_self"
	ReasonMagicDNSNoName   = "magicdns_no_name"
)

// Report es el estado completo de Tailscale con motivo tipado y datos útiles
// (IP, nombre MagicDNS y si MagicDNS está activo). Es la API que consumen el
// CLI y el MCP y que la TUI podrá usar para reevaluar el aviso de MagicDNS.
type Report struct {
	State           State     `json:"state"`
	Reason          string    `json:"reason"`
	Message         string    `json:"message"`
	Hint            string    `json:"hint,omitempty"`
	BackendState    string    `json:"backend_state,omitempty"`
	IP              string    `json:"ip,omitempty"`
	Hostname        string    `json:"hostname,omitempty"`
	MagicDNS        string    `json:"magic_dns,omitempty"`
	MagicDNSEnabled bool      `json:"magic_dns_enabled"`
	MagicDNSReason  string    `json:"magic_dns_reason,omitempty"`
	PeersOnline     int       `json:"peers_online"`
	CheckedAt       time.Time `json:"checked_at"`
}

// Ready indica si Tailscale está operativo.
func (r Report) Ready() bool { return r.State.Ready() }

// Probe consulta el estado de Tailscale con el entorno real.
func Probe(ctx context.Context) Report { return ProbeWith(ctx, DefaultEnv()) }

// ProbeWith es la variante inyectable de Probe.
func ProbeWith(ctx context.Context, env Env) Report {
	rep := Report{CheckedAt: time.Now().UTC()}
	if !env.lookPath("tailscale") {
		rep.State = StateNotInstalled
		rep.Reason = ReasonNotInstalled
		rep.Message = "Tailscale no está instalado."
		rep.Hint = "instálalo desde tailscale.com y ejecuta 'tailscale up'"
		return rep
	}
	res, err := env.run(ctx, "tailscale", "status", "--json")
	if err != nil {
		rep.State = StateNotRunning
		rep.Reason = ReasonNotRunning
		rep.Message = "Tailscale está instalado pero el servicio no responde."
		rep.Hint = "inicia Tailscale (o 'tailscaled') y vuelve a intentarlo"
		return rep
	}
	var st Status
	if jerr := json.Unmarshal([]byte(res.Stdout), &st); jerr != nil {
		rep.State = StateInvalidOutput
		rep.Reason = ReasonInvalidOutput
		rep.Message = "La salida de 'tailscale status' no es legible."
		rep.Hint = "comprueba tu instalación de tailscale"
		return rep
	}
	return reportFromStatus(st)
}

// reportFromStatus traduce el estado del tailnet a Report.
func reportFromStatus(st Status) Report {
	rep := Report{CheckedAt: time.Now().UTC(), BackendState: st.BackendState}
	if st.Self != nil {
		rep.Hostname = st.Self.HostName
	}
	name, enabled, magicReason := MagicDNSFromStatus(st)
	rep.MagicDNS, rep.MagicDNSEnabled, rep.MagicDNSReason = name, enabled, magicReason
	rep.IP = ipv4FromStatus(st)
	rep.PeersOnline = onlinePeers(st)

	switch st.BackendState {
	case "Running":
		if rep.IP == "" {
			rep.State = StateNoIP
			rep.Reason = ReasonNoIP
			rep.Message = "Tailscale está en marcha pero aún no tiene IPv4 del tailnet."
			rep.Hint = "ejecuta 'tailscale up' y espera unos segundos"
			return rep
		}
		rep.State = StateReady
		rep.Reason = ReasonOK
		rep.Message = "Tailscale está listo (" + rep.IP + ")."
		return rep
	case "NeedsLogin":
		rep.State = StateNeedsLogin
		rep.Reason = ReasonNeedsLogin
		rep.Message = "Tailscale necesita que inicies sesión."
		rep.Hint = "ejecuta 'tailscale up' y autentícate en el navegador"
		return rep
	case "NeedsMachineAuth":
		rep.State = StateNeedsMachineAuth
		rep.Reason = ReasonNeedsMachineAuth
		rep.Message = "Este equipo espera aprobación del administrador del tailnet."
		rep.Hint = "pide a un administrador que apruebe el equipo en la consola de Tailscale"
		return rep
	case "Starting":
		rep.State = StateStarting
		rep.Reason = ReasonStarting
		rep.Message = "Tailscale está arrancando."
		rep.Hint = "espera unos segundos"
		return rep
	case "Stopped", "NoState":
		rep.State = StateStopped
		rep.Reason = ReasonStopped
		rep.Message = "Tailscale está detenido."
		rep.Hint = "inicia Tailscale o ejecuta 'tailscale up'"
		return rep
	}
	rep.State = StateUnknown
	rep.Reason = ReasonUnknown
	rep.Message = "No pude determinar el estado de Tailscale."
	rep.Hint = "ejecuta 'tailscale status' para ver el detalle"
	return rep
}

// ipv4FromStatus busca la primera IPv4 del tailnet (rango 100.64.0.0/10).
func ipv4FromStatus(st Status) string {
	if st.Self == nil {
		return ""
	}
	for _, ip := range st.Self.TailscaleIPs {
		if InRange(ip) {
			return ip
		}
	}
	return ""
}

// onlinePeers cuenta los nodos en línea (el propio y los peers).
func onlinePeers(st Status) int {
	n := 0
	if st.Self != nil && st.Self.Online {
		n++
	}
	for _, p := range st.Peer {
		if p != nil && p.Online {
			n++
		}
	}
	return n
}
