// Package tps diagnostica el rendimiento de una instancia: TPS y MSPT por
// consola (Paper/Spigot), CPU/RAM por docker stats, y evidencias de sobrecarga
// en los logs ("Can't keep up!"). Es la base del comando `doctor tps`.
package tps

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/execx"
	"github.com/MrZudro/badomens/internal/runtime"
)

// Sample es una lectura en un instante.
type Sample struct {
	CPUPercent float64 `json:"cpu_percent,omitempty"`
	MemUsedMB  float64 `json:"mem_used_mb,omitempty"`
	MemLimitMB float64 `json:"mem_limit_mb,omitempty"`
	TPS        float64 `json:"tps"`  // -1 si no se puede medir
	MSPT       float64 `json:"mspt"` // -1 si no se puede medir
}

// Report es el resultado del diagnóstico de rendimiento.
type Report struct {
	Instance    string   `json:"instance"`
	Engine      string   `json:"engine"`
	Samples     []Sample `json:"samples"`
	AvgTPS      float64  `json:"avg_tps"` // -1 si no se puede medir
	MSPT        float64  `json:"mspt"`    // -1 si no se puede medir
	CPUAvg      float64  `json:"cpu_avg_percent"`
	MemUsedMB   float64  `json:"mem_used_mb"`
	LagEvents   int      `json:"lag_events"`
	Warnings    []string `json:"warnings,omitempty"`
	Score       string   `json:"score"` // good | degraded | critical | unknown
	Suggestions []string `json:"suggestions,omitempty"`
}

// Healthy indica si el panel debe considerarse ok (no crítico).
func (r Report) Healthy() bool { return r.Score == "good" || r.Score == "degraded" }

// Options configura la recogida de muestras.
type Options struct {
	Samples  int           // por defecto 3
	Interval time.Duration // por defecto 2s
}

// Collect recoge N muestras de rendimiento de una instancia.
func Collect(ctx context.Context, rt *runtime.Runtime, slug, engine string, o Options) (*Report, error) {
	if o.Samples <= 0 {
		o.Samples = 3
	}
	if o.Interval <= 0 {
		o.Interval = 2 * time.Second
	}

	rep := &Report{Instance: slug, Engine: engine, AvgTPS: -1, MSPT: -1}

	// Primera medición de TPS/MSPT por consola (Paper/Spigot).
	_ = rt.Console(ctx, slug, "tps")
	time.Sleep(o.Interval)
	lastTPS := readConsoleTPS(ctx, rt, slug)
	lastMSPT := -1.0
	_ = rt.Console(ctx, slug, "mspt")
	time.Sleep(500 * time.Millisecond)
	if l := readConsoleMSPT(ctx, rt, slug); l > -1 {
		lastMSPT = l
	}

	for i := 0; i < o.Samples; i++ {
		s := Sample{TPS: lastTPS, MSPT: lastMSPT}
		if cpu, used, limit, err := ContainerStats(ctx, rt.ContainerName(slug)); err == nil {
			s.CPUPercent, s.MemUsedMB, s.MemLimitMB = cpu, used, limit
		}
		rep.Samples = append(rep.Samples, s)
		if i < o.Samples-1 {
			time.Sleep(o.Interval)
		}
	}

	rep.LagEvents = countLagEvents(ctx, rt, slug)
	finishReport(rep)
	return rep, nil
}

func finishReport(rep *Report) {
	var tpsSum, cpuSum, memSum float64
	var tpsN, cpuN, memN int
	for _, s := range rep.Samples {
		if s.TPS > 0 {
			tpsSum += s.TPS
			tpsN++
		}
		if s.CPUPercent > 0 {
			cpuSum += s.CPUPercent
			cpuN++
		}
		if s.MemUsedMB > 0 {
			memSum += s.MemUsedMB
			memN++
		}
	}
	if tpsN > 0 {
		rep.AvgTPS = tpsSum / float64(tpsN)
	}
	if cpuN > 0 {
		rep.CPUAvg = cpuSum / float64(cpuN)
	}
	if memN > 0 {
		rep.MemUsedMB = memSum / float64(memN)
	}
	for _, s := range rep.Samples {
		if s.MSPT > rep.MSPT {
			rep.MSPT = s.MSPT
		}
	}
	scoreAndSuggest(rep)
}

