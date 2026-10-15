package svc

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/MrZudro/badomens/internal/backup"
	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/events"
	"github.com/MrZudro/badomens/internal/idle"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/ports"
	"github.com/MrZudro/badomens/internal/provision"
	"github.com/MrZudro/badomens/internal/result"
)

// FakeInstances es un adaptador en memoria.
type FakeInstances struct {
	mu    sync.Mutex
	specs map[string]*instance.Spec
	state map[string]string
}

func (f *FakeInstances) Rows(ctx context.Context) ([]Row, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Row, 0, len(f.specs))
	for _, s := range f.specs {
		state := f.state[s.Slug]
		online := -1
		if state == "running" {
			online = 0
		}
		out = append(out, Row{
			Slug: s.Slug, Name: s.Name, Engine: s.Engine, Version: s.Version,
			Memory: s.Memory, Port: s.Port, State: state,
			OnlineMode: s.OnlineMode, MaxPlayers: s.MaxPlayers,
			Host: "100.64.0.1", PlayersOnline: online,
			MemUsedMB: 512, MemLimitMB: 4096,
			IdleEnabled: s.Idle.Enabled, IdleTimeout: s.Idle.Timeout, StoppedBy: s.StoppedBy,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

func (f *FakeInstances) Detail(ctx context.Context, slug string) (*Detail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.specs[slug]
	if !ok {
		return nil, errors.New("no existe " + slug)
	}
	return &Detail{Spec: s, State: f.state[slug], Host: "100.64.0.1"}, nil
}

func (f *FakeInstances) Logs(context.Context, string, int) (string, error) {
	return "línea 1\nlínea 2", nil
}

func (f *FakeInstances) Console(context.Context, string, string) error { return nil }

func (f *FakeInstances) Save(ctx context.Context, spec *instance.Spec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.specs[spec.Slug] = spec
	return nil
}

// SetState fija el estado de una instancia en el fake (tests de estado).
func (f *FakeInstances) SetState(slug, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state[slug] = state
}

// remove quita una instancia del registro del fake (borrado).
func (f *FakeInstances) remove(slug string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.specs, slug)
	delete(f.state, slug)
}

// FakeLifecycle registra las llamadas sin ejecutar nada. Delete sí quita la
// instancia del registro del fake: así la verificación posterior al borrado (T-08)
// se comporta como con el servicio real.
type FakeLifecycle struct {
	mu    sync.Mutex
	calls []string
	inst  *FakeInstances
}

func (f *FakeLifecycle) Start(_ context.Context, slug string) error {
	if err := f.record("start", slug); err != nil {
		return err
	}
	f.setState(slug, "running")
	return nil
}

func (f *FakeLifecycle) Stop(_ context.Context, slug string) error {
	if err := f.record("stop", slug); err != nil {
		return err
	}
	f.setState(slug, "exited")
	return nil
}

func (f *FakeLifecycle) Close(_ context.Context, slug string) error {
	if err := f.record("close", slug); err != nil {
		return err
	}
	f.setState(slug, "exited")
	return nil
}

func (f *FakeLifecycle) Restart(_ context.Context, slug string) error {
	if err := f.record("restart", slug); err != nil {
		return err
	}
	f.setState(slug, "running")
	return nil
}

// setState refleja el efecto del ciclo de vida en el registro del fake (como haría
// Docker), para que las verificaciones posteriores de la UI sean significativas.
func (f *FakeLifecycle) setState(slug, state string) {
	if f.inst != nil {
		f.inst.SetState(slug, state)
	}
}

func (f *FakeLifecycle) Delete(_ context.Context, slug string) error {
	if err := f.record("delete", slug); err != nil {
		return err
	}
	if f.inst != nil {
		f.inst.remove(slug)
	}
	return nil
}

func (f *FakeLifecycle) record(op, slug string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, op+":"+slug)
	return nil
}

// Calls devuelve las operaciones registradas.
func (f *FakeLifecycle) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

// FakeBackups devuelve una lista fija y registra lo que se pidió (List, Create,\n// Restore y Prune son observables desde los tests).
type FakeBackups struct {
	mu    sync.Mutex
	items []backup.Info
	calls []string
	// restoreErr fuerza el fallo de la restauración.
	restoreErr error
}

func (f *FakeBackups) List(context.Context, string) ([]backup.Info, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.items, nil
}

func (f *FakeBackups) Create(_ context.Context, slug string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "create:"+slug)
	return nil
}

