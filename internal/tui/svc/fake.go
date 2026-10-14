package svc

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/MrZudro/badomens/internal/backup"
	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/events"
	"github.com/MrZudro/badomens/internal/idle"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/ports"
	"github.com/MrZudro/badomens/internal/provision"
)

// fakeInstances es un adaptador en memoria.
type fakeInstances struct {
	mu    sync.Mutex
	specs map[string]*instance.Spec
	state map[string]string
}

func (f *fakeInstances) Rows(ctx context.Context) ([]Row, error) {
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

func (f *fakeInstances) Detail(ctx context.Context, slug string) (*Detail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.specs[slug]
	if !ok {
		return nil, errors.New("no existe " + slug)
	}
	return &Detail{Spec: s, State: f.state[slug], Host: "100.64.0.1"}, nil
}

func (f *fakeInstances) Logs(context.Context, string, int) (string, error) {
	return "línea 1\nlínea 2", nil
}

func (f *fakeInstances) Console(context.Context, string, string) error { return nil }

func (f *fakeInstances) Save(ctx context.Context, spec *instance.Spec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.specs[spec.Slug] = spec
	return nil
}

// fakeLifecycle registra las llamadas sin ejecutar nada.
type fakeLifecycle struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeLifecycle) Start(_ context.Context, slug string) error { return f.record("start", slug) }
func (f *fakeLifecycle) Stop(_ context.Context, slug string) error  { return f.record("stop", slug) }
func (f *fakeLifecycle) Close(_ context.Context, slug string) error { return f.record("close", slug) }
func (f *fakeLifecycle) Restart(_ context.Context, slug string) error {
	return f.record("restart", slug)
}

func (f *fakeLifecycle) Delete(_ context.Context, slug string) error {
	return f.record("delete", slug)
}

func (f *fakeLifecycle) record(op, slug string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, op+":"+slug)
	return nil
}

// Calls devuelve las operaciones registradas.
func (f *fakeLifecycle) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

// fakeBackups devuelve una lista fija.
type fakeBackups struct {
	items []backup.Info
}

func (f *fakeBackups) List(context.Context, string) ([]backup.Info, error) { return f.items, nil }
func (f *fakeBackups) Create(context.Context, string) error                { return nil }
func (f *fakeBackups) Restore(context.Context, string, string) error       { return nil }
func (f *fakeBackups) Prune(context.Context, string, int) ([]string, error) {
	return nil, nil
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

// fakeCatalog devuelve unas versiones fijas por motor.
type fakeCatalog struct{ items []engines.Version }

func (f *fakeCatalog) Versions(context.Context, string, bool) ([]engines.Version, error) {
	return f.items, nil
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

// fakeCreator registra las peticiones de creación.
type fakeCreator struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeCreator) Create(_ context.Context, req provision.Request) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	slug, err := instance.NormalizeSlug(req.Name)
	if err != nil {
		return "", err
	}
	return slug, nil
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
}

// NewFakeWith construye una fachada falsa con opciones.
func NewFakeWith(specs []*instance.Spec, opts FakeOptions) *Services {
	m := map[string]*instance.Spec{}
	state := map[string]string{}
	for _, s := range specs {
		m[s.Slug] = s
	}
	return &Services{
		MaxParallel:  3,
		MaxInstances: 5,
		DataDir:      ".",
		Repo:         "MrZudro/badomens",
		Engines:      engines.Order,
		Instances:    &fakeInstances{specs: m, state: state},
		Lifecycle:    &fakeLifecycle{},
		Backups:      &fakeBackups{},
		Ports:        &fakePorts{res: opts.PortResult},
		DockerState:  &fakeDocker{},
		Idle: &fakeIdle{
			config:      map[string]idle.Config{},
			active:      opts.IdleWatcherActive,
			watchEvents: opts.IdleEvents,
			watchErr:    opts.IdleWatchErr,
		},
		Catalog: &fakeCatalog{items: []engines.Version{{Version: "1.21.4", Channel: engines.ChannelRelease, Recommended: true, Compatible: true, JavaMajor: 21}}},
		Creator: &fakeCreator{},
		Access:  newFakeAccess(specs, opts),
		Worlds:  newFakeWorlds(specs, opts),
	}
}
