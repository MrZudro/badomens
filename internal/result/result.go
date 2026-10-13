// Package result define el documento único que imprime cada comando en modo
// JSON. El contrato es estable y lo consumen Emiria/Jamcord, los scripts y el
// servidor MCP:
//
//	{"ok":true,"exit_code":0,"action":"doctor","message":"...","data":{...},
//	 "timestamp":"..."}
//
// Claves siempre presentes: ok, exit_code, action, message, timestamp. Las
// claves data, phases y error son opcionales.
package result

import (
	"time"

	"github.com/MrZudro/badomens/internal/errx"
)

// Phase describe una etapa de una operación con fases (setup, start…).
type Phase struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// ErrorInfo es la representación JSON de un error tipado.
type ErrorInfo struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Hint    string         `json:"hint,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

// Result es el documento de salida de un comando.
type Result struct {
	OK        bool       `json:"ok"`
	ExitCode  int        `json:"exit_code"`
	Action    string     `json:"action"`
	Message   string     `json:"message"`
	Data      any        `json:"data,omitempty"`
	Phases    []Phase    `json:"phases,omitempty"`
	Error     *ErrorInfo `json:"error,omitempty"`
	Timestamp string     `json:"timestamp"`
}

// Now devuelve la marca de tiempo del contrato (UTC, RFC3339).
func Now() string { return time.Now().UTC().Format(time.RFC3339) }

// OK construye un resultado exitoso (exit_code 0).
func OK(action, message string, data any) *Result {
	return &Result{OK: true, ExitCode: 0, Action: action, Message: message, Data: data, Timestamp: Now()}
}

// Warn construye un resultado con avisos (exit_code 1): el comando terminó,
// pero hay algo que revisar.
func Warn(action, message string, data any) *Result {
	return &Result{
		OK:        false,
		ExitCode:  1,
		Action:    action,
		Message:   message,
		Data:      data,
		Error:     &ErrorInfo{Code: errx.CodeWarning, Message: message},
		Timestamp: Now(),
	}
}

// FromError construye un resultado de fallo a partir de un error.
func FromError(action string, err error) *Result {
	code, message, hint := errx.From(err)
	return &Result{
		OK:        false,
		ExitCode:  errx.ExitCode(err),
		Action:    action,
		Message:   message,
		Error:     &ErrorInfo{Code: code, Message: message, Hint: hint, Details: errx.Details(err)},
		Timestamp: Now(),
	}
}

// WithPhases añade las fases al resultado y lo devuelve.
func (r *Result) WithPhases(phases []Phase) *Result {
	r.Phases = phases
	return r
}
