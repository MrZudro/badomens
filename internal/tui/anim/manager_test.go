package anim

import (
	"testing"
	"time"
)

// TestSinAnimacionesNoEmiteTicks es el test de regla de §10.2: sin animaciones
// activas el Manager no produce ningún tick. Nota: no se ejecuta el Cmd (un
// tea.Tick espera su intervalo); solo se comprueba su presencia/ausencia.
func TestSinAnimacionesNoEmiteTicks(t *testing.T) {
	m := New(false)
	if m.Active() != 0 {
		t.Fatalf("recién creado no debe tener animaciones: %d", m.Active())
	}
	if cmd := m.Tick(); cmd != nil {
		t.Fatal("sin animaciones no debe haber tea.Cmd de tick")
	}
}

func TestTicksSoloConAnimacionesActivas(t *testing.T) {
	m := New(false)
	if m.Tick() != nil {
		t.Fatal("sin activas no hay tick")
	}
	m.Start("pulso")
	if m.Active() != 1 {
		t.Fatalf("quiero 1 activa, tengo %d", m.Active())
	}
	if m.Tick() == nil {
		t.Fatal("con una animación activa debe haber tick")
	}
	m.Stop("pulso")
	if m.Tick() != nil {
		t.Fatal("al terminar las animaciones no debe haber más ticks")
	}
}

func TestMovimientoReducidoDesactiva(t *testing.T) {
	m := New(true) // movimiento reducido
	m.Start("pulso")
	if m.Active() != 0 {
		t.Fatalf("con movimiento reducido nada debe activarse: %d", m.Active())
	}
	if m.Tick() != nil {
		t.Fatal("movimiento reducido: no debe haber tick")
	}
	m.SetReduced(false)
	m.Start("pulso")
	if m.Tick() == nil {
		t.Fatal("al recuperar el movimiento deben volver los ticks")
	}
	m.SetReduced(true)
	if m.Tick() != nil {
		t.Fatal("al activar el movimiento reducido se suspenden los ticks")
	}
}

func TestTopeDeFPS(t *testing.T) {
	m := New(false)
	m.Start("a")
	m.ttf = 5 * time.Millisecond // forzar el tope
	if iv := m.Interval(); iv < MinInterval {
		t.Fatalf("el intervalo no puede bajar de 30 fps: %s", iv)
	}
	if iv := Interval; iv <= 0 {
		t.Fatalf("intervalo por defecto inválido: %s", iv)
	}
}
