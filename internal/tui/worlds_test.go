package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tui/svc"
)

// --- WS-4: pestaña Mundos (semilla, biblioteca, regenerar y cargar) ---

func worldsEnter() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }
func worldsEsc() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyEsc} }
func worldsLeft() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyLeft} }

// worldsScreen abre el detalle en la pestaña Mundos con la biblioteca ya
// inyectada, tal y como llegaría por svc.Worlds.Library.
func worldsScreen(t *testing.T, lib []string) (*App, *instanceScreen, *svc.FakeWorlds) {
	t.Helper()
	a := NewApp(svc.NewFakeWith([]*instance.Spec{goldenSpec()}, svc.FakeOptions{WorldLibrary: lib}), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 4
	fw, ok := a.svc.Worlds.(*svc.FakeWorlds)
	if !ok {
		t.Fatalf("el servicio de mundos debe ser el fake, tengo %T", a.svc.Worlds)
	}
	inst.Update(worldsLibMsg{names: fw.LibraryNames()})
	return a, inst, fw
}

// worldsCalls filtra las llamadas registradas de una operación.
func worldsCalls(fw *svc.FakeWorlds, op string) []svc.WorldCall {
	var out []svc.WorldCall
	for _, c := range fw.Calls() {
		if c.Op == op {
			out = append(out, c)
		}
	}
	return out
}

// TestWorldsSemillaYBiblioteca: la vista muestra la semilla del Spec y los mundos
// de la biblioteca; la lectura se hace con un comando (nunca en View).
func TestWorldsSemillaYBiblioteca(t *testing.T) {
	a, inst, fw := worldsScreen(t, []string{"arena", "ciudad"})
	out := stripANSI(a.View())
	for _, want := range []string{"MUNDO", "Semilla: 42", "Biblioteca de mundos", "arena", "ciudad", "[G]", "[L]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("la pestaña Mundos debe mostrar %q:\n%s", want, out)
		}
	}
	if !inst.worldsLibLoaded || len(inst.worldsLib) != 2 {
		t.Fatalf("la biblioteca debe quedar cargada: loaded=%v %v", inst.worldsLibLoaded, inst.worldsLib)
	}
	// La lectura real se hace con un comando (nunca en View).
	if _, ok := ejecutaCmd(inst.worldsLibCmd()).(worldsLibMsg); !ok {
		t.Fatal("la lectura de la biblioteca debe cerrarse con worldsLibMsg")
	}
	if len(worldsCalls(fw, "library")) == 0 {
		t.Fatal("la biblioteca debe leerse a través del servicio")
	}
	if names := fw.LibraryNames(); len(names) != 2 || names[0] != "arena" {
		t.Fatalf("el fake debe conservar la biblioteca, tengo %v", names)
	}
}

// TestWorldsBibliotecaEstados: cargando, error y vacío se distinguen en el panel.
func TestWorldsBibliotecaEstados(t *testing.T) {
	a, inst, _ := worldsScreen(t, nil)
	if out := stripANSI(a.View()); !strings.Contains(out, "biblioteca vacía") {
		t.Fatalf("sin mundos debe verse el estado vacío con su acción:\n%s", out)
	}

	inst.worldsLibLoaded = false
	if out := stripANSI(a.View()); !strings.Contains(out, "cargando…") {
		t.Fatalf("sin respuesta todavía debe verse «cargando…»:\n%s", out)
	}

	inst.Update(worldsLibMsg{err: errors.New("disco no disponible")})
	if out := stripANSI(a.View()); !strings.Contains(out, "no se pudo leer la biblioteca") ||
		!strings.Contains(out, "disco no disponible") {
		t.Fatalf("el error de lectura debe verse en el panel:\n%s", out)
	}
	if len(inst.worldsLib) != 0 {
		t.Fatalf("con error no debe quedar lista previa, tengo %v", inst.worldsLib)
	}

	inst.Update(worldsLibMsg{names: []string{"arena"}})
	if inst.worldsLibErr != "" || !strings.Contains(stripANSI(a.View()), "arena") {
		t.Fatalf("una lectura correcta debe limpiar el error, tengo %q", inst.worldsLibErr)
	}
}

