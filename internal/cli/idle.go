package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/MrZudro/badomens/internal/backup"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/idle"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/MrZudro/badomens/internal/runtime"
	"github.com/MrZudro/badomens/internal/tailscale"
	"github.com/spf13/cobra"
)

func newIdleCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "idle",
		Short: "Auto-apagado por inactividad (RF-8)",
	}
	cmd.AddCommand(
		newIdleSetCmd(e),
		newIdleStatusCmd(e),
		newIdleDisableCmd(e),
		newIdleDaemonCmd(e),
		newIdleInstallServiceCmd(e),
		newIdleUninstallServiceCmd(e),
	)
	return cmd
}

// requireInstance devuelve el slug activo (-i) o un error de uso.
func (e *Env) requireInstance() (string, error) {
	if e.instanceFlag == "" {
		return "", errx.Usage("falta --instance", "indica la instancia con -i <slug>")
	}
	return e.instanceFlag, nil
}

func newIdleSetCmd(e *Env) *cobra.Command {
	var timeout, grace string
	var backup bool
	var noBackup bool
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Configura el auto-apagado por inactividad de una instancia",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			slug, err := e.requireInstance()
			if err != nil {
				return e.finish(result.FromError("idle set", err), "")
			}
			m, merr := e.manager()
			if merr != nil {
				return e.finish(result.FromError("idle set", merr), "")
			}
			spec, lerr := m.Load(slug)
			if lerr != nil {
				return e.finish(result.FromError("idle set", lerr), "")
			}
			cfg := spec.Idle.Normalized()
			if timeout != "" {
				cfg.Timeout, err = idle.ParseDuration(timeout)
				if err != nil {
					return e.finish(result.FromError("idle set", errx.Usage(err.Error(), "ej. --timeout 30m (mínimo 1m, máximo 24h)")), "")
				}
			}
			if grace != "" {
				cfg.FirstJoinGrace, err = idle.ParseDuration(grace)
				if err != nil {
					return e.finish(result.FromError("idle set", errx.Usage(err.Error(), "ej. --grace 10m")), "")
				}
			}
			if backup {
				cfg.BackupBeforeStop = true
			}
			if noBackup {
				cfg.BackupBeforeStop = false
			}
			cfg.Enabled = true
			cfg = cfg.Normalized()
			if verr := cfg.Validate(); verr != nil {
				return e.finish(result.FromError("idle set", errx.Usage(verr.Error(), "elige un tiempo entre 1m y 24h")), "")
			}
			spec.Idle = cfg
			if serr := m.Save(spec); serr != nil {
				return e.finish(result.FromError("idle set", serr), "")
			}
			data := idleData(spec.Idle)
			human := fmt.Sprintf("Auto-apagado de %s: tras %s sin jugadores", slug, idle.HumanDuration(cfg.Timeout))
			return e.finish(result.OK("idle set", human, data), human)
		},
	}
	cmd.Flags().StringVar(&timeout, "timeout", "", "tiempo sin jugadores antes de apagar (ej. 30m)")
	cmd.Flags().StringVar(&grace, "grace", "", "tiempo tras iniciar antes de contar (ej. 10m)")
	cmd.Flags().BoolVar(&backup, "backup", false, "hacer un backup antes de apagar")
	cmd.Flags().BoolVar(&noBackup, "no-backup", false, "no hacer backup antes de apagar")
	return cmd
}

func newIdleStatusCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Estado de auto-apagado de una instancia",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			slug, err := e.requireInstance()
			if err != nil {
				return e.finish(result.FromError("idle status", err), "")
			}
			m, merr := e.manager()
			if merr != nil {
				return e.finish(result.FromError("idle status", merr), "")
			}
			spec, lerr := m.Load(slug)
			if lerr != nil {
				return e.finish(result.FromError("idle status", lerr), "")
			}
			rt, rerr := e.runtime()
			if rerr != nil {
				return e.finish(result.FromError("idle status", rerr), "")
			}
			running := false
			if st, serr := rt.State(cmd.Context(), slug); serr == nil {
				running = st == "running"
			}
			data := idleData(spec.Idle)
			data["running"] = running
			human := fmt.Sprintf("Auto-apagado de %s: %s (servidor %s)", slug, enabledText(spec.Idle), boolRunning(running))
			return e.finish(result.OK("idle status", human, data), human)
		},
	}
}

func newIdleDisableCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Desactiva el auto-apagado de una instancia",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			slug, err := e.requireInstance()
			if err != nil {
				return e.finish(result.FromError("idle disable", err), "")
			}
			m, merr := e.manager()
			if merr != nil {
				return e.finish(result.FromError("idle disable", merr), "")
			}
			spec, lerr := m.Load(slug)
			if lerr != nil {
				return e.finish(result.FromError("idle disable", lerr), "")
			}
			spec.Idle.Enabled = false
			if serr := m.Save(spec); serr != nil {
				return e.finish(result.FromError("idle disable", serr), "")
			}
			return e.finish(result.OK("idle disable", "auto-apagado desactivado en "+slug, idleData(spec.Idle)), "auto-apagado desactivado")
		},
	}
}

func newIdleDaemonCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Vigilante de auto-apagado en segundo plano (un proceso para todas las instancias)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, cerr := e.requireConfig()
			if cerr != nil {
				return e.finish(result.FromError("idle daemon", cerr), "")
			}
			if _, cerr = cfg.EnsureDataDir(); cerr != nil {
				return e.finish(result.FromError("idle daemon", cerr), "")
			}
			lock, lerr := idle.Acquire(filepath.Join(cfg.DataDir, "idle.lock"))
			if lerr != nil {
				if errors.Is(lerr, idle.ErrStandby) {
					return e.finish(result.OK("idle daemon", "ya hay un vigilante activo (este proceso queda a la espera)", nil), "ya hay un vigilante activo")
				}
				return e.finish(result.FromError("idle daemon", lerr), "")
			}
			defer func() { _ = lock.Release() }()

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			w := newIdleWatcher(e)
			_ = lock.Touch()
			w.TickOnce(ctx)
			tick := time.NewTicker(15 * time.Second)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return e.finish(result.OK("idle daemon", "vigilante detenido", nil), "vigilante detenido")
				case <-tick.C:
					_ = lock.Touch()
					w.TickOnce(ctx)
				}
			}
		},
	}
}

// newIdleWatcher construye el vigilante sobre los paquetes reales.
func newIdleWatcher(e *Env) *idle.Watcher {
	rt, _ := e.runtime()
	cfg, _ := e.requireConfig()
	w := idle.NewWatcher()
	w.Targets = func(ctx context.Context) ([]idle.Target, error) {
		m, merr := e.manager()
		if merr != nil {
			return nil, merr
		}
		specs, lerr := m.List()
		if lerr != nil {
			return nil, lerr
		}
		var out []idle.Target
		for _, s := range specs {
			if !s.Idle.Enabled {
				continue
			}
			running := false
			if st, serr := rt.State(ctx, s.Slug); serr == nil {
				running = st == "running"
			}
			out = append(out, idle.Target{Instance: s.Slug, Config: s.Idle, Running: running})
		}
		return out, nil
	}
	w.Counter = idle.ChainCounter{
		&idle.SLPCounter{
			Endpoint: func(slug string) (string, int, bool) {
				m, merr := e.manager()
				if merr != nil {
					return "", 0, false
				}
				spec, lerr := m.Load(slug)
				if lerr != nil || spec.Port == 0 {
					return "", 0, false
				}
				ip := ""
				if spec.Connection != nil {
					ip = spec.Connection.Host
				}
				if ip == "" {
					ip, _ = tailscale.ResolveIPv4(context.Background())
				}
				if ip == "" {
					return "", 0, false
				}
				return ip, spec.Port, true
			},
			Timeout: 2 * time.Second,
		},
		// Respaldo: `docker exec … rcon-cli list` si el SLP no responde.
		&idle.DockerCounter{
			Container: func(slug string) string {
				if rt == nil {
					return ""
				}
				return rt.ContainerName(slug)
			},
		},
	}
	be := &backup.Backend{DataDir: cfg.DataDir}
	w.Controller = &runtimeIdleController{rt: rt, be: be}
	w.Announce = func(ctx context.Context, instance, msg string) error {
		if rt == nil {
			return nil
		}
		return rt.Console(ctx, instance, "say "+msg)
	}
	w.Record = func(instance string, phase idle.Phase, d idle.Decision) {
		if phase == idle.PhaseStopped {
			_ = recordStopped(e, instance, "idle")
		}
	}
	return w
}

// runtimeIdleController adapta runtime + backup a idle.Controller.
type runtimeIdleController struct {
	rt *runtime.Runtime
	be *backup.Backend
}

func (c *runtimeIdleController) Running(_ context.Context, slug string) (bool, error) {
	if c.rt == nil {
		return false, nil
	}
	st, err := c.rt.State(context.Background(), slug)
	return st == "running", err
}

func (c *runtimeIdleController) Flush(ctx context.Context, slug string) error {
	if c.rt == nil {
		return nil
	}
	return c.rt.Console(ctx, slug, "save-all flush")
}

func (c *runtimeIdleController) Stop(ctx context.Context, slug string) error {
	if c.rt == nil {
		return nil
	}
	// Parada limpia: "stop" por consola (guarda el mundo y cierra Java) y luego
	// compose stop con el stop_grace_period del compose (≥ 60 s).
	_ = c.rt.Console(ctx, slug, "stop")
	return c.rt.Stop(ctx, slug)
}

func (c *runtimeIdleController) Backup(ctx context.Context, slug string) error {
	if c.be == nil {
		return nil
	}
	_, err := c.be.Create(slug, "")
	return err
}

// recordStopped deja constancia del apagado en el registro.
func recordStopped(e *Env, slug, by string) error {
	m, merr := e.manager()
	if merr != nil {
		return merr
	}
	spec, lerr := m.Load(slug)
	if lerr != nil {
		return lerr
	}
	spec.StoppedBy = by
	spec.StoppedAt = resultNow()
	return m.Save(spec)
}

func idleData(cfg idle.Config) map[string]any {
	return map[string]any{
		"enabled":            cfg.Enabled,
		"timeout":            durString(cfg.Timeout),
		"first_join_grace":   durString(cfg.FirstJoinGrace),
		"warn_before":        durString(cfg.WarnBefore),
		"action":             cfg.Action,
		"backup_before_stop": cfg.BackupBeforeStop,
	}
}

func durString(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.String()
}

func enabledText(cfg idle.Config) string {
	if !cfg.Enabled {
		return "desactivado"
	}
	return "activado (" + idle.HumanDuration(cfg.Timeout) + ")"
}

func boolRunning(b bool) string {
	if b {
		return "en marcha"
	}
	return "detenido"
}

func resultNow() string { return time.Now().UTC().Format(time.RFC3339) }
