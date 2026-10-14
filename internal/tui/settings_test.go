package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/svc"
)

// settingIndex devuelve la posición del campo con esa clave (-1 si no está).
func settingIndex(fields []settingField, key string) int {
	for i, f := range fields {
		if f.key == key {
			return i
		}
	}
	return -1
}

// settingsApp abre el detalle por la ruta de producción con la pestaña Ajustes.
func settingsApp(t *testing.T) (*App, *instanceScreen) {
	t.Helper()
	a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 8
	return a, inst
}

// persistido devuelve el Spec que quedó guardado en el servicio.
func persistido(t *testing.T, a *App) *instance.Spec {
	t.Helper()
	d, err := a.svc.Instances.Detail(context.Background(), "survival")
	if err != nil {
		t.Fatalf("detalle: %v", err)
	}
	return d.Spec
}

// confirmaGuardado ejecuta el guardado pendiente y aplica su resultado.
func confirmaGuardado(t *testing.T, a *App, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("el cambio debe pedir el guardado")
	}
	msg := ejecutaCmd(cmd)
	sm, ok := msg.(instanceSavedMsg)
	if !ok {
		t.Fatalf("se esperaba instanceSavedMsg, tengo %T", msg)
	}
	if sm.err != nil {
		t.Fatalf("guardar: %v", sm.err)
	}
	a.Update(sm)
}

