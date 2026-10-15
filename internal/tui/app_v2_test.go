package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"

	"github.com/MrZudro/badomens/internal/events"
	"github.com/MrZudro/badomens/internal/idle"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

func testApp() *App {
	s := svc.NewFake([]*instance.Spec{{
		Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4",
		Memory: "4G", Port: 25565, MaxPlayers: 20,
	}})
	return NewApp(s, AppOptions{Theme: theme.Dark})
}

func press(r string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r)} }

// ejecutaCmd ejecuta un comando (o un lote) y devuelve el mensaje relevante.
func ejecutaCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			m := c()
			if m == nil {
				continue
			}
			if _, busy := m.(busyMsg); busy {
				continue
			}
			return m
		}
	}
	return msg
}

func resize(a *App, w, h int) { a.Update(tea.WindowSizeMsg{Width: w, Height: h}) }

// TestAppResizeSinPanico (RF-1): la secuencia grande → pequeño → TooSmall →
// grande no debe entrar en pánico ni perder contenido.
func TestAppResizeSinPanico(t *testing.T) {
	a := testApp()
	for _, sz := range [][2]int{{110, 30}, {70, 20}, {40, 12}, {30, 10}, {200, 50}} {
		resize(a, sz[0], sz[1])
		out := a.View()
		if out == "" {
			t.Fatalf("vista vacía en %dx%d", sz[0], sz[1])
		}
	}
	resize(a, 30, 10)
	if !strings.Contains(a.View(), "Agranda la terminal") {
		t.Fatalf("TooSmall debe avisar: %q", a.View())
	}
	resize(a, 120, 40)
	if strings.Contains(a.View(), "Agranda la terminal") {
		t.Fatal("al crecer debe volver al panel")
	}
}

func TestAppHomeMuestraFilasYConectar(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	rows := []svc.Row{{Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4", Port: 25565, State: "running"}}
	a.Update(pollRowsMsg{rows: rows})
	a.Update(homeDetailMsg{d: &svc.Detail{Spec: &instance.Spec{
		Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4", Port: 25565,
	}, State: "running"}})
	out := a.View()
	if !strings.Contains(out, "Survival") {
		t.Fatalf("la home debe listar la instancia:\n%s", out)
	}
	if !strings.Contains(out, "Cómo conectarse") {
		t.Fatalf("la home debe mostrar la tarjeta de conexión:\n%s", out)
	}
}

func TestAppAyudaYPaleta(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	a.Update(tea.KeyMsg{Type: tea.KeyF1})
	if a.router.Overlay() == nil || a.router.Overlay().ID() != "help" {
		t.Fatal("F1 debe abrir la ayuda")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyF1})
	if a.router.Overlay() != nil {
		t.Fatal("F1 de nuevo cierra la ayuda")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if a.router.Overlay() == nil || a.router.Overlay().ID() != "palette" {
		t.Fatal("Ctrl+P debe abrir la paleta")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if a.router.Overlay() != nil {
		t.Fatal("Esc cierra la paleta")
	}
}

func TestAppWizardCreaInstancia(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	a.Update(press("n"))
	if a.router.Top().ID() != "wizard" {
		t.Fatalf("n debe abrir el asistente, tengo %s", a.router.Top().ID())
	}
	a.Update(press("r")) // modo rápido
	for _, r := range "mi-servidor" {
		a.Update(press(string(r)))
	}
	wz, _ := a.router.Top().(*wizardScreen)
	// Al entrar en «Revisar» el asistente pide la resolución del servicio (motor,
	// versión y pines) para que el resumen sea exactamente lo que se crea (AD-02):
	// hay que entregarla antes de poder crear.
	for i := 0; i < 20 && wz != nil && wz.step != len(wz.steps)-1; i++ {
		_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyTab})
		if cmd != nil {
			a.Update(ejecutaCmd(cmd))
		}
	}
	if wz.resolution == nil {
		t.Fatal("el resumen exige la resolución del servicio antes de crear")
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter}) // crear en "Revisar"
	if cmd == nil {
		t.Fatal("enter en revisión debe lanzar la creación")
	}
	msg := ejecutaCmd(cmd)
	created, ok := msg.(wizardCreatedMsg)
	if !ok {
		t.Fatalf("mensaje inesperado: %T", msg)
	}
	if created.err != nil {
		t.Fatalf("creación: %v", created.err)
	}
	a.Update(created)
	if a.router.Top().ID() == "wizard" {
		t.Fatal("tras crear debe salir del asistente")
	}
	if len(a.toasts) == 0 {
		t.Fatal("tras crear debe haber un toast")
	}
}

