package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/MrZudro/badomens/internal/events"
	"github.com/MrZudro/badomens/internal/idle"
)

// §5.10: la cuenta atrás del auto-apagado llega por el bus de eventos y se
// refleja en la pestaña Auto-apagado; cancelarla la limpia.
func TestIdleCuentaAtrasDesdeBus(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	inst := openInstance(t, a, "survival")
	inst.tab = 7
	inst.idleLoaded = true
	inst.idleCfg = idle.Config{Enabled: true, Timeout: 15 * time.Minute}

	a.Update(idleEventMsg{ev: events.Event{
		Kind: events.IdleCountdownStarted, Instance: "survival",
		Data: map[string]any{"remaining_seconds": 90},
	}})
	if out := stripANSI(a.View()); !strings.Contains(out, "apagado en 1:30") {
		t.Fatalf("la cuenta atrás debe verse en la pestaña:\n%s", out)
	}

	a.Update(idleEventMsg{ev: events.Event{Kind: events.IdleCountdownCancelled, Instance: "survival"}})
	if out := stripANSI(a.View()); strings.Contains(out, "apagado en") {
		t.Fatalf("cancelar debe limpiar la cuenta atrás:\n%s", out)
	}
}
