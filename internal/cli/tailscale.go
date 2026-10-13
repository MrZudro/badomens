package cli

import (
	"fmt"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/execx"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/MrZudro/badomens/internal/tailscale"
	"github.com/spf13/cobra"
)

func newTailscaleCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tailscale",
		Short: "Estado de la red privada Tailscale (RF-6)",
	}
	cmd.AddCommand(newTailscaleStatusCmd(e), newTailscaleEnsureCmd(e))
	return cmd
}

func newTailscaleStatusCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Estado de Tailscale (IP, MagicDNS, peers)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := tailscale.Snapshot(cmd.Context())
			if err != nil {
				return e.finish(result.FromError("tailscale status", err), "")
			}
			ip, _ := tailscale.ResolveIPv4(cmd.Context())
			magic, _ := tailscale.MagicDNS(cmd.Context())
			data := tailscaleData(st, ip, magic)
			human := fmt.Sprintf("IP: %s", orDash(ip))
			if magic != "" {
				human += "\nNombre (MagicDNS): " + magic
			}
			human += fmt.Sprintf("\nPeers en línea: %d", onlinePeers(st))
			if ip == "" {
				return e.finish(result.Warn("tailscale status", "Tailscale está instalado pero sin IPv4", data), human)
			}
			return e.finish(result.OK("tailscale status", "Tailscale listo", data), human)
		},
	}
}

func newTailscaleEnsureCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "ensure",
		Short: "Intenta dejar la red privada lista (run + espera de IP)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !execx.LookPath("tailscale") {
				return e.finish(result.FromError("tailscale ensure", errx.Dependency(
					"no se encontró 'tailscale'",
					"instálalo en tailscale.com y ejecuta 'tailscale up'",
				)), "")
			}
			if ip, err := tailscale.ResolveIPv4(cmd.Context()); err == nil && ip != "" {
				return e.finish(result.OK("tailscale ensure", "Tailscale ya está listo: "+ip, map[string]any{"ip": ip}), "Tailscale listo: "+ip)
			}
			// Intenta levantar la sesión; si pide autenticación, lo dice el error.
			res, err := execx.Run(cmd.Context(), "tailscale", "up")
			if err != nil {
				msg := "no se pudo iniciar la sesión de Tailscale"
				if res.Stderr != "" {
					msg += ": " + res.Stderr
				}
				return e.finish(result.FromError("tailscale ensure", errx.Dependency(msg, "ejecuta 'tailscale up' manualmente y autentícate (o 'tailscale login' para ver la URL)")), "")
			}
			ip, ierr := tailscale.ResolveIPv4(cmd.Context())
			if ierr != nil {
				return e.finish(result.FromError("tailscale ensure", ierr), "")
			}
			return e.finish(result.OK("tailscale ensure", "Tailscale listo: "+ip, map[string]any{"ip": ip}), "Tailscale listo: "+ip)
		},
	}
}

func tailscaleData(st tailscale.Status, ip, magic string) map[string]any {
	self := map[string]any{"hostname": "", "magic_dns": magic, "ip": ip}
	if st.Self != nil {
		self["hostname"] = st.Self.HostName
	}
	peers := []map[string]any{}
	for _, p := range st.Peer {
		peers = append(peers, map[string]any{"hostname": p.HostName, "online": p.Online})
	}
	return map[string]any{"self": self, "peers": peers, "peers_online": onlinePeers(st)}
}

func onlinePeers(st tailscale.Status) int {
	n := 0
	if st.Self != nil && st.Self.Online {
		n++
	}
	for _, p := range st.Peer {
		if p.Online {
			n++
		}
	}
	return n
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
