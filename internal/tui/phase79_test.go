package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/provision"
	"github.com/MrZudro/badomens/internal/tui/svc"
)

// ===========================================================================
// Fase 7 · Creación rápida y avanzada (WIZ-*)
// ===========================================================================

// wizScreen construye el asistente por la ruta de producción (App → Router → svc)
// con un catálogo determinista.
func wizScreen(t *testing.T, opts svc.FakeOptions, mode int) (*App, *wizardScreen) {
	t.Helper()
	a := NewApp(svc.NewFakeWith(nil, opts), darkOpts())
	resize(a, 120, 40)
	a.router.Push(newWizardScreen(a.svc, a.pal, a.ascii))
	wz, ok := a.router.Top().(*wizardScreen)
	if !ok {
		t.Fatal("se esperaba el asistente en la cima")
	}
	if cmd := wz.start(mode); cmd != nil {
		// La carga de versiones es asíncrona: se entrega su resultado para dejar el
		// catálogo listo, como haría el bucle real.
		a.Update(ejecutaCmd(cmd))
	}
	return a, wz
}

// WIZ-01 (UX-02/UX-03): ↓ NO cambia de paso y Enter sobre «Continuar a X →» sí.
func TestWIZ01FlechaAbajoNoCambiaDePaso(t *testing.T) {
	_, wz := wizScreen(t, svc.FakeOptions{IdleWatcherActive: true}, 0) // modo rápido
	if wz.step != 0 {
		t.Fatalf("el asistente arranca en el paso 0, tengo %d", wz.step)
	}
	// El primer campo es de texto y captura las teclas mientras se escribe: se
	// confirma con Enter (precedencia del campo con foco, P-06) y desde ahí ↓
	// recorre la lista de campos.
	if _, _ = wz.handleStepKey(tea.KeyMsg{Type: tea.KeyEnter}); wz.edit {
		t.Fatal("Enter debe confirmar el campo de texto y devolver el foco a la lista")
	}
	// ↓ hasta el final de la lista de campos: nunca cambia de paso.
	for i := 0; i < len(wz.steps[0].fields)+3; i++ {
		_, _ = wz.handleStepKey(press("down"))
		if wz.step != 0 {
			t.Fatalf("↓ no debe cambiar de paso (paso %d tras %d pulsaciones)", wz.step, i+1)
		}
	}
	last := len(wz.steps[0].fields) - 1
	if wz.cur != last {
		t.Fatalf("↓ debe detenerse en la última celda (%d), tengo %d", last, wz.cur)
	}
	if wz.steps[0].fields[last].kind != "continue" {
		t.Fatalf("la última celda del paso 0 debe ser la de continuación, tengo %q", wz.steps[0].fields[last].kind)
	}
	if got := wz.continueLabel(); !strings.Contains(got, "Continuar a Recursos") {
		t.Fatalf("la celda debe decir «Continuar a Recursos →», tengo %q", got)
	}
	if out := stripANSI(wz.View(106, 25)); !strings.Contains(out, "Continuar a Recursos →") {
		t.Fatalf("la celda de continuación debe verse en el paso:\n%s", out)
	}

	// Enter sobre la celda de continuación SÍ avanza de paso.
	_, cmd := wz.enterField()
	if wz.step != 1 {
		t.Fatalf("Enter sobre «Continuar» debe avanzar al paso 1, tengo %d", wz.step)
	}
	_ = cmd
	// Y ↑ vuelve a la lista sin cambiar de paso.
	_, _ = wz.handleStepKey(press("up"))
	if wz.step != 1 {
		t.Fatalf("↑ no debe cambiar de paso, tengo %d", wz.step)
	}
}

