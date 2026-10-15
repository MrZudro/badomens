package components

import (
	"strings"
	"time"
)

// MaxNotificationTTL es el tope absoluto de cualquier notificación transitoria
// (SPEC 2 §12): ninguna notificación puede durar más de 30 s.
const MaxNotificationTTL = 30 * time.Second

// Severity clasifica una notificación para fijar su tiempo de vida.
type Severity int

// Niveles de severidad.
const (
	// SeverityInfo es información neutra (por defecto).
	SeverityInfo Severity = iota
	// SeveritySuccess confirma una acción completada.
	SeveritySuccess
	// SeverityWarning avisa de un resultado parcial o de algo que requiere atención.
	SeverityWarning
	// SeverityError informa de un fallo que el usuario debe atender.
	SeverityError
)

// TTL es el tiempo de vida por defecto de cada severidad. Info/éxito duran poco
// (5–8 s); avisos y errores que requieren atención llegan hasta 30 s. El tope
// absoluto de 30 s se garantiza en NotificationTTL.
func (s Severity) TTL() time.Duration {
	switch s {
	case SeverityError:
		return 30 * time.Second
	case SeverityWarning:
		return 20 * time.Second
	case SeveritySuccess:
		return 8 * time.Second
	default:
		return 6 * time.Second
	}
}

// SeverityFor deduce la severidad por el tipo de acción y por el icono inicial
// del texto (✓, ✖, ⚠), de modo que toda notificación existente queda clasificada
// sin tener que anotar cada llamada.
func SeverityFor(kind Kind, text string) Severity {
	t := strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(t, "✖"), strings.HasPrefix(t, "[XX]"):
		return SeverityError
	case strings.HasPrefix(t, "⚠"), strings.HasPrefix(t, "[!!]"):
		return SeverityWarning
	case strings.HasPrefix(t, "✓"), strings.HasPrefix(t, "[OK]"):
		return SeveritySuccess
	}
	switch kind {
	case Destructive:
		return SeverityError
	case Primary:
		return SeveritySuccess
	}
	return SeverityInfo
}

// NotificationTTL devuelve el tiempo de vida efectivo de una notificación,
// acotado a MaxNotificationTTL pase lo que pase.
func NotificationTTL(kind Kind, text string) time.Duration {
	ttl := SeverityFor(kind, text).TTL()
	if ttl > MaxNotificationTTL {
		return MaxNotificationTTL
	}
	return ttl
}
