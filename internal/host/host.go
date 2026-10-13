// Package host detecta la capacidad del equipo (CPU y RAM) para planificar
// cuántas instancias pueden correr en paralelo y cuánta memoria dar a cada una.
// La fuente principal es el propio Docker (`docker info`), que conocemos que
// está presente porque es un requisito de badomens.
package host

import (
	"context"
	"encoding/json"
	"runtime"
	"strconv"
	"strings"

	"github.com/MrZudro/badomens/internal/execx"
)

// Capacity describe los recursos del host.
type Capacity struct {
	// NCPU es el número de CPUs lógicas.
	NCPU int `json:"ncpu"`
	// MemTotalBytes es la memoria total del host en bytes (0 si se desconoce).
	MemTotalBytes uint64 `json:"mem_total_bytes"`
	// Source indica de dónde salió la información ("docker" o "runtime").
	Source string `json:"source"`
}

// MemTotalGiB devuelve la memoria total en GiB redondeada.
func (c Capacity) MemTotalGiB() float64 {
	return float64(c.MemTotalBytes) / (1024 * 1024 * 1024)
}

// Detect consulta a Docker la capacidad del host; si no está disponible, cae a
// los datos del runtime (solo CPU).
func Detect(ctx context.Context) Capacity {
	cap := Capacity{NCPU: runtime.NumCPU(), Source: "runtime"}
	if !execx.LookPath("docker") {
		return cap
	}
	res, err := execx.Run(ctx, "docker", "info", "--format", "{{json .}}")
	if err != nil {
		return cap
	}
	var info struct {
		NCPU     int    `json:"NCPU"`
		MemTotal uint64 `json:"MemTotal"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &info); err != nil {
		// Algunos formatos antiguos no devuelven JSON; se intenta el texto.
		return cap
	}
	if info.NCPU > 0 {
		cap.NCPU = info.NCPU
	}
	if info.MemTotal > 0 {
		cap.MemTotalBytes = info.MemTotal
		cap.Source = "docker"
	}
	return cap
}

// ParseMemory convierte un valor tipo "4G" o "512M" en bytes. Acepta también
// bytes en crudo. Devuelve 0 si no se puede interpretar.
func ParseMemory(s string) uint64 {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0
	}
	mult := uint64(1)
	switch {
	case strings.HasSuffix(s, "G"):
		mult = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "G")
	case strings.HasSuffix(s, "M"):
		mult = 1024 * 1024
		s = strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "K"):
		mult = 1024
		s = strings.TrimSuffix(s, "K")
	}
	n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n * mult
}
