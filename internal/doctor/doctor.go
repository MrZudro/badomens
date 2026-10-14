// Package doctor ejecuta el diagnóstico del entorno: dependencias, red privada
// y coherencia de la configuración. No modifica nada.
package doctor

import (
	"context"
	"strings"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/execx"
	"github.com/MrZudro/badomens/internal/host"
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

func checkTailscale(ctx context.Context, cfg *config.Config, add func(name, status, message string)) {
	if !cfg.Tailscale.Enabled {
		add("Tailscale", StatusOK, "desactivado en la configuración")
		return
	}
	if !execx.LookPath("tailscale") {
		add("Tailscale", StatusWarn, "no se encontró 'tailscale'; las instancias no podrán publicarse en el tailnet")
	} else if ip := tailscaleIPv4(ctx); ip != "" {
		add("Tailscale IP", StatusOK, ip)
	} else {
		add("Tailscale IP", StatusWarn, "no se detectó una IPv4 del tailnet; ejecuta 'tailscale up'")
	}
}

func tailscaleIPv4(ctx context.Context) string {
	res, err := execx.Run(ctx, "tailscale", "ip", "-4")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "100.") {
			return line
		}
	}
	return strings.TrimSpace(res.Stdout)
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
