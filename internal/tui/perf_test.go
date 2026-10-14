package tui

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/svc"
)

// --- PERF: concurrencia, debounce y no bloquear el loop ---

// PERF-01: recorrer instancias rápido agenda un debounce; los ticks obsoletos no
// cargan el detalle y solo el vigente lo hace (§7).
func TestPERF01DebounceNavegacion(t *testing.T) {
	a := NewApp(svc.NewFake(nil), darkOpts())
	resize(a, 120, 40)
	rows := make([]svc.Row, 20)
	for i := range rows {
		rows[i] = svc.Row{Slug: fmt.Sprintf("i%02d", i), Name: "I", State: "exited"}
	}
	a.Update(pollRowsMsg{rows: rows})
	hs, ok := a.router.Top().(*homeScreen)
	if !ok {
		t.Fatal("se esperaba home")
	}
	// Navegación rápida: cada pulsación agenda un tick, no una carga en línea.
	for i := 0; i < 10; i++ {
		_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyDown})
		if cmd == nil {
			t.Fatal("la navegación debe agendar el debounce")
		}
	}
	gen := hs.detailGen
	if _, stale := a.Update(homeDetailDebounceMsg{gen: gen - 1}); stale != nil {
		t.Fatal("un tick obsoleto no debe cargar el detalle")
	}
	if _, fresh := a.Update(homeDetailDebounceMsg{gen: gen}); fresh == nil {
		t.Fatal("el tick vigente debe cargar el detalle")
	}
}

// PERF-02: el sondeo no se repite antes de su intervalo (sin consultas solapadas).
func TestPERF02SinSondeosSolapados(t *testing.T) {
	p := &Poller{Svc: svc.NewFake(nil)}
	now := time.Now()
	if p.Cmds(now, true, true) == nil {
		t.Fatal("el primer sondeo debe emitir comandos")
	}
	if p.Cmds(now.Add(500*time.Millisecond), true, true) != nil {
		t.Fatal("no debe volver a sondear antes del intervalo")
	}
	if p.Cmds(now.Add(4*time.Second), true, true) == nil {
		t.Fatal("debe volver a sondear tras el intervalo")
	}
}

// PERF-03: una acción lenta avisa de progreso (busy) sin congelar la UI.
func TestPERF03AccionLentaMuestraProgreso(t *testing.T) {
	a := NewApp(svc.NewFake([]*instance.Spec{{Slug: "survival", Name: "Survival"}}), darkOpts())
	resize(a, 120, 40)
	a.Update(pollRowsMsg{rows: []svc.Row{{Slug: "survival", Name: "Survival", State: "running"}}})
	_, cmd := a.Update(press("s")) // iniciar
	if cmd == nil {
		t.Fatal("la acción debe devolver un comando")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("se esperaba un lote de comandos, tengo %T", cmd())
	}
	found := false
	for _, c := range batch {
		if c == nil {
			continue
		}
		if _, busy := c().(busyMsg); busy {
			found = true
		}
	}
	if !found {
		t.Fatal("falta el aviso de progreso (busyMsg)")
	}
}

// slowInstances simula un servicio bloqueado (con retardo) para comprobar que
// ningún I/O se ejecuta dentro de Update (§7).
type slowInstances struct{ calls int32 }

func (s *slowInstances) touch() {
	atomic.AddInt32(&s.calls, 1)
	time.Sleep(200 * time.Millisecond)
}

func (s *slowInstances) Rows(context.Context) ([]svc.Row, error) { s.touch(); return nil, nil }
func (s *slowInstances) Detail(_ context.Context, slug string) (*svc.Detail, error) {
	s.touch()
	return &svc.Detail{Spec: &instance.Spec{Slug: slug}}, nil
}
func (s *slowInstances) Logs(context.Context, string, int) (string, error) { s.touch(); return "", nil }
func (s *slowInstances) Console(context.Context, string, string) error     { s.touch(); return nil }
func (s *slowInstances) Save(context.Context, *instance.Spec) error        { s.touch(); return nil }