// WIZ-02 (UX-03): el panel lateral de versiones —Enter selecciona y cierra, Esc
// cierra sin cambiar y las 4 flechas navegan la rejilla.
func TestWIZ02PanelVersiones(t *testing.T) {
	opts := svc.FakeOptions{IdleWatcherActive: true, CatalogVersions: []engines.Version{
		{Version: "1.21.4", Channel: engines.ChannelRelease, Recommended: true, Compatible: true, Compatibility: engines.CompatibilityCompatible, JavaMajor: 21},
		{Version: "1.21.1", Channel: engines.ChannelRelease, Compatible: true, Compatibility: engines.CompatibilityCompatible, JavaMajor: 21},
		{Version: "1.20.6", Channel: engines.ChannelRelease, Compatible: true, Compatibility: engines.CompatibilityCompatible, JavaMajor: 21},
		{Version: "1.20.1", Channel: engines.ChannelRelease, Compatible: true, Compatibility: engines.CompatibilityCompatible, JavaMajor: 17},
	}}
	_, wz := wizScreen(t, opts, 1) // avanzado: paso 0 con campos + celda de continuación
	wz.cur = 2                     // campo Versión
	wz.pickingVer = true
	wz.pickerW, wz.pickerCols = 40, 2

	// El panel se pinta en el lateral (ancho) con las versiones del servicio.
	out := stripANSI(wz.View(120, 40))
	if !strings.Contains(out, "VERSIONES") || !strings.Contains(out, "1.21.4") {
		t.Fatalf("el panel de versiones debe verse en el lateral:\n%s", out)
	}
	// Se listan TODAS las versiones que devuelve el servicio (ni una menos).
	sel := wz.verSelect(40)
	if sel.Len() != len(opts.CatalogVersions) {
		t.Fatalf("la UI no debe filtrar versiones: %d en el selector, %d del servicio", sel.Len(), len(opts.CatalogVersions))
	}

	// Las 4 flechas navegan: → y ↓ cambian de versión, ← y ↑ vuelven.
	wz.handleVersionPicker(press("down"))
	if wz.verCursor == 0 {
		t.Fatal("↓ debe mover la selección en la rejilla")
	}
	down := wz.verCursor
	wz.handleVersionPicker(press("up"))
	if wz.verCursor != 0 {
		t.Fatalf("↑ debe volver a la primera fila, tengo %d", wz.verCursor)
	}
	wz.handleVersionPicker(press("right"))
	if wz.verCursor == 0 {
		t.Fatal("→ debe avanzar una columna en la rejilla")
	}
	wz.handleVersionPicker(press("left"))
	if wz.verCursor != 0 {
		t.Fatalf("← debe volver a la primera columna, tengo %d", wz.verCursor)
	}
	wz.verCursor = down

	// Esc cierra sin cambiar la versión elegida.
	antes := wz.versionValue()
	wz.handleVersionPicker(press("down"))
	wz.handleVersionPicker(tea.KeyMsg{Type: tea.KeyEsc})
	if wz.pickingVer {
		t.Fatal("Esc debe cerrar el panel de versiones")
	}
	if got := wz.versionValue(); got != antes {
		t.Fatalf("Esc no debe cambiar la versión elegida (%q → %q)", antes, got)
	}

	// Enter selecciona y cierra, guardando el VALOR señalado.
	wz.cur = 2
	wz.pickingVer = true
	wz.verCursor = 0
	wz.moveVerCursor(0, 1) // baja una fila
	selNow := wz.verSelect(40)
	selNow.SetCursor(wz.verCursor)
	want, _ := selNow.Selected()
	_, cmd := wz.handleVersionPicker(tea.KeyMsg{Type: tea.KeyEnter})
	if wz.pickingVer {
		t.Fatal("Enter debe cerrar el panel de versiones")
	}
	if got := wz.versionValue(); got != want.Value {
		t.Fatalf("Enter debe guardar el VALOR señalado (%q), tengo %q", want.Value, got)
	}
	_ = cmd
}

