// Package components contiene los bloques de la TUI v2: botones y acciones con
// contrato rígido (RF-7), que más adelante alimentan la barra de acciones y la
// paleta de comandos.
package components

import "strings"

// Kind clasifica el papel de una acción.
type Kind int

// Tipos de acción.
const (
	// Secondary es la acción normal.
	Secondary Kind = iota
	// Primary es la acción destacada (una por vista).
	Primary
	// Destructive es una acción peligrosa (roja, exige confirmación).
	Destructive
	// Navigation no modifica nada (abre pantallas).
	Navigation
)

// String devuelve el identificador estable del tipo.
func (k Kind) String() string {
	switch k {
	case Primary:
		return "primary"
	case Destructive:
		return "destructive"
	case Navigation:
		return "navigation"
	}
	return "secondary"
}

// Action describe una acción interactiva (RF-7/§4.4). Etiqueta = verbo + objeto;
// el hint explica en una línea qué pasa al pulsarla.
type Action struct {
	// ID es el identificador estable ("instance.start").
	ID string
	// Verb es el verbo ("Iniciar").
	Verb string
	// Object es el objeto ("servidor «Survival»").
	Object string
	// Key es el atajo de teclado (siempre se muestra: "[S] Iniciar…").
	Key string
	// Hint es una línea con el resultado de la acción (≤ 90 caracteres).
	Hint string
	// Kind clasifica el papel.
	Kind Kind
	// Disabled oculta el botón.
	Disabled bool
	// DisabledReason es obligatorio si Disabled ("Docker no está activo…").
	DisabledReason string
}

// Label devuelve la etiqueta completa (verbo + objeto).
func (a Action) Label() string {
	return strings.TrimSpace(strings.TrimSpace(a.Verb) + " " + strings.TrimSpace(a.Object))
}

// Problemas de etiqueta prohibidas a solas (RF-7 regla 2).
var bannedAlone = map[string]bool{
	"ok": true, "aceptar": true, "sí": true, "si": true, "no": true,
	"cancelar": true, "continuar": true, "siguiente": true, "cerrar": true,
	"...": true, "…": true, "sí,": true,
}

// Validate aplica las reglas de RF-7 y devuelve los problemas encontrados.
func (a Action) Validate() []string {
	var problems []string
	if a.ID == "" {
		problems = append(problems, "la acción no tiene ID estable")
	}
	if a.Verb == "" {
		problems = append(problems, "la acción no tiene verbo")
	}
	label := strings.ToLower(strings.TrimSpace(a.Label()))
	if a.Object == "" && bannedAlone[label] {
		problems = append(problems, "etiqueta genérica sin objeto («"+a.Label()+"»); usa verbo + objeto")
	}
	if strings.TrimSpace(a.Hint) == "" {
		problems = append(problems, "la acción «"+a.ID+"» no tiene Hint")
	}
	if len([]rune(strings.TrimSpace(a.Hint))) > 90 {
		problems = append(problems, "el Hint de «"+a.ID+"» supera 90 caracteres")
	}
	if a.Disabled && strings.TrimSpace(a.DisabledReason) == "" {
		problems = append(problems, "la acción «"+a.ID+"» está deshabilitada sin DisabledReason")
	}
	if !a.Disabled && a.Key == "" {
		// Solo las acciones habilitadas y no navegación global exigen tecla
		// visible; las destructivas van por modal.
		problems = append(problems, "la acción «"+a.ID+"» no tiene tecla (Key)")
	}
	return problems
}

// Invalid indica si la acción incumple alguna regla.
func (a Action) Invalid() bool { return len(a.Validate()) > 0 }
