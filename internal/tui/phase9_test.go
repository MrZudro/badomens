package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/backup"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tailscale"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/svc"
)

// ===========================================================================
// Fase 9 · Funciones de instancia (PLAY-*, REG-*, DEL-*, COPY-*, BK-*)
// ===========================================================================

// playersFake devuelve el adaptador de jugadores del fake para poder observar si
// se consultó o no.
func playersFake(s *svc.Services) *svc.FakePlayers {
	if p, ok := s.Players.(*svc.FakePlayers); ok {
		return p
	}
	return nil
}

// instanceApp construye la app con una instancia en marcha y devuelve también el
// fake de servicios para observar las llamadas.
func instanceApp(t *testing.T, opts svc.FakeOptions) (*App, *svc.Services, *instance.Spec) {
	t.Helper()
	spec := goldenSpec()
	s := svc.NewFakeWith([]*instance.Spec{spec}, opts)
	a := NewApp(s, darkOpts())
	resize(a, 120, 40)
	return a, s, spec
}

// deliverPlayers ejecuta la consulta de jugadores de la pantalla y adopta su
// resultado, como haría el bucle de Bubble Tea.
func deliverPlayers(a *App, inst *instanceScreen) {
	msg := ejecutaCmd(inst.playersCmd())
	if msg != nil {
		a.Update(msg)
	}
}

// PLAY-01 (§7): con el servidor 🟡 Cargando NO se consulta y se dice que está
// iniciando.
func TestPLAY01NoConsultaMientrasCarga(t *testing.T) {
	a, s, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	// Operación de ciclo de vida en vuelo: el semáforo está en «Cargando».
	inst.lifecycle("start")
	if inst.serverState() != components.ServerStarting {
		t.Fatalf("el estado debe ser Cargando, tengo %q", inst.serverState())
	}
	deliverPlayers(a, inst)
	if fp := playersFake(s); fp == nil || len(fp.Calls()) != 0 {
		t.Fatalf("no debe consultarse mientras está cargando: %v", fp.Calls())
	}
	line := stripANSI(inst.onlineLine())
	if !strings.Contains(line, "iniciando") {
		t.Fatalf("debe decir que está iniciando:\n%s", line)
	}
}

// PLAY-02 (§7): mensajes DIFERENCIADOS por causa: apagado, iniciando, consulta no
// disponible (RCON/SLP) y error de conexión.
func TestPLAY02MensajesDiferenciados(t *testing.T) {
	casos := []struct {
		name   string
		prep   func(*instanceScreen)
		want   string
		raises string
	}{
		{"apagado", func(i *instanceScreen) {
			i.detail.State = "exited"
			i.operateReport(svc.PlayerStopped, "")
		}, "apagado", ""},
		{"iniciando", func(i *instanceScreen) {
			i.opPending = "start"
			i.operateReport(svc.PlayerStarting, "")
		}, "iniciando", ""},
		{"consulta no disponible", func(i *instanceScreen) {
			i.operateReport(svc.PlayerQueryDisabled, "")
		}, "Consulta no disponible", "RCON"},
		{"error de conexión", func(i *instanceScreen) {
			i.operateReport(svc.PlayerUnreachable, "connection refused")
		}, "Error de conexión", "connection refused"},
		{"sin dirección", func(i *instanceScreen) {
			i.operateReport(svc.PlayerNoAddress, "")
		}, "Sin dirección", "tailscale"},
	}
	vistos := map[string]bool{}
	for _, c := range casos {
		a, _, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
		inst := openInstance(t, a, "survival")
		inst.tab = 2
		c.prep(inst)
		texto := stripANSI(inst.onlineLine())
		if !strings.Contains(texto, c.want) {
			t.Fatalf("%s: la línea debe contener %q:\n%s", c.name, c.want, texto)
		}
		if c.raises != "" && !strings.Contains(texto, c.raises) {
			t.Fatalf("%s: debe explicar %q:\n%s", c.name, c.raises, texto)
		}
		if vistos[texto] {
			t.Fatalf("los mensajes deben ser distintos entre causas: %q repetido", texto)
		}
		vistos[texto] = true
	}
}

// operateReport deja la pantalla con el informe indicado (sin pasar por el
// servicio), como si hubiera llegado por mensaje.
func (s *instanceScreen) operateReport(reason svc.PlayerReason, detail string) {
	s.playersSeq++
	s.playersKnown = false
	s.playersReason, s.playersDetail = reason, detail
}