func (f *FakeBackups) Restore(_ context.Context, slug, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "restore:"+slug+":"+name)
	return f.restoreErr
}

func (f *FakeBackups) Prune(context.Context, string, int) ([]string, error) {
	return nil, nil
}

// Calls devuelve las operaciones registradas.
func (f *FakeBackups) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.calls...)
}

// fakeIdle guarda la configuración de auto-apagado en memoria y simula el
// vigilante en proceso para los tests de la TUI.
type fakeIdle struct {
	mu     sync.Mutex
	config map[string]idle.Config
	// active simula un vigilante latiendo; eventos/err guían Watch.
	active      bool
	watchEvents []events.Event
	watchErr    error
}

func (f *fakeIdle) Config(_ context.Context, slug string) (idle.Config, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.config[slug]; ok {
		return c.Normalized(), nil
	}
	return idle.DefaultConfig(), nil
}

func (f *fakeIdle) Set(_ context.Context, slug string, cfg idle.Config) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.config[slug] = cfg.Normalized()
	return nil
}

// WatcherActive simula un vigilante activo (los tests no quieren el banner).
func (f *fakeIdle) WatcherActive() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active
}

// Watch emite los eventos guionizados y luego espera a que se cancele el
// contexto, imitando al vigilante real.
func (f *fakeIdle) Watch(ctx context.Context, emit func(events.Event)) error {
	f.mu.Lock()
	evs := append([]events.Event{}, f.watchEvents...)
	err := f.watchErr
	f.mu.Unlock()
	for _, e := range evs {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if emit != nil {
			emit(e)
		}
	}
	if err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

// fakeCatalog devuelve unas versiones fijas por motor y resuelve sin red. Es un
// stub determinista: la validación real (pins obligatorios, versión existente)
// vive en el servicio de catálogo.
type fakeCatalog struct {
	items []engines.Version
	// resolveErr, si no es nil, hace fallar toda resolución.
	resolveErr error
	// stale etiqueta el catálogo como caché vencida y warning lo describe.
	stale   bool
	warning string
	// listErr hace fallar la lista (nunca una lista vacía silenciosa) y
	// refreshErr el refresco forzado.
	listErr    error
	refreshErr error
}

func (f *fakeCatalog) Versions(context.Context, string, bool) (engines.ListResult, error) {
	if f.listErr != nil {
		return engines.ListResult{Engine: "fake", Stale: f.stale, Status: f.status()}, f.listErr
	}
	return engines.ListResult{
		Engine: "fake", Versions: f.items, Stale: f.stale, Status: f.status(), Warning: f.warning,
	}, nil
}

// Refresh fuerza la consulta: los fakes no tienen red, así que devuelve el mismo
// catálogo y limpia la etiqueta de caché vencida.
func (f *fakeCatalog) Refresh(context.Context, string) (engines.RefreshResult, error) {
	if f.refreshErr != nil {
		return engines.RefreshResult{
			Engine: "fake",
			Status: engines.CatalogStatus{Engine: "fake", State: engines.CatalogStale, Warning: "sin red"},
		}, f.refreshErr
	}
	f.stale, f.warning = false, ""
	return engines.RefreshResult{Engine: "fake", Versions: f.items, Status: f.status()}, nil
}

// status informa del estado del catálogo falso (fresco o vencido).
func (f *fakeCatalog) status() engines.CatalogStatus {
	state := engines.CatalogFresh
	if f.stale {
		state = engines.CatalogStale
	}
	st := engines.CatalogStatus{Engine: "fake", State: state, Source: "fake", Versions: len(f.items)}
	if f.stale {
		st.Warning = f.warning
	}
	return st
}

// Resolve imita al servicio: devuelve la versión pedida (o la recomendada) con
// TODOS los pins obligatorios del motor.
func (f *fakeCatalog) Resolve(_ context.Context, engine, version string) (engines.Resolved, error) {
	if f.resolveErr != nil {
		return engines.Resolved{}, f.resolveErr
	}
	e, ok := engines.ByName(engine)
	if !ok {
		return engines.Resolved{}, errx.Usage("motor desconocido "+engine, "usa uno de los conocidos")
	}
	v := strings.TrimSpace(version)
	if v == "" {
		for i := len(f.items) - 1; i >= 0; i-- {
			if f.items[i].Recommended {
				v = f.items[i].Version
				break
			}
		}
	}
	if v == "" {
		return engines.Resolved{}, errx.NotFound("no hay ninguna versión que resolver", "")
	}
	pins := map[string]string{}
	for _, dep := range engines.RequiredPins(e) {
		pins[dep] = "1"
	}
	return engines.Resolved{Version: v, Pins: pins, Source: e.Name}, nil
}

// Status informa de un catálogo fresco (los fakes no usan disco).
func (f *fakeCatalog) Status(context.Context, string) (engines.CatalogStatus, error) {
	return f.status(), nil
}

// fakePorts informa de que todo está libre (o del resultado guionizado).
type fakePorts struct{ res *ports.Result }

func (f *fakePorts) Check(context.Context, int) (ports.Result, error) {
	if f.res != nil {
		return *f.res, nil
	}
	return ports.Result{Free: true}, nil
}

// fakeDocker informa OK.
type fakeDocker struct{}

func (*fakeDocker) Status(context.Context) (dockerx.Diagnosis, error) {
	return dockerx.Diagnosis{State: dockerx.StateOK}, nil
}

func (*fakeDocker) Ensure(context.Context) (dockerx.Diagnosis, error) {
	return dockerx.Diagnosis{State: dockerx.StateOK}, nil
}

// fakeCreator registra las peticiones de creación y deja el spec «persistido»
// en el registro del fake, para poder verificar que coincide con el resumen.
type fakeCreator struct {
	mu    sync.Mutex
	calls int
	// m es el registro del fake donde queda el spec del alta.
	m map[string]*instance.Spec
	// versions permite resolver la versión por defecto como el servicio.
	versions []engines.Version
	// createErr fuerza un fallo PARCIAL (spec escrito, runtime no).
	createErr error
}

func (f *fakeCreator) Create(_ context.Context, req provision.Request) (CreateOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	slug, err := instance.NormalizeSlug(req.Name)
	if err != nil {
		return CreateOutcome{Outcome: result.OutcomeFailed}, err
	}
	spec := f.specFor(req, slug)
	if f.createErr != nil {
		return CreateOutcome{Slug: slug, Spec: spec, Outcome: result.OutcomePartial}, f.createErr
	}
	return CreateOutcome{Slug: slug, Spec: spec, Outcome: result.OutcomeApplied}, nil
}

// specFor construye el spec que «se persiste»: motor, versión y los pines
// obligatorios del motor, como hace el servicio de catálogo.
func (f *fakeCreator) specFor(req provision.Request, slug string) *instance.Spec {
	engine := req.Engine
	if engine == "" {
		engine = engines.Paper.Name
	}
	version := req.Version
	if version == "" && len(f.versions) > 0 {
		version = f.versions[len(f.versions)-1].Version
	}
	pins := map[string]string{}
	// Si la petición trae los pins EXACTOS ya resueltos (la TUI los toma del
	// objeto Resolved), el fake los persiste tal cual: es lo que permite probar
	// que la configuración guardada coincide con el resumen (AD-02).
	if len(req.Pins) > 0 {
		for k, v := range req.Pins {
			pins[k] = v
		}
	} else if e, ok := engines.ByName(engine); ok {
		for _, dep := range engines.RequiredPins(e) {
			pins[dep] = "1"
		}
	}
	spec := &instance.Spec{Slug: slug, Name: req.Name, Engine: engine, Version: version, Pins: pins}
	if f.m != nil {
		f.m[slug] = spec
	}
	return spec
}

// fakeCatalogItems es el catálogo por defecto de los fakes: la recomendada y una
// alternativa, para poder cambiar de versión en los tests.
func fakeCatalogItems(opts FakeOptions) []engines.Version {
	if len(opts.CatalogVersions) > 0 {
		return opts.CatalogVersions
	}
	return []engines.Version{
		{Version: "1.21.4", Channel: engines.ChannelRelease, Recommended: true, Compatible: true, JavaMajor: 21},
		{Version: "1.20.1", Channel: engines.ChannelRelease, Compatible: true, JavaMajor: 17},
	}
}

// fakePlayerReport es el informe de jugadores por defecto de los fakes: el SLP
// respondió, así que la pestaña muestra una medición correcta.
func fakePlayerReport(opts FakeOptions) PlayerReport {
	if opts.PlayerReport != nil {
		return *opts.PlayerReport
	}
	return PlayerReport{Online: 0, Max: 20, Known: true, State: "running", Method: "slp"}
}

// NewFake construye una fachada falsa para los tests de la TUI.
func NewFake(specs []*instance.Spec) *Services {
	return NewFakeWith(specs, FakeOptions{IdleWatcherActive: true})
}

// FakeOptions ajusta el comportamiento del fake para casos concretos.
type FakeOptions struct {
	// IdleWatcherActive simula que ya hay un vigilante latiendo (por defecto sí,
	// para que el banner de aviso no aparezca en los tests).
	IdleWatcherActive bool
	// IdleEvents son los eventos que emite el vigilante en proceso.
	IdleEvents []events.Event
	// IdleWatchErr hace que Watch devuelva este error (p. ej. idle.ErrStandby).
	IdleWatchErr error
	// PortResult fuerza el resultado de la comprobación de puertos.
	PortResult *ports.Result
	// AccessSavedOnly simula un servidor apagado: la lectura de acceso solo
	// devuelve la configuración guardada (Source=saved).
	AccessSavedOnly bool
	// AccessReadErr hace que la lectura de acceso falle con este error y
	// Source=unavailable.
	AccessReadErr error
	// AccessVerifyFail hace que las mutaciones de acceso apliquen localmente
	// pero no puedan verificarse en vivo (Source=saved).
	AccessVerifyFail bool
	// WorldLibrary son los mundos que devuelve la biblioteca falsa.
	WorldLibrary []string
	// WorldErr hace que toda operación de mundos falle con este error.
	WorldErr error
	// InfraDaemonDown simula el daemon apagado (arranque garantizado, Fase 8).
	InfraDaemonDown bool
	// InfraDaemonInstalled simula el servicio persistente ya instalado.
	InfraDaemonInstalled bool
	// CatalogResolveErr hace que el catálogo falso no pueda resolver (rutas de
	// error del cambio de versión).
	CatalogResolveErr error
	// CatalogVersions, si no está vacío, sustituye la lista de versiones del
	// catálogo falso.
	CatalogVersions []engines.Version
	// CatalogStale etiqueta la lista como caché vencida y CatalogWarning es su
	// aviso (etiqueta «stale» del panel de versiones).
	CatalogStale   bool
	CatalogWarning string
	// CatalogListErr hace fallar la lista de versiones (ruta de error y reintento).
	CatalogListErr error
	// CatalogRefreshErr hace fallar el refresco forzado del catálogo.
	CatalogRefreshErr error
	// PlayerReport es el informe que devuelve la consulta de jugadores.
	PlayerReport *PlayerReport
	// PlayerErr hace fallar la consulta de jugadores.
	PlayerErr error
	// CreateErr fuerza un fallo PARCIAL del alta (spec escrito, runtime no).
	CreateErr error
	// Backups fija la lista de backups del fake.
	Backups []backup.Info
	// BackupRestoreErr hace fallar la restauración.
	BackupRestoreErr error
}

// NewFakeWith construye una fachada falsa con opciones.
func NewFakeWith(specs []*instance.Spec, opts FakeOptions) *Services {
	m := map[string]*instance.Spec{}
	state := map[string]string{}
	for _, s := range specs {
		m[s.Slug] = s
	}
	inst := &FakeInstances{specs: m, state: state}
	return &Services{
		MaxParallel:  3,
		MaxInstances: 5,
		DataDir:      ".",
		Repo:         "MrZudro/badomens",
		Engines:      engines.Order,
		Instances:    inst,
		Lifecycle:    &FakeLifecycle{inst: inst},
		Backups:      &FakeBackups{items: opts.Backups, restoreErr: opts.BackupRestoreErr},
		Ports:        &fakePorts{res: opts.PortResult},
		DockerState:  &fakeDocker{},
		Idle: &fakeIdle{
			config:      map[string]idle.Config{},
			active:      opts.IdleWatcherActive,
			watchEvents: opts.IdleEvents,
			watchErr:    opts.IdleWatchErr,
		},
		Catalog: &fakeCatalog{
			items: fakeCatalogItems(opts), resolveErr: opts.CatalogResolveErr,
			stale: opts.CatalogStale, warning: opts.CatalogWarning,
			listErr: opts.CatalogListErr, refreshErr: opts.CatalogRefreshErr,
		},
		Creator: &fakeCreator{m: m, versions: fakeCatalogItems(opts), createErr: opts.CreateErr},
		Players: &FakePlayers{report: fakePlayerReport(opts), err: opts.PlayerErr},
		Access:  newFakeAccess(specs, opts),
		Worlds:  newFakeWorlds(specs, opts),
		Infra:   &fakeInfra{DaemonDown: opts.InfraDaemonDown, Installed: opts.InfraDaemonInstalled},
	}
}
