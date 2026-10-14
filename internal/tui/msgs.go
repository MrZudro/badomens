package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/dockerx"
)

// paletteChosenMsg indica que la paleta eligió una acción por su ID.
type paletteChosenMsg struct{ id string }

// busyMsg informa de que hay (o ya no hay) una operación en curso.
type busyMsg struct{ on bool }

// confirmResultMsg es el resultado de un modal de confirmación.
type confirmResultMsg struct {
	confirmed bool
	action    string
}

// dockerRepairMsg es el resultado de intentar reparar Docker.
type dockerRepairMsg struct {
	diag dockerx.Diagnosis
	err  error
}

// Actioner lo implementan las pantallas capaces de ejecutar una de sus acciones
// por su ID estable (paleta de comandos y botones, P-02).
type Actioner interface {
	Invoke(id string) tea.Cmd
}

// popScreenMsg pide al App que desapile la pantalla actual.
type popScreenMsg struct{}
