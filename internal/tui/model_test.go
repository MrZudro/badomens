package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/manuelserver/badomens/internal/backup"
	"github.com/manuelserver/badomens/internal/instance"
)

// fakeBackend implementa Backend sin Docker para los tests del modelo.
type fakeBackend struct {
	maxParallel  int
	maxInstances int

	rows    []Row
	running int
	listErr error

	detail    *Detail
	detailErr error

	logs    string
	logsErr error

	backups []backup.Info

	health map[string]string

	ops   map[string]error // slug -> error devuelto por cualquier operación
	calls []string         // registro de operaciones
}

func newFake() *fakeBackend {
	return &fakeBackend{
		maxParallel:  3,
		maxInstances: 5,
		ops:          map[string]error{},
	}
}

func (f *fakeBackend) Config() (int, int) { return f.maxParallel, f.maxInstances }

func (f *fakeBackend) ListRows(context.Context) ([]Row, int, error) {
	f.calls = append(f.calls, "list")
	return f.rows, f.running, f.listErr
}

func (f *fakeBackend) Health(context.Context) (map[string]string, error) { return f.health, nil }

func (f *fakeBackend) Detail(_ context.Context, slug string) (*Detail, error) {
	f.calls = append(f.calls, "detail:"+slug)
	if f.detailErr != nil {
		return nil, f.detailErr
	}
	if f.detail != nil {
		return f.detail, nil
	}
	return &Detail{Spec: rowSpec(slug), State: "running"}, nil
}

func (f *fakeBackend) Logs(context.Context, string, int) (string, error) { return f.logs, f.logsErr }

func (f *fakeBackend) Backups(context.Context, string) ([]backup.Info, error) {
	return f.backups, nil
}

func (f *fakeBackend) Start(_ context.Context, slug string, _ bool) error {
	return f.record("start:" + slug)
}

func (f *fakeBackend) Stop(_ context.Context, slug string) error { return f.record("stop:" + slug) }

func (f *fakeBackend) Close(_ context.Context, slug string) error { return f.record("close:" + slug) }

func (f *fakeBackend) Restart(_ context.Context, slug string) error {
	return f.record("restart:" + slug)
}

func (f *fakeBackend) CreateBackup(_ context.Context, slug string) error {
	return f.record("backup:" + slug)
}

func (f *fakeBackend) PruneBackups(_ context.Context, slug string, _ int) ([]string, error) {
	_ = f.record("prune:" + slug)
	return []string{"viejo.zip"}, nil
}

func (f *fakeBackend) Console(_ context.Context, slug, command string) error {
	return f.record("console:" + slug + ":" + command)
}

func (f *fakeBackend) record(op string) error {
	f.calls = append(f.calls, op)
	return f.ops[op]
}

func rowSpec(slug string) *instance.Spec {
	return &instance.Spec{Slug: slug, Name: "N " + slug, Engine: "paper", Version: "1.21.6",
		Memory: "4G", Port: 25565, Difficulty: "normal", Gamemode: "survival", MaxPlayers: 20}
}

func sampleRows() []Row {
	return []Row{
		{Slug: "alpha", Name: "Alpha", Engine: "paper", Version: "1.21.6", Memory: "4G", Port: 25565, State: "running", MaxPlayers: 20},
		{Slug: "beta", Name: "Beta", Engine: "fabric", Version: "1.21.1", Memory: "5G", Port: 25566, State: "exited", MaxPlayers: 10},
		{Slug: "gamma", Name: "Gamma", Engine: "vanilla", Version: "1.21", Memory: "6G", Port: 25567, State: "", OnlineMode: true},
	}
}

// tecla construye un KeyMsg de caracteres normales.
func tecla(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// upd ejecuta Update y devuelve el modelo tipado + el comando.
func upd(m Model, msg tea.Msg) (Model, tea.Cmd) {
	mm, cmd := m.Update(msg)
	return mm.(Model), cmd
}

// nuevoTestModel construye un modelo con filas ya cargadas (salta Init).
func nuevoTestModel(t *testing.T, f *fakeBackend) Model {
	t.Helper()
	if f.rows == nil {
		f.rows = sampleRows()
		f.running = 1
	}
	m, err := NewModel(f)
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := upd(m, rowsMsg{rows: f.rows, running: f.running})
	if cmd == nil {
		t.Fatal("el refresco inicial debe programar el siguiente")
	}
	return m
}

// ejecuta un tea.Cmd y devuelve el mensaje producido.
func ejecuta(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("esperaba un comando no nulo")
	}
	return cmd()
}

