package svc

import (
	"context"
	"sync"

	"github.com/MrZudro/badomens/internal/infra"
)

// fakeInfra simula el mecanismo común de infraestructura sin tocar el sistema.
// Por defecto todo está «listo» para que los tests de TUI no levanten nada.
type fakeInfra struct {
	mu sync.Mutex
	// DaemonDown hace que el daemon no esté en marcha (para probar el arranque).
	DaemonDown bool
	// Installed indica si el servicio persistente está instalado.
	Installed bool
	// InstallFails hace que instalar el servicio falle.
	InstallFails bool
	calls        []string
}

func (f *fakeInfra) record(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
}

// Calls devuelve las llamadas registradas (para los tests).
func (f *fakeInfra) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

func readyReport(d infra.Domain) infra.Report {
	return infra.Report{Domain: d, Status: infra.StatusReady, Ready: true, Message: string(d) + " listo"}
}

func (f *fakeInfra) Daemon(_ context.Context, _ bool, _ func(infra.Progress)) infra.Report {
	f.record("daemon")
	if f.DaemonDown {
		return infra.Report{
			Domain: infra.DomainDaemon, Status: infra.StatusDown,
			Message: "El daemon de badomens no está en marcha.",
			Hint:    "badomens lo levantará como daemon de sesión",
			Manual:  []string{"Ejecuta 'badomens idle daemon' en una terminal."},
			Phases:  []infra.Phase{{Name: "detect", Status: string(infra.StatusDown)}},
		}
	}
	return readyReport(infra.DomainDaemon)
}

func (f *fakeInfra) Docker(_ context.Context, _ bool, _ func(infra.Progress)) infra.Report {
	f.record("docker")
	return readyReport(infra.DomainDocker)
}

func (f *fakeInfra) Tailscale(_ context.Context, _ bool, _ func(infra.Progress)) infra.Report {
	f.record("tailscale")
	return readyReport(infra.DomainTailscale)
}

func (f *fakeInfra) DaemonInstalled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Installed
}

func (f *fakeInfra) InstallDaemonService(_ context.Context, _ bool, _ func(infra.Progress)) infra.Report {
	f.record("install-service")
	if f.InstallFails {
		return infra.Report{
			Domain: infra.DomainDaemon, Status: infra.StatusNeedsElevation,
			NeedsElevation: true, Degraded: true,
			Message: "no se pudo instalar el servicio: elevación denegada",
		}
	}
	f.mu.Lock()
	f.Installed = true
	f.mu.Unlock()
	r := readyReport(infra.DomainDaemon)
	r.Message = "daemon instalado como servicio"
	return r
}
