package tui

import "github.com/charmbracelet/lipgloss"

// Paleta de la TUI. Base adaptativa (funciona en terminales claras y oscuras)
// y acentos fijos en hex.
var (
	colText   = lipgloss.AdaptiveColor{Light: "#1f2430", Dark: "#e6e9ef"}
	colDim    = lipgloss.AdaptiveColor{Light: "#5a6070", Dark: "#8a92a3"}
	colAccent = lipgloss.Color("#8b5cf6")
	colOK     = lipgloss.Color("#22c55e")
	colWarn   = lipgloss.Color("#f59e0b")
	colBad    = lipgloss.Color("#ef4444")
)

var (
	styleHeader   = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	styleDim      = lipgloss.NewStyle().Foreground(colDim)
	styleKey      = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	styleStatus   = lipgloss.NewStyle().Foreground(colDim)
	styleErr      = lipgloss.NewStyle().Foreground(colBad)
	styleTitle    = lipgloss.NewStyle().Bold(true).Foreground(colText)
	styleMuted    = lipgloss.NewStyle().Foreground(colDim).Faint(true)
	styleSelected = lipgloss.NewStyle().Background(colAccent).Foreground(lipgloss.Color("#ffffff"))
	styleRunning  = lipgloss.NewStyle().Foreground(colOK)
	styleStopped  = lipgloss.NewStyle().Foreground(colDim)
	styleWarn     = lipgloss.NewStyle().Foreground(colWarn)
	styleBad      = lipgloss.NewStyle().Foreground(colBad)
	styleGood     = lipgloss.NewStyle().Foreground(colOK)
)

// stateStyle colorea el estado según su significado.
func stateStyle(state string) lipgloss.Style {
	switch state {
	case "running":
		return styleRunning
	case "":
		return styleMuted
	default:
		return styleStopped
	}
}

// healthStyle colorea la salud TPS.
func healthStyle(score string) lipgloss.Style {
	switch score {
	case "good":
		return styleGood
	case "degraded", "not-running":
		return styleWarn
	case "critical", "error":
		return styleBad
	}
	return styleDim
}
