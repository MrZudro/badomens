// Package tailscale resuelve la IP IPv4 efectiva del tailnet del host y
// consulta el estado de la red privada para el diagnóstico (RF-6).
package tailscale

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/MrZudro/badomens/internal/errx"
)

// Info describe un nodo del tailnet.
type Info struct {
	HostName     string   `json:"HostName"`
	DNSName      string   `json:"DNSName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
}

// Status es el subconjunto de `tailscale status --json` que usamos.
type Status struct {
	Self *Info            `json:"Self"`
	Peer map[string]*Info `json:"Peer"`
	// BackendState es el estado del backend ("Running", "Stopped",
	// "NeedsLogin", "NeedsMachineAuth", "Starting", "NoState"). Es el campo que
	// distingue «apagado» de «sin autenticar»; mirar solo Self no lo permite.
	BackendState string `json:"BackendState"`
	// MagicDNSSuffix es el sufijo DNS del tailnet ("tail1234.ts.net") cuando
	// MagicDNS está activo. Vacío si MagicDNS está deshabilitado.
	MagicDNSSuffix string `json:"MagicDNSSuffix"`
	// CurrentTailnet describe el tailnet activo (nombre y sufijo DNS).
	CurrentTailnet *Tailnet `json:"CurrentTailnet"`
}

// Tailnet es la descripción del tailnet activo.
type Tailnet struct {
	Name           string `json:"Name"`
	MagicDNSSuffix string `json:"MagicDNSSuffix"`
}

// ResolveIPv4 devuelve la primera IP IPv4 del tailnet (rango CGNAT 100.64.0.0/10).
// Devuelve un error de dependencia si Tailscale no está o no hay una IPv4.
func ResolveIPv4(ctx context.Context) (string, error) {
	return ResolveIPv4With(ctx, DefaultEnv())
}

// ResolveIPv4With es la variante inyectable de ResolveIPv4.
func ResolveIPv4With(ctx context.Context, env Env) (string, error) {
	if !env.lookPath("tailscale") {
		return "", errx.Dependency(
			"no se encontró 'tailscale'",
			"instálalo en tailscale.com y ejecuta 'tailscale up'",
		)
	}
	res, err := env.run(ctx, "tailscale", "ip", "-4")
	if err != nil {
		return "", errx.Dependency(
			"no se pudo obtener la IP del tailnet",
			"ejecuta 'tailscale up' y vuelve a intentarlo",
		)
	}
	return firstIPv4(res.Stdout), nil
}

// firstIPv4 devuelve la primera línea no vacía de la salida, vacío si no hay.
func firstIPv4(stdout string) string {
	for _, line := range strings.Split(stdout, "\n") {
		ip := strings.TrimSpace(line)
		if ip != "" {
			return ip
		}
	}
	return ""
}

// Snapshot consulta el estado del tailnet (--json). Devuelve error de
// dependencia si Tailscale no está.
func Snapshot(ctx context.Context) (Status, error) {
	return SnapshotWith(ctx, DefaultEnv())
}

// SnapshotWith es la variante inyectable de Snapshot.
func SnapshotWith(ctx context.Context, env Env) (Status, error) {
	var st Status
	if !env.lookPath("tailscale") {
		return st, errx.Dependency(
			"no se encontró 'tailscale'",
			"instálalo en tailscale.com y ejecuta 'tailscale up'",
		)
	}
	res, err := env.run(ctx, "tailscale", "status", "--json")
	if err != nil {
		return st, errx.Dependency(
			"no se pudo consultar el estado de Tailscale",
			"ejecuta 'tailscale up' y vuelve a intentarlo",
		)
	}
	if err := json.Unmarshal([]byte(res.Stdout), &st); err != nil {
		return st, errx.Dependency(
			"la salida de tailscale status no es legible",
			"comprueba tu instalación de tailscale",
		)
	}
	return st, nil
}

// MagicDNS devuelve el nombre DNS del nodo propio (maquina.tail1234.ts.net),
// vacío si no hay MagicDNS.
func MagicDNS(ctx context.Context) (string, error) {
	return MagicDNSWith(ctx, DefaultEnv())
}

// MagicDNSWith es la variante inyectable de MagicDNS.
func MagicDNSWith(ctx context.Context, env Env) (string, error) {
	st, err := SnapshotWith(ctx, env)
	if err != nil {
		return "", err
	}
	name, _, _ := MagicDNSFromStatus(st)
	return name, nil
}

// MagicDNSFromStatus decide, a partir del estado, si MagicDNS está activo y
// devuelve el nombre del nodo, la decisión y un motivo tipado. El motivo
// distingue «deshabilitado» de «campo ausente» para que la UI pueda explicar
// qué pasa y reevaluar cuando cambie.
func MagicDNSFromStatus(st Status) (name string, enabled bool, reason string) {
	if st.Self == nil {
		return "", false, ReasonMagicDNSNoSelf
	}
	raw := strings.TrimSuffix(strings.TrimSpace(st.Self.DNSName), ".")
	if raw == "" {
		return "", false, ReasonMagicDNSNoName
	}
	// Tailscale marca con el sufijo ".invalid." los nodos sin MagicDNS.
	if strings.Contains(raw, ".invalid.") || strings.HasSuffix(raw, ".invalid") {
		return "", false, ReasonMagicDNSDisabled
	}
	return raw, true, ReasonMagicDNSOK
}

// InRange indica si ip pertenece al rango CGNAT de Tailscale (100.64.0.0/10).
//
// El segundo octeto va de 64 a 127 y se compara numéricamente: compararlo como
// texto fallaba para los valores de dos dígitos (p. ej. "64" <= "127" es falso
// lexicográficamente), así que 100.64.x.x…100.99.x.x se descartaban por error.
func InRange(ip string) bool {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 || parts[0] != "100" {
		return false
	}
	second, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	if second < 64 || second > 127 {
		return false
	}
	for _, p := range parts[1:] {
		n, perr := strconv.Atoi(p)
		if perr != nil || n < 0 || n > 255 {
			return false
		}
	}
	return true
}