// PLAY-03 (§7): una respuesta tardía de otra instancia (o de una consulta
// anterior) no pisa el estado de la actual.
func TestPLAY03RespuestaTardiaNoPisaElEstado(t *testing.T) {
	a, _, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	deliverPlayers(a, inst)
	if !inst.playersKnown {
		t.Fatal("el escenario necesita una medición correcta")
	}
	// Respuesta tardía de OTRA instancia.
	a.Update(instancePlayersMsg{slug: "otra", seq: inst.playersSeq, online: 9, max: 20, known: true})
	if inst.playersOnline != 0 || !inst.playersKnown {
		t.Fatalf("una respuesta de otra instancia no debe aplicarse: %d/%v", inst.playersOnline, inst.playersKnown)
	}
	// Respuesta con una secuencia anterior de la MISMA instancia.
	a.Update(instancePlayersMsg{slug: "survival", seq: inst.playersSeq - 1, online: 9, max: 20, known: true})
	if inst.playersOnline != 0 {
		t.Fatalf("una respuesta tardía de la misma instancia no debe aplicarse: %d", inst.playersOnline)
	}
}

// PLAY-04 (§7): con el servidor en línea y el SLP respondiendo, se muestran los
// jugadores y el MÉTODO; un respaldo por RCON también se identifica.
func TestPLAY04MedicionCorrectaYMetodo(t *testing.T) {
	rep := svc.PlayerReport{Online: 3, Max: 20, Known: true, State: "running", Method: "slp"}
	spec := goldenSpec()
	s := svc.NewFakeWith([]*instance.Spec{spec}, svc.FakeOptions{IdleWatcherActive: true, PlayerReport: &rep})
	a := NewApp(s, darkOpts())
	resize(a, 120, 40)
	inst := openInstance(t, a, "survival")
	inst.tab = 2
	deliverPlayers(a, inst)
	line := stripANSI(inst.onlineLine())
	if !strings.Contains(line, "3 de 20") || !strings.Contains(line, "Server List Ping") {
		t.Fatalf("la medición debe mostrar jugadores y método:\n%s", line)
	}
	if playersFake(s) == nil || len(playersFake(s).Calls()) != 1 {
		t.Fatalf("debe consultarse UNA vez al abrir Jugadores: %v", playersFake(s).Calls())
	}

	// Respaldo por RCON.
	rcon := svc.PlayerReport{Online: 2, Max: 20, Known: true, Method: "rcon", State: "running"}
	s.Players = &svc.FakePlayers{}
	s.Players.(*svc.FakePlayers).SetReport(rcon)
	deliverPlayers(a, inst)
	if line := stripANSI(inst.onlineLine()); !strings.Contains(line, "RCON") {
		t.Fatalf("el respaldo RCON debe identificarse:\n%s", line)
	}
}

// --- Regeneración (§9) ---

// REG-01 (§9/UX-10): el diálogo declara la POLÍTICA antes de confirmar, ofrece
// backup previo y el foco arranca en el botón seguro; ↑/↓ y ←/→ mueven el foco.
func TestREG01PoliticaExplicitaAntesDeConfirmar(t *testing.T) {
	a, s, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
	inst := openInstance(t, a, "survival")
	inst.tab = 4 // Mundos
	a.Update(press("g"))
	if !inst.prompt {
		t.Fatal("G debe pedir la semilla")
	}
	a.Update(press("4"))
	a.Update(press("2"))
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if inst.modal == nil || inst.modalKind != "world_regen" {
		t.Fatalf("debía abrirse el modal de regeneración: %+v", inst.modal)
	}
	body := inst.modal.Body
	for _, want := range []string{"apagar y regenerar", "DETENDRÁ", "ARRANCAR", "backup"} {
		if !strings.Contains(body, want) {
			t.Fatalf("el diálogo debe explicar la política (%q):\n%s", want, body)
		}
	}
	if inst.modal.Focused != 2 {
		t.Fatalf("el foco inicial debe estar en cancelar, tengo %d", inst.modal.Focused)
	}
	// ↑/↓ también mueven el foco entre las acciones del diálogo (UX-10).
	a.Update(tea.KeyMsg{Type: tea.KeyUp})
	if inst.modal.Focused != 1 {
		t.Fatalf("↑ debe subir el foco a «backup y regenerar», tengo %d", inst.modal.Focused)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyDown})
	if inst.modal.Focused != 2 {
		t.Fatalf("↓ debe volver a cancelar, tengo %d", inst.modal.Focused)
	}
	// Enter con el foco en cancelar no regenera nada.
	if _, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("Enter en cancelar no debe regenerar")
	}
	if got := regenCalls(s); len(got) != 0 {
		t.Fatalf("cancelar no debe llamar al servicio: %v", got)
	}
}

