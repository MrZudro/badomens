package cli

import (
	"os"
	"runtime"
	"strings"

	"github.com/MrZudro/badomens/internal/infra"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

// idleServiceSpec describe el servicio persistente del vigilante (ámbito de
// usuario) para el SO actual. La generación de la unidad y la instalación viven
// en internal/infra y se reutilizan aquí y en el comando `daemon`.
func idleServiceSpec() (infra.ServiceSpec, error) {
	exe, err := os.Executable()
	if err != nil {
		return infra.ServiceSpec{}, err
	}
	home, _ := os.UserHomeDir()
	return infra.DaemonService(runtime.GOOS, home, exe, infra.ScopeUser)
}

func newIdleInstallServiceCmd(e *Env) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "install-service",
		Short: "Instala el vigilante de auto-apagado como servicio del sistema",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := idleServiceSpec()
			if err != nil {
				return e.finish(result.FromError("idle install-service", err), "")
			}
			data := map[string]any{
				"os": spec.OS, "exe": spec.Exe, "path": spec.Path,
				"unit": spec.Unit, "scope": string(spec.Scope), "dry_run": dryRun,
			}
			if dryRun {
				return e.finish(result.OK("idle install-service", "simulación: no se escribió nada", data), renderServicePlan(spec))
			}
			if err := infra.InstallDaemonService(cmd.Context(), infra.DefaultEnv(), spec); err != nil {
				return e.finish(result.FromError("idle install-service", err), "")
			}
			msg := "vigilante instalado como servicio (" + spec.OS + ")"
			return e.finish(result.OK("idle install-service", msg, data), msg)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "muestra lo que se instalaría sin escribir nada")
	return cmd
}

func newIdleUninstallServiceCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall-service",
		Short: "Quita el vigilante de auto-apagado del sistema",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := idleServiceSpec()
			if err != nil {
				return e.finish(result.FromError("idle uninstall-service", err), "")
			}
			if err := infra.UninstallDaemonService(cmd.Context(), infra.DefaultEnv(), spec); err != nil {
				return e.finish(result.FromError("idle uninstall-service", err), "")
			}
			return e.finish(result.OK("idle uninstall-service", "servicio del vigilante eliminado",
				map[string]any{"path": spec.Path}), "servicio eliminado")
		},
	}
}

// renderServicePlan devuelve el texto humano del plan (para --dry-run).
func renderServicePlan(spec infra.ServiceSpec) string {
	if spec.Path == "" {
		return "Windows: se creará la tarea programada «" + infra.ServiceName + "» al iniciar sesión:\n  schtasks " +
			strings.Join(spec.InstallArgs, " ")
	}
	return "Se escribirá " + spec.Path + ":\n\n" + spec.Unit
}
