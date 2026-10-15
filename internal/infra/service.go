package infra

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ServiceName es el identificador del servicio del daemon de badomens.
const ServiceName = "badomens-idle"

// ServiceScope indica si la unidad es del usuario o de todo el sistema.
type ServiceScope string

// Ámbitos posibles.
const (
	// ScopeUser: unidad del usuario (systemd --user / LaunchAgent). Preferido.
	ScopeUser ServiceScope = "user"
	// ScopeSystem: unidad del sistema (systemd de sistema / LaunchDaemon).
	ScopeSystem ServiceScope = "system"
)

// ServiceKind es el mecanismo por SO.
type ServiceKind string

// Mecanismos posibles.
const (
	KindSystemd ServiceKind = "systemd"
	KindLaunchd ServiceKind = "launchd"
	KindWinTask ServiceKind = "windows-task"
)

// ServiceSpec describe el servicio persistente del daemon por SO.
type ServiceSpec struct {
	OS    string       `json:"os"`
	Scope ServiceScope `json:"scope"`
	Kind  ServiceKind  `json:"kind"`
	// Path es la ruta del archivo de unidad (systemd/launchd); vacío en Windows.
	Path string `json:"path,omitempty"`
	// Unit es el contenido del archivo de unidad (systemd/launchd).
	Unit string `json:"unit,omitempty"`
	// Exe es la ruta del binario badomens.
	Exe string `json:"exe,omitempty"`
	// DaemonArgs es el argv del daemon (exe idle daemon).
	DaemonArgs []string `json:"daemon_args,omitempty"`
	// InstallArgs son los argumentos del instalador en Windows (schtasks).
	InstallArgs []string `json:"install_args,omitempty"`
	// RequiresElevation indica que instalar requiere privilegios.
	RequiresElevation bool `json:"requires_elevation,omitempty"`
}

// DaemonArgv devuelve el argv para arrancar el daemon del vigilante.
func DaemonArgv(exe string) []string { return []string{exe, "idle", "daemon"} }

// DaemonService construye la unidad/plist/tarea del daemon para el SO y el
// ámbito pedidos. No instala nada: solo describe.
func DaemonService(goos, home, exe string, scope ServiceScope) (ServiceSpec, error) {
	spec := ServiceSpec{OS: goos, Scope: scope, Exe: exe, DaemonArgs: DaemonArgv(exe)}
	switch goos {
	case "linux":
		spec.Kind = KindSystemd
		if scope == ScopeSystem {
			spec.Path = "/etc/systemd/system/" + ServiceName + ".service"
			spec.RequiresElevation = true
		} else {
			if home == "" {
				return spec, fmt.Errorf("no se pudo resolver el directorio del usuario")
			}
			spec.Path = filepath.Join(home, ".config", "systemd", "user", ServiceName+".service")
		}
		spec.Unit = systemdUnit(exe, scope)
	case "darwin":
		spec.Kind = KindLaunchd
		if scope == ScopeSystem {
			spec.Path = "/Library/LaunchDaemons/com." + ServiceName + ".plist"
			spec.RequiresElevation = true
		} else {
			if home == "" {
				return spec, fmt.Errorf("no se pudo resolver el directorio del usuario")
			}
			spec.Path = filepath.Join(home, "Library", "LaunchAgents", "com."+ServiceName+".plist")
		}
		spec.Unit = launchdUnit(exe)
	case "windows":
		spec.Kind = KindWinTask
		cmd := windowsCommandLine(DaemonArgv(exe)...)
		spec.InstallArgs = []string{"/Create", "/SC", "ONLOGON", "/TN", ServiceName, "/TR", cmd, "/F"}
	default:
		return spec, fmt.Errorf("SO no soportado: %s", goos)
	}
	return spec, nil
}

// systemdUnit genera la unidad systemd del daemon. El binario se cita para
// admitir rutas con espacios; los argumentos siguen separados.
func systemdUnit(exe string, scope ServiceScope) string {
	wanted := "default.target"
	if scope == ScopeSystem {
		wanted = "multi-user.target"
	}
	return fmt.Sprintf(`[Unit]
Description=badomens — daemon de auto-apagado por inactividad
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s idle daemon
Restart=on-failure
RestartSec=10

[Install]
WantedBy=%s
`, quoteSystemd(exe), wanted)
}

