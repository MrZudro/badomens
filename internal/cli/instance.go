package cli

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/ports"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

// describeConflicts resume quién ocupa un puerto para el mensaje de error.
func describeConflicts(cs []ports.Conflict) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		if c.Owner.Detail != "" {
			parts = append(parts, c.Owner.Detail)
		} else {
			parts = append(parts, c.Owner.Kind.String())
		}
	}
	return strings.Join(parts, "; ")
}

func newInstanceCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instance",
		Short: "Crear, listar, inspeccionar y gestionar instancias",
	}
	cmd.AddCommand(
		newInstanceListCmd(e),
		newInstanceInspectCmd(e),
		newInstanceCreateCmd(e),
		newInstanceStartCmd(e),
		newInstanceStopCmd(e),
		newInstanceCloseCmd(e),
		newInstanceRestartCmd(e),
		newInstanceRemoveCmd(e),
	)
	return cmd
}

func newInstanceListCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Lista las instancias registradas y su estado",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := e.manager()
			if err != nil {
				return e.finish(result.FromError("instance list", err), "")
			}
			specs, err := m.List()
			if err != nil {
				return e.finish(result.FromError("instance list", err), "")
			}
			rt, err := e.runtime()
			if err != nil {
				return e.finish(result.FromError("instance list", err), "")
			}
			out := make([]map[string]any, 0, len(specs))
			human := fmt.Sprintf("Instancias (%d):", len(specs))
			for _, s := range specs {
				state, _ := rt.State(cmd.Context(), s.Slug)
				out = append(out, map[string]any{
					"slug": s.Slug, "name": s.Name, "engine": s.Engine, "version": s.Version,
					"memory": s.Memory, "port": s.Port, "state": state, "gamemode": s.Gamemode,
					"online": s.OnlineMode, "max_players": s.MaxPlayers,
				})
				human += fmt.Sprintf("\n  %-16s %-8s %-8s %-8s pto %-6d estado %-8s", s.Slug, s.Engine, s.Version, s.Memory, s.Port, state)
			}
			return e.finish(result.OK("instance list", "instancias registradas", out), human)
		},
	}
}

func newInstanceInspectCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <instancia>",
		Short: "Muestra la configuración completa de una instancia",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.specFor(args[0])
			if err != nil {
				return e.finish(result.FromError("instance inspect", err), "")
			}
			data := map[string]any{
				"slug": spec.Slug, "name": spec.Name, "engine": spec.Engine, "version": spec.Version,
				"pins": spec.Pins, "memory": spec.Memory, "cpu": spec.CPU, "port": spec.Port,
				"difficulty": spec.Difficulty, "gamemode": spec.Gamemode, "max_players": spec.MaxPlayers,
				"online_mode": spec.OnlineMode, "motd": spec.Motd, "seed": spec.Seed,
				"whitelist_enabled": spec.WhitelistEnabled, "whitelist": spec.Whitelist,
				"ops": spec.Ops, "plugins": spec.Plugins, "created_at": spec.CreatedAt,
				"stopped_by": spec.StoppedBy, "stopped_at": spec.StoppedAt,
			}
			// Conexión (RF-2): la guardada o, si falta, la calculada al vuelo.
			if spec.Connection != nil {
				data["connection"] = connectionData(*spec.Connection)
			} else {
				conn := buildConnection(cmd.Context(), e, spec)
				data["connection"] = connectionData(conn)
			}
			// Auto-apagado (RF-8).
			data["idle"] = idleData(spec.Idle)
			human := fmt.Sprintf("Instancia %s (%s)\n", spec.Slug, spec.Name)
			human += fmt.Sprintf("  motor %s %s pines=%v\n", spec.Engine, spec.Version, spec.Pins)
			human += fmt.Sprintf("  recursos %s (cpu %d) · puerto %d\n", spec.Memory, spec.CPU, spec.Port)
			human += fmt.Sprintf("  dificultad %s · modo %s · max %d jugadores · online=%v\n", spec.Difficulty, spec.Gamemode, spec.MaxPlayers, spec.OnlineMode)
			if spec.Motd != "" {
				human += fmt.Sprintf("  motd: %s\n", spec.Motd)
			}
			human += fmt.Sprintf("  whitelist=%v [%s] · ops [%s]\n", spec.WhitelistEnabled, strings.Join(spec.Whitelist, ", "), strings.Join(spec.Ops, ", "))
			return e.finish(result.OK("instance inspect", spec.Slug, data), strings.TrimRight(human, "\n"))
		},
	}
}

func newInstanceStartCmd(e *Env) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "start <instancia>",
		Short: "Arranca una instancia (respeta el límite de instancias en paralelo)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			spec, err := e.specFor(slug)
			if err != nil {
				return e.finish(result.FromError("instance start", err), "")
			}
			cfg, _ := e.requireConfig()
			res, err := startInstance(cmd, e, spec, cfg.MaxParallel, force)
			if err != nil {
				return e.finish(res, "")
			}
			return e.finish(res, humanFor(res))
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "arrancar aunque se supere el límite de paralelos")
	return cmd
}

