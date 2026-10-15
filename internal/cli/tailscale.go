package cli

import (
	"fmt"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/execx"
	"github.com/MrZudro/badomens/internal/infra"
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
			// Estado tipado con motivo (incluye el de MagicDNS): es la API que la
			// TUI puede consultar para reevaluar el aviso de MagicDNS.
			typed := infra.NewTailscale(tailscale.DefaultEnv()).Detect(cmd.Context())
			data := map[string]any{"status": infraStateData(typed)}
			human := typed.Message

			st, serr := tailscale.Snapshot(cmd.Context())
			if serr != nil {
				res := result.FromError("tailscale status", serr)
				addReason(res, typed.Reason)
				return e.finish(res, human)
			}
			ip, _ := typed.Extra["ip"].(string)
			magic, _ := typed.Extra["magic_dns"].(string)
			for k, v := range tailscaleData(st, ip, magic) {
				data[k] = v
			}
			data["magic_dns"] = magic
			data["magic_dns_enabled"] = typed.Extra["magic_dns_enabled"]
			data["magic_dns_reason"] = typed.Extra["magic_dns_reason"]
			data["reason"] = typed.Reason

			human += "\nIP: " + orDash(ip)
			if magic != "" {
				human += "\nNombre (MagicDNS): " + magic
			}
			human += fmt.Sprintf("\nPeers en línea: %d", onlinePeers(st))
			if typed.Status == infra.StatusReady {
				return e.finish(result.OK("tailscale status", "Tailscale listo", data), human)
			}
			if ip == "" {
				res := result.Warn("tailscale status", "Tailscale está instalado pero sin IPv4", data)
				addReason(res, typed.Reason)
				return e.finish(res, human)
			}
			return e.finish(result.OK("tailscale status", "Tailscale listo", data), human)
		},
	}
}

func newTailscaleEnsureCmd(e *Env) *cobra.Command {
	var wait int
	var allowElevation, login bool
	cmd := &cobra.Command{
		Use:   "ensure",
		Short: "Intenta dejar la red privada lista (levanta el servicio y espera la IP)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := infra.Options{Timeout: 60 * time.Second, AllowElevation: allowElevation}
			if wait > 0 {
				opts.Timeout = time.Duration(wait) * time.Second
			}
			// La autenticación es interactiva: solo se intenta si el usuario lo pide.
			if login {
				if _, lerr := execx.Run(cmd.Context(), "tailscale", "up"); lerr != nil {
					// el estado tipado de abajo explicará qué falta
					_ = lerr
				}
			}
			comp := infra.NewTailscale(tailscale.DefaultEnv())
			rep := infra.Ensure(cmd.Context(), infra.DefaultEnv(), comp, opts)
			data := infraReportData(rep)
			human := rep.Message
			if rep.Ready {
				ip, _ := rep.Extra["ip"].(string)
				if ip != "" {
					data["ip"] = ip
					human = "Tailscale listo: " + ip
				}
				return e.finish(result.OK("tailscale ensure", human, data), human)
			}
			for _, m := range rep.Manual {
				human += "\n  manual: " + m
			}
			if rep.Status == infra.StatusNotInstalled {
				return e.finish(result.FromError("tailscale ensure", errx.Dependency(rep.Message, rep.Hint)), human)
			}
			res := result.FromError("tailscale ensure", errx.Unavailable(rep.Message, firstHint(rep)))
			addReason(res, rep.Reason)
			res.Data = data
			return e.finish(res, human)
		},
	}
	cmd.Flags().IntVar(&wait, "wait", 0, "segundos máximos de espera (por defecto 60)")
	cmd.Flags().BoolVar(&allowElevation, "allow-elevation", false, "autoriza pedir permisos de administrador (se explica el motivo)")
	cmd.Flags().BoolVar(&login, "login", false, "ejecuta 'tailscale up' (autenticación interactiva) antes de comprobar")
	return cmd
}

// addReason añade el motivo estable a error.details del documento, sin tocar el
// resto del contrato.
func addReason(res *result.Result, reason string) {
	if res == nil || res.Error == nil || reason == "" {
		return
	}
	if res.Error.Details == nil {
		res.Error.Details = map[string]any{}
	}
	res.Error.Details["reason"] = reason
}

func tailscaleData(st tailscale.Status, ip, magic string) map[string]any {
	self := map[string]any{"hostname": "", "magic_dns": magic, "ip": ip}
	if st.Self != nil {
		self["hostname"] = st.Self.HostName
	}
	peers := []map[string]any{}
	for _, p := range st.Peer {
		if p == nil {
			continue
		}
		peers = append(peers, map[string]any{"hostname": p.HostName, "online": p.Online})
	}
	return map[string]any{
		"self":          self,
		"peers":         peers,
		"peers_online":  onlinePeers(st),
		"backend_state": st.BackendState,
	}
}

func onlinePeers(st tailscale.Status) int {
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

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