// PERF-04: con un servicio bloqueado, Update responde de inmediato y no llama al
// servicio (el I/O vive en el Cmd).
func TestPERF04ServicioLentoNoCongela(t *testing.T) {
	a := NewApp(svc.NewFake(nil), darkOpts())
	resize(a, 120, 40)
	slow := &slowInstances{}
	a.svc.Instances = slow
	start := time.Now()
	_, cmd := a.Update(pollTickMsg{At: time.Now()})
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("Update bloqueó con un servicio lento (%v)", elapsed)
	}
	if cmd == nil {
		t.Fatal("debe devolver un Cmd asíncrono")
	}
	if n := atomic.LoadInt32(&slow.calls); n != 0 {
		t.Fatalf("Update no debe llamar al servicio (llamadas=%d)", n)
	}
}

// PERF-05: benchmarks de render (se ejecutan con `go test -bench`).
func BenchmarkViewHome(b *testing.B) {
	a := NewApp(svc.NewFake(nil), darkOpts())
	rows := make([]svc.Row, 50)
	for i := range rows {
		rows[i] = svc.Row{Slug: fmt.Sprintf("i%02d", i), Name: "Instancia", State: "running", Port: 25565, MaxPlayers: 20, PlayersOnline: 3, MemUsedMB: 512, MemLimitMB: 4096}
	}
	a.Update(pollRowsMsg{rows: rows})
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = a.View()
	}
}

// Baseline medido (Windows, 12 CPU, Go 1.27, -benchmem): ~2,4 ms/op (2,41–2,51 en dos pasadas), ~874 KB/op y
// ~2.820 allocs/op. Es el ÚNICO benchmark por encima del objetivo de 2 ms/op del
// §6.6: wrapWidth recorre y envuelve el buffer completo (O(n)) en cada View antes
// de recortar a la ventana visible, así que el coste crece con el tamaño del log y
// no con el alto del terminal. Se deja documentado como baseline (PERF-05: sin
// umbrales frágiles por máquina); la optimización pendiente es envolver solo la
// cola visible cuando follow está activo.
func BenchmarkViewConsolaGrande(b *testing.B) {
	a := NewApp(goldenSvc(), darkOpts())
	a.router.Push(newInstanceScreen(a.svc, a.pal, a.ascii, "survival"))
	if inst, ok := a.router.Top().(*instanceScreen); ok {
		inst.detail = &svc.Detail{Spec: goldenSpec(), State: "running"}
		inst.tab = 1
		var sb strings.Builder
		for i := 0; i < 2000; i++ {
			fmt.Fprintf(&sb, "[22:10:%02d INFO]: linea de log numero %d con texto suficiente para medir el ancho\n", i%60, i)
		}
		inst.logs = sb.String()
	}
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = a.View()
	}
}

// --- benchmarks por pantalla (§6.6) ---

func benchScreen(b *testing.B, build func() *App, w, h int) {
	a := build()
	a.Update(tea.WindowSizeMsg{Width: w, Height: h})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = a.View()
	}
}

func BenchmarkViewPlayers(b *testing.B) { benchScreen(b, gDetail(2), 110, 30) }
func BenchmarkViewHealth(b *testing.B)  { benchScreen(b, gHealth, 110, 30) }
func BenchmarkViewWizard(b *testing.B)  { benchScreen(b, gWizard(1, 0), 110, 30) }
func BenchmarkViewHelp(b *testing.B)    { benchScreen(b, gHelp, 110, 30) }

// Benchmarks por pantalla (§6.6) para el resto de pestañas del detalle y la
// paleta: mismo escenario determinista a 110×30 que la serie anterior, para que
// una regresión de render en cualquier pantalla quede visible (PERF-05).
func BenchmarkViewResumen(b *testing.B)  { benchScreen(b, gDetail(0), 110, 30) }
func BenchmarkViewBackups(b *testing.B)  { benchScreen(b, gDetail(3), 110, 30) }
func BenchmarkViewWorlds(b *testing.B)   { benchScreen(b, gDetail(4), 110, 30) }
func BenchmarkViewPlugins(b *testing.B)  { benchScreen(b, gDetail(5), 110, 30) }
func BenchmarkViewNet(b *testing.B)      { benchScreen(b, gDetail(6), 110, 30) }
func BenchmarkViewIdle(b *testing.B)     { benchScreen(b, gDetail(7), 110, 30) }
func BenchmarkViewSettings(b *testing.B) { benchScreen(b, gDetail(8), 110, 30) }
func BenchmarkViewPalette(b *testing.B)  { benchScreen(b, gPalette, 110, 30) }

