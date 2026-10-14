package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/tui/layout"
)

// testLogLines construye un buffer de log de n líneas con texto realista.
func testLogLines(n int) string {
	if n <= 0 {
		return ""
	}
	lines := make([]string, 0, n)
	for i := 0; i < n; i++ {
		lines = append(lines, "[22:10:03 INFO]: línea "+itoa(i)+" de log con texto suficiente para medir el ancho disponible")
	}
	return strings.Join(lines, "\n")
}

// UI-03 (Fase C): la Consola respeta alto y ancho en los tamaños de referencia,
// con el pie de atajos fijo en la última línea, para 0, 1, 200 y 2.000 líneas.
func TestUI03ConsolaRespetaAltoYAncho(t *testing.T) {
	for _, sz := range [][2]int{{40, 12}, {80, 24}, {100, 30}} {
		w, h := sz[0], sz[1]
		for _, n := range []int{0, 1, 200, 2000} {
			a := testApp()
			resize(a, w, h)
			inst := openInstance(t, a, "survival")
			inst.tab = 1
			inst.logs = testLogLines(n)
			out := stripANSI(a.View())
			lines := strings.Split(out, "\n")
			// El marco llena el terminal exactamente: si faltan líneas, el cuerpo
			// no reserva bien su altura y el pie dejaría de estar al fondo.
			if len(lines) != h {
				t.Fatalf("%dx%d con %d líneas: la vista debe ocupar %d líneas, tengo %d", w, h, n, h, len(lines))
			}
			if last := lines[len(lines)-1]; !strings.Contains(last, "Ayuda") {
				t.Fatalf("%dx%d con %d líneas: falta el pie fijo de atajos: %q", w, h, n, last)
			}
			for i, ln := range lines {
				if layout.Width(ln) > w {
					t.Fatalf("%dx%d con %d líneas: la línea %d mide %d celdas: %q", w, h, n, i+1, layout.Width(ln), ln)
				}
			}
		}
	}
}

// Consola sin líneas: placeholder atenuado dentro del panel, con la línea de entrada.
func TestConsolaVaciaPlaceholder(t *testing.T) {
	a := testApp()
	resize(a, 80, 24)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	inst.logs = ""
	out := stripANSI(a.View())
	if !strings.Contains(out, "CONSOLA") {
		t.Fatalf("el cuerpo debe titularse CONSOLA:\n%s", out)
	}
	if !strings.Contains(out, "(vacío)") {
		t.Fatalf("una consola sin líneas debe mostrar el placeholder:\n%s", out)
	}
	if !strings.Contains(out, "> ") {
		t.Fatalf("la línea de entrada debe seguir visible:\n%s", out)
	}
}