// El selector de dificultad arranca en el valor actual, se mueve con ↑/↓, aplica
// con Enter y persiste; el Spec no se muta antes de que el guardado se confirme.
func TestSettingsSelectorDificultad(t *testing.T) {
	a, inst := settingsApp(t)
	i := settingIndex(inst.settingFields(), "difficulty")
	if i < 0 {
		t.Fatal("debe existir el campo difficulty")
	}
	inst.setCursor = i
	antes := inst.detail.Spec.Difficulty

	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !inst.selecting || inst.selectKey != "difficulty" {
		t.Fatalf("Enter debe abrir el selector de dificultad; selecting=%v key=%q", inst.selecting, inst.selectKey)
	}
	if inst.editing {
		t.Fatal("un campo select no debe abrir la edición de texto")
	}
	if inst.selectCursor != 2 { // «normal» es la tercera opción
		t.Fatalf("el selector debe arrancar en el valor actual, tengo %d", inst.selectCursor)
	}
	vista := stripANSI(inst.settingsView(106, 24))
	for _, want := range []string{"Pacífico", "Fácil", "Normal", "Difícil", "elegir", "Enter: guardar", "(actual)"} {
		if !strings.Contains(vista, want) {
			t.Fatalf("el selector debe mostrar %q:\n%s", want, vista)
		}
	}
	if !strings.Contains(vista, components.Pointer(false)) {
		t.Fatalf("la opción señalada debe llevar puntero:\n%s", vista)
	}
	if !strings.Contains(stripANSI(inst.settingsView(106, 24)), "eligiendo") {
		t.Fatal("el panel debe indicar que hay un selector abierto")
	}

	a.Update(tea.KeyMsg{Type: tea.KeyUp})
	if inst.selectCursor != 1 { // «easy»
		t.Fatalf("↑ debe mover la selección, tengo %d", inst.selectCursor)
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if inst.selecting {
		t.Fatal("Enter debe cerrar el selector")
	}
	if inst.detail.Spec.Difficulty != antes {
		t.Fatalf("el Spec no debe mutarse antes de persistir, tengo %q", inst.detail.Spec.Difficulty)
	}
	if inst.pendingSave == nil || inst.pendingSave.Difficulty != "easy" {
		t.Fatalf("el cambio debe quedar preparado en pendingSave, tengo %+v", inst.pendingSave)
	}
	confirmaGuardado(t, a, cmd)
	if got := persistido(t, a).Difficulty; got != "easy" {
		t.Fatalf("la dificultad persistida debe ser easy, tengo %q", got)
	}
}

// La memoria es un selector: no admite texto libre, ofrece los tamaños habituales
// y conserva (sin perderlo) un valor que no esté en la lista.
func TestSettingsMemoriaEsSelector(t *testing.T) {
	a, inst := settingsApp(t)
	inst.detail.Spec.Memory = "24G" // valor fuera de la lista
	fields := inst.settingFields()
	i := settingIndex(fields, "memory")
	if i < 0 {
		t.Fatal("debe existir el campo memory")
	}
	if fields[i].kind != "select" {
		t.Fatalf("la memoria debe ser un selector, tengo %q", fields[i].kind)
	}
	if !containsStr(fields[i].options, "24G") {
		t.Fatalf("el valor actual debe seguir disponible: %v", fields[i].options)
	}
	if !containsStr(fields[i].options, "8G") {
		t.Fatalf("el selector debe ofrecer los tamaños habituales: %v", fields[i].options)
	}
	if fields[i].options[len(fields[i].options)-1] != "24G" {
		t.Fatalf("el valor añadido debe ordenarse por tamaño: %v", fields[i].options)
	}

	inst.setCursor = i
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !inst.selecting || inst.selectKey != "memory" {
		t.Fatalf("Enter debe abrir el selector de memoria; selecting=%v key=%q", inst.selecting, inst.selectKey)
	}
	if inst.editing {
		t.Fatal("la memoria no debe abrir una edición de texto")
	}
	// Escribir letras no cambia nada: no hay campo de texto donde escribirlas.
	if _, cmd := a.Update(press("x")); cmd != nil {
		t.Fatal("escribir en un selector no debe guardar nada")
	}
	if inst.pendingSave != nil {
		t.Fatal("escribir en un selector no debe preparar cambios")
	}
	if v := inst.settingsInput.Value(); v != "" {
		t.Fatalf("el campo de Ajustes no debe recibir texto de un selector, tengo %q", v)
	}

	want := optionIndex(fields[i].options, "8G")
	for inst.selectCursor < want {
		a.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	for inst.selectCursor > want {
		a.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	confirmaGuardado(t, a, cmd)
	if got := persistido(t, a).Memory; got != "8G" {
		t.Fatalf("la memoria persistida debe ser 8G, tengo %q", got)
	}
}

// «Cuentas» alterna crackeado↔premium y lo persiste como OnlineMode del Spec.
func TestSettingsCuentasCrackeadoPremium(t *testing.T) {
	a, inst := settingsApp(t)
	if !inst.detail.Spec.OnlineMode {
		t.Fatal("el escenario debe arrancar en cuentas premium")
	}
	i := settingIndex(inst.settingFields(), "online_mode")
	if i < 0 {
		t.Fatal("debe existir el campo online_mode")
	}
	inst.setCursor = i
	if vista := stripANSI(inst.settingsView(106, 24)); !strings.Contains(vista, "Cuentas: Premium") {
		t.Fatalf("el modo online debe mostrarse como «Premium»:\n%s", vista)
	}

	// premium → crackeado
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if inst.selectCursor != 1 { // «premium» es la segunda opción
		t.Fatalf("el selector debe arrancar en premium, tengo %d", inst.selectCursor)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyUp})
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	confirmaGuardado(t, a, cmd)
	if persistido(t, a).OnlineMode {
		t.Fatal("tras elegir crackeado, el Spec no debe quedar en modo online")
	}
	if vista := stripANSI(inst.settingsView(106, 24)); !strings.Contains(vista, "Cuentas: Crackeado") {
		t.Fatalf("el panel debe mostrar «Crackeado»:\n%s", vista)
	}

	// crackeado → premium (vuelta)
	inst.setCursor = settingIndex(inst.settingFields(), "online_mode")
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	a.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd = a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	confirmaGuardado(t, a, cmd)
	if !persistido(t, a).OnlineMode {
		t.Fatal("volver a premium debe dejar OnlineMode=true")
	}
}

// El campo de texto de Ajustes usa su propio modelo (settingsInput), no el de la
// Consola: se edita, se confirma con Enter y se persiste.
func TestSettingsCampoTextoPropio(t *testing.T) {
	a, inst := settingsApp(t)
	inst.setCursor = settingIndex(inst.settingFields(), "max_players")
	// Enter enfoca el campo de texto (el comando de parpadeo del cursor no
	// guarda nada): lo que importa es que no se prepare ningún cambio.
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if inst.pendingSave != nil || inst.status == "guardando…" {
		t.Fatalf("empezar a editar no debe preparar ningún guardado; pendingSave=%v, status=%q", inst.pendingSave, inst.status)
	}
	if !inst.editing || inst.editKey != "max_players" {
		t.Fatalf("Enter debe iniciar la edición del campo de texto; editing=%v key=%q", inst.editing, inst.editKey)
	}
	if inst.settingsInput.Value() != "20" {
		t.Fatalf("el campo de Ajustes debe cargar el valor actual, tengo %q", inst.settingsInput.Value())
	}
	if inst.input.Value() != "" {
		t.Fatalf("la Consola no debe compartir el campo de Ajustes, tengo %q", inst.input.Value())
	}
	for i := 0; i < 2; i++ { // borra «20»
		a.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	if inst.settingsInput.Value() != "" {
		t.Fatalf("borrar debe vaciar el campo de Ajustes, tengo %q", inst.settingsInput.Value())
	}
	for _, r := range "35" {
		a.Update(press(string(r)))
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	confirmaGuardado(t, a, cmd)
	if got := persistido(t, a).MaxPlayers; got != 35 {
		t.Fatalf("los jugadores máximos persistidos deben ser 35, tengo %d", got)
	}
	if inst.editing || inst.settingsInput.Value() != "" || inst.settingsInput.Focused() {
		t.Fatalf("al confirmar, el campo de Ajustes debe quedar limpio y sin foco (editing=%v, valor=%q)",
			inst.editing, inst.settingsInput.Value())
	}
}

// Cambiar de pestaña descarta la edición y el selector abiertos, sin preparar
// ningún cambio.
func TestSettingsCambioDePestanaDescartaElFormulario(t *testing.T) {
	a, inst := settingsApp(t)
	inst.setCursor = settingIndex(inst.settingFields(), "motd")
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !inst.editing {
		t.Fatal("Enter debe iniciar la edición del MOTD")
	}
	a.Update(press("X"))
	inst.switchTab(1) // Ajustes → Resumen
	if inst.editing || inst.editKey != "" {
		t.Fatalf("cambiar de pestaña debe cerrar la edición; editing=%v key=%q", inst.editing, inst.editKey)
	}
	if inst.settingsInput.Value() != "" || inst.settingsInput.Focused() {
		t.Fatalf("el campo de Ajustes debe quedar limpio y sin foco, tengo %q", inst.settingsInput.Value())
	}
	if inst.pendingSave != nil {
		t.Fatal("abandonar la edición no debe preparar ningún cambio")
	}

	inst.tab = 8
	inst.setCursor = settingIndex(inst.settingFields(), "memory")
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !inst.selecting {
		t.Fatal("Enter debe abrir el selector de memoria")
	}
	inst.switchTab(1)
	if inst.selecting || inst.selectKey != "" || inst.selectCursor != 0 {
		t.Fatalf("cambiar de pestaña debe cerrar el selector; selecting=%v key=%q cursor=%d",
			inst.selecting, inst.selectKey, inst.selectCursor)
	}
}

// Con un selector abierto, la pestaña no desborda en los tamaños de referencia.
func TestSettingsSelectorSinDesborde(t *testing.T) {
	a, inst := settingsApp(t)
	inst.setCursor = settingIndex(inst.settingFields(), "gamemode")
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !inst.selecting {
		t.Fatal("Enter debe abrir el selector de modo de juego")
	}
	for _, sz := range [][2]int{{40, 12}, {70, 20}, {110, 30}, {200, 50}} {
		resize(a, sz[0], sz[1])
		inst.tab = 8
		assertWithinSize(t, stripANSI(a.View()), sz[0], sz[1])
	}

	// v1.2.1: en compacto (40×12) el foco debe seguir visible dentro del panel
	// aunque la ayuda de teclas se sacrifique: la opción elegida no puede quedar
	// fuera de la vista. Se comprueba sobre el ancho/alto reales del cuerpo.
	compact := stripANSI(inst.settingsView(36, 7))
	if !strings.Contains(compact, "Supervivencia") {
		t.Fatalf("en 40×12 la opción enfocada del selector debe verse:\n%s", compact)
	}
	if !strings.Contains(compact, components.Pointer(false)) {
		t.Fatalf("en 40×12 la opción enfocada debe llevar puntero:\n%s", compact)
	}
}

// En modo ASCII el selector sigue sin desbordar y sin dibujar cajas Unicode
// (§4.2): el puntero y la banda del panel son ASCII.
func TestSettingsSelectorASCII(t *testing.T) {
	opts := darkOpts()
	opts.ASCII = true
	a := NewApp(svc.NewFake([]*instance.Spec{goldenSpec()}), opts)
	resize(a, 100, 30)
	inst := openInstance(t, a, "survival")
	inst.tab = 8
	inst.setCursor = settingIndex(inst.settingFields(), "memory")
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !inst.selecting {
		t.Fatal("Enter debe abrir el selector de memoria en modo ASCII")
	}
	vista := stripANSI(inst.settingsView(96, 24))
	for _, r := range vista {
		if (r >= 0x2500 && r <= 0x257F) || (r >= 0x2580 && r <= 0x259F) {
			t.Fatalf("en ASCII el selector no debe dibujar cajas Unicode (%q):\n%s", r, vista)
		}
	}
	if !strings.Contains(vista, components.Pointer(true)) {
		t.Fatalf("la opción señalada debe usar el puntero ASCII:\n%s", vista)
	}
	assertWithinSize(t, stripANSI(a.View()), 100, 30)
}
