package infra

import (
	"context"
	"path/filepath"

	"github.com/MrZudro/badomens/internal/idle"
)

// DaemonOptions describe el daemon propio de badomens (el vigilante de
// auto-apagado, `badomens idle daemon`).
type DaemonOptions struct {
	// Exe es la ruta del binario badomens. Vacío si no se pudo resolver.
	Exe string
	// LockPath es el pidfile con latido del vigilante (data dir/idle.lock).
	LockPath string
	// Service describe el servicio persistente conocido (si está instalado).
	// Su Path se usa para saber si la unidad existe.
	Service *ServiceSpec
	// Installed permite inyectar la comprobación de instalación del servicio
	// (necesario en Windows, donde no hay archivo de unidad).
	Installed func() bool
}

// Daemon adapta el daemon propio de badomens al mecanismo común.
type Daemon struct {
	Opts DaemonOptions
}

// NewDaemon construye el componente del daemon.
func NewDaemon(opts DaemonOptions) Daemon { return Daemon{Opts: opts} }

// Domain identifica el componente.
func (Daemon) Domain() Domain { return DomainDaemon }

// Detect comprueba si el vigilante está activo (latido reciente) y, si no, si
// está instalado como servicio. No ejecuta procesos.
func (d Daemon) Detect(_ context.Context) State {
	st := State{Domain: DomainDaemon}
	if d.Opts.Exe == "" {
		st.Status = StatusNotInstalled
		st.Reason = "daemon_binary_missing"
		st.Message = "No se encontró el binario de badomens."
		st.Hint = "ejecuta badomens desde su ruta completa o reinstálalo"
		return st
	}
	if d.Opts.LockPath != "" && idle.WatcherActive(d.Opts.LockPath) {
		st.Status = StatusReady
		st.Reason = "daemon_running"
		st.Message = "El daemon de badomens está en marcha."
		return st
	}
	if d.installed() {
		st.Status = StatusDown
		st.Reason = "daemon_service_stopped"
		st.Message = "El daemon de badomens está instalado como servicio pero no corre."
		st.Hint = "badomens intentará arrancarlo con el servicio del sistema"
		return st
	}
	st.Status = StatusDown
	st.Reason = "daemon_stopped"
	st.Message = "El daemon de badomens no está en marcha."
	st.Hint = "badomens lo levantará como daemon de sesión"
	return st
}

// StartPlan arranca el servicio instalado si existe; si no, levanta un daemon de
// sesión desacoplado.
func (d Daemon) StartPlan(_ State, _ string) []Step {
	if d.installed() && d.Opts.Service != nil {
		return []Step{{
			Description:       "Arrancando el servicio del daemon…",
			Command:           StartDaemonServiceArgs(*d.Opts.Service),
			RequiresElevation: d.Opts.Service.RequiresElevation,
		}}
	}
	if d.Opts.Exe == "" {
		return nil
	}
	return []Step{{
		Description: "Levantando el daemon de badomens (sesión)…",
		Command:     DaemonArgv(d.Opts.Exe),
		Detached:    true,
	}}
}

// InstallHint es la instrucción si falta el binario.
func (Daemon) InstallHint(string) string {
	return "no se encontró el binario badomens; instálalo o ejecútalo desde su ruta completa"
}

// ManualGuide devuelve los pasos manuales por SO.
func (Daemon) ManualGuide(_ State, platform string) []string {
	switch platform {
	case "windows":
		return []string{"Ejecuta 'badomens idle daemon' en una terminal (o crea la tarea programada con 'badomens idle install-service')."}
	case "darwin":
		return []string{"Ejecuta 'badomens idle daemon' en una terminal, o instálalo con 'badomens idle install-service'."}
	}
	return []string{"Ejecuta 'badomens idle daemon' en una terminal, o instálalo con 'badomens idle install-service'."}
}

// installed indica si el servicio persistente está instalado.
func (d Daemon) installed() bool {
	if d.Opts.Installed != nil {
		return d.Opts.Installed()
	}
	if d.Opts.Service == nil {
		return false
	}
	return ServiceInstalled(*d.Opts.Service)
}

// DaemonLockPath devuelve la ruta del pidfile del vigilante dentro del data dir.
func DaemonLockPath(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "idle.lock")
}