func TestAppAbreDetalleDeInstancia(t *testing.T) {
	a := testApp()
	resize(a, 100, 30)
	a.Update(pollRowsMsg{rows: []svc.Row{{Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4", Port: 25565, State: "running"}}})
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter debe pedir abrir el detalle")
	}
	a.Update(cmd()) // openInstanceMsg
	if a.router.Top().ID() != "instance" {
		t.Fatalf("debe abrirse el detalle, tengo %s", a.router.Top().ID())
	}
	// Simular carga del detalle.
	a.Update(homeDetailMsg{d: &svc.Detail{Spec: &instance.Spec{Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4", Port: 25565}, State: "running"}})
	if !strings.Contains(a.View(), "Survival") {
		t.Fatal("el detalle debe mostrar el nombre")
	}
}

func TestAppClicDeRatonAbreDetalle(t *testing.T) {
	a := testApp()
	resize(a, 120, 40)
	// Slug propio de esta prueba: el gestor de zonas de bubblezone es GLOBAL al
	// proceso de pruebas, así que reutilizar el id de otra prueba permitiría
	// satisfacer (o envenenar) la comprobación con una zona ajena.
	slug := "clic-zona"
	rows := []svc.Row{{Slug: slug, Name: "Survival", Engine: "paper", Version: "1.21.4", Port: 25565, State: "running"}}
	a.Update(pollRowsMsg{rows: rows})
	a.Update(homeDetailMsg{d: &svc.Detail{Spec: &instance.Spec{Slug: slug, Name: "Survival", Engine: "paper", Version: "1.21.4", Port: 25565}, State: "running"}})

	// `bubblezone.Scan` es ASÍNCRONO: encola las zonas y un goroutine del gestor
	// las guarda después, así que `Get` justo tras `View` puede devolver nil según
	// lo planifique el runtime (con `-cpu=1` falla siempre). Se espera de forma
	// ACOTADA a que la zona quede registrada: el test pasa a ser determinista y a
	// no depender del orden de las pruebas, sin tocar el código de producción.
	zone.Clear("row:" + slug)
	t.Cleanup(func() { zone.Clear("row:" + slug) })
	_ = a.View() // registra las zonas de ratón
	var z *zone.ZoneInfo
	limite := time.Now().Add(2 * time.Second)
	for {
		z = zone.Get("row:" + slug)
		if z != nil && !z.IsZero() {
			break
		}
		if time.Now().After(limite) {
			t.Fatal("la fila del Home debía registrar su zona de ratón")
		}
		time.Sleep(time.Millisecond)
	}
	click := tea.MouseMsg{X: z.StartX + 1, Y: z.StartY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	_, cmd := a.Update(click)
	if cmd == nil {
		t.Fatal("el clic en una fila debía abrir el detalle")
	}
	msg, ok := cmd().(openInstanceMsg)
	if !ok || msg.slug != slug {
		t.Fatalf("el clic debía abrir «%s», tengo %#v", slug, cmd())
	}
}

// TestVigilanteIdleEnProceso (RF-8 §6): con instancias con auto-apagado activo
// y sin vigilante externo, la TUI arranca su propio vigilante y convierte sus
// eventos en avisos.
func TestVigilanteIdleEnProceso(t *testing.T) {
	// Con un vigilante ya activo (demonio), la TUI no arranca el suyo.
	activa := svc.NewFakeWith([]*instance.Spec{{Slug: "survival"}}, svc.FakeOptions{IdleWatcherActive: true})
	aActiva := NewApp(activa, AppOptions{Theme: theme.Dark, ReducedMotion: true})
	if cmd := aActiva.maybeStartIdleWatch([]svc.Row{{Slug: "survival", IdleEnabled: true}}); cmd != nil {
		t.Fatal("no debe arrancar el vigilante si ya hay uno activo")
	}

	evs := []events.Event{
		{Kind: events.IdleCountdownStarted, Instance: "survival", Data: map[string]any{"remaining_seconds": 90}},
		{Kind: events.IdleStopped, Instance: "survival"},
	}
	spec := &instance.Spec{
		Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4",
		Memory: "4G", Port: 25565, Idle: idle.Config{Enabled: true, Timeout: 5 * time.Minute},
	}
	s := svc.NewFakeWith([]*instance.Spec{spec}, svc.FakeOptions{IdleWatcherActive: false, IdleEvents: evs})
	a := NewApp(s, AppOptions{Theme: theme.Dark, ReducedMotion: true})
	resize(a, 120, 40)

	// maybeStartIdleWatch (sobre una app limpia) decide arrancarlo.
	if cmd := a.maybeStartIdleWatch([]svc.Row{{Slug: "survival", IdleEnabled: true}}); cmd == nil || !a.watchStarted {
		t.Fatal("debía decidir arrancar el vigilante en proceso")
	}
	a.stopWatch() // se descarta el comando por lotes; se prueba abajo por separado

	b := NewApp(s, AppOptions{Theme: theme.Dark, ReducedMotion: true})
	resize(b, 120, 40)
	start := b.startIdleWatch()
	if start == nil {
		t.Fatal("startIdleWatch debía devolver un comando")
	}
	// El comando del vigilante se queda corriendo; se ejecuta en segundo plano.
	done := make(chan tea.Msg, 1)
	go func() { done <- start() }()
	for i := 0; i < len(evs); i++ {
		msg := b.waitIdleEvent()()
		ev, ok := msg.(idleEventMsg)
		if !ok {
			t.Fatalf("mensaje inesperado: %T", msg)
		}
		_ = b.handleIdleEvent(ev.ev)
	}
	b.stopWatch()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("el vigilante no se detuvo al cancelar")
	}
	if len(b.toasts) < 2 {
		t.Fatalf("se esperaban avisos de los eventos, tengo %d", len(b.toasts))
	}
}

func TestPollerCtxNoBloquea(t *testing.T) {
	// El poller debe devolver comandos que terminan rápido con el fake.
	s := svc.NewFake(nil)
	p := &Poller{Svc: s}
	cmd := p.Cmds(nowUTC(), true, true)
	if cmd == nil {
		t.Fatal("se esperaba comando de sondeo")
	}
	done := make(chan struct{})
	go func() { _ = cmd(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("el sondeo no terminó")
	}
}
