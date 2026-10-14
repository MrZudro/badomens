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

// Clicker lo implementan las pantallas con elementos clicables con el ratón
// (además de los botones, que el App resuelve por zonas, §4.1).
type Clicker interface {
	Click(m tea.MouseMsg) tea.Cmd
}

// InputCaptor lo implementan las pantallas cuyo campo de texto o modal tiene el
// foco. Mientras CapturesInput() sea true, las teclas imprimibles se entregan a
// la pantalla y no disparan atajos globales (§5 del rediseño).
type InputCaptor interface {
	CapturesInput() bool
}

// Chromed lo implementan las pantallas que dibujan una franja fija (título y
// pestañas) que el App reserva por encima del cuerpo desplazable (§4: la altura
// del cuerpo es alto − cabecera − pestañas − estado − barra de atajos).
type Chromed interface {
	Chrome(w int) string
}

// popScreenMsg pide al App que desapile la pantalla actual.
type popScreenMsg struct{}
