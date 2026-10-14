package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// --- §5.13 Salud ---

// TestSaludEstadosPorTexto: desconocido, aviso y crítico se distinguen por icono
// Y texto (nunca solo por color) y el tag del panel resume el estado.
func TestSaludEstadosPorTexto(t *testing.T) {
	h := newHealthScreen(svc.NewFake(nil), theme.For(theme.Dark), false)

	// Sin comprobar: paneles presentes y estado «sin comprobar» con «·».
	vacio := stripANSI(h.View(106, 24))
	for _, want := range []string{"ENTORNO", "RECURSOS", "sin comprobar", "·"} {
		if !strings.Contains(vacio, want) {
			t.Fatalf("sin datos debe verse %q:\n%s", want, vacio)
		}
	}

	// Aviso: disco con poco espacio (<10 GB) y actualización disponible.
	h.diskOK, h.freeGB, h.totalGB = true, 5, 500
	h.checkedUp, h.hasUpdate, h.latest = true, true, "1.0.1"
	aviso := stripANSI(h.View(106, 24))
	if !strings.Contains(aviso, "aviso (<10 GB)") || !strings.Contains(aviso, components.Warn(false)) {
		t.Fatalf("el aviso lleva icono y texto:\n%s", aviso)
	}
	if !strings.Contains(aviso, "2 avisos") {
		t.Fatalf("el tag debe contar los avisos del panel:\n%s", aviso)
	}

	// Crítico: Docker caído.
	h.docker = dockerx.Diagnosis{State: dockerx.StateDaemonDown, Message: "el demonio no responde"}
	crit := stripANSI(h.View(106, 24))
	if !strings.Contains(crit, "crítico") || !strings.Contains(crit, components.Cross(false)) {
		t.Fatalf("el crítico lleva icono y texto:\n%s", crit)
	}
	if !strings.Contains(crit, "DaemonDown") {
		t.Fatalf("debe indicarse el estado de Docker:\n%s", crit)
	}
}

// TestSaludBusyVisible: mientras hay una operación en curso se ven el indicador y
// el tag «reparando…», y la respuesta libera el estado ocupado.
func TestSaludBusyVisible(t *testing.T) {
	h := newHealthScreen(svc.NewFake(nil), theme.For(theme.Dark), false)
	if cmd := h.Invoke("health.retry"); cmd == nil {
		t.Fatal("R debe pedir una nueva comprobación")
	}
	out := stripANSI(h.View(106, 24))
	if !strings.Contains(out, "comprobando…") || !strings.Contains(out, "reparando…") {
		t.Fatalf("la operación en curso debe verse:\n%s", out)
	}
	if h.Status() != "comprobando…" {
		t.Fatalf("Status debe reflejar la operación en curso, tengo %q", h.Status())
	}
	h.Update(healthReexpMsg{})
	if h.busy != "" {
		t.Fatal("la respuesta debe liberar el estado ocupado")
	}
}

// TestSaludSinDesborde: la pantalla no excede el ancho ni el alto en los tamaños
// de referencia, en Unicode y ASCII, y en compacto apila una sola columna.
func TestSaludSinDesborde(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		for _, sz := range [][2]int{{40, 12}, {80, 24}, {110, 30}, {132, 40}} {
			a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), AppOptions{Theme: theme.Dark, ASCII: ascii})
			resize(a, sz[0], sz[1])
			a.Update(pollDockerMsg{diag: dockerx.Diagnosis{State: dockerx.StateOK, Message: "Docker está listo.", ComposeVersion: "v2.30.1"}})
			a.invoke("global.health")
			assertWithinSize(t, stripANSI(a.View()), sz[0], sz[1])
		}
	}

	// Compacto: una sola columna (ENTORNO encima de RECURSOS) sin desbordar.
	h := newHealthScreen(svc.NewFake(nil), theme.For(theme.Dark), false)
	compacto := stripANSI(h.View(60, 20))
	if !strings.Contains(compacto, "ENTORNO") || !strings.Contains(compacto, "RECURSOS") {
		t.Fatalf("en compacto deben apilarse los dos paneles:\n%s", compacto)
	}
	if strings.Index(compacto, "ENTORNO") > strings.Index(compacto, "RECURSOS") {
		t.Fatalf("ENTORNO debe ir encima de RECURSOS:\n%s", compacto)
	}
	for _, ln := range strings.Split(compacto, "\n") {
		if layout.Width(ln) > 60 {
			t.Fatalf("compacto desborda 60 celdas: %q", ln)
		}
	}

	// El mínimo soportado por la app (40×12 → cuerpo 36×7) tampoco desborda.
	minimo := stripANSI(h.View(36, 7))
	if !strings.Contains(minimo, "ENTORNO") {
		t.Fatalf("en el mínimo debe verse al menos ENTORNO:\n%s", minimo)
	}
	if n := len(strings.Split(minimo, "\n")); n > 7 {
		t.Fatalf("el mínimo ocupa %d líneas (máx 7):\n%s", n, minimo)
	}
}

// --- §5.14 Ayuda y paleta ---

