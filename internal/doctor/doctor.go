// Package doctor ejecuta el diagnóstico del entorno: dependencias, red privada
// y coherencia de la configuración. No modifica nada.
package doctor

import (
	"context"
	"strings"

	"github.com/manuelserver/badomens/internal/config"
	"github.com/manuelserver/badomens/internal/execx"
	"github.com/manuelserver/badomens/internal/host"
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
	add := func(name, status, message string) {
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

	// Directorio de configuración.
	if dir, err := config.Dir(); err != nil {
		add("Config dir", StatusError, "no se pudo resolver el directorio de configuración: "+err.Error())
	} else {
		add("Config dir", StatusOK, dir)
	}

	// Directorio de datos.
	if dir, err := cfg.EnsureDataDir(); err != nil {
		add("Data dir", StatusError, "no se puede escribir en "+cfg.DataDir+": "+err.Error())
	} else {
		add("Data dir", StatusOK, dir)
	}

	// Docker CLI y daemon.
	if !execx.LookPath("docker") {
		add("Docker CLI", StatusError, "no se encontró 'docker' en el PATH (https://docs.docker.com/get-docker/)")
	} else {
		add("Docker CLI", StatusOK, "docker presente")
		res, err := execx.Run(ctx, "docker", "info", "--format", "{{.ServerVersion}}")
		if err != nil {
			add("Docker daemon", StatusError, "el daemon de Docker no responde: "+strings.TrimSpace(res.Stderr))
		} else {
			add("Docker daemon", StatusOK, "server "+res.Stdout)
		}
		// Docker Compose v2.
		res, err = execx.Run(ctx, "docker", "compose", "version", "--short")
		if err != nil {
			add("Docker Compose", StatusWarn, "no se pudo ejecutar 'docker compose version'")
		} else {
			add("Docker Compose", StatusOK, res.Stdout)
		}
	}

	// Red privada (Tailscale).
	if cfg.Tailscale.Enabled {
		if !execx.LookPath("tailscale") {
			add("Tailscale", StatusWarn, "no se encontró 'tailscale'; las instancias no podrán publicarse en el tailnet")
		} else if ip := tailscaleIPv4(ctx); ip != "" {
			add("Tailscale IP", StatusOK, ip)
		} else {
			add("Tailscale IP", StatusWarn, "no se detectó una IPv4 del tailnet; ejecuta 'tailscale up'")
		}
	} else {
		add("Tailscale", StatusOK, "desactivado en la configuración")
	}

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