// WIZ-03 (§2.2): un fallo de carga de versiones muestra error con reintento y la
// etiqueta de caché vencida; nunca una lista vacía silenciosa.
func TestWIZ03FalloDeCargaConReintento(t *testing.T) {
	opts := svc.FakeOptions{
		IdleWatcherActive: true,
		CatalogListErr:    errors.New("el catálogo no está disponible (timeout)"),
		CatalogStale:      true,
		CatalogWarning:    "caché vencida hace 3 días",
	}
	_, wz := wizScreen(t, opts, 0)
	wz.pickingVer = true
	wz.pickerW = 40
	out := stripANSI(strings.Join(wz.pickerBody(40, 12), "\n"))
	for _, want := range []string{"no se pudieron cargar las versiones", "Reintentar"} {
		if !strings.Contains(out, want) {
			t.Fatalf("el fallo debe verse con reintento (%q):\n%s", want, out)
		}
	}
	if len(wz.versions) != 0 {
		t.Fatalf("sin respuesta válida no debe quedar una lista a medias: %v", wz.versions)
	}
	if !strings.Contains(out, "stale") {
		t.Fatalf("debe etiquetarse la caché vencida (stale):\n%s", out)
	}
	// Un panel vacío no es una lista vacía silenciosa: hay texto y una acción.
	if strings.Contains(out, "(sin versiones; se usará el catálogo embebido)") {
		t.Fatalf("no debe sugerirse un catálogo embebido silencioso:\n%s", out)
	}
}

// WIZ-04 (AD-02/§3.2): al cambiar de motor se resetea la versión, se recarga la
// lista válida y la resolución anterior se descarta.
func TestWIZ04CambioDeMotorReseteaVersionYResolucion(t *testing.T) {
	_, wz := wizScreen(t, svc.FakeOptions{IdleWatcherActive: true}, 0)
	if wz.versionValue() != "1.21.4" {
		t.Fatalf("la versión por defecto debe ser la recomendada, tengo %q", wz.versionValue())
	}
	// Resolución vigente para paper.
	res := engines.Resolved{Version: "1.21.4", Pins: map[string]string{"PAPER_BUILD": "123"}, Source: "paper"}
	wz.resolution, wz.resEngine, wz.resVersion = &res, "paper", "1.21.4"

	wz.cur = 1 // campo Motor
	cmd := wz.cycle(1)
	if wz.engine.Name != "fabric" {
		t.Fatalf("el motor debe ser fabric, tengo %q", wz.engine.Name)
	}
	if wz.versionValue() != "" {
		t.Fatalf("al cambiar de motor la versión debe resetearse, tengo %q", wz.versionValue())
	}
	if wz.resolution != nil {
		t.Fatal("al cambiar de motor la resolución anterior debe descartarse (AD-02)")
	}
	if cmd == nil {
		t.Fatal("cambiar de motor debe recargar la lista de versiones")
	}
	wz.Update(cmd())
	if wz.versionValue() != "1.21.4" {
		t.Fatalf("la lista nueva debe dejar elegida su recomendada, tengo %q", wz.versionValue())
	}
	// Retroceder con Esc hasta el primer paso conserva motor y versión elegidos.
	wz.step = 1
	_, _ = wz.handleStepKey(tea.KeyMsg{Type: tea.KeyEsc})
	if wz.engine.Name != "fabric" {
		t.Fatalf("Esc no debe cambiar el motor, tengo %q", wz.engine.Name)
	}
	if wz.versionValue() != "1.21.4" {
		t.Fatalf("Esc no debe cambiar la versión, tengo %q", wz.versionValue())
	}
}

