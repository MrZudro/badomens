package cli

import (
	"os"
	"runtime"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/infra"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

// newDaemonCmd expone el daemon propio de badomens (el vigilante de auto-apagado)
// con el mecanismo común detectar → levantar → elevar → informar (Fase 8).
func newDaemonCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Daemon propio de badomens: estado, arranque y servicio persistente",
	}
	cmd.AddCommand(
		newDaemonStatusCmd(e),
		newDaemonEnsureCmd(e),
		newDaemonInstallCmd(e),
		newDaemonUninstallCmd(e),
	)
	return cmd
}

// daemonComponent construye el componente del daemon con su estado real.
func daemonComponent(e *Env) infra.Daemon {
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	lock := ""
	if cfg, cerr := e.requireConfig(); cerr == nil && cfg != nil {
		lock = infra.DaemonLockPath(cfg.DataDir)
	}
	home, _ := os.UserHomeDir()
	opts := infra.DaemonOptions{Exe: exe, LockPath: lock}
	if spec, serr := infra.DaemonService(runtime.GOOS, home, exe, infra.ScopeUser); serr == nil {
		opts.Service = &spec
	}
	return infra.NewDaemon(opts)
}

func newDaemonStatusCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Estado del daemon de badomens (en marcha, instalado como servicio…)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st := daemonComponent(e).Detect(cmd.Context())
			data := map[string]any{"daemon": infraStateData(st)}
			human := "Daemon: " + st.Message
			switch st.Status {
			case infra.StatusReady:
				return e.finish(result.OK("daemon status", st.Message, data), human)
			case infra.StatusNotInstalled:
				return e.finish(result.FromError("daemon status",
					errx.Dependency(st.Message, st.Hint).WithDetail("reason", st.Reason)), human)
			default:
				data["daemon_installed"] = daemonServiceInstalled(e)
				return e.finish(result.Warn("daemon status", st.Message, data), human)
			}
		},
	}
}

func newDaemonEnsureCmd(e *Env) *cobra.Command {
	var wait int
	var allowElevation bool
	cmd := &cobra.Command{
		Use:   "ensure",
		Short: "Comprueba y fuerza el arranque del daemon de badomens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := infra.Options{
				Timeout:        30 * time.Second,
				AllowElevation: allowElevation,
			}
			if wait > 0 {
				opts.Timeout = time.Duration(wait) * time.Second
			}
			rep := infra.Ensure(cmd.Context(), infra.DefaultEnv(), daemonComponent(e), opts)
			data := infraReportData(rep)
			data["service_installed"] = daemonServiceInstalled(e)
			human := daemonEnsureHuman(rep)
			if rep.Ready {
				return e.finish(result.OK("daemon ensure", rep.Message, data), human)
			}
			if rep.Status == infra.StatusNotInstalled {
				return e.finish(result.FromError("daemon ensure",
					errx.Dependency(rep.Message, rep.Hint)), human)
			}
			return e.finish(result.FromError("daemon ensure",
				errx.New(errx.CodeDependency, rep.Message, firstHint(rep)).WithDetail("reason", rep.Reason)), human)
		},
	}
	cmd.Flags().IntVar(&wait, "wait", 0, "segundos máximos de espera (por defecto 30)")
	cmd.Flags().BoolVar(&allowElevation, "allow-elevation", false, "autoriza pedir permisos de administrador (se explica el motivo antes)")
	return cmd
}

// newDaemonInstallCmd instala el daemon como servicio persistente por SO.
func newDaemonInstallCmd(e *Env) *cobra.Command {
	var dryRun, system bool
	cmd := &cobra.Command{
		Use:   "install-service",
		Short: "Instala el daemon como servicio persistente (systemd/launchd/tarea programada)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := daemonServiceSpec(system)
			if err != nil {
				return e.finish(result.FromError("daemon install-service", err), "")
			}
			data := map[string]any{
				"os": spec.OS, "scope": string(spec.Scope), "exe": spec.Exe,
				"path": spec.Path, "unit": spec.Unit, "install_args": spec.InstallArgs,
				"requires_elevation": spec.RequiresElevation, "dry_run": dryRun,
			}
			if dryRun {
				return e.finish(result.OK("daemon install-service", "simulación: no se escribió nada", data), renderServicePlan(spec))
			}
			if err := infra.InstallDaemonService(cmd.Context(), infra.DefaultEnv(), spec); err != nil {
				return e.finish(result.FromError("daemon install-service", err), "")
			}
			msg := "daemon instalado como servicio (" + spec.OS + ", " + string(spec.Scope) + ")"
			return e.finish(result.OK("daemon install-service", msg, data), msg)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "muestra lo que se instalaría sin escribir nada")
	cmd.Flags().BoolVar(&system, "system", false, "instala a nivel de sistema (requiere permisos de administrador)")
	return cmd
}

func newDaemonUninstallCmd(e *Env) *cobra.Command {
	var system bool
	cmd := &cobra.Command{
		Use:   "uninstall-service",
		Short: "Quita el daemon del sistema",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := daemonServiceSpec(system)
			if err != nil {
				return e.finish(result.FromError("daemon uninstall-service", err), "")
			}
			if err := infra.UninstallDaemonService(cmd.Context(), infra.DefaultEnv(), spec); err != nil {
				return e.finish(result.FromError("daemon uninstall-service", err), "")
			}
			return e.finish(result.OK("daemon uninstall-service", "daemon eliminado del sistema",
				map[string]any{"path": spec.Path}), "servicio eliminado")
		},
	}
	cmd.Flags().BoolVar(&system, "system", false, "opera sobre el servicio de sistema")
	return cmd
}

// daemonServiceSpec describe el servicio persistente del daemon.
func daemonServiceSpec(system bool) (infra.ServiceSpec, error) {
	exe, err := os.Executable()
	if err != nil {
		return infra.ServiceSpec{}, err
	}
	home, _ := os.UserHomeDir()
	scope := infra.ScopeUser
	if system {
		scope = infra.ScopeSystem
	}
	return infra.DaemonService(runtime.GOOS, home, exe, scope)
}

// daemonServiceInstalled informa si la unidad del daemon ya existe.
func daemonServiceInstalled(e *Env) bool {
	spec, err := daemonServiceSpec(false)
	if err != nil {
		return false
	}
	return infra.ServiceInstalled(spec)
}

func daemonEnsureHuman(rep infra.Report) string {
	out := rep.Message
	if rep.Ready {
		return out
	}
	for _, m := range rep.Manual {
		out += "\n  manual: " + m
	}
	return out
}

func firstHint(rep infra.Report) string {
	if rep.Hint != "" {
		return rep.Hint
	}
	if len(rep.Manual) > 0 {
		return rep.Manual[0]
	}
	return "revisa el estado con 'badomens daemon status'"
}