func TestNuevoModelAplicaLimites(t *testing.T) {
	f := newFake()
	m, err := NewModel(f)
	if err != nil {
		t.Fatal(err)
	}
	if m.maxParallel != 3 || m.maxInstances != 5 {
		t.Fatalf("límites: quiero 3/5, tengo %d/%d", m.maxParallel, m.maxInstances)
	}
	if m.mode != modeList {
		t.Fatalf("modo inicial: quiero lista, tengo %d", m.mode)
	}
}

func TestRefrescoCargaFilas(t *testing.T) {
	f := newFake()
	m, _ := NewModel(f)
	m, cmd := upd(m, rowsMsg{rows: sampleRows(), running: 1})
	if cmd == nil {
		t.Fatal("el refresco debe programar el siguiente tick")
	}
	if len(m.rows) != 3 || m.running != 1 {
		t.Fatalf("filas: quiero 3 y 1 corriendo, tengo %d/%d", len(m.rows), m.running)
	}
}

func TestMovimientoCursor(t *testing.T) {
	m := nuevoTestModel(t, newFake())
	if got := m.rows[m.cursor].Slug; got != "alpha" {
		t.Fatalf("cursor inicial: quiero alpha, tengo %s", got)
	}
	for _, key := range []string{"j", "j", "down", "up"} {
		var msg tea.Msg = tecla(key)
		if key == "down" {
			msg = tea.KeyMsg{Type: tea.KeyDown}
		}
		if key == "up" {
			msg = tea.KeyMsg{Type: tea.KeyUp}
		}
		m, _ = upd(m, msg)
	}
	// j, j, down, up: índices 1,2,0,2 -> gamma (se envuelve por debajo de 0)
	if got := m.rows[m.cursor].Slug; got != "gamma" {
		t.Fatalf("cursor tras movimientos: quiero gamma, tengo %s", got)
	}
}

func TestOpcionesDespachanOperacion(t *testing.T) {
	f := newFake()
	m := nuevoTestModel(t, f)

	cases := []struct {
		key  string
		want string
	}{
		{"s", "start:alpha"},
		{"p", "stop:alpha"},
		{"c", "close:alpha"},
		{"r", "restart:alpha"},
	}
	for _, tc := range cases {
		f.calls = nil
		m, cmd := upd(m, tecla(tc.key))
		done := ejecuta(t, cmd).(opDoneMsg)
		if done.err != nil {
			t.Fatalf("%s: error inesperado %v", tc.key, done.err)
		}
		if len(f.calls) != 1 || f.calls[0] != tc.want {
			t.Fatalf("%s: quiero llamada %q, tengo %v", tc.key, tc.want, f.calls)
		}
		m, _ = upd(m, done)
		if _, busy := m.busy["alpha"]; busy {
			t.Fatalf("%s: alpha debe dejar de estar ocupada tras el done", tc.key)
		}
		if m.status == "" {
			t.Fatalf("%s: quiero un estado en la barra tras terminar", tc.key)
		}
	}
}

func TestNoDespachaSiOcupada(t *testing.T) {
	f := newFake()
	m := nuevoTestModel(t, f)
	m, cmd := upd(m, tecla("s"))
	if cmd == nil {
		t.Fatal("la primera 's' debe despachar start")
	}
	before := len(f.calls)
	m, cmd = upd(m, tecla("s"))
	if cmd != nil {
		t.Fatal("con la instancia ocupada no se debe despachar otra operación")
	}
	if len(f.calls) != before {
		t.Fatalf("no deben registrar más llamadas con instancia ocupada: %v", f.calls)
	}
}

func TestErrorDeOperacionVaALaBarra(t *testing.T) {
	f := newFake()
	f.ops["start:alpha"] = context.DeadlineExceeded
	m := nuevoTestModel(t, f)
	m, cmd := upd(m, tecla("s"))
	done := ejecuta(t, cmd).(opDoneMsg)
	m, _ = upd(m, done)
	if m.errText == "" {
		t.Fatal("quiero el error visible en la barra de estado")
	}
	if m.status != "" {
		t.Fatal("tras un error, el estado de éxito debe quedar vacío")
	}
	if _, busy := m.busy["alpha"]; busy {
		t.Fatal("la instancia debe liberarse aunque la operación falle")
	}
}

