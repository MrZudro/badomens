package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/MrZudro/badomens/internal/execx"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

// idleServiceName es el identificador del servicio de vigilante por SO.
const idleServiceName = "badomens-idle"

// idleServicePath devuelve la ruta del archivo de servicio/unidad según el SO.
func idleServicePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "linux":
		return filepath.Join(home, ".config", "systemd", "user", idleServiceName+".service"), nil
	case "darwin":
		return filepath.Join(home, "Library", "LaunchAgents", "com."+idleServiceName+".plist"), nil
	default:
		// En Windows el "servicio" es una tarea programada; no hay archivo.
		return "", nil
	}
}

// idleServiceUnit genera el contenido de la unidad/plist para el SO actual.
func idleServiceUnit(exe string) (string, error) {
	switch runtime.GOOS {
	case "linux":
		return fmt.Sprintf(`[Unit]
Description=badomens — vigilante de auto-apagado por inactividad
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s idle daemon
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
`, exe), nil
	case "darwin":
		return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.%s</string>
  <key>ProgramArguments</key>
  <array><string>%s</string><string>idle</string><string>daemon</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict>
</plist>
`, idleServiceName, exe), nil
	default:
		return "", nil
	}
}

func newIdleInstallServiceCmd(e *Env) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "install-service",
		Short: "Instala el vigilante de auto-apagado como servicio del sistema",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			exe, err := os.Executable()
			if err != nil {
				return e.finish(result.FromError("idle install-service", err), "")
			}
			path, err := idleServicePath()
			if err != nil {
				return e.finish(result.FromError("idle install-service", err), "")
			}
			unit, _ := idleServiceUnit(exe)
			data := map[string]any{"os": runtime.GOOS, "exe": exe, "path": path, "unit": unit, "dry_run": dryRun}
			if dryRun {
				return e.finish(result.OK("idle install-service", "simulación: no se escribió nada", data), renderServicePlan(path, unit))
			}
			switch runtime.GOOS {
			case "linux", "darwin":
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					return e.finish(result.FromError("idle install-service", err), "")
				}
				if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
					return e.finish(result.FromError("idle install-service", err), "")
				}
				_ = enableService(cmd, path)
			default:
				// Windows: tarea programada al iniciar sesión.
				_, werr := execx.Run(cmd.Context(), "schtasks", "/Create", "/SC", "ONLOGON",
					"/TN", idleServiceName, "/TR", fmt.Sprintf("%q idle daemon", exe), "/F")
				if werr != nil {
					return e.finish(result.FromError("idle install-service", werr), "")
				}
			}
			msg := "vigilante instalado como servicio (" + runtime.GOOS + ")"
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
			path, err := idleServicePath()
			if err != nil {
				return e.finish(result.FromError("idle uninstall-service", err), "")
			}
			switch runtime.GOOS {
			case "linux", "darwin":
				_ = disableService(cmd, path)
				if path != "" {
					if rerr := os.Remove(path); rerr != nil && !os.IsNotExist(rerr) {
						return e.finish(result.FromError("idle uninstall-service", rerr), "")
					}
				}
			default:
				_, _ = execx.Run(cmd.Context(), "schtasks", "/Delete", "/TN", idleServiceName, "/F")
			}
			return e.finish(result.OK("idle uninstall-service", "servicio del vigilante eliminado", map[string]any{"path": path}), "servicio eliminado")
		},
	}
}

func enableService(cmd *cobra.Command, path string) error {
	if runtime.GOOS == "darwin" {
		_, err := execx.Run(cmd.Context(), "launchctl", "load", "-w", path)
		return err
	}
	_, _ = execx.Run(cmd.Context(), "systemctl", "--user", "daemon-reload")
	_, err := execx.Run(cmd.Context(), "systemctl", "--user", "enable", "--now", idleServiceName+".service")
	return err
}

func disableService(cmd *cobra.Command, path string) error {
	if runtime.GOOS == "darwin" {
		_, err := execx.Run(cmd.Context(), "launchctl", "unload", "-w", path)
		return err
	}
	_, err := execx.Run(cmd.Context(), "systemctl", "--user", "disable", "--now", idleServiceName+".service")
	return err
}

// renderServicePlan devuelve el texto humano del plan (para --dry-run).
func renderServicePlan(path, unit string) string {
	if path == "" {
		return "Windows: se creará la tarea programada «" + idleServiceName + "» al iniciar sesión."
	}
	return "Se escribirá " + path + ":\n\n" + unit
}