// regenCalls devuelve las regeneraciones registradas por el fake.
func regenCalls(s *svc.Services) []svc.WorldCall {
	fw, ok := s.Worlds.(*svc.FakeWorlds)
	if !ok {
		return nil
	}
	var out []svc.WorldCall
	for _, c := range fw.Calls() {
		if c.Op == "regenerate" {
			out = append(out, c)
		}
	}
	return out
}

// REG-02 (§9/T-08): feedback de INICIO (indicador persistente en el panel) y de
// RESULTADO (notificación); el resultado solo se confirma si se pudo verificar.
func TestREG02FeedbackDeInicioYResultado(t *testing.T) {
	a, s, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
	// El registro del fake deja la instancia en marcha (el servicio la reiniciará
	// tras regenerar) para poder verificar el resultado.
	s.Instances.(*svc.FakeInstances).SetState("survival", "running")
	inst := openInstance(t, a, "survival")
	inst.tab = 4
	a.Update(press("g"))
	a.Update(press("4"))
	a.Update(press("2"))
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Confirmar con la tecla anunciada [y].
	_, cmd := a.Update(press("y"))
	if cmd == nil {
		t.Fatal("confirmar debe lanzar la regeneración")
	}
	if !inst.regenPending {
		t.Fatal("debe quedar un indicador persistente de regeneración en curso")
	}
	if out := stripANSI(inst.worldsView(106, 24)); !strings.Contains(out, "Regeneración en curso") {
		t.Fatalf("el panel debe mostrar el indicador persistente:\n%s", out)
	}
	if inst.serverState() != components.ServerStarting {
		t.Fatalf("mientras se regenera, el semáforo debe estar en Cargando, tengo %q", inst.serverState())
	}
	if !strings.Contains(stripANSI(inst.Chrome(100)), "Cargando") {
		t.Fatal("el progreso 🟡 debe verse con texto en la franja")
	}
	msg := ejecutaCmd(cmd)
	op, ok := msg.(worldsOpMsg)
	if !ok {
		t.Fatalf("se esperaba worldsOpMsg, tengo %T", msg)
	}
	if op.err != nil || op.verify != "" {
		t.Fatalf("la regeneración debía verificarse: %+v", op)
	}
	_, after := a.Update(op)
	if after == nil {
		t.Fatal("el desenlace debe publicar un toast")
	}
	aviso, ok := ejecutaCmd(after).(toastMsg)
	if !ok || !strings.Contains(aviso.Text, "regenerado") {
		t.Fatalf("el toast debe contar el resultado, tengo %+v", aviso)
	}
	if inst.regenPending {
		t.Fatal("el indicador persistente debe desaparecer al terminar")
	}
	if got := regenCalls(s); len(got) != 1 || got[0].Seed != "42" {
		t.Fatalf("debía regenerarse con la semilla 42: %+v", got)
	}
}

// REG-03 (§9): [B] en el diálogo hace el backup previo antes de regenerar.
func TestREG03BackupPrevioDesdeElDialogo(t *testing.T) {
	a, s, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
	inst := openInstance(t, a, "survival")
	inst.tab = 4
	a.Update(press("g"))
	a.Update(tea.KeyMsg{Type: tea.KeyEnter}) // semilla aleatoria
	if inst.modal == nil {
		t.Fatal("debía abrirse el modal")
	}
	_, cmd := a.Update(press("b"))
	if cmd == nil {
		t.Fatal("[B] debe hacer backup y regenerar")
	}
	msg := ejecutaCmd(cmd)
	op, ok := msg.(worldsOpMsg)
	if !ok || op.err != nil {
		t.Fatalf("se esperaba una regeneración correcta, tengo %#v", msg)
	}
	if !strings.Contains(op.backup, "backup") {
		t.Fatalf("el desenlace debe mencionar el backup previo: %+v", op)
	}
	fb, ok := s.Backups.(*svc.FakeBackups)
	if !ok {
		t.Fatal("el fake de backups debe estar accesible")
	}
	creados := 0
	for _, c := range fb.Calls() {
		if strings.HasPrefix(c, "create:") {
			creados++
		}
	}
	if creados != 1 {
		t.Fatalf("debe hacerse UN backup previo, tengo %d (%v)", creados, fb.Calls())
	}
}