func startInstance(cmd *cobra.Command, e *Env, spec *instance.Spec, maxParallel int, force bool) (*result.Result, error) {
	rt, err := e.runtime()
	if err != nil {
		return result.FromError("instance start", err), err
	}
	state, err := rt.State(cmd.Context(), spec.Slug)
	if err != nil {
		return result.FromError("instance start", err), err
	}
	if state == "running" {
		return result.OK("instance start", "la instancia "+spec.Slug+" ya está corriendo", nil), nil
	}
	if !force {
		running, err := rt.RunningCount(cmd.Context(), allBut(e))
		if err != nil {
			return result.FromError("instance start", err), err
		}
		if running >= maxParallel {
			return result.FromError("instance start", errx.Capacity(
				fmt.Sprintf("ya hay %d instancias en paralelo (máximo %d)", running, maxParallel),
				"detiene una antes, o usa --force",
			)), err
		}
	}
	// RF-3: revalidar el puerto (pudo ocuparse desde la creación).
	if checker, cerr := e.portsChecker(); cerr == nil {
		checker.Exclude = map[string]bool{spec.Slug: true}
		if res, rerr := checker.Check(cmd.Context(), netip.Addr{}, spec.Port); rerr == nil && !res.Free {
			msg := fmt.Sprintf("el puerto %d de %s está ocupado", spec.Port, spec.Slug)
			if who := describeConflicts(res.Conflicts); who != "" {
				msg += " (" + who + ")"
			}
			err := errx.Capacity(msg, fmt.Sprintf("usa 'badomens ports check %d' o cambia el puerto", spec.Port)).
				WithDetail("reason", "port_in_use").
				WithDetail("port", spec.Port).
				WithDetail("suggested", res.Suggested)
			return result.FromError("instance start", err), err
		}
	}
	if err := rt.Start(cmd.Context(), spec); err != nil {
		return result.FromError("instance start", err), err
	}
	// RF-2: se recalcula y guarda la información de conexión en cada arranque.
	data := map[string]any{"instance": spec.Slug}
	if conn, cerr := rememberConnection(cmd.Context(), e, spec); cerr == nil {
		data["connection"] = connectionData(conn)
	}
	return result.OK("instance start", "la instancia "+spec.Slug+" está arrancando", data), nil
}

func newInstanceStopCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "stop <instancia>",
		Short: "Detiene el contenedor (apagado rápido)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := e.runtime()
			if err != nil {
				return e.finish(result.FromError("instance stop", err), "")
			}
			if err := rt.Stop(cmd.Context(), args[0]); err != nil {
				return e.finish(result.FromError("instance stop", err), "")
			}
			return e.finish(result.OK("instance stop", "instancia "+args[0]+" detenida", nil), "instancia "+args[0]+" detenida")
		},
	}
}

func newInstanceCloseCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "close <instancia>",
		Short: "Apaga con cuidado enviando 'stop' a la consola (guarda todo)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := e.runtime()
			if err != nil {
				return e.finish(result.FromError("instance close", err), "")
			}
			if err := rt.Close(cmd.Context(), args[0]); err != nil {
				return e.finish(result.FromError("instance close", err), "")
			}
			return e.finish(result.OK("instance close", "instancia "+args[0]+" cerrada con cuidado", nil), "instancia "+args[0]+" cerrada")
		},
	}
}

func newInstanceRestartCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "restart <instancia>",
		Short: "Reinicia y recrea el contenedor",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := e.runtime()
			if err != nil {
				return e.finish(result.FromError("instance restart", err), "")
			}
			if err := rt.Restart(cmd.Context(), args[0]); err != nil {
				return e.finish(result.FromError("instance restart", err), "")
			}
			return e.finish(result.OK("instance restart", "instancia "+args[0]+" reiniciada", nil), "instancia "+args[0]+" reiniciada")
		},
	}
}

func newInstanceRemoveCmd(e *Env) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "remove <instancia>",
		Short: "Elimina la instancia (contenedor y carpeta de datos)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			if _, err := e.specFor(slug); err != nil {
				return e.finish(result.FromError("instance remove", err), "")
			}
			ok, err := e.confirm("borrar la instancia", slug, yes)
			if err != nil || !ok {
				return e.finish(result.FromError("instance remove", errx.Usage("operación cancelada", "")), "eliminación cancelada")
			}
			rt, err := e.runtime()
			if err != nil {
				return e.finish(result.FromError("instance remove", err), "")
			}
			_ = rt.Down(cmd.Context(), slug) // no bloquea: si no existe, se ignora
			m, err := e.manager()
			if err != nil {
				return e.finish(result.FromError("instance remove", err), "")
			}
			if err := m.Remove(slug); err != nil {
				return e.finish(result.FromError("instance remove", err), "")
			}
			return e.finish(result.OK("instance remove", "instancia "+slug+" eliminada", nil), "instancia "+slug+" eliminada")
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirmar sin preguntar")
	return cmd
}

// allBut devuelve todas las instancias (para el recuento de paralelos) o las
// que puedan cargarse; errores de lectura se ignoran aquí.
func allBut(e *Env) []*instance.Spec {
	m, err := e.manager()
	if err != nil {
		return nil
	}
	specs, err := m.List()
	if err != nil {
		return nil
	}
	return specs
}

// humanFor devuelve el mensaje legible de un resultado exitoso.
func humanFor(res *result.Result) string {
	if res == nil || res.Error != nil {
		return ""
	}
	return res.Message
}
