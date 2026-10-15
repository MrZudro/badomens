package tui

import (
	"context"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/svc"
)

// ===========================================================================
// Fase 12.2 · Rendimiento (PERF-06 … PERF-12)
// ===========================================================================

// PERF-06 (§6.2): el buffer de la consola es CIRCULAR y acotado: trimLogs recorta
// a las últimas N líneas para 0, 1, 200 y 2.000 líneas (y por encima del límite).
func TestPERF06TrimLogsBufferCircular(t *testing.T) {
	cases := []struct{ in, max, want int }{
		{0, maxLogLines, 0},
		{1, maxLogLines, 1},
		{200, maxLogLines, 200},
		{2000, maxLogLines, 2000},
		{3000, maxLogLines, maxLogLines},
		{500, 10, 10},
	}
	for _, c := range cases {
		got := trimLogs(testLogLines(c.in), c.max)
		n := 0
		if strings.TrimSpace(got) != "" {
			n = len(strings.Split(got, "\n"))
		}
		if n != c.want {
			t.Fatalf("trimLogs(%d líneas, max=%d) = %d líneas, quiero %d", c.in, c.max, n, c.want)
		}
	}
}

// PERF-08 (§6.2): el renderizado de la consola está acotado a la zona visible:
// con 2.000 líneas y una línea de 500+ caracteres, el cuerpo del panel ocupa
// exactamente su región (no reconstruye ni pinta el buffer completo).
func TestPERF08ViewportAcotadoAZonaVisible(t *testing.T) {
	a := testApp()
	resize(a, 60, 18)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	inst.logs = testLogLines(2000) + "\n" + strings.Repeat("Z", 520)
	inst.follow = true
	body := stripANSI(inst.consoleView(56, 10))
	lines := strings.Split(body, "\n")
	if len(lines) > 10 {
		t.Fatalf("el viewport debe ocupar como mucho su región (10), tengo %d", len(lines))
	}
	for i, ln := range lines {
		if layout.Width(ln) > 56 {
			t.Fatalf("línea %d del viewport desborda: %d celdas", i+1, layout.Width(ln))
		}
	}
	// El pie fijo sigue en su sitio con el buffer gigante.
	out := strings.Split(stripANSI(a.View()), "\n")
	if last := out[len(out)-1]; !strings.Contains(last, "Ayuda") {
		t.Fatalf("el pie no debe desplazarse con el buffer grande: %q", last)
	}
}

// PERF-12 (SPEC 2 §12): la cola de avisos/eventos está acotada; muchos avisos no
// acumulan entradas sin límite.
func TestPERF12EventosAcotados(t *testing.T) {
	a := NewApp(svc.NewFake(nil), darkOpts())
	for i := 0; i < 50; i++ {
		a.pushToast("aviso " + itoa(i))
	}
	if len(a.toasts) > 3 {
		t.Fatalf("la lista de avisos debe quedar acotada a 3, tengo %d", len(a.toasts))
	}
}

// contandoInstances cuenta las llamadas a Rows (una por ciclo de sondeo) para
// demostrar que no se duplican tareas.
type contandoInstances struct {
	svc.Instances
	n int32
}

func (c *contandoInstances) Rows(ctx context.Context) ([]svc.Row, error) {
	atomic.AddInt32(&c.n, 1)
	return c.Instances.Rows(ctx)
}

// PERF-09 (§6.2): el sondeo NO acumula tareas: en 90 latidos de 1 s (intervalo de
// estado 3 s) se consulta el estado exactamente 30 veces (una por intervalo), ni
// una de más, aunque haya ciclos de navegación de por medio.
func TestPERF09PollingSinTareasDuplicadas(t *testing.T) {
	a := NewApp(svc.NewFake(nil), darkOpts())
	resize(a, 120, 40)
	cnt := &contandoInstances{Instances: a.svc.Instances}
	a.svc.Instances = cnt
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 90; i++ {
		// Navegación de por medio: ciclos que no deben duplicar tareas.
		a.Update(tea.KeyMsg{Type: tea.KeyDown})
		if cmd := a.poller.Cmds(base.Add(time.Duration(i)*time.Second), true, false); cmd != nil {
			ejecutaCmd(cmd)
		}
	}
	if got := atomic.LoadInt32(&cnt.n); got != 30 {
		t.Fatalf("el sondeo debe consultar el estado 30 veces (90/3), tengo %d", got)
	}
}

// PERF-09b (§6.2): mantener la app abierta con ciclos de navegación NO acumula
// goroutines.
func TestPERF09bNavegacionSinAcumularGoroutines(t *testing.T) {
	a := NewApp(svc.NewFake(nil), darkOpts())
	resize(a, 120, 40)
	rows := make([]svc.Row, 10)
	for i := range rows {
		rows[i] = svc.Row{Slug: "i" + itoa(i), Name: "I", State: "exited"}
	}
	a.Update(pollRowsMsg{rows: rows})
	runtime.GC()
	before := runtime.NumGoroutine()
	for i := 0; i < 400; i++ {
		a.Update(tea.KeyMsg{Type: tea.KeyDown})
		a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		_ = a.View()
	}
	runtime.GC()
	after := runtime.NumGoroutine()
	if after-before > 10 {
		t.Fatalf("los ciclos de navegación no deben acumular goroutines: antes=%d después=%d", before, after)
	}
}

// PERF-10 (§6.2): una operación lenta (con busy) NO bloquea el teclado: Update
// responde al instante y la vista sigue pintándose.
func TestPERF10OperacionLentaNoBloqueaTeclado(t *testing.T) {
	a := NewApp(svc.NewFake(nil), darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	a.svc.Instances = &slowInstances{}
	a.Update(busyMsg{on: true})
	start := time.Now()
	a.Update(press("tab")) // desplazarse de zona: se procesa aunque haya operación
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Fatalf("el teclado no debe bloquearse durante una operación lenta (%v)", elapsed)
	}
	if out := a.View(); out == "" {
		t.Fatal("la vista debe seguir pintándose durante una operación")
	}
	_ = inst
}

// PERF-11 (Fase 12.2 «métricas mínimas»): se miden la latencia de interacción, el
// tiempo de render, el uso de memoria y se documentan las peticiones concurrentes
// (acotadas por el semáforo del servicio, PERF-07). Los umbrales son holgados para
// no ser frágiles en CI compartida; el valor real queda en el log.
func TestPERF11MetricasMinimas(t *testing.T) {
	a := NewApp(goldenSvc(), darkOpts())
	resize(a, 120, 40)

	start := time.Now()
	for i := 0; i < 100; i++ {
		a.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	inter := time.Since(start) / 100

	start = time.Now()
	for i := 0; i < 50; i++ {
		_ = a.View()
	}
	render := time.Since(start) / 50

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	t.Logf("métricas TUI: latencia_interacción=%v tiempo_render=%v heap_alloc=%d B goroutines=%d", inter, render, ms.HeapAlloc, runtime.NumGoroutine())
	if inter > 100*time.Millisecond {
		t.Fatalf("la latencia de interacción debe ser baja, tengo %v", inter)
	}
	if render > 100*time.Millisecond {
		t.Fatalf("el tiempo de render debe ser bajo, tengo %v", render)
	}
}