// REG-04 (§9/T-08/AC-10): un fallo deja un estado recuperable y explicado, y un
// resultado que NO se pudo verificar no se presenta como éxito.
func TestREG04FalloYVerificacionFallida(t *testing.T) {
	// Fallo del servicio al regenerar.
	a, _, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true, WorldErr: errors.New("sin espacio")})
	inst := openInstance(t, a, "survival")
	inst.tab = 4
	a.Update(press("g"))
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd := a.Update(press("y"))
	op := ejecutaCmd(cmd).(worldsOpMsg)
	if op.err == nil {
		t.Fatal("el escenario exige que la regeneración falle")
	}
	if op.text() == "" || !strings.Contains(op.text(), "no se pudo") {
		t.Fatalf("el desenlace debe explicar el fallo: %q", op.text())
	}
	_, after := a.Update(op)
	aviso, ok := ejecutaCmd(after).(toastMsg)
	if !ok || !strings.Contains(aviso.Text, "revisa el estado") {
		t.Fatalf("el aviso debe indicar cómo recuperarse, tengo %+v", aviso)
	}
	if inst.regenPending {
		t.Fatal("el indicador persistente debe limpiarse también al fallar")
	}

	// Resultado que no se puede verificar: el fake no aplica la semilla.
	a2, s2, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
	fw, _ := s2.Worlds.(*svc.FakeWorlds)
	if fw == nil {
		t.Fatal("se necesita el fake de mundos")
	}
	s2.Worlds = sinGuardarSemilla{fw}
	inst2 := openInstance(t, a2, "survival")
	inst2.tab = 4
	a2.Update(press("g"))
	a2.Update(press("7"))
	a2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd2 := a2.Update(press("y"))
	op2 := ejecutaCmd(cmd2).(worldsOpMsg)
	if op2.err != nil {
		t.Fatalf("el servicio debía responder bien: %v", op2.err)
	}
	if op2.verify == "" {
		t.Fatal("un resultado no verificable debe marcarse (T-08)")
	}
	if !strings.Contains(op2.text(), "no se pudo verificar") {
		t.Fatalf("el texto debe decir que no se pudo verificar: %q", op2.text())
	}
}

// sinGuardarSemilla envuelve Worlds para que la regeneración NO deje la semilla
// guardada (simula un resultado que la verificación no puede confirmar).
type sinGuardarSemilla struct{ svc.Worlds }

func (s sinGuardarSemilla) Regenerate(ctx context.Context, slug, seed string) error {
	// Responde bien pero NO deja la semilla guardada: la verificación de la UI
	// debe detectarlo (T-08).
	return nil
}

// --- Borrado (§11) ---

// DEL-01 (§11): nunca se borra sin confirmación; Esc cancela y un nombre que no
// coincide no borra nada.
func TestDEL01NuncaBorraSinConfirmacion(t *testing.T) {
	a, s, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
	navConInstancia(a, "survival")
	a.Update(press("D"))
	hs, _ := a.router.Top().(*homeScreen)
	if hs.modal == nil {
		t.Fatal("D debe abrir la confirmación desde la lista")
	}
	if got := lifecycleCalls(s); len(got) != 0 {
		t.Fatalf("abrir el diálogo no debe borrar nada: %v", got)
	}
	// Esc cancela sin llamar al servicio.
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if hs.modal != nil {
		t.Fatal("Esc debe cerrar la confirmación")
	}
	if got := lifecycleCalls(s); len(got) != 0 {
		t.Fatalf("Esc no debe borrar: %v", got)
	}
	// Nombre equivocado: no borra.
	a.Update(press("D"))
	for _, r := range "otra-cosa" {
		a.Update(press(string(r)))
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("un nombre que no coincide no debe borrar")
	}
	if hs.modal == nil {
		t.Fatal("con el nombre equivocado el diálogo debe seguir abierto")
	}
}