// WIZ-05 (AD-02/UX-05): recorrer combinaciones motor/versión, cambiar de motor,
// retroceder con Esc, avanzar y comprobar que la config FINAL coincide con la
// selección (y con el resumen mostrado).
func TestWIZ05ConfigFinalCoincideConLaSeleccion(t *testing.T) {
	combos := []struct {
		engine  string
		version string
	}{
		{"paper", "1.20.1"},
		{"fabric", "1.21.1"},
		{"vanilla", "1.20.1"},
	}
	catálogo := []engines.Version{
		{Version: "1.21.4", Channel: engines.ChannelRelease, Recommended: true, Compatible: true, Compatibility: engines.CompatibilityCompatible, JavaMajor: 21},
		{Version: "1.21.1", Channel: engines.ChannelRelease, Compatible: true, Compatibility: engines.CompatibilityCompatible, JavaMajor: 21},
		{Version: "1.20.1", Channel: engines.ChannelRelease, Compatible: true, Compatibility: engines.CompatibilityCompatible, JavaMajor: 17},
	}
	for _, c := range combos {
		a, wz := wizScreen(t, svc.FakeOptions{IdleWatcherActive: true, CatalogVersions: catálogo}, 0)
		// Nombre: se escribe y se confirma con Enter (la ruta real del formulario).
		wz.cur = 0
		wz.enterField()
		for _, r := range "mi-servidor" {
			a.Update(press(string(r)))
		}
		a.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if got := wz.nameValue(); got != "mi-servidor" {
			t.Fatalf("el nombre debe quedar confirmado, tengo %q", got)
		}
		// Motor: ciclar hasta el pedido.
		wz.cur = 1
		for i := 0; i < len(engines.Order); i++ {
			if wz.engine.Name == c.engine {
				break
			}
			if cmd := wz.cycle(1); cmd != nil {
				a.Update(ejecutaCmd(cmd))
			}
		}
		if wz.engine.Name != c.engine {
			t.Fatalf("no se pudo elegir el motor %q, tengo %q", c.engine, wz.engine.Name)
		}
		// Versión: elegirla en el panel por VALOR.
		wz.cur = 2
		wz.pickingVer = true
		wz.pickerW = 40
		sel := wz.verSelect(40)
		if !sel.SetValue(c.version) {
			t.Fatalf("%s: la versión %q debe estar en el panel", c.engine, c.version)
		}
		wz.verCursor = sel.Cursor()
		wz.handleVersionPicker(tea.KeyMsg{Type: tea.KeyEnter})
		if got := wz.versionValue(); got != c.version {
			t.Fatalf("%s: versión elegida %q, quiero %q", c.engine, got, c.version)
		}
		// Retroceder con Esc y volver a avanzar: la selección se conserva.
		if cmd := wz.nextStep(); cmd != nil {
			a.Update(ejecutaCmd(cmd))
		}
		_, _ = wz.handleStepKey(tea.KeyMsg{Type: tea.KeyEsc})
		if wz.versionValue() != c.version || wz.engine.Name != c.engine {
			t.Fatalf("%s: Esc no debe perder la selección (%q/%q)", c.engine, wz.engine.Name, wz.versionValue())
		}
		// Avanzar hasta el resumen entregando las resoluciones que pida.
		for i := 0; i < 20 && wz.step != len(wz.steps)-1; i++ {
			_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyTab})
			if cmd != nil {
				a.Update(ejecutaCmd(cmd))
			}
		}
		if wz.step != len(wz.steps)-1 {
			t.Fatalf("%s: no se llegó al resumen", c.engine)
		}
		if wz.resolution == nil {
			t.Fatalf("%s: el resumen necesita la resolución del servicio (AD-02)", c.engine)
		}
		if wz.resolution.Version != c.version {
			t.Fatalf("%s: el resumen muestra la versión %q, quiero %q", c.engine, wz.resolution.Version, c.version)
		}
		// El resumen declara motor, versión, pines y compatibilidad.
		out := stripANSI(wz.View(106, 30))
		for _, want := range []string{"Resolución del servicio de catálogo", c.version, "Compatibilidad", "Pines"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%s: el resumen debe mostrar %q:\n%s", c.engine, want, out)
			}
		}
		// Crear y comparar la configuración persistida con la selección.
		_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatalf("%s: Enter en el resumen debe crear", c.engine)
		}
		msg := ejecutaCmd(cmd)
		created, ok := msg.(wizardCreatedMsg)
		if !ok {
			t.Fatalf("%s: mensaje inesperado %T", c.engine, msg)
		}
		if created.err != nil {
			t.Fatalf("%s: creación: %v", c.engine, created.err)
		}
		if created.mismatch != "" {
			t.Fatalf("%s: la config guardada no coincide con el resumen: %s", c.engine, created.mismatch)
		}
		d, err := a.svc.Instances.Detail(context.Background(), created.slug)
		if err != nil {
			t.Fatalf("%s: detalle tras crear: %v", c.engine, err)
		}
		if d.Spec.Engine != c.engine || d.Spec.Version != c.version {
			t.Fatalf("%s: se creó %s %s (quiero %s %s)", c.engine, d.Spec.Engine, d.Spec.Version, c.engine, c.version)
		}
		if eng, ok := engines.ByName(c.engine); ok && eng.DepVar != "" && d.Spec.Pins[eng.DepVar] == "" {
			t.Fatalf("%s: la config debe guardar los pins del motor: %v", c.engine, d.Spec.Pins)
		}
	}
}

