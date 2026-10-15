// Package doctor ejecuta el diagnóstico del entorno: dependencias, red privada
// y coherencia de la configuración. No modifica nada.
package doctor

import (
	"context"
	"os"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/host"
	"github.com/MrZudro/badomens/internal/infra"
	"github.com/MrZudro/badomens/internal/tailscale"
)

// Estados posibles de un chequeo.
const (
	StatusOK    = "ok"
	StatusWarn  = "warn"
	StatusError = "error"
)

// Check es el resultado de una comprobación.
type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Report es el resultado completo del diagnóstico.
type Report struct {
	Checks []Check `json:"checks"`
	OK     int     `json:"ok"`
	Warns  int     `json:"warns"`
	Errors int     `json:"errors"`
}

// OK indica si no hay errores (puede haber avisos).
func (r Report) Healthy() bool { return r.Errors == 0 }

// Run ejecuta el diagnóstico.
func Run(ctx context.Context, cfg *config.Config) Report {
	var r Report
	add := r.adder()

	checkConfigDir(add)
	checkDataDir(cfg, add)
	checkDocker(ctx, add)
	checkTailscale(ctx, cfg, add)
	checkDaemon(cfg, add)

	// Capacidad del host.
	cap := host.Detect(ctx)
	memGiB := cap.MemTotalGiB()
	if cap.MemTotalBytes > 0 {
		add("Host capacity", StatusOK, humanCapacity(cap.NCPU, memGiB, cap.Source))
	} else {
		add("Host capacity", StatusWarn, "no se pudo leer la RAM total desde Docker (solo CPU disponible)")
	}

	// Coherencia de recursos: RAM reservada vs RAM total.
	if cap.MemTotalBytes > 0 {
		perInstance := host.ParseMemory(cfg.DefaultMemory)
		reserved := perInstance * uint64(cfg.MaxParallel)
		if reserved > 0 && reserved > uint64(float64(cap.MemTotalBytes)*0.8) {
			add("Resource plan", StatusWarn, "la memoria reservada (max_parallel × default_memory) supera el 80% de la RAM del host")
		} else {
			add("Resource plan", StatusOK, "recursos coherentes con la capacidad del host")
		}
	}

	return r
}

// Preflight ejecuta las comprobaciones mínimas para operar (directorios,
// Docker/Compose y red privada) sin medir la capacidad ni planificar recursos.
// Es lo que la TUI usa para avisar de sus requisitos al abrirse.
func Preflight(ctx context.Context, cfg *config.Config) Report {
	var r Report
	add := r.adder()
	checkConfigDir(add)
	checkDataDir(cfg, add)
	checkDocker(ctx, add)
	checkTailscale(ctx, cfg, add)
	checkDaemon(cfg, add)
	return r
}

// adder devuelve la función que registra un chequeo en el reporte y actualiza
// sus contadores.
func (r *Report) adder() func(name, status, message string) {
	return func(name, status, message string) {
		r.Checks = append(r.Checks, Check{Name: name, Status: status, Message: message})
		switch status {
		case StatusOK:
			r.OK++
		case StatusWarn:
			r.Warns++
		case StatusError:
			r.Errors++
		}
	}
}

func checkConfigDir(add func(name, status, message string)) {
	if dir, err := config.Dir(); err != nil {
		add("Config dir", StatusError, "no se pudo resolver el directorio de configuración: "+err.Error())
	} else {
		add("Config dir", StatusOK, dir)
	}
}

func checkDataDir(cfg *config.Config, add func(name, status, message string)) {
	if dir, err := cfg.EnsureDataDir(); err != nil {
		add("Data dir", StatusError, "no se puede escribir en "+cfg.DataDir+": "+err.Error())
	} else {
		add("Data dir", StatusOK, dir)
	}
}