// El panel muestra el log con seguir/pausa/filtro y el comando peligroso pendiente
// como aviso de una línea, sin duplicarlo con el aviso genérico.
func TestConsolaPanelYComandoPendiente(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	inst.logs = testLogLines(50)
	out := stripANSI(a.View())
	if !strings.Contains(out, "seguir=") && !strings.Contains(out, "seg=") {
		t.Fatalf("el panel debe mostrar el estado de la consola:\n%s", out)
	}
	inst.pending = "stop"
	out = stripANSI(a.View())
	if got := strings.Count(out, "Comando peligroso"); got != 1 {
		t.Fatalf("el aviso del comando pendiente debe verse una sola vez, tengo %d:\n%s", got, out)
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 30 {
		t.Fatalf("la vista excede 30 líneas: %d", len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "Ayuda") {
		t.Fatalf("el pie debe seguir en la última línea: %q", lines[len(lines)-1])
	}
}

// Buffer acotado: un mensaje con 3.000 líneas se recorta a maxLogLines.
func TestConsolaBufferAcotado(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	inst.Update(instanceLogsMsg{slug: "survival", text: testLogLines(3000), gen: inst.logsGen})
	if got := len(strings.Split(inst.logs, "\n")); got > maxLogLines {
		t.Fatalf("el buffer debe recortarse a %d líneas, tengo %d", maxLogLines, got)
	}
}

// Generación: una respuesta de otra instancia o de una vista anterior no se aplica.
func TestConsolaDescartaRespuestasObsoletas(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	inst.logs = "actual"
	inst.Update(instanceLogsMsg{slug: "survival", text: "viejo", gen: inst.logsGen - 1})
	if inst.logs != "actual" {
		t.Fatalf("una generación anterior no debe aplicarse, tengo %q", inst.logs)
	}
	inst.Update(instanceLogsMsg{slug: "otra", text: "ajena", gen: inst.logsGen})
	if inst.logs != "actual" {
		t.Fatalf("los logs de otra instancia no deben aplicarse, tengo %q", inst.logs)
	}
	inst.Update(instanceLogsMsg{slug: "survival", text: "nuevo", gen: inst.logsGen})
	if inst.logs != "nuevo" {
		t.Fatalf("la respuesta vigente debe aplicarse, tengo %q", inst.logs)
	}
}

// Salir de la pestaña Consola invalida el buffer en vuelo.
func TestConsolaSalirInvalidaGeneracion(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	gen := inst.logsGen
	inst.switchTab(1)
	if inst.tab == 1 {
		t.Fatal("switchTab debe abandonar la pestaña Consola")
	}
	if inst.logsGen != gen+1 {
		t.Fatalf("salir de la Consola debe invalidar la generación (%d → %d)", gen, inst.logsGen)
	}
}

// logsCmd captura la generación vigente y pide el buffer acotado.
func TestConsolaLogsCmdLlevaGeneracion(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	msg, ok := ejecutaCmd(inst.logsCmd()).(instanceLogsMsg)
	if !ok {
		t.Fatal("logsCmd debe devolver instanceLogsMsg")
	}
	if msg.slug != "survival" || msg.gen != inst.logsGen {
		t.Fatalf("mensaje = slug %q gen %d; quiero slug survival gen %d", msg.slug, msg.gen, inst.logsGen)
	}
}

// Gate de I/O: sin caída y fuera de la pestaña Consola, applyState no pide logs;
// con una caída detectada sí (para deducir la causa).
func TestConsolaGateDeIO(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	inst := openInstance(t, a, "survival")
	inst.tab = 0
	inst.lastState = "exited"
	inst.userStopped = false
	if cmd := inst.applyState("exited"); cmd != nil {
		t.Fatal("sin cambio de estado no debe pedirse el log")
	}
	inst.lastState = "running"
	if cmd := inst.applyState("exited"); cmd == nil {
		t.Fatal("una caída detectada debe pedir los logs aunque no esté la Consola")
	}

	b := testApp()
	resize(b, 100, 30)
	inst2 := openInstance(t, b, "survival")
	inst2.tab = 1
	inst2.lastState = "exited"
	if cmd := inst2.applyState("exited"); cmd == nil {
		t.Fatal("en la pestaña Consola applyState debe refrescar los logs")
	}
}

// Error recuperable (§8): si la lectura de logs falla, la consola no se vacía;
// se conserva el buffer y el aviso vive dentro del panel, sin mover el pie.
func TestConsolaErrorConservaBuffer(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	inst.logs = "buffer previo"
	inst.Update(instanceLogsMsg{slug: "survival", text: "", err: errors.New("contenedor no listo"), gen: inst.logsGen})
	if inst.logs != "buffer previo" {
		t.Fatalf("un error de lectura no debe vaciar el buffer, tengo %q", inst.logs)
	}
	out := stripANSI(a.View())
	if !strings.Contains(out, "no se pudieron leer los logs") {
		t.Fatalf("el error debe explicarse dentro del panel:\n%s", out)
	}
	if !strings.Contains(out, "buffer previo") {
		t.Fatalf("el buffer debe seguir visible:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 30 || !strings.Contains(lines[len(lines)-1], "Ayuda") {
		t.Fatalf("el aviso de error no debe desplazar el pie:\n%s", out)
	}
	// Un acierto posterior limpia el aviso y aplica el buffer nuevo.
	inst.Update(instanceLogsMsg{slug: "survival", text: "nuevo", gen: inst.logsGen})
	if inst.logsErr != "" || inst.logs != "nuevo" {
		t.Fatalf("tras el éxito deben limpiarse el aviso y aplicar el log: err=%q logs=%q", inst.logsErr, inst.logs)
	}
}

// El contador seguir/pausa/filtro se abrevia pero nunca desaparece del panel:
// en 40 columnas el interior no da para título + tres indicadores y se
// conserva el estado de seguimiento; desde 70 caben los tres (§8).
func TestConsolaFlagsSiempreVisibles(t *testing.T) {
	for _, tc := range []struct {
		w, h       int
		wantFiltro bool
	}{{40, 12, false}, {70, 20, true}, {100, 30, true}} {
		a := testApp()
		resize(a, tc.w, tc.h)
		inst := openInstance(t, a, "survival")
		inst.tab = 1
		inst.logs = testLogLines(5)
		inst.filter = "INFO"
		out := stripANSI(a.View())
		if !strings.Contains(out, "seguir=") && !strings.Contains(out, "seg=") {
			t.Fatalf("%dx%d: el panel debe mostrar seguir/pausa:\n%s", tc.w, tc.h, out)
		}
		if tc.wantFiltro && !strings.Contains(out, "filtro=") {
			t.Fatalf("%dx%d: el filtro activo debe verse:\n%s", tc.w, tc.h, out)
		}
	}
}
