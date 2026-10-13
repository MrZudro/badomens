// Package tui implementa el panel interactivo multi-instancia (F4). Es un
// cliente del propio binario: importa directamente los paquetes internos
// (config, instance, runtime, backup, tps) y no usa el contrato JSON, que queda
// reservado para el CLI no interactivo (Emiria/Jamcord, scripts, MCP).
package tui

import (
	"github.com/manuelserver/badomens/internal/backup"
	"github.com/manuelserver/badomens/internal/instance"
)

// Row resume una instancia para el panel de lista.
type Row struct {
	Slug       string
	Name       string
	Engine     string
	Version    string
	Memory     string
	CPU        int
	Port       int
	State      string // estado crudo de docker: running, exited, created, restarting, paused o ""
	Gamemode   string
	OnlineMode bool
	MaxPlayers int
}

// Metrics agrupa las métricas de una instancia corriendo.
type Metrics struct {
	CPUPercent float64
	MemUsedMB  float64
	MemLimitMB float64
	WorldMB    float64
}

// Detail es el detalle completo que muestra la vista de instancia.
type Detail struct {
	Spec    *instance.Spec
	State   string
	HostIP  string
	Metrics *Metrics // nil si la instancia no está corriendo
	Backups []backup.Info
}

// op es una operación ejecutable sobre una instancia.
type op int

const (
	opStart op = iota
	opStop
	opClose
	opRestart
	opBackupCreate
	opPrune
	opConsole
)

func (o op) String() string {
	switch o {
	case opStart:
		return "start"
	case opStop:
		return "stop"
	case opClose:
		return "close"
	case opRestart:
		return "restart"
	case opBackupCreate:
		return "backup create"
	case opPrune:
		return "backup prune"
	case opConsole:
		return "console"
	}
	return "op"
}

// stateLabel devuelve la etiqueta legible de un estado de contenedor.
func stateLabel(state string) string {
	switch state {
	case "running":
		return "corriendo"
	case "exited":
		return "detenida"
	case "created":
		return "creada"
	case "restarting":
		return "reiniciando"
	case "paused":
		return "pausada"
	case "":
		return "sin contenedor"
	}
	return state
}

// healthLabel traduce la puntuación de doctor tps para el panel.
func healthLabel(score string) string {
	switch score {
	case "good":
		return "buena"
	case "degraded":
		return "degradada"
	case "critical":
		return "crítica"
	case "not-running":
		return "—"
	case "error":
		return "err"
	case "":
		return ""
	}
	return score
}
