package svc

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/infra"
	"github.com/MrZudro/badomens/internal/tailscale"
)

// Infra comprueba y levanta los servicios externos (daemon, Docker y Tailscale)
// con el ÚNICO mecanismo común de internal/infra (Fase 8): detectar → levantar →
// elevar explicando el motivo → informar. No crea mecanismos paralelos.
type Infra interface {
	// Daemon comprueba y fuerza el arranque del daemon propio de badomens.
	Daemon(ctx context.Context, allowElevation bool, progress func(infra.Progress)) infra.Report
	// Docker comprueba y levanta Docker si está apagado.
	Docker(ctx context.Context, allowElevation bool, progress func(infra.Progress)) infra.Report
	// Tailscale comprueba y levanta Tailscale si está apagado.
	Tailscale(ctx context.Context, allowElevation bool, progress func(infra.Progress)) infra.Report
	// DaemonInstalled indica si el daemon ya está instalado como servicio persistente.
	DaemonInstalled() bool
	// InstallDaemonService instala el daemon como servicio persistente por SO.
	InstallDaemonService(ctx context.Context, allowElevation bool, progress func(infra.Progress)) infra.Report
}

// realInfra adapta internal/infra sobre la configuración real.
type realInfra struct{ cfg *config.Config }

func newRealInfra(cfg *config.Config) *realInfra { return &realInfra{cfg: cfg} }

// daemonComponent construye el componente del daemon con su estado real: mismo
// camino que «badomens daemon status», sin un segundo mecanismo.
func (r *realInfra) daemonComponent() infra.Daemon {
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	lock := ""
	if r.cfg != nil {
		lock = infra.DaemonLockPath(r.cfg.DataDir)
	}
	home, _ := os.UserHomeDir()
	opts := infra.DaemonOptions{Exe: exe, LockPath: lock}
	if spec, serr := infra.DaemonService(runtime.GOOS, home, exe, infra.ScopeUser); serr == nil {
		opts.Service = &spec
	}
	return infra.NewDaemon(opts)
}

// daemonSpec describe el servicio persistente del daemon (ámbito de usuario).
func (r *realInfra) daemonSpec() (infra.ServiceSpec, error) {
	exe, err := os.Executable()
	if err != nil {
		return infra.ServiceSpec{}, err
	}
	home, _ := os.UserHomeDir()
	return infra.DaemonService(runtime.GOOS, home, exe, infra.ScopeUser)
}

func (r *realInfra) Daemon(ctx context.Context, allowElevation bool, progress func(infra.Progress)) infra.Report {
	opts := infra.Options{Timeout: 30 * time.Second, AllowElevation: allowElevation, Progress: progress}
	return infra.Ensure(ctx, infra.DefaultEnv(), r.daemonComponent(), opts)
}

func (r *realInfra) Docker(ctx context.Context, allowElevation bool, progress func(infra.Progress)) infra.Report {
	opts := infra.Options{Timeout: 90 * time.Second, AllowElevation: allowElevation, Progress: progress}
	return infra.Ensure(ctx, infra.DefaultEnv(), infra.NewDocker(dockerx.Env{}), opts)
}

func (r *realInfra) Tailscale(ctx context.Context, allowElevation bool, progress func(infra.Progress)) infra.Report {
	opts := infra.Options{Timeout: 60 * time.Second, AllowElevation: allowElevation, Progress: progress}
	return infra.Ensure(ctx, infra.DefaultEnv(), infra.NewTailscale(tailscale.Env{}), opts)
}

// DaemonInstalled informa si la unidad del daemon ya existe.
func (r *realInfra) DaemonInstalled() bool {
	spec, err := r.daemonSpec()
	if err != nil {
		return false
	}
	// En Windows no hay archivo de unidad (la tarea se consulta con schtasks):
	// infra.ServiceInstalled devuelve false sin ejecutar procesos.
	return infra.ServiceInstalled(spec)
}

// InstallDaemonService instala el daemon como servicio persistente (systemd
// --user / LaunchAgent / tarea programada). Nunca instala en silencio: el
// llamante confirma antes, y la elevación se autoriza explícitamente.
func (r *realInfra) InstallDaemonService(ctx context.Context, allowElevation bool, progress func(infra.Progress)) infra.Report {
	rep := infra.Report{Domain: infra.DomainDaemon}
	spec, err := r.daemonSpec()
	if err != nil {
		rep.Status = infra.StatusUnavailable
		rep.Message = "no se pudo describir el servicio del daemon: " + err.Error()
		return rep
	}
	if progress != nil {
		progress(infra.Progress{Domain: infra.DomainDaemon, Phase: "install", Status: infra.StatusStarting, Message: "Instalando el servicio del daemon…"})
	}
	if spec.RequiresElevation && !allowElevation {
		rep.Status = infra.StatusNeedsElevation
		rep.NeedsElevation = true
		rep.Message = "instalar el servicio requiere permisos de administrador"
		rep.Hint = "autoriza la elevación para instalar el servicio persistente"
		return rep
	}
	if ierr := infra.InstallDaemonService(ctx, infra.DefaultEnv(), spec); ierr != nil {
		rep.Status = infra.StatusUnavailable
		rep.Message = "no se pudo instalar el servicio: " + ierr.Error()
		rep.Manual = []string{"instálalo manualmente con 'badomens daemon install-service'"}
		return rep
	}
	rep.Status = infra.StatusReady
	rep.Ready = true
	rep.Message = "daemon instalado como servicio (" + spec.OS + ", " + string(spec.Scope) + ")"
	return rep
}
