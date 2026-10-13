// Package tailscale resuelve la IP IPv4 efectiva del tailnet del host.
package tailscale

import (
	"context"
	"strings"

	"github.com/manuelserver/badomens/internal/errx"
	"github.com/manuelserver/badomens/internal/execx"
)

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

// InRange indica si ip pertenece al rango CGNAT de Tailscale (100.64.0.0/10).
func InRange(ip string) bool {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 || parts[0] != "100" {
		return false
	}
	// El segundo byte va de 64 a 127. Comparación lexicográfica válida para 0-255.
	return parts[1] >= "64" && parts[1] <= "127"
}
