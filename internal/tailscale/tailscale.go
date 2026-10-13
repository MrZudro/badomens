// Package tailscale resuelve la IP IPv4 efectiva del tailnet del host y
// consulta el estado de la red privada para el diagnóstico (RF-6).
package tailscale

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/execx"
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
}

// ResolveIPv4 devuelve la primera IP IPv4 del tailnet (rango CGNAT 100.64.0.0/10).
// Devuelve un error de dependencia si Tailscale no está o no hay una IPv4.
func ResolveIPv4(ctx context.Context) (string, error) {
	if !execx.LookPath("tailscale") {
		return "", errx.Dependency(
			"no se encontró 'tailscale'",
			"instálalo en tailscale.com y ejecuta 'tailscale up'",
		)
	}
	res, err := execx.Run(ctx, "tailscale", "ip", "-4")
	if err != nil {
		return "", errx.Dependency(
			"no se pudo obtener la IP del tailnet",
			"ejecuta 'tailscale up' y vuelve a intentarlo",
		)
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		ip := strings.TrimSpace(line)
		if ip != "" {
			return ip, nil
		}
	}
	return "", errx.Dependency(
		"no se detectó una IPv4 del tailnet",
		"ejecuta 'tailscale up' y vuelve a intentarlo",
	)
}

// Snapshot consulta el estado del tailnet (--json). Devuelve error de
// dependencia si Tailscale no está.
func Snapshot(ctx context.Context) (Status, error) {
	var st Status
	if !execx.LookPath("tailscale") {
		return st, errx.Dependency(
			"no se encontró 'tailscale'",
			"instálalo en tailscale.com y ejecuta 'tailscale up'",
		)
	}
	res, err := execx.Run(ctx, "tailscale", "status", "--json")
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
	st, err := Snapshot(ctx)
	if err != nil {
		return "", err
	}
	if st.Self == nil {
		return "", nil
	}
	name := strings.TrimSuffix(st.Self.DNSName, ".")
	if name == "" || strings.Contains(name, ".invalid.") {
		return "", nil
	}
	return name, nil
}

// InRange indica si ip pertenece al rango CGNAT de Tailscale (100.64.0.0/10).
func InRange(ip string) bool {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 || parts[0] != "100" {
		return false
	}
	// El segundo byte va de 64 a 127. Comparación lexicográfica válida para 0-255.
	return parts[1] >= "64" && parts[1] <= "127"
}