func TestAbrirDetalle(t *testing.T) {
	f := newFake()
	m := nuevoTestModel(t, f)
	m, cmd := upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter debe lanzar la carga del detalle")
	}
	msg := ejecuta(t, cmd).(detailMsg)
	m, _ = upd(m, msg)
	if m.mode != modeDetail {
		t.Fatalf("quiero modo detalle, tengo %d", m.mode)
	}
	if m.detail == nil || m.detail.Spec.Slug != "alpha" {
		t.Fatalf("detalle: quiero alpha, tengo %+v", m.detail)
	}
	if !strings.Contains(m.View(), "Instancia alpha") {
		t.Fatal("la vista de detalle debe incluir el nombre de la instancia")
	}
}

func TestErrorDeDetalleVuelveALista(t *testing.T) {
	f := newFake()
	f.detailErr = errors.New("la instancia no existe")
	m := nuevoTestModel(t, f)
	m, cmd := upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	msg := ejecuta(t, cmd).(detailMsg)
	m, _ = upd(m, msg)
	if m.mode != modeList {
		t.Fatalf("con error de detalle quiero volver a la lista, tengo %d", m.mode)
	}
	if m.errText == "" {
		t.Fatal("quiero el error de detalle visible")
	}
}

func TestConsolaEnviaComando(t *testing.T) {
	f := newFake()
	m := nuevoTestModel(t, f)
	m, cmd := upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = upd(m, ejecuta(t, cmd).(detailMsg))
	m, cmd = upd(m, tecla("e"))
	if m.mode != modeConsole {
		t.Fatalf("con 'e' quiero modo consola, tengo %d", m.mode)
	}
	for _, r := range "say hi" {
		m, _ = upd(m, tecla(string(r)))
	}
	m, cmd = upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter debe enviar el comando a la consola")
	}
	done := ejecuta(t, cmd).(opDoneMsg)
	if done.err != nil {
		t.Fatalf("console: %v", done.err)
	}
	found := false
	for _, c := range f.calls {
		if strings.HasPrefix(c, "console:alpha:say hi") {
			found = true
		}
	}
	if !found {
		t.Fatalf("quiero el comando registrado, tengo %v", f.calls)
	}
	if m.mode != modeDetail {
		t.Fatalf("tras enviar quiero volver al detalle, tengo %d", m.mode)
	}
}

func TestBackupsYBackupCreate(t *testing.T) {
	f := newFake()
	f.backups = []backup.Info{{Name: "a.zip", Size: 1234, Sha256: "abc", Created: "2026-01-01"}}
	m := nuevoTestModel(t, f)
	m, cmd := upd(m, tecla("b"))
	msg := ejecuta(t, cmd).(backupsMsg)
	m, _ = upd(m, msg)
	if m.mode != modeBackups {
		t.Fatalf("con 'b' quiero modo backups, tengo %d", m.mode)
	}
	if len(m.backups) == 0 {
		t.Fatal("quiero la lista de backups cargada")
	}
	if !strings.Contains(m.View(), "Backups de alpha") {
		t.Fatal("la vista de backups debe citar la instancia")
	}
	f.calls = nil
	m, cmd = upd(m, tecla("c"))
	if cmd == nil {
		t.Fatal("'c' debe crear un backup")
	}
	done := ejecuta(t, cmd).(opDoneMsg)
	m, _ = upd(m, done)
	found := false
	for _, c := range f.calls {
		if c == "backup:alpha" {
			found = true
		}
	}
	if !found {
		t.Fatalf("quiero backup:alpha registrado, tengo %v", f.calls)
	}
}