// TestWorldsRegenerarConConfirmacion: [G] pide la semilla, avisa de la pérdida de
// datos y solo regenera al confirmar el modal (el foco arranca en cancelar).
func TestWorldsRegenerarConConfirmacion(t *testing.T) {
	a, inst, fw := worldsScreen(t, nil)

	a.Update(press("g"))
	if !inst.prompt || inst.promptAct != "__regen__" {
		t.Fatalf("G debe abrir el prompt de semilla; prompt=%v act=%q", inst.prompt, inst.promptAct)
	}
	a.Update(press("4"))
	a.Update(press("2"))
	if _, cmd := a.Update(worldsEnter()); cmd != nil {
		t.Fatal("confirmar la semilla no debe operar todavía: falta el modal")
	}
	if inst.modal == nil || inst.modalKind != "world_regen" || inst.modalValue != "42" {
		t.Fatalf("debía abrirse el modal de regeneración: %+v", inst.modal)
	}
	if got := inst.modal.Actions[inst.modal.Focused].ID; got != "cancel" {
		t.Fatalf("el foco inicial del modal debe estar en cancelar, tengo %q", got)
	}
	out := stripANSI(a.View())
	for _, want := range []string{"se pierde el actual", "BORRARÁ", "Nether y End", "conservar"} {
		if !strings.Contains(out, want) {
			t.Fatalf("el modal debe avisar de la pérdida de datos (%q):\n%s", want, out)
		}
	}

	// Con el foco en cancelar, Enter cierra sin llamar al servicio.
	if _, cmd := a.Update(worldsEnter()); cmd != nil {
		t.Fatal("Enter con el foco en cancelar no debe regenerar")
	}
	if inst.modal != nil {
		t.Fatal("cancelar debe cerrar el modal")
	}
	if len(worldsCalls(fw, "regenerate")) != 0 {
		t.Fatalf("cancelar no debe llamar al servicio: %+v", fw.Calls())
	}

	// Segunda vuelta: se mueve el foco a confirmar y sí regenera.
	a.Update(press("g"))
	a.Update(press("4"))
	a.Update(press("2"))
	a.Update(worldsEnter())
	// ← recorre las acciones hacia la izquierda hasta «Sí, apagar y regenerar»
	// (el diálogo ofrece además el backup previo).
	for i := 0; i < len(inst.modal.Actions); i++ {
		if inst.modal != nil && inst.modal.Actions[inst.modal.Focused].ID == "confirm" {
			break
		}
		a.Update(worldsLeft())
	}
	if inst.modal == nil || inst.modal.Actions[inst.modal.Focused].ID != "confirm" {
		t.Fatalf("← debe llevar el foco a la acción de confirmar: %+v", inst.modal)
	}
	_, cmd := a.Update(worldsEnter())
	if cmd == nil {
		t.Fatal("confirmar debe lanzar la regeneración")
	}
	msg := ejecutaCmd(cmd)
	op, ok := msg.(worldsOpMsg)
	if !ok {
		t.Fatalf("se esperaba worldsOpMsg, tengo %T", msg)
	}
	calls := worldsCalls(fw, "regenerate")
	if len(calls) != 1 || calls[0].Slug != "survival" || calls[0].Seed != "42" {
		t.Fatalf("debía regenerarse «survival» con semilla 42, tengo %+v", calls)
	}
	if inst.modal != nil {
		t.Fatal("tras confirmar, el modal debe cerrarse")
	}

	// El desenlace se publica como toast y refresca detalle y biblioteca.
	_, after := a.Update(op)
	if after == nil {
		t.Fatal("el desenlace debe publicar un toast y refrescar los datos")
	}
	aviso, ok := ejecutaCmd(after).(toastMsg)
	if !ok || !strings.Contains(aviso.Text, "regenerado") || !strings.Contains(aviso.Text, "42") {
		t.Fatalf("el toast debía contar la regeneración, tengo %+v", aviso)
	}
}