// WIZ-06 (T-08/AD-02): si la configuración persistida NO coincide con el resumen,
// no se informa de un éxito silencioso.
func TestWIZ06DiscrepanciaConElResumenSeAvisa(t *testing.T) {
	s := svc.NewFakeWith(nil, svc.FakeOptions{IdleWatcherActive: true})
	// El «servicio» persiste otro motor del que se revisó: el aviso debe salir.
	s.Creator = persistenciaDivergente{s.Creator}
	a := NewApp(s, darkOpts())
	resize(a, 120, 40)
	a.router.Push(newWizardScreen(a.svc, a.pal, a.ascii))
	wz, _ := a.router.Top().(*wizardScreen)
	if cmd := wz.start(0); cmd != nil {
		a.Update(ejecutaCmd(cmd))
	}
	wz.cur = 0
	wz.enterField()
	for _, r := range "divergente" {
		a.Update(press(string(r)))
	}
	for i := 0; i < 20 && wz.step != len(wz.steps)-1; i++ {
		_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyTab})
		if cmd != nil {
			a.Update(ejecutaCmd(cmd))
		}
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := ejecutaCmd(cmd)
	created, ok := msg.(wizardCreatedMsg)
	if !ok {
		t.Fatalf("mensaje inesperado: %T", msg)
	}
	if created.mismatch == "" {
		t.Fatal("una configuración distinta del resumen debe avisarse (T-08)")
	}
	a.Update(created)
	aviso := ""
	for _, tt := range a.toasts {
		aviso += tt.Text + "\n"
	}
	if !strings.Contains(aviso, "no coincide con el resumen") {
		t.Fatalf("el aviso debe explicar la discrepancia, tengo %q", aviso)
	}
}

// persistenciaDivergente envuelve el creador para devolver un spec con OTRO motor
// (simula la clase de fallo que causa AD-02).
type persistenciaDivergente struct{ svc.Creator }

func (p persistenciaDivergente) Create(ctx context.Context, req provision.Request) (svc.CreateOutcome, error) {
	out, err := p.Creator.Create(ctx, req)
	if err != nil {
		return out, err
	}
	if out.Spec != nil {
		out.Spec.Engine = "vanilla"
		out.Spec.Pins = nil
	}
	return out, nil
}

