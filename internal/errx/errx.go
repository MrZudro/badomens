// Package errx define el contrato de errores de badomens: cada error tipado
// lleva un código estable (clave `error.code` del documento JSON), un mensaje
// legible y una pista de qué hacer. El código de salida del proceso se deriva
// del error: 0 éxito, 1 aviso, 2 error.
package errx

import (
	"errors"
	"fmt"
)

// Code es un identificador estable de error. No cambia entre releases; los
// consumidores (Emiria/Jamcord, scripts, MCP) comparan contra estos códigos.
type Code = string

// Códigos de error estables del proyecto.
const (
	// CodeGeneral es un error general no clasificado.
	CodeGeneral Code = "error_general"
	// CodeUsage indica uso inválido: flags, argumentos o configuración.
	CodeUsage Code = "error_usage"
	// CodeNotFound indica que el recurso pedido no existe (instancia, mundo…).
	CodeNotFound Code = "error_not_found"
	// CodeDependency indica que falta una dependencia externa (Docker, Tailscale).
	CodeDependency Code = "error_dependency_missing"
	// CodeCapacity indica que la operación excede la capacidad del host.
	CodeCapacity Code = "error_capacity_exceeded"
	// CodeUnavailable indica un servicio de red no disponible.
	CodeUnavailable Code = "error_unavailable"
	// CodeWarning no es un fallo: el comando terminó pero con avisos.
	CodeWarning Code = "warn"
)

// Error es un error tipado de badomens.
type Error struct {
	// Code es el código estable del error.
	Code Code `json:"code"`
	// Message es la descripción legible del error.
	Message string `json:"message"`
	// Hint es la pista de qué hacer a continuación. Puede estar vacía.
	Hint string `json:"hint,omitempty"`
	// Details lleva detalles estructurados y aditivos (por ejemplo, el motivo
	// estable en la clave "reason"). Nunca sustituye a `code`.
	Details map[string]any `json:"details,omitempty"`
	// Cause es la causa subyacente, si la hay. No se serializa.
	Cause error `json:"-"`
}

// WithDetail añade un detalle estructurado al error y lo devuelve para encadenar.
func (e *Error) WithDetail(key string, value any) *Error {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details[key] = value
	return e
}

// Error implementa la interfaz error.
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (causa: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap permite a errors.Is/errors.As llegar a la causa.
func (e *Error) Unwrap() error { return e.Cause }

// New crea un error tipado sin causa.
func New(code Code, message, hint string) *Error {
	return &Error{Code: code, Message: message, Hint: hint}
}

// Wrap crea un error tipado con causa. Si cause ya es un *Error, conserva su
// código salvo que code sea explícitamente distinto de CodeGeneral.
func Wrap(code Code, cause error, message, hint string) *Error {
	var typed *Error
	e := &Error{Code: code, Message: message, Hint: hint, Cause: cause}
	if errors.As(cause, &typed) && code == CodeGeneral {
		e.Code = typed.Code
		if hint == "" {
			e.Hint = typed.Hint
		}
		if typed.Details != nil {
			e.Details = map[string]any{}
			for k, v := range typed.Details {
				e.Details[k] = v
			}
		}
	}
	return e
}

// Is informa si el error (o su cadena de causas) tiene el código dado.
func Is(err error, code Code) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	return e.Code == code
}

// From extrae el código, mensaje y pista del error. Para errores no tipados
// devuelve CodeGeneral y el texto del error.
func From(err error) (code Code, message, hint string) {
	var e *Error
	if errors.As(err, &e) {
		return e.Code, e.Message, e.Hint
	}
	return CodeGeneral, err.Error(), ""
}

// Details extrae los detalles estructurados de un error tipado (nil si no los
// hay o el error no es de badomens).
func Details(err error) map[string]any {
	var e *Error
	if errors.As(err, &e) {
		return e.Details
	}
	return nil
}

// ExitCode devuelve el código de salida del proceso: 0 éxito, 1 aviso, 2 error.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if Is(err, CodeWarning) {
		return 1
	}
	return 2
}

// Atajos para errores frecuentes.

// Usage crea un error de uso inválido con la pista dada.
func Usage(message, hint string) *Error { return New(CodeUsage, message, hint) }

// NotFound crea un error de recurso inexistente con pista.
func NotFound(message, hint string) *Error { return New(CodeNotFound, message, hint) }

// Dependency crea un error de dependencia ausente con pista.
func Dependency(message, hint string) *Error { return New(CodeDependency, message, hint) }

// Capacity crea un error de capacidad excedida con pista.
func Capacity(message, hint string) *Error { return New(CodeCapacity, message, hint) }

// Unavailable crea un error de servicio no disponible con pista.
func Unavailable(message, hint string) *Error { return New(CodeUnavailable, message, hint) }

// Warn crea un aviso tipado: el comando terminó, pero hay algo que revisar.
func Warn(message, hint string) *Error { return New(CodeWarning, message, hint) }