// TestWorldsRegenerarSemillaVaciaEsAleatoria: un prompt vacío vale por semilla
// aleatoria (el resto de formularios lo trata como cancelación).
func TestWorldsRegenerarSemillaVaciaEsAleatoria(t *testing.T) {
	a, inst, fw := worldsScreen(t, nil)
	a.Update(press("g"))
	a.Update(worldsEnter())
	if inst.modal == nil || inst.modalKind != "world_regen" || inst.modalValue != "" {
		t.Fatalf("vacío debe valer por semilla aleatoria: %+v", inst.modal)
	}
	if !strings.Contains(stripANSI(a.View()), "una semilla") {
		t.Fatalf("el modal debe explicar que la semilla será aleatoria:\n%s", stripANSI(a.View()))
	}
	a.Update(worldsLeft())
	_, cmd := a.Update(worldsEnter())
	if cmd == nil {
		t.Fatal("confirmar debe regenerar con semilla aleatoria")
	}
	if _, ok := ejecutaCmd(cmd).(worldsOpMsg); !ok {
		t.Fatal("la regeneración debe cerrarse con worldsOpMsg")
	}
	calls := worldsCalls(fw, "regenerate")
	if len(calls) != 1 || calls[0].Seed != "" {
		t.Fatalf("la semilla debía quedar vacía (aleatoria), tengo %+v", calls)
	}
}

// TestWorldsRegenerarErrorAvisa: un fallo del servicio no deja el mundo a medias
// ni un estado silencioso: se avisa en el toast.
func TestWorldsRegenerarErrorAvisa(t *testing.T) {
	a := NewApp(svc.NewFakeWith([]*instance.Spec{goldenSpec()}, svc.FakeOptions{WorldErr: errors.New("sin espacio")}), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 4
	a.Update(press("g"))
	a.Update(worldsEnter())
	a.Update(worldsLeft())
	_, cmd := a.Update(worldsEnter())
	if cmd == nil {
		t.Fatal("confirmar debe intentar la regeneración")
	}
	msg := ejecutaCmd(cmd)
	_, after := a.Update(msg)
	if after == nil {
		t.Fatal("un error debe publicar un toast")
	}
	aviso, ok := ejecutaCmd(after).(toastMsg)
	if !ok || !strings.Contains(aviso.Text, "✖") || !strings.Contains(aviso.Text, "sin espacio") {
		t.Fatalf("el error debe verse en el toast, tengo %+v", aviso)
	}
}

// TestWorldsExploradorCargaZip: [L] abre el explorador, j/k mueven la selección,
// Enter en un .zip pide confirmación (pérdida de datos) y al confirmar importa
// con el nombre derivado del archivo.
func TestWorldsExploradorCargaZip(t *testing.T) {
	a, inst, fw := worldsScreen(t, nil)

	a.Update(press("l"))
	if !inst.browsing {
		t.Fatal("L debe abrir el explorador de archivos")
	}
	if !inst.CapturesInput() {
		t.Fatal("el explorador debe capturar las teclas")
	}
	if strings.TrimSpace(inst.browseDir) == "" {
		t.Fatal("el explorador necesita una carpeta inicial")
	}

	dir := filepath.Join("C:", "mundos")
	zip := filepath.Join(dir, "mundo.zip")
	inst.Update(worldsBrowseMsg{dir: dir, entries: []browseEntry{
		{Name: "otro", Path: filepath.Join(dir, "otro"), IsDir: true},
		{Name: "mundo.zip", Path: zip},
	}})
	if inst.browseErr != "" {
		t.Fatalf("la lista debía cargarse sin error, tengo %q", inst.browseErr)
	}
	out := stripANSI(a.View())
	for _, want := range []string{"CARGAR MUNDO", "Carpeta:", dir, "[dir] otro", "[zip] mundo.zip", "[Esc]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("el explorador debe mostrar %q:\n%s", want, out)
		}
	}

	// j/k mueven la selección y no se sale de la lista.
	a.Update(press("j"))
	if inst.browseCursor != 1 {
		t.Fatalf("j debe bajar la selección, tengo %d", inst.browseCursor)
	}
	a.Update(press("k"))
	a.Update(press("k"))
	if inst.browseCursor != 0 {
		t.Fatalf("k no debe salir de la lista, tengo %d", inst.browseCursor)
	}
	a.Update(press("j"))

	// Enter en el .zip no importa nada todavía.
	if _, cmd := a.Update(worldsEnter()); cmd != nil {
		t.Fatal("elegir el .zip no debe importar sin confirmar")
	}
	if inst.modal == nil || inst.modalKind != "world_import" {
		t.Fatalf("Enter en un .zip debe abrir el modal de importación: %+v", inst.modal)
	}
	if inst.modalValue != zip || inst.modalName != "mundo" {
		t.Fatalf("el modal debe recordar archivo y nombre: %q / %q", inst.modalValue, inst.modalName)
	}
	if inst.modal.Focused != 1 {
		t.Fatal("el foco inicial del modal de importación debe estar en cancelar")
	}
	if out := stripANSI(a.View()); !strings.Contains(out, "BORRARÁ") || !strings.Contains(out, "mundo") {
		t.Fatalf("el modal debe avisar de la pérdida de datos:\n%s", out)
	}

	// Confirmar importa con el nombre derivado del archivo.
	a.Update(worldsLeft())
	_, cmd := a.Update(worldsEnter())
	if cmd == nil {
		t.Fatal("confirmar debe importar el .zip")
	}
	op, ok := ejecutaCmd(cmd).(worldsOpMsg)
	if !ok {
		t.Fatal("la importación debe cerrarse con worldsOpMsg")
	}
	calls := worldsCalls(fw, "import")
	if len(calls) != 1 || calls[0].Slug != "survival" || calls[0].Archive != zip || calls[0].Name != "mundo" {
		t.Fatalf("debía importarse el .zip como «mundo», tengo %+v", calls)
	}
	if inst.modal != nil {
		t.Fatal("tras confirmar, el modal debe cerrarse")
	}

	// Al terminar bien, el explorador se cierra y la biblioteca se refresca.
	_, after := a.Update(op)
	if after == nil {
		t.Fatal("el desenlace debe publicar un toast y refrescar los datos")
	}
	if inst.browsing {
		t.Fatal("tras importar debe cerrarse el explorador")
	}
	aviso, ok := ejecutaCmd(after).(toastMsg)
	if !ok || !strings.Contains(aviso.Text, "mundo") {
		t.Fatalf("el toast debía contar la importación, tengo %+v", aviso)
	}
}

