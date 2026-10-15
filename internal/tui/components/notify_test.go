package components

import (
	"testing"
	"time"
)

// UX-11/§12: TTL por tipo, con tope absoluto de 30 s.
func TestNotificationTTLPorTipo(t *testing.T) {
	cases := []struct {
		name string
		kind Kind
		text string
		want time.Duration
	}{
		{"info", Secondary, "guardando…", 6 * time.Second},
		{"éxito", Secondary, "✓ cambios guardados", 8 * time.Second},
		{"aviso", Secondary, "⚠ reinicio necesario", 20 * time.Second},
		{"error", Destructive, "✖ no se pudo guardar", 30 * time.Second},
		{"destructivo sin icono", Destructive, "fallo", 30 * time.Second},
	}
	for _, c := range cases {
		if got := NotificationTTL(c.kind, c.text); got != c.want {
			t.Fatalf("%s: TTL = %v, quiero %v", c.name, got, c.want)
		}
	}
}

// Ninguna severidad puede superar el máximo absoluto de 30 s.
func TestNotificationTTLNuncaSuperaElMaximo(t *testing.T) {
	if MaxNotificationTTL != 30*time.Second {
		t.Fatalf("el máximo absoluto debe ser 30 s, tengo %v", MaxNotificationTTL)
	}
	for _, s := range []Severity{SeverityInfo, SeveritySuccess, SeverityWarning, SeverityError} {
		if ttl := s.TTL(); ttl <= 0 || ttl > MaxNotificationTTL {
			t.Fatalf("la severidad %d tiene un TTL fuera de rango: %v", s, ttl)
		}
	}
}

// La severidad se deduce del icono del texto aunque no se anote el tipo.
func TestSeverityForDeducePorIcono(t *testing.T) {
	cases := []struct {
		kind Kind
		text string
		want Severity
	}{
		{Secondary, "✓ hecho", SeveritySuccess},
		{Secondary, "✖ fallo", SeverityError},
		{Secondary, "⚠ aviso", SeverityWarning},
		{Secondary, "creada", SeverityInfo},
		{Destructive, "creada", SeverityError},
		{Primary, "creada", SeveritySuccess},
	}
	for _, c := range cases {
		if got := SeverityFor(c.kind, c.text); got != c.want {
			t.Fatalf("SeverityFor(%v, %q) = %v, quiero %v", c.kind, c.text, got, c.want)
		}
	}
}