// TestAyudaPantallaYGlobales: la ayuda lista los atajos de la pantalla activa
// (tecla + etiqueta) y los globales, y F1/Esc la cierran devolviendo el foco.
func TestAyudaPantallaYGlobales(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	openInstance(t, a, "survival")

	a.Update(tea.KeyMsg{Type: tea.KeyF1})
	ov := a.router.Overlay()
	if ov == nil || ov.ID() != "help" {
		t.Fatal("F1 debe abrir la ayuda")
	}
	out := stripANSI(ov.View(90, 24))
	for _, want := range []string{"Pantalla", "Globales", "Iniciar", "Eliminar", "Ctrl+P", "Ctrl+Q"} {
		if !strings.Contains(out, want) {
			t.Fatalf("la ayuda debe contener %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "[S]") {
		t.Fatalf("los atajos de la pantalla deben mostrar su tecla:\n%s", out)
	}
	if strings.Index(out, "Pantalla") > strings.Index(out, "Globales") {
		t.Fatalf("el bloque Pantalla debe ir antes que Globales:\n%s", out)
	}
	if strings.Count(out, "Globales") != 1 {
		t.Fatalf("el bloque Globales debe aparecer una sola vez:\n%s", out)
	}

	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if a.router.Overlay() != nil {
		t.Fatal("Esc debe cerrar la ayuda")
	}
	if a.router.Active().ID() != "instance" {
		t.Fatalf("al cerrar debe restaurarse el detalle, tengo %s", a.router.Active().ID())
	}
}

// pickerHas indica si la lista filtrada contiene el valor dado.
func pickerHas(items []components.PickerItem, value string) bool {
	for _, it := range items {
		if it.Value == value {
			return true
		}
	}
	return false
}

// TestPaletaDifusaHintYEnter: la paleta muestra la pista de filtrado, encuentra
// por subcadena y por subsecuencia, y Enter ejecuta / Esc cierra.
func TestPaletaDifusaHintYEnter(t *testing.T) {
	a := testApp()
	resize(a, 110, 30)
	openInstance(t, a, "survival")
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	pal, ok := a.router.Overlay().(*paletteScreen)
	if !ok {
		t.Fatal("Ctrl+P debe abrir la paleta")
	}

	if !strings.Contains(stripANSI(pal.View(106, 24)), "Escribe para filtrar") {
		t.Fatalf("la paleta debe mostrar la pista de filtrado:\n%s", stripANSI(pal.View(106, 24)))
	}

	// Subcadena.
	for _, r := range "elim" {
		a.Update(press(string(r)))
	}
	if it, ok := pal.picker.Selected(); !ok || it.Value != "instance.delete" {
		t.Fatalf("la subcadena debe seleccionar Eliminar, tengo %+v", it)
	}

	// Difusa (subsecuencia): «lmnr» no es subcadena de ninguna etiqueta.
	for i := 0; i < 4; i++ {
		a.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	for _, r := range "lmnr" {
		a.Update(press(string(r)))
	}
	got := pal.picker.Filtered()
	if len(got) == 0 || !pickerHas(got, "instance.delete") {
		t.Fatalf("la búsqueda difusa debe incluir Eliminar: %+v", got)
	}

	// Enter ejecuta el comando seleccionado y cierra la paleta.
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter debe ejecutar el comando seleccionado")
	}
	msg, ok := cmd().(paletteChosenMsg)
	if !ok {
		t.Fatalf("se esperaba paletteChosenMsg, tengo %T", msg)
	}
	if msg.id != "instance.delete" {
		t.Fatalf("debía elegirse instance.delete, tengo %q", msg.id)
	}
	if a.router.Overlay() != nil {
		t.Fatal("al elegir, la paleta debe cerrarse")
	}
}

// --- §5.15 Modales ---

// TestModalBorradoPorLaRutaDeProduccion: en el detalle, «D» abre el modal de
// borrado; las letras se escriben (no son atajos) y Esc cancela sin borrar.
func TestModalBorradoPorLaRutaDeProduccion(t *testing.T) {
	a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
	resize(a, 110, 30)
	inst := openInstance(t, a, "survival")

	a.Update(press("D"))
	if inst.modal == nil || inst.modalKind != "delete" {
		t.Fatal("D debe abrir el modal de borrado")
	}
	out := stripANSI(a.View())
	for _, want := range []string{"Escribe «survival»", "No, volver", "para siempre"} {
		if !strings.Contains(out, want) {
			t.Fatalf("el modal debe contener %q:\n%s", want, out)
		}
	}
	// Una letra que también es navegación del modal se escribe en el nombre.
	a.Update(press("l"))
	if inst.modal == nil || inst.modal.Typed != "l" {
		t.Fatalf("«l» debe escribirse en el modal, tengo %+v", inst.modal)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if inst.modal != nil {
		t.Fatal("Esc debe cancelar el borrado")
	}
}

// TestModalDeSalidaConOperacionEnCurso: con una operación en curso, Ctrl+Q no sale
// directamente: abre el modal de confirmación (Esc cancela, «y» sale).
func TestModalDeSalidaConOperacionEnCurso(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	a.busy = true
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if cmd != nil {
		t.Fatal("con una operación en curso no debe salir directamente")
	}
	if a.confirm == nil {
		t.Fatal("debe abrirse el modal de salida")
	}
	out := stripANSI(a.View())
	if !strings.Contains(strings.ToLower(out), "salir") {
		t.Fatalf("el modal de salida debe verse:\n%s", out)
	}

	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if a.confirm != nil {
		t.Fatal("Esc debe cerrar el modal de salida")
	}
	if a.quitting {
		t.Fatal("Esc no debe salir")
	}

	// Reabrir y confirmar con la acción enfocada (se navega con →).
	a.busy = true
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if a.confirm == nil {
		t.Fatal("debe reabrirse el modal de salida")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyRight})
	_, cmd = a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("confirmar la salida debe pedir salir")
	}
	if !a.quitting {
		t.Fatal("confirmar debe marcar la salida")
	}
}