// scoreAndSuggest clasifica y llena sugerencias.
func scoreAndSuggest(rep *Report) {
	rep.Score = "good"

	if rep.AvgTPS >= 0 {
		switch {
		case rep.AvgTPS >= 19.5:
			rep.Score = "good"
		case rep.AvgTPS >= 15:
			rep.Score = "degraded"
			rep.Suggestions = append(rep.Suggestions, "TPS bajo; revisa plugins exigentes, entidades o chunks de redstone")
		default:
			rep.Score = "critical"
			rep.Suggestions = append(rep.Suggestions, "TPS crítico: considera reducir jugadores, desactivar plugins pesados o revisar el mundo")
		}
	}
	switch {
	case rep.LagEvents >= 10:
		rep.Score = "critical"
		rep.Suggestions = append(rep.Suggestions, fmt.Sprintf("%d eventos de retraso registrados en los logs", rep.LagEvents))
	case rep.LagEvents >= 3:
		if rep.Score == "good" {
			rep.Score = "degraded"
		}
		rep.Suggestions = append(rep.Suggestions, fmt.Sprintf("%d eventos de retraso registrados en los logs", rep.LagEvents))
	}
	if rep.CPUAvg > 90 {
		if rep.Score == "good" {
			rep.Score = "degraded"
		}
		rep.Suggestions = append(rep.Suggestions, "uso de CPU muy alto (>90%); revisa lo que esté comiendo CPU")
	}
	if rep.MSPT >= 40 {
		rep.Suggestions = append(rep.Suggestions, "MSPT alto (>40ms); probabilidad de ticks degradados")
	}
	if rep.Score == "good" {
		rep.Warnings = nil
	}
}

// ContainerStats lee CPU y memoria del contenedor con docker stats.
func ContainerStats(ctx context.Context, container string) (cpu, usedMB, limitMB float64, err error) {
	args := []string{"stats", "--no-stream", "--format", "{{json .}}"}
	if container != "" {
		args = append(args, container)
	}
	res, err := execx.Run(ctx, "docker", args...)
	if err != nil {
		return 0, 0, 0, err
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var s struct {
			Name     string `json:"Name"`
			CPUPerc  string `json:"CPUPerc"`
			MemUsage string `json:"MemUsage"`
		}
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			continue
		}
		if container != "" && s.Name != container {
			continue
		}
		used, limit := parseMemUsage(s.MemUsage)
		return parsePercent(s.CPUPerc), used, limit, nil
	}
	return 0, 0, 0, fmt.Errorf("no se encontró el contenedor %q en docker stats", container)
}

// readConsoleTPS ejecuta el comando tps y parsea la última línea "TPS from...".
func readConsoleTPS(ctx context.Context, rt *runtime.Runtime, slug string) float64 {
	out, err := rt.Logs(ctx, slug, 200)
	if err != nil {
		return -1
	}
	for _, line := range splitLinesReversed(out) {
		if !strings.Contains(line, "TPS from last 1m") {
			continue
		}
		if i := strings.Index(line, ":"); i >= 0 {
			last := strings.TrimSpace(strings.Split(line[i+1:], ",")[0])
			if v, err := strconv.ParseFloat(last, 64); err == nil {
				return v
			}
		}
	}
	return -1
}

// readConsoleMSPT parsea la línea "Server tick report ... Mean:" de /mspt.
func readConsoleMSPT(ctx context.Context, rt *runtime.Runtime, slug string) float64 {
	out, err := rt.Logs(ctx, slug, 200)
	if err != nil {
		return -1
	}
	for _, line := range splitLinesReversed(out) {
		if !strings.Contains(line, "Server tick report") {
			continue
		}
		if i := strings.Index(line, "Mean:"); i >= 0 {
			vals := strings.Fields(line[i+5:])
			if len(vals) > 0 {
				if v, err := strconv.ParseFloat(strings.Trim(vals[0], ","), 64); err == nil {
					return v
				}
			}
		}
	}
	return -1
}

// countLagEvents cuenta "Can't keep up!" en las últimas líneas de logs.
func countLagEvents(ctx context.Context, rt *runtime.Runtime, slug string) int {
	out, err := rt.Logs(ctx, slug, 1000)
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Can't keep up!") {
			count++
		}
	}
	return count
}

func splitLinesReversed(s string) []string {
	lines := strings.Split(s, "\n")
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return lines
}

// parsePercent convierte "12.34%" en 12.34.
func parsePercent(s string) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(s, "%"))
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// parseMemUsage convierte "1.2GiB / 4GiB" en usado y límite (MiB).
func parseMemUsage(s string) (usedMB, limitMB float64) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return 0, 0
	}
	return parseSize(parts[0]), parseSize(parts[1])
}

func parseSize(s string) float64 {
	s = strings.TrimSpace(s)
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "GiB"):
		mult = 1024
		s = strings.TrimSuffix(s, "GiB")
	case strings.HasSuffix(s, "MiB"):
		mult = 1
		s = strings.TrimSuffix(s, "MiB")
	case strings.HasSuffix(s, "KiB"):
		mult = 1.0 / 1024
		s = strings.TrimSuffix(s, "KiB")
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v * mult
}