// WIZ-07 (§2.2/requisito 3): el atajo [R] del panel fuerza el refresco del
// catálogo (catalog refresh) y limpia la etiqueta de caché vencida.
func TestWIZ07RefrescoDeCatalogoDesdeElPanel(t *testing.T) {
	cat := &countingCatalog{
		calls: map[string]int{},
		items: map[string][]engines.Version{
			"paper|false": {{Version: "1.21.4", Channel: engines.ChannelRelease, Recommended: true, Compatible: true}},
		},
	}
	s := svc.NewFakeWith(nil, svc.FakeOptions{IdleWatcherActive: true, CatalogStale: true, CatalogWarning: "caché vencida"})
	s.Catalog = cat
	_, wz := wizScreen(t, svc.FakeOptions{IdleWatcherActive: true}, 0)
	wz.svc = s
	wz.verStale, wz.verWarning = true, "caché vencida"
	if note := wz.verNote(); !strings.Contains(note, "stale") {
		t.Fatalf("la etiqueta de caché vencida debe verse, tengo %q", note)
	}
	wz.pickingVer = true
	_, cmd := wz.handleVersionPicker(press("R"))
	if cmd == nil {
		t.Fatal("[R] debe forzar el refresco del catálogo")
	}
	if !wz.verLoading {
		t.Fatal("el refresco debe dejar el panel cargando")
	}
	wz.Update(ejecutaCmd(cmd))
	cat.mu.Lock()
	refrescos := cat.refreshes
	cat.mu.Unlock()
	if refrescos != 1 {
		t.Fatalf("el atajo debe pedir un refresco del catálogo, tengo %d", refrescos)
	}
	if note := wz.verNote(); strings.Contains(note, "stale") {
		t.Fatalf("tras refrescar, la etiqueta de caché vencida debe desaparecer, tengo %q", note)
	}
	if wz.verErr != "" {
		t.Fatalf("el refresco no debe dejar error, tengo %q", wz.verErr)
	}
}

// WIZ-08 (§7/AD-02): una resolución TARDÍA de un motor/versión anterior no pisa la
// selección vigente.
func TestWIZ08ResolucionTardiaSeDescarta(t *testing.T) {
	_, wz := wizScreen(t, svc.FakeOptions{IdleWatcherActive: true}, 0)
	wz.cur = 1
	if cmd := wz.cycle(1); cmd != nil { // paper → fabric: la resolución anterior queda obsoleta
		wz.Update(cmd())
	}
	vigente := wz.resSeq
	tardia := wizardResolvedMsg{
		engine: "paper", version: "1.21.4", seq: vigente - 1,
		res: engines.Resolved{Version: "1.20.1", Pins: map[string]string{"PAPER_BUILD": "1"}, Source: "paper"},
	}
	wz.Update(tardia)
	if wz.resolution != nil {
		t.Fatalf("una resolución tardía de otro motor no debe aplicarse: %+v", wz.resolution)
	}
	// Una respuesta de otro MOTOR con la secuencia vigente tampoco.
	wz.Update(wizardResolvedMsg{
		engine: "paper", version: wz.versionValue(), seq: vigente,
		res: engines.Resolved{Version: "1.20.1", Source: "paper"},
	})
	if wz.resolution != nil {
		t.Fatal("una resolución de otro motor no debe aplicarse")
	}
	// La vigente sí.
	wz.Update(wizardResolvedMsg{
		engine: "fabric", version: wz.versionValue(), seq: vigente,
		res: engines.Resolved{Version: "1.21.4", Pins: map[string]string{"FABRIC_LOADER_VERSION": "0.16"}, Source: "fabric"},
	})
	if wz.resolution == nil || wz.resEngine != "fabric" {
		t.Fatalf("la resolución vigente debe aplicarse: %+v (%q)", wz.resolution, wz.resEngine)
	}
}

// WIZ-09 (requisito 5): la UI lista TODAS las versiones del servicio; solo marca
// como no elegible lo que el servicio declara incompatible.
func TestWIZ09LaUINoFiltraVersionesDelServicio(t *testing.T) {
	opts := svc.FakeOptions{IdleWatcherActive: true, CatalogVersions: []engines.Version{
		{Version: "1.21.4", Channel: engines.ChannelRelease, Recommended: true, Compatible: true, Compatibility: engines.CompatibilityCompatible},
		{Version: "1.21.5", Channel: engines.ChannelRelease, Compatibility: engines.CompatibilityIncompatible, IncompatibleWhy: "sin artefacto descargable"},
		{Version: "26.2-snapshot", Channel: engines.ChannelSnapshot, Compatibility: engines.CompatibilityUnknown},
	}}
	_, wz := wizScreen(t, opts, 0)
	wz.showSNAPS = true
	wz.verSelect(70)
	sel := wz.verSelect(70)
	if sel.Len() != 3 {
		t.Fatalf("deben listarse las 3 versiones del servicio, tengo %d", sel.Len())
	}
	incompatible, _ := sel.Selected()
	if incompatible.Value != "1.21.4" {
		t.Fatalf("el cursor arranca en la primera opción, tengo %q", incompatible.Value)
	}
	sel.SetValue("1.21.5")
	it, _ := sel.Selected()
	if !it.Disabled || !strings.Contains(it.DisabledWhy, "artefacto") {
		t.Fatalf("la incompatible debe explicarse, no ocultarse: %+v", it)
	}
	sel.SetValue("26.2-snapshot")
	it, _ = sel.Selected()
	if it.Disabled {
		t.Fatalf("una versión sin verificar no debe bloquearse por la UI: %+v", it)
	}
	if !strings.Contains(it.Detail, "sin verificar") {
		t.Fatalf("la falta de verificación debe verse en el detalle: %q", it.Detail)
	}
}