func TestSaludYPoda(t *testing.T) {
	f := newFake()
	f.health = map[string]string{"alpha": "good", "beta": "critical", "gamma": "not-running"}
	m := nuevoTestModel(t, f)
	m, cmd := upd(m, tecla("d"))
	if cmd == nil {
		t.Fatal("'d' debe lanzar el análisis de salud")
	}
	m, _ = upd(m, healthMsg{health: f.health})
	if m.health["alpha"] != "good" || m.health["beta"] != "critical" {
		t.Fatalf("salud: quiero alpha good y beta critical, tengo %v", m.health)
	}
	if m.querying {
		t.Fatal("el análisis debe terminar")
	}
	m, cmd = upd(m, tecla("b"))
	m, _ = upd(m, ejecuta(t, cmd).(backupsMsg))
	f.calls = nil
	m, cmd = upd(m, tecla("p"))
	if cmd == nil {
		t.Fatal("'p' debe podar backups")
	}
	done := ejecuta(t, cmd).(opDoneMsg)
	m, _ = upd(m, done)
	if !strings.Contains(m.status, "prune") {
		t.Fatalf("quiero confirmación de poda en el estado, tengo %q", m.status)
	}
}

func TestAyudaYSalida(t *testing.T) {
	f := newFake()
	m := nuevoTestModel(t, f)
	m, _ = upd(m, tecla("?"))
	if m.mode != modeHelp {
		t.Fatalf("con '?' quiero ayuda, tengo %d", m.mode)
	}
	if !strings.Contains(m.View(), "Teclas") {
		t.Fatal("la vista de ayuda debe renderizar el bloque de teclas")
	}
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeList {
		t.Fatalf("con esc quiero volver a la lista, tengo %d", m.mode)
	}
	m, cmd := upd(m, tecla("q"))
	if cmd == nil {
		t.Fatal("'q' debe devolver la orden de salida")
	}
	if msg := cmd(); msg != (tea.QuitMsg{}) {
		t.Fatalf("quiero QuitMsg, tengo %T", msg)
	}
}

func TestVistaListaRenderizaFilas(t *testing.T) {
	f := newFake()
	m := nuevoTestModel(t, f)
	v := m.View()
	for _, want := range []string{"badomens", "alpha", "beta", "gamma", "corriendo"} {
		if !strings.Contains(v, want) {
			t.Fatalf("la vista debe incluir %q:\n%s", want, v)
		}
	}
}

func TestVistaListaVacia(t *testing.T) {
	f := newFake()
	f.rows = nil
	m, _ := NewModel(f)
	m, _ = upd(m, rowsMsg{rows: nil})
	v := m.View()
	if !strings.Contains(v, "Sin instancias") {
		t.Fatalf("vista vacía debe avisarlo:\n%s", v)
	}
}

func TestEtiquetasDeEstado(t *testing.T) {
	cases := map[string]string{
		"running": "corriendo", "exited": "detenida", "created": "creada",
		"restarting": "reiniciando", "paused": "pausada", "": "sin contenedor", "weird": "weird",
	}
	for in, want := range cases {
		if got := stateLabel(in); got != want {
			t.Fatalf("stateLabel(%q): quiero %q, tengo %q", in, want, got)
		}
	}
}

func TestLogsScrollYPegadoAbajo(t *testing.T) {
	m := nuevoTestModel(t, newFake())
	m, cmd := upd(m, tecla("l"))
	if cmd == nil {
		t.Fatal("'l' debe abrir los logs")
	}
	_ = ejecuta(t, cmd).(logsMsg)
	m, _ = upd(m, logsMsg{slug: "alpha", text: strings.Repeat("línea\n", 30)})
	if m.mode != modeLogs {
		t.Fatalf("quiero modo logs, tengo %d", m.mode)
	}
	if m.logsView.ScrollPercent() < 0.99 {
		t.Fatalf("el viewport debe quedar pegado abajo, scroll %.2f", m.logsView.ScrollPercent())
	}
	if !strings.Contains(m.View(), "línea") {
		t.Fatal("la vista de logs debe mostrar el contenido")
	}
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeDetail {
		t.Fatalf("con esc quiero volver al detalle, tengo %d", m.mode)
	}
}

func TestListaConservaSeleccionTrasRefresco(t *testing.T) {
	m := nuevoTestModel(t, newFake())
	m, _ = upd(m, tecla("j"))
	m, _ = upd(m, tecla("j"))
	if got := m.rows[m.cursor].Slug; got != "gamma" {
		t.Fatalf("antes del refresco: quiero gamma, tengo %s", got)
	}
	rows := sampleRows()
	rows[2].State = "running"
	m, _ = upd(m, rowsMsg{rows: rows, running: 2})
	if got := m.rows[m.cursor].Slug; got != "gamma" {
		t.Fatalf("tras el refresco quiero seguir en gamma, tengo %s", got)
	}
}
