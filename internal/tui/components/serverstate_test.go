package components

import (
	"strings"
	"testing"
)

// A11Y-01 (§6.1): el semáforo NUNCA depende solo del color. Cada estado, incluido
// el DESCONOCIDO, lleva una luz y un TEXTO propios, y los cuatro textos se
// distinguen entre sí; en modo ASCII la luz es imprimible (sin color ni emoji).
func TestServerStateTextoSiemprePresente(t *testing.T) {
	states := []ServerState{ServerOnline, ServerStarting, ServerStopped, ServerUnknown}
	labels := map[string]bool{}
	for _, ascii := range []bool{false, true} {
		for _, st := range states {
			led := st.LED(ascii)
			label := st.Label(ascii)
			if strings.TrimSpace(led) == "" {
				t.Fatalf("%s (ascii=%v): la luz no puede estar vacía", st, ascii)
			}
			if strings.TrimSpace(label) == "" {
				t.Fatalf("%s (ascii=%v): el texto no puede estar vacío (nunca solo color)", st, ascii)
			}
			if got := st.Pill(ascii); got != led+" "+label {
				t.Fatalf("%s (ascii=%v): Pill debe ser luz + texto, tengo %q", st, ascii, got)
			}
			labels[label] = true
		}
	}
	// Cuatro etiquetas distintas: los estados se comunican por texto, no por color.
	if len(labels) != len(states) {
		t.Fatalf("las etiquetas deben ser distintas por estado, tengo %d: %v", len(labels), labels)
	}
	// El modo ASCII no usa caracteres no imprimibles ni emoji.
	for _, st := range states {
		for _, r := range st.LED(true) {
			if r > 0x7e || r < 0x20 {
				t.Fatalf("%s: la luz ASCII debe ser imprimible, tengo %q", st, st.LED(true))
			}
		}
	}
	// El estado DESCONOCIDO es distinto del apagado (no se confunde con color).
	if ServerUnknown.Label(false) == ServerStopped.Label(false) {
		t.Fatal("«Desconocido» no puede compartir texto con «Apagado»")
	}
}

// A11Y-01b: el estado crudo se traduce de forma conservadora; cualquier valor no
// reconocido cae en DESCONOCIDO (con texto), nunca en un estado inventado.
func TestServerStateOfDesconocido(t *testing.T) {
	cases := map[string]ServerState{
		"running":    ServerOnline,
		"restarting": ServerStarting,
		"starting":   ServerStarting,
		"exited":     ServerStopped,
		"created":    ServerStopped,
		"":           ServerStopped,
		"paused":     ServerUnknown,
		"dead":       ServerStopped,
		"weird":      ServerUnknown,
	}
	for raw, want := range cases {
		if got := ServerStateOf(raw); got != want {
			t.Fatalf("ServerStateOf(%q) = %s, quiero %s", raw, got, want)
		}
	}
}
