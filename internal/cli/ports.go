package cli

import (
	"fmt"
	"net/netip"
	"strconv"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/ports"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

func newPortsCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ports",
		Short: "Estado y reserva de puertos (RF-3)",
	}
	cmd.AddCommand(newPortsCheckCmd(e), newPortsListCmd(e))
	return cmd
}

func newPortsCheckCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "check <puerto>",
		Short: "Comprueba si un puerto está libre (registro, docker, host y tailnet)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			port, err := strconv.Atoi(args[0])
			if err != nil || port < 1 || port > 65535 {
				return e.finish(result.FromError("ports check", errx.Usage("puerto inválido "+args[0], "usa un número entre 1 y 65535")), "")
			}
			alloc, aerr := e.portsAllocator(cmd.Context())
			if aerr != nil {
				return e.finish(result.FromError("ports check", aerr), "")
			}
			res, cerr := alloc.Check(cmd.Context(), netip.Addr{}, port)
			if cerr != nil {
				return e.finish(result.FromError("ports check", cerr), "")
			}
			data := map[string]any{
				"port":      res.Port,
				"free":      res.Free,
				"suggested": res.Suggested,
				"conflicts": conflictsData(res.Conflicts),
			}
			human := fmt.Sprintf("Puerto %d: %s", port, boolFree(res.Free))
			for _, cf := range res.Conflicts {
				human += "\n  ✖ " + cf.Owner.Detail + " (" + cf.Severity.String() + ")"
			}
			if res.Suggested != 0 {
				human += "\n  → sugerido: " + strconv.Itoa(res.Suggested)
			}
			if res.Free {
				return e.finish(result.OK("ports check", "el puerto "+strconv.Itoa(port)+" está libre", data), human)
			}
			return e.finish(result.Warn("ports check", "el puerto "+strconv.Itoa(port)+" está ocupado", data), human)
		},
	}
}

func newPortsListCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Lista qué puertos usa badomens (instancias y reservas)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			alloc, aerr := e.portsAllocator(cmd.Context())
			if aerr != nil {
				return e.finish(result.FromError("ports list", aerr), "")
			}
			rs, rerr := alloc.Reservations(cmd.Context())
			if rerr != nil {
				return e.finish(result.FromError("ports list", rerr), "")
			}
			m, merr := e.manager()
			if merr != nil {
				return e.finish(result.FromError("ports list", merr), "")
			}
			specs, _ := m.List()
			instances := make([]map[string]any, 0, len(specs))
			for _, s := range specs {
				instances = append(instances, map[string]any{"instance": s.Slug, "port": s.Port})
			}
			reservations := make([]map[string]any, 0, len(rs))
			for _, r := range rs {
				reservations = append(reservations, map[string]any{"instance": r.Instance, "port": r.Port, "expires": r.Expires})
			}
			data := map[string]any{"instances": instances, "reservations": reservations}
			human := fmt.Sprintf("Puertos de badomens (%d instancias, %d reservas):", len(specs), len(rs))
			for _, s := range specs {
				human += fmt.Sprintf("\n  %-16s pto %-6d", s.Slug, s.Port)
			}
			for _, r := range rs {
				human += fmt.Sprintf("\n  %-16s reserva pto %d (expira %s)", r.Instance, r.Port, r.Expires.Local().Format("2006-01-02 15:04"))
			}
			return e.finish(result.OK("ports list", "estado de puertos", data), human)
		},
	}
}

func boolFree(free bool) string {
	if free {
		return "libre"
	}
	return "ocupado"
}

func conflictsData(cs []ports.Conflict) []map[string]any {
	out := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		out = append(out, map[string]any{
			"port":     c.Port,
			"severity": c.Severity.String(),
			"owner": map[string]any{
				"kind":   c.Owner.Kind.String(),
				"name":   c.Owner.Name,
				"pid":    c.Owner.PID,
				"detail": c.Owner.Detail,
			},
		})
	}
	return out
}