// launchdUnit genera el LaunchAgent/LaunchDaemon del daemon. Cada argumento va
// en su propio <string> del array ProgramArguments (sin concatenación).
func launchdUnit(exe string) string {
	var args strings.Builder
	for _, a := range DaemonArgv(exe) {
		args.WriteString("    <string>")
		args.WriteString(xmlEscape(a))
		args.WriteString("</string>\n")
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.%s</string>
  <key>ProgramArguments</key>
  <array>
%s  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict>
</plist>
`, ServiceName, args.String())
}

// InstallDaemonService escribe la unidad y la habilita (systemd/launchd), o crea
// la tarea programada (Windows). Todos los procesos van con argumentos separados.
func InstallDaemonService(ctx context.Context, env Env, spec ServiceSpec) error {
	env = env.withDefaults()
	switch spec.Kind {
	case KindSystemd:
		if err := writeServiceFile(ctx, env, spec); err != nil {
			return err
		}
		if _, err := env.run(ctx, "systemctl", systemctlArgs(spec.Scope, "daemon-reload")...); err != nil {
			return err
		}
		_, err := env.run(ctx, "systemctl", systemctlArgs(spec.Scope, "enable", "--now", ServiceName+".service")...)
		return err
	case KindLaunchd:
		if err := writeServiceFile(ctx, env, spec); err != nil {
			return err
		}
		_, err := env.run(ctx, "launchctl", "load", "-w", spec.Path)
		return err
	case KindWinTask:
		_, err := env.run(ctx, "schtasks", spec.InstallArgs...)
		return err
	}
	return fmt.Errorf("tipo de servicio no soportado: %s", spec.Kind)
}

// UninstallDaemonService deshabilita y elimina el servicio del daemon.
func UninstallDaemonService(ctx context.Context, env Env, spec ServiceSpec) error {
	env = env.withDefaults()
	switch spec.Kind {
	case KindSystemd:
		_, _ = env.run(ctx, "systemctl", systemctlArgs(spec.Scope, "disable", "--now", ServiceName+".service")...)
		if err := removeServiceFile(ctx, env, spec); err != nil {
			return err
		}
		_, err := env.run(ctx, "systemctl", systemctlArgs(spec.Scope, "daemon-reload")...)
		return err
	case KindLaunchd:
		_, _ = env.run(ctx, "launchctl", "unload", "-w", spec.Path)
		return removeServiceFile(ctx, env, spec)
	case KindWinTask:
		_, err := env.run(ctx, "schtasks", "/Delete", "/TN", ServiceName, "/F")
		return err
	}
	return fmt.Errorf("tipo de servicio no soportado: %s", spec.Kind)
}

// StartDaemonServiceArgs devuelve el argv que arranca el servicio ya instalado.
func StartDaemonServiceArgs(spec ServiceSpec) []string {
	switch spec.Kind {
	case KindSystemd:
		args := []string{"systemctl"}
		args = append(args, systemctlArgs(spec.Scope, "start", ServiceName+".service")...)
		return args
	case KindLaunchd:
		if spec.Scope == ScopeSystem {
			return []string{"sudo", "-n", "launchctl", "start", "com." + ServiceName}
		}
		return []string{"launchctl", "start", "com." + ServiceName}
	case KindWinTask:
		return []string{"schtasks", "/Run", "/TN", ServiceName}
	}
	return nil
}

// ServiceInstalled indica si la unidad del servicio ya existe.
func ServiceInstalled(spec ServiceSpec) bool {
	if spec.Kind == KindWinTask {
		// En Windows la tarea se consulta con schtasks; sin ejecutar procesos se
		// asume instalada si el llamante lo indica (ver DaemonOptions.Installed).
		return false
	}
	if spec.Path == "" {
		return false
	}
	_, err := os.Stat(spec.Path)
	return err == nil
}

// writeServiceFile escribe la unidad. Para unidades de sistema usa un archivo
// temporal y `sudo -n install`, sin concatenar shell.
func writeServiceFile(ctx context.Context, env Env, spec ServiceSpec) error {
	if spec.Kind == KindWinTask {
		return nil
	}
	if spec.Path == "" {
		return fmt.Errorf("ruta de unidad vacía")
	}
	if spec.Scope == ScopeUser {
		if err := os.MkdirAll(filepath.Dir(spec.Path), 0o700); err != nil {
			return err
		}
		return os.WriteFile(spec.Path, []byte(spec.Unit), 0o644)
	}
	tmp, err := os.CreateTemp("", "badomens-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, werr := tmp.WriteString(spec.Unit); werr != nil {
		_ = tmp.Close()
		return werr
	}
	if cerr := tmp.Close(); cerr != nil {
		return cerr
	}
	_, err = env.run(ctx, "sudo", "-n", "install", "-m", "0644", tmp.Name(), spec.Path)
	return err
}

// removeServiceFile elimina la unidad (con privilegios si es de sistema).
func removeServiceFile(ctx context.Context, env Env, spec ServiceSpec) error {
	if spec.Path == "" {
		return nil
	}
	if spec.Scope == ScopeUser {
		if err := os.Remove(spec.Path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	_, err := env.run(ctx, "sudo", "-n", "rm", "-f", spec.Path)
	return err
}

// systemctlArgs antepone --user para unidades de usuario.
func systemctlArgs(scope ServiceScope, args ...string) []string {
	if scope == ScopeUser {
		return append([]string{"--user"}, args...)
	}
	return args
}

// quoteSystemd cita la ruta del binario si contiene espacios.
func quoteSystemd(s string) string {
	if !strings.ContainsAny(s, " \t\"") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// xmlEscape escapa texto para el plist de launchd.
func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
}

// windowsCommandLine construye una línea de comandos de Windows según las reglas
// de CommandLineToArgvW. No es una shell: es el formato que exige schtasks en su
// argumento /TR, y cada elemento se cita por separado.
func windowsCommandLine(argv ...string) string {
	parts := make([]string, 0, len(argv))
	for _, a := range argv {
		parts = append(parts, quoteWindows(a))
	}
	return strings.Join(parts, " ")
}

// quoteWindows cita un argumento según las reglas de CommandLineToArgvW.
func quoteWindows(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0
	for _, r := range s {
		switch r {
		case '\\':
			backslashes++
			b.WriteRune(r)
		case '"':
			b.WriteString(strings.Repeat(`\`, backslashes+1))
			b.WriteRune('"')
			backslashes = 0
		default:
			backslashes = 0
			b.WriteRune(r)
		}
	}
	// Las barras invertidas finales se duplican para no escapar la comilla final.
	b.WriteString(strings.Repeat(`\`, backslashes))
	b.WriteByte('"')
	return b.String()
}