func checkDocker(ctx context.Context, add func(name, status, message string)) {
	diag := dockerx.Diagnose(ctx, dockerx.DefaultEnv())
	switch diag.State {
	case dockerx.StateOK:
		add("Docker CLI", StatusOK, "docker presente")
		add("Docker daemon", StatusOK, "server "+diag.ServerVersion)
		add("Docker Compose", StatusOK, diag.ComposeVersion)
	case dockerx.StateNotInstalled:
		add("Docker CLI", StatusError, diag.Message+" (https://docs.docker.com/get-docker/)")
	case dockerx.StateComposeMissing:
		add("Docker CLI", StatusOK, "docker presente")
		if diag.ServerVersion != "" {
			add("Docker daemon", StatusOK, "server "+diag.ServerVersion)
		}
		add("Docker Compose", StatusWarn, "no se pudo ejecutar 'docker compose version'")
	default:
		add("Docker CLI", StatusOK, "docker presente")
		msg := diag.Message
		if diag.Reason != "" {
			msg += " [" + diag.Reason + "]"
		}
		add("Docker daemon", StatusError, msg)
	}
}

// checkTailscale informa del estado del tailnet y de MagicDNS con motivo tipado
// (Fase 8, sección 5): la causa queda registrada para que Salud pueda mostrarla
// o descartarla en lugar de un banner permanente.
func checkTailscale(ctx context.Context, cfg *config.Config, add func(name, status, message string)) {
	if !cfg.Tailscale.Enabled {
		add("Tailscale", StatusOK, "desactivado en la configuración")
		return
	}
	rep := tailscale.Probe(ctx)
	switch rep.State {
	case tailscale.StateNotInstalled:
		add("Tailscale", StatusWarn, "no se encontró 'tailscale'; las instancias no podrán publicarse en el tailnet")
		return
	case tailscale.StateNotRunning, tailscale.StateStopped:
		add("Tailscale", StatusWarn, rep.Message+" ("+rep.Hint+")")
		return
	case tailscale.StateNeedsLogin, tailscale.StateNeedsMachineAuth:
		add("Tailscale", StatusWarn, rep.Message+" ("+rep.Hint+")")
		return
	}
	if rep.IP != "" {
		add("Tailscale IP", StatusOK, rep.IP)
	} else {
		add("Tailscale IP", StatusWarn, "no se detectó una IPv4 del tailnet; ejecuta 'tailscale up'")
	}
	add("MagicDNS", magicDNSStatus(rep), magicDNSMessage(rep))
}

// magicDNSStatus traduce el motivo tipado de MagicDNS a un nivel de chequeo.
func magicDNSStatus(rep tailscale.Report) string {
	switch rep.MagicDNSReason {
	case tailscale.ReasonMagicDNSOK:
		return StatusOK
	case tailscale.ReasonMagicDNSDisabled:
		return StatusWarn
	}
	return StatusWarn
}

func magicDNSMessage(rep tailscale.Report) string {
	switch rep.MagicDNSReason {
	case tailscale.ReasonMagicDNSOK:
		return rep.MagicDNS
	case tailscale.ReasonMagicDNSDisabled:
		return "MagicDNS está desactivado en el tailnet: sin nombre de conexión, comparte la dirección IP (motivo: magicdns_disabled)"
	}
	return "no se pudo determinar MagicDNS (motivo: " + rep.MagicDNSReason + ")"
}

// checkDaemon informa del estado del daemon propio de badomens.
func checkDaemon(cfg *config.Config, add func(name, status, message string)) {
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	lock := ""
	if cfg != nil {
		lock = infra.DaemonLockPath(cfg.DataDir)
	}
	st := infra.NewDaemon(infra.DaemonOptions{Exe: exe, LockPath: lock}).Detect(context.Background())
	switch st.Status {
	case infra.StatusReady:
		add("Daemon", StatusOK, st.Message)
	default:
		msg := st.Message
		if st.Hint != "" {
			msg += " — " + st.Hint
		}
		add("Daemon", StatusWarn, msg)
	}
}

func humanCapacity(ncpu int, memGiB float64, source string) string {
	return itoa(ncpu) + " CPU · " + trimFloat(memGiB) + " GiB RAM (" + source + ")"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func trimFloat(f float64) string {
	// Una decimal es suficiente para la capacidad del host.
	whole := int64(f)
	frac := int64((f - float64(whole)) * 10)
	if frac == 0 {
		return itoa(int(whole))
	}
	return itoa(int(whole)) + "." + itoa(int(frac))
}