// TestWorldsExploradorNavegaYDescarta: entrar, subir y cerrar con Esc; una
// respuesta tardía no reabre el explorador.
func TestWorldsExploradorNavegaYDescarta(t *testing.T) {
	a, inst, _ := worldsScreen(t, nil)
	a.Update(press("l"))
	if !inst.browsing {
		t.Fatal("L debe abrir el explorador")
	}

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.zip"), []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notas.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	inst.Update(ejecutaCmd(inst.browseCmd(dir)).(worldsBrowseMsg))
	if len(inst.browseEntries) != 2 {
		t.Fatalf("solo se listan carpetas y .zip, tengo %+v", inst.browseEntries)
	}
	if !inst.browseEntries[0].IsDir || inst.browseEntries[0].Name != "sub" || inst.browseEntries[1].Name != "a.zip" {
		t.Fatalf("las carpetas van primero y el orden es alfabético: %+v", inst.browseEntries)
	}

	// Enter entra en la carpeta y lanza su lectura.
	_, cmd := a.Update(worldsEnter())
	if cmd == nil {
		t.Fatal("entrar en una carpeta debe leerla en un comando")
	}
	if inst.browseDir != sub {
		t.Fatalf("la carpeta actual debía ser %q, tengo %q", sub, inst.browseDir)
	}
	inst.Update(ejecutaCmd(cmd).(worldsBrowseMsg))
	if !inst.browsing || inst.browseErr != "" {
		t.Fatalf("la subcarpeta vacía debe mostrarse sin error: %q", inst.browseErr)
	}

	// ← y Retroceso suben al directorio padre; Esc cierra.
	a.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if inst.browseDir != dir {
		t.Fatalf("← debe subir al padre, tengo %q", inst.browseDir)
	}
	parent := filepath.Dir(dir)
	a.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if inst.browseDir != parent {
		t.Fatalf("← debe seguir subiendo, tengo %q", inst.browseDir)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if inst.browseDir != filepath.Dir(parent) {
		t.Fatalf("Retroceso debe subir al padre, tengo %q", inst.browseDir)
	}
	a.Update(worldsEsc())
	if inst.browsing {
		t.Fatal("Esc debe cerrar el explorador")
	}

	// Con el explorador cerrado, una respuesta tardía no lo reabre.
	inst.Update(worldsBrowseMsg{dir: dir, entries: []browseEntry{{Name: "a.zip", Path: filepath.Join(dir, "a.zip")}}})
	if inst.browsing || len(inst.browseEntries) != 0 {
		t.Fatal("una respuesta con el explorador cerrado debe descartarse")
	}

	// En la raíz del volumen no hay padre: ← cierra el explorador.
	a.Update(press("l"))
	root := filepath.VolumeName(dir) + string(os.PathSeparator)
	inst.browseDir = root
	a.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if inst.browsing {
		t.Fatal("en la raíz del volumen ← debe cerrar el explorador")
	}

	// Un fallo al leer una carpeta no cierra el explorador: se explica en el panel.
	a.Update(press("l"))
	inst.Update(worldsBrowseMsg{dir: filepath.Join(dir, "no-existe"), err: errors.New("acceso denegado")})
	if !inst.browsing {
		t.Fatal("un error de lectura no debe cerrar el explorador")
	}
	if out := stripANSI(a.View()); !strings.Contains(out, "no se pudo abrir la carpeta") || !strings.Contains(out, "acceso denegado") {
		t.Fatalf("el error de lectura debe verse en el panel:\n%s", out)
	}
	a.Update(worldsEsc())
}

// TestWorldsAccionesEInvoke: la barra y la paleta anuncian [G] y [L] y las
// invocan igual que las teclas.
func TestWorldsAccionesEInvoke(t *testing.T) {
	_, inst, _ := worldsScreen(t, nil)
	teclas := map[string]string{}
	for _, act := range inst.Actions() {
		teclas[act.ID] = act.Key
	}
	if teclas["instance.world.regen"] != "g" {
		t.Fatalf("Regenerar debe anunciarse con [g], tengo %q", teclas["instance.world.regen"])
	}
	if teclas["instance.world.load"] != "l" {
		t.Fatalf("Cargar debe anunciarse con [l], tengo %q", teclas["instance.world.load"])
	}

	// Invoke abre lo mismo que las teclas, desde cualquier pestaña.
	inst.tab = 0
	if cmd := inst.Invoke("instance.world.regen"); cmd == nil {
		t.Fatal("Invoke debe enfocar el campo de la semilla")
	}
	if inst.tab != 4 || !inst.prompt || inst.promptAct != "__regen__" {
		t.Fatalf("Invoke debía abrir el prompt en Mundos: tab=%d prompt=%v act=%q", inst.tab, inst.prompt, inst.promptAct)
	}
	inst.Update(worldsEsc())
	inst.tab = 0
	if cmd := inst.Invoke("instance.world.load"); cmd == nil {
		t.Fatal("Invoke debe lanzar la lectura del explorador")
	}
	if inst.tab != 4 || !inst.browsing {
		t.Fatalf("Invoke debía abrir el explorador en Mundos: tab=%d browsing=%v", inst.tab, inst.browsing)
	}
}

// TestWorldsSinDesborde: la pestaña Mundos y el explorador caben en los tamaños
// de referencia, con rutas y nombres largos incluidos.
func TestWorldsSinDesborde(t *testing.T) {
	for _, sz := range [][2]int{{40, 12}, {80, 24}, {110, 30}, {132, 40}} {
		w, h := sz[0], sz[1]
		a, inst, _ := worldsScreen(t, []string{"arena", "una-ciudad-con-un-nombre-bastante-largo"})
		resize(a, w, h)
		inst.tab = 4
		assertWithinSize(t, stripANSI(a.View()), w, h)

		a.Update(press("l"))
		inst.Update(worldsBrowseMsg{
			dir: filepath.Join("C:", "Users", "alguien", "Documentos", "mundos", "guardados", "de-minecraft"),
			entries: []browseEntry{
				{Name: "una-carpeta-con-nombre-larguisimo", Path: "x", IsDir: true},
				{Name: "un-mundo-con-un-nombre-larguisimo-para-probar-el-recorte.zip", Path: "y.zip"},
			},
		})
		assertWithinSize(t, stripANSI(a.View()), w, h)
	}
}

// TestWorldsSinDesbordeConSemillaVacia: sin semilla y sin biblioteca (los dos
// estados vacíos) tampoco se desborda ni esconden la acción.
func TestWorldsSinDesbordeConSemillaVacia(t *testing.T) {
	for _, sz := range [][2]int{{40, 12}, {80, 24}, {110, 30}, {132, 40}} {
		w, h := sz[0], sz[1]
		a, inst, _ := worldsScreen(t, nil)
		resize(a, w, h)
		inst.tab = 4
		inst.detail.Spec.Seed = ""
		out := stripANSI(a.View())
		assertWithinSize(t, out, w, h)
		if !strings.Contains(out, "sin semilla") {
			t.Fatalf("%dx%d: sin semilla debe explicarse el mundo aleatorio:\n%s", w, h, out)
		}
	}
}