// lifecycleCalls devuelve las operaciones de ciclo de vida registradas.
func lifecycleCalls(s *svc.Services) []string {
	if f, ok := s.Lifecycle.(*svc.FakeLifecycle); ok {
		return f.Calls()
	}
	return nil
}

// DEL-02 (§11): el diálogo detalla qué se elimina, apaga primero si el servidor
// está en línea, ofrece backup con Ctrl+B y el resultado se notifica tras
// VERIFICAR que la instancia ya no existe.
func TestDEL02DesdeLaListaApagaYVerifica(t *testing.T) {
	a, s, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
	navConInstancia(a, "survival")
	a.Update(press("D"))
	hs, _ := a.router.Top().(*homeScreen)
	for _, want := range []string{"contenedor", "datos", "mundo", "backups", "Ctrl+B"} {
		if !strings.Contains(hs.modal.Body, want) {
			t.Fatalf("el diálogo debe detallar qué se elimina (%q):\n%s", want, hs.modal.Body)
		}
	}
	if !strings.Contains(hs.modal.Body, "EN MARCHA") {
		t.Fatalf("con el servidor en línea debe avisar del apagado:\n%s", hs.modal.Body)
	}
	for _, r := range "survival" {
		a.Update(press(string(r)))
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("con el nombre exacto debe borrar")
	}
	msg := ejecutaCmd(cmd)
	if _, ok := msg.(instanceDeletedMsg); !ok {
		t.Fatalf("se esperaba instanceDeletedMsg, tengo %T", msg)
	}
	calls := lifecycleCalls(s)
	if len(calls) != 2 || calls[0] != "stop:survival" || calls[1] != "delete:survival" {
		t.Fatalf("debía apagarse ANTES de borrar: %v", calls)
	}
	// El resultado se notifica (el aviso viaja como comando del App) y la fila
	// desaparece de la lista.
	_, notificar := a.Update(msg)
	if notificar != nil {
		a.Update(ejecutaCmd(notificar))
	}
	if _, err := s.Instances.Detail(context.Background(), "survival"); err == nil {
		t.Fatal("la instancia debe haber desaparecido del registro")
	}
	if len(hs.rows) != 0 {
		t.Fatalf("la fila debe desaparecer de la lista: %+v", hs.rows)
	}
	if out := stripANSI(a.View()); !strings.Contains(out, "Todavía no hay instancias") {
		t.Fatalf("el Inicio debe quedar en su estado vacío:\n%s", out)
	}
	toasts := ""
	for _, tt := range a.toasts {
		toasts += tt.Text + "\n"
	}
	if !strings.Contains(toasts, "eliminada") {
		t.Fatalf("el resultado debe notificarse, tengo %q", toasts)
	}
}