// capturaPins envuelve un Creator para registrar los pins que viajan con la
// petición de creación (AD-02).
type capturaPins struct {
	svc.Creator
	got map[string]string
}

func (c *capturaPins) Create(ctx context.Context, req provision.Request) (svc.CreateOutcome, error) {
	c.got = req.Pins
	return c.Creator.Create(ctx, req)
}

// PIN-01 (AD-02/T-08, hueco P0 de la Fase 7): el asistente inyecta en la petición
// de creación los PINS EXACTOS del objeto Resolved del servicio, no solo motor y
// versión, de modo que la configuración persistida coincide con el resumen aunque
// el catálogo publique un build nuevo entre la resolución y la creación.
func TestPIN01InyeccionDePinsDelResumen(t *testing.T) {
	s := svc.NewFakeWith(nil, svc.FakeOptions{IdleWatcherActive: true})
	cap := &capturaPins{Creator: s.Creator}
	s.Creator = cap
	a := NewApp(s, darkOpts())
	resize(a, 120, 40)
	a.router.Push(newWizardScreen(a.svc, a.pal, a.ascii))
	wz, _ := a.router.Top().(*wizardScreen)
	if cmd := wz.start(0); cmd != nil {
		a.Update(ejecutaCmd(cmd))
	}
	wz.cur = 0
	wz.enterField()
	for _, r := range "pins" {
		a.Update(press(string(r)))
	}
	for i := 0; i < 20 && wz.step != len(wz.steps)-1; i++ {
		_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyTab})
		if cmd != nil {
			a.Update(ejecutaCmd(cmd))
		}
	}
	if wz.resolution == nil || len(wz.resolution.Pins) == 0 {
		t.Fatalf("el resumen debe tener una resolución con pins: %+v", wz.resolution)
	}
	want := map[string]string{}
	for k, v := range wz.resolution.Pins {
		want[k] = v
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := ejecutaCmd(cmd)
	created, ok := msg.(wizardCreatedMsg)
	if !ok {
		t.Fatalf("mensaje inesperado: %T", msg)
	}
	if created.err != nil {
		t.Fatalf("creación: %v", created.err)
	}
	if created.mismatch != "" {
		t.Fatalf("la config persistida no debe discrepar del resumen: %s", created.mismatch)
	}
	if len(cap.got) == 0 {
		t.Fatal("la petición de creación debe llevar los pins del Resolved (AD-02)")
	}
	for k, v := range want {
		if cap.got[k] != v {
			t.Fatalf("la petición debe inyectar el pin %s=%q, tengo %q (mapa %v)", k, v, cap.got[k], cap.got)
		}
	}
	d, err := a.svc.Instances.Detail(context.Background(), created.slug)
	if err != nil {
		t.Fatalf("detalle tras crear: %v", err)
	}
	for k, v := range want {
		if d.Spec.Pins[k] != v {
			t.Fatalf("el spec persistido debe guardar el pin %s=%q, tengo %q", k, v, d.Spec.Pins[k])
		}
	}
}