// BenchmarkNavHome mide repintar tras mover el cursor por 50 instancias.
func BenchmarkNavHome(b *testing.B) {
	a := NewApp(svc.NewFake(nil), darkOpts())
	rows := make([]svc.Row, 50)
	for i := range rows {
		rows[i] = svc.Row{Slug: fmt.Sprintf("i%02d", i), Name: "Instancia", State: "running", Port: 25565, MaxPlayers: 20, PlayersOnline: 3, MemUsedMB: 512, MemLimitMB: 4096}
	}
	a.Update(pollRowsMsg{rows: rows})
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := tea.KeyMsg{Type: tea.KeyDown}
		if i%2 == 1 {
			key = tea.KeyMsg{Type: tea.KeyUp}
		}
		a.Update(key)
		_ = a.View()
	}
}

// --- UI-07: resize con la consola activa ---

func TestUI07ResizeConsolaActiva(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	inst := openInstance(t, a, "survival")
	inst.tab = 1
	a.Update(press("e")) // consola enfocada
	inst.logs = strings.Repeat("log con acentos áéí y emoji 🚀 de prueba\n", 50)
	for _, sz := range [][2]int{{132, 40}, {80, 24}, {60, 18}, {100, 30}} {
		w, h := sz[0], sz[1]
		resize(a, w, h)
		out := stripANSI(a.View())
		lines := strings.Split(out, "\n")
		if len(lines) > h {
			t.Fatalf("%dx%d: la vista excede el alto (%d líneas)", w, h, len(lines))
		}
		if last := lines[len(lines)-1]; !strings.Contains(last, "Ayuda") {
			t.Fatalf("%dx%d: falta la barra de atajos: %q", w, h, last)
		}
		for _, ln := range lines {
			if layout.Width(ln) > w {
				t.Fatalf("%dx%d: línea de %d celdas desborda", w, h, layout.Width(ln))
			}
		}
	}
	// El ancho del campo se recalcula con el terminal (no ancho fijo): al ancho
	// interior (marco de 4 columnas) el detalle le resta 2 para el prompt.
	resize(a, 132, 40)
	_ = a.View()
	if want := (132 - 4) - 2; inst.input.Width != want {
		t.Fatalf("el ancho del input debe seguir al terminal, tengo %d, quiero %d", inst.input.Width, want)
	}
}

// §5.2: el detalle por slug se cachea con TTL corto; volver a una fila reciente
// no dispara otra consulta al servicio.
func TestHomeCacheDetallePorSlug(t *testing.T) {
	a := NewApp(svc.NewFake([]*instance.Spec{{Slug: "survival", Name: "Survival"}}), darkOpts())
	resize(a, 120, 40)
	a.Update(pollRowsMsg{rows: []svc.Row{{Slug: "survival", Name: "Survival", State: "running"}}})
	hs, ok := a.router.Top().(*homeScreen)
	if !ok {
		t.Fatal("se esperaba home")
	}
	d := &svc.Detail{Spec: &instance.Spec{Slug: "survival", Name: "Survival"}, State: "running"}
	a.Update(homeDetailMsg{d: d, slug: "survival", gen: hs.detailGen})
	if got := hs.cachedDetail("survival"); got != d {
		t.Fatal("el detalle debe quedar cacheado por slug")
	}
	// Servir desde caché no toca el servicio: el comando devuelve el mismo dato.
	msg, ok := hs.detailCmd()().(homeDetailMsg)
	if !ok || msg.d != d {
		t.Fatalf("el detalle debía servirse desde la caché, tengo %#v", msg)
	}
	// Pasado el TTL, la caché caduca.
	hs.cache["survival"] = detailCacheEntry{d: d, at: nowUTC().Add(-homeDetailTTL - time.Second)}
	if hs.cachedDetail("survival") != nil {
		t.Fatal("la caché debe caducar pasado el TTL")
	}
}