// DEL-03 (§11): [D] funciona también desde el detalle, con la misma confirmación.
func TestDEL03DesdeElDetalle(t *testing.T) {
	a, s, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
	navConInstancia(a, "survival")
	inst := openInstance(t, a, "survival")
	// La acción está disponible en el detalle y en la barra con la etiqueta Borrar.
	encontrada := false
	for _, act := range inst.Actions() {
		if act.ID == "instance.delete" {
			encontrada = true
			if act.Key != "D" {
				t.Fatalf("la tecla del borrado en el detalle debe ser D, tengo %q", act.Key)
			}
		}
	}
	if !encontrada {
		t.Fatal("el detalle debe exponer la acción de borrado")
	}
	a.Update(press("D"))
	if inst.modal == nil || inst.modalKind != "delete" {
		t.Fatalf("D en el detalle debe abrir la confirmación: %+v", inst.modal)
	}
	for _, r := range "survival" {
		a.Update(press(string(r)))
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := ejecutaCmd(cmd)
	if _, ok := msg.(instanceDeletedMsg); !ok {
		t.Fatalf("se esperaba instanceDeletedMsg, tengo %T", msg)
	}
	if calls := lifecycleCalls(s); len(calls) == 0 || calls[len(calls)-1] != "delete:survival" {
		t.Fatalf("debía borrarse desde el detalle: %v", calls)
	}
}

// DEL-04 (§11): Ctrl+B hace un backup previo y después borra; sin el nombre
// escrito no borra (la confirmación reforzada es obligatoria).
func TestDEL04BackupPrevioYConfirmacionReforzada(t *testing.T) {
	a, s, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
	navConInstancia(a, "survival")
	a.Update(press("D"))
	// Ctrl+B sin el nombre: no borra nada.
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if got := lifecycleCalls(s); len(got) != 0 {
		t.Fatalf("sin el nombre escrito no debe borrarse nada: %v", got)
	}
	hs, _ := a.router.Top().(*homeScreen)
	if hs.modal == nil {
		t.Fatal("el diálogo debe seguir abierto")
	}
	for _, r := range "survival" {
		a.Update(press(string(r)))
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if cmd == nil {
		t.Fatal("Ctrl+B con el nombre escrito debe backup-ear y borrar")
	}
	if hs.modal != nil {
		t.Fatal("la confirmación ya se aceptó: el diálogo debe cerrarse")
	}
	if _, ok := ejecutaCmd(cmd).(instanceDeletedMsg); !ok {
		t.Fatal("tras el backup debe completarse el borrado")
	}
	fb, _ := s.Backups.(*svc.FakeBackups)
	creados := 0
	for _, c := range fb.Calls() {
		if strings.HasPrefix(c, "create:") {
			creados++
		}
	}
	if creados != 1 {
		t.Fatalf("Ctrl+B debe crear un backup previo, tengo %d (%v)", creados, fb.Calls())
	}
	calls := lifecycleCalls(s)
	if len(calls) != 2 || calls[1] != "delete:survival" {
		t.Fatalf("tras el backup debe borrarse: %v", calls)
	}
}

// DEL-05 (§11/AC-10): un fallo parcial no se presenta como éxito y explica el
// estado en el que queda la instancia.
func TestDEL05FalloParcialRecuperable(t *testing.T) {
	a, s, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
	s.Lifecycle = borradoQueFalla{s.Lifecycle}
	navConInstancia(a, "survival")
	a.Update(press("D"))
	for _, r := range "survival" {
		a.Update(press(string(r)))
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := ejecutaCmd(cmd)
	aviso, ok := msg.(toastMsg)
	if !ok {
		t.Fatalf("un fallo debe avisarse, tengo %T", msg)
	}
	if !strings.Contains(aviso.Text, "No se pudo borrar") || !strings.Contains(aviso.Text, "detenida") {
		t.Fatalf("el aviso debe explicar el estado recuperable, tengo %q", aviso.Text)
	}
	if _, err := s.Instances.Detail(context.Background(), "survival"); err != nil {
		t.Fatal("un borrado fallido no debe quitar la instancia del registro")
	}
}

// borradoQueFalla simula un borrado que no termina (Docker/Down a medias).
type borradoQueFalla struct{ svc.Lifecycle }

func (b borradoQueFalla) Delete(ctx context.Context, slug string) error {
	return errors.New("el contenedor no se pudo eliminar")
}

// --- Copiar dirección (§10) ---

// COPY-01 (§10): se copia la dirección preferida (tailnet) desde la lista y desde
// el detalle.
func TestCOPY01CopiaLaDireccionPreferida(t *testing.T) {
	a, _, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
	navConInstancia(a, "survival")
	// La IP del tailnet llega con la comprobación de prerrequisitos de Inicio.
	a.Update(homeReqMsg{dockerOK: true, tsIP: "100.64.0.1", tsOK: true, localIP: "192.168.1.50"})
	_, cmd := a.Update(press("c"))
	if cmd == nil {
		t.Fatal("c debe copiar la dirección")
	}
	aviso, ok := ejecutaCmd(cmd).(toastMsg)
	if !ok || !strings.Contains(aviso.Text, "100.64.0.1:25565") {
		t.Fatalf("debe copiarse la dirección del tailnet, tengo %+v", aviso)
	}
	openInstance(t, a, "survival")
	_, cmd = a.Update(press("c"))
	aviso, ok = ejecutaCmd(cmd).(toastMsg)
	if !ok || !strings.Contains(aviso.Text, "100.64.0.1:25565") {
		t.Fatalf("el detalle también debe copiarla, tengo %+v", aviso)
	}
}

// COPY-02 (§10): si no hay NINGUNA dirección, el mensaje explica por qué y qué
// hacer (no un «no hay dirección» seco).
func TestCOPY02SinDireccionExplicaPorQueYQueHacer(t *testing.T) {
	spec := &instance.Spec{Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4", Port: 0}
	s := svc.NewFakeWith([]*instance.Spec{spec}, svc.FakeOptions{IdleWatcherActive: true})
	a := NewApp(s, darkOpts())
	resize(a, 120, 40)
	navConInstancia(a, "survival")
	hs, _ := a.router.Top().(*homeScreen)
	hs.detail = &svc.Detail{Spec: spec, State: "exited", Host: ""}
	hs.magic = magicState{Known: true}
	_, cmd := a.Update(press("c"))
	aviso, ok := ejecutaCmd(cmd).(toastMsg)
	if !ok {
		t.Fatalf("debe avisarse, tengo %T", cmd)
	}
	if !strings.Contains(aviso.Text, "Inicia") || !strings.Contains(aviso.Text, "tailscale up") {
		t.Fatalf("el mensaje debe explicar qué hacer, tengo %q", aviso.Text)
	}
	if !strings.Contains(aviso.Text, "sin IP del tailnet") {
		t.Fatalf("el mensaje debe explicar por qué, tengo %q", aviso.Text)
	}
}

// COPY-03 (§10): cuando hay varias direcciones válidas se puede ELEGIR cuál
// copiar (en la pestaña Red con [C]) y la elegida es la que se copia.
func TestCOPY03VariasDireccionesSeEligeConElSelector(t *testing.T) {
	a, _, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
	inst := openInstance(t, a, "survival")
	inst.tab = 6
	// Estado en vivo con MagicDNS y una IP local: tres direcciones posibles.
	a.Update(magicProbeMsg{rep: magicReportWithName("maquina.tail1234.ts.net")})
	a.Update(localIPMsg{ip: "192.168.1.50"})
	if got := len(inst.copyCandidates()); got != 3 {
		t.Fatalf("deben ofrecerse 3 direcciones, tengo %d: %+v", got, inst.copyCandidates())
	}
	_, cmd := a.Update(press("C"))
	if cmd == nil {
		t.Fatal("C debe ofrecer elegir la dirección")
	}
	a.Update(ejecutaCmd(cmd)) // chooserOpenMsg → overlay
	if a.router.Overlay() == nil || a.router.Overlay().ID() != "chooser" {
		t.Fatalf("debe abrirse el selector de direcciones, tengo %v", a.router.Overlay())
	}
	// ↓ elige la segunda (MagicDNS) y Enter la copia.
	a.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, pick := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if pick == nil {
		t.Fatal("Enter debe elegir")
	}
	a.Update(pick())
	if a.router.Overlay() != nil {
		t.Fatal("tras elegir, el selector debe cerrarse")
	}
	toasts := ""
	for _, tt := range a.toasts {
		toasts += tt.Text + "\n"
	}
	if !strings.Contains(toasts, "maquina.tail1234.ts.net:25565") {
		t.Fatalf("debe copiarse la dirección ELEGIDA, tengo %q", toasts)
	}
}

// COPY-04 (§10): la dirección se deriva ANTES del primer arranque cuando hay una
// IP local (y sin tailnet no se promete nada que no exista).
func TestCOPY04DerivaAntesDelPrimerArranque(t *testing.T) {
	spec := &instance.Spec{Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4", Port: 25565}
	s := svc.NewFakeWith([]*instance.Spec{spec}, svc.FakeOptions{IdleWatcherActive: true})
	a := NewApp(s, darkOpts())
	resize(a, 120, 40)
	navConInstancia(a, "survival")
	hs, _ := a.router.Top().(*homeScreen)
	hs.detail = &svc.Detail{Spec: spec, State: "exited", Host: ""}
	hs.tsIP = ""
	hs.magic = magicState{Known: true} // sin MagicDNS
	a.Update(localIPMsg{ip: "192.168.1.50"})
	_, cmd := a.Update(press("c"))
	aviso, ok := ejecutaCmd(cmd).(toastMsg)
	if !ok || !strings.Contains(aviso.Text, "192.168.1.50:25565") {
		t.Fatalf("debe copiarse la dirección de red local, tengo %+v", aviso)
	}
	if !strings.Contains(aviso.Text, "Red local") {
		t.Fatalf("debe decirse por qué red funciona, tengo %q", aviso.Text)
	}
}

// --- Backup (§9.5/§13) ---

// BK-01 (§13): [B] Backup está visible en la lista y en el detalle, y funciona.
func TestBK01BackupVisibleYFuncional(t *testing.T) {
	a, s, _ := instanceApp(t, svc.FakeOptions{IdleWatcherActive: true})
	navConInstancia(a, "survival")
	if bar := stripANSI(a.actionsText(120)); !strings.Contains(bar, "[B] Backup") {
		t.Fatalf("la lista debe mostrar [B] Backup:\n%s", bar)
	}
	_, cmd := a.Update(press("b"))
	if cmd == nil {
		t.Fatal("b debe crear un backup desde la lista")
	}
	a.Update(ejecutaCmd(cmd))

	_ = openInstance(t, a, "survival")
	etiquetas := map[string]string{}
	for _, it := range a.footerContextKeys() {
		etiquetas[it.Key] = it.Label
	}
	if etiquetas["B"] != "Backup" {
		t.Fatalf("el detalle debe anunciar [B] Backup, tengo %q", etiquetas["B"])
	}
	_, cmd = a.Update(press("b"))
	if cmd == nil {
		t.Fatal("b debe crear un backup también en el detalle")
	}
	a.Update(ejecutaCmd(cmd))
	fb, _ := s.Backups.(*svc.FakeBackups)
	creados := 0
	for _, c := range fb.Calls() {
		if strings.HasPrefix(c, "create:") {
			creados++
		}
	}
	if creados != 2 {
		t.Fatalf("debían crearse dos backups (lista y detalle), tengo %d (%v)", creados, fb.Calls())
	}
}

// BK-02 (§13): restaurar exige confirmación, avisa del checksum y del backup
// previo; un backup sin checksum se señala en el diálogo.
func TestBK02RestaurarConfirmaYValidaChecksum(t *testing.T) {
	a, s, _ := instanceApp(t, svc.FakeOptions{
		IdleWatcherActive: true,
		Backups:           bakItems(),
	})
	inst := openInstance(t, a, "survival")
	inst.tab = 3
	a.Update(ejecutaDirecto(inst.backupsCmd()))
	if len(inst.backups) == 0 {
		t.Fatal("la pestaña debe tener backups")
	}
	inst.bkCursor = 0
	_, cmd := a.Update(press("x"))
	if cmd != nil {
		t.Fatal("restaurar no debe ejecutarse sin confirmar")
	}
	if inst.modal == nil || inst.modalKind != "restore" {
		t.Fatalf("x debe abrir la confirmación, tengo %+v", inst.modal)
	}
	for _, want := range []string{"checksum", "backup del estado actual"} {
		if !strings.Contains(inst.modal.Body, want) {
			t.Fatalf("el diálogo debe explicar %q:\n%s", want, inst.modal.Body)
		}
	}
	// Esc cancela sin restaurar.
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	fb, _ := s.Backups.(*svc.FakeBackups)
	for _, c := range fb.Calls() {
		if strings.HasPrefix(c, "restore:") {
			t.Fatalf("Esc no debe restaurar: %v", fb.Calls())
		}
	}
	// Confirmar con [y] restaura el backup señalado.
	inst.bkCursor = 0
	a.Update(press("x"))
	_, cmd = a.Update(press("y"))
	if cmd == nil {
		t.Fatal("confirmar debe restaurar")
	}
	msg := ejecutaCmd(cmd)
	op, ok := msg.(instanceOpMsg)
	if !ok || op.err != nil {
		t.Fatalf("se esperaba una restauración correcta, tengo %#v", msg)
	}
	encontrado := false
	for _, c := range fb.Calls() {
		if c == "restore:survival:"+inst.backups[0].Name {
			encontrado = true
		}
	}
	if !encontrado {
		t.Fatalf("debía restaurarse el backup señalado: %v", fb.Calls())
	}
}

// bakItems es la lista de backups determinista de los tests de restauración.
func bakItems() []backup.Info {
	return []backup.Info{
		{Name: "20260114T101500", Size: 2 << 20, Sha256: strings.Repeat("ab", 32)},
		{Name: "20260113T220000", Size: 1 << 20, Sha256: strings.Repeat("cd", 32)},
	}
}

// magicReportWithName construye un informe en vivo de Tailscale con MagicDNS.
func magicReportWithName(name string) tailscale.Report {
	return tailscale.Report{
		State: tailscale.StateReady, IP: "100.64.0.1",
		MagicDNS: name, MagicDNSEnabled: true, MagicDNSReason: tailscale.ReasonMagicDNSOK,
	}
}
