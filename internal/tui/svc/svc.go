// Package svc es la fachada de servicios de la TUI v2 (Fase 0.2): interfaces
// pequeñas que la TUI consume y adaptadores reales sobre los paquetes de
// dominio. Los tests usan fakes (NewFake) sin Docker, red ni tailnet.
package svc

import (
	"context"
	"net/netip"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/MrZudro/badomens/internal/backup"
	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/events"
	"github.com/MrZudro/badomens/internal/idle"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/ports"
	"github.com/MrZudro/badomens/internal/provision"
	"github.com/MrZudro/badomens/internal/runtime"
	"github.com/MrZudro/badomens/internal/tailscale"
	"github.com/MrZudro/badomens/internal/tps"
	"github.com/MrZudro/badomens/internal/worlds"
)

func engineNames() []string {
	names := make([]string, 0, len(engines.Order))
	for _, e := range engines.Order {
		names = append(names, e.Name)
	}
	return names
}

// Instances lista, detalla y opera sobre las instancias (lectura + consola).
type Instances interface {
	Rows(ctx context.Context) ([]Row, error)
	Detail(ctx context.Context, slug string) (*Detail, error)
	Logs(ctx context.Context, slug string, lines int) (string, error)
	Console(ctx context.Context, slug, command string) error
	// Save persiste cambios en la definición de una instancia.
	Save(ctx context.Context, spec *instance.Spec) error
}

// Row resume una instancia para las listas.
type Row struct {
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Engine     string `json:"engine"`
	Version    string `json:"version"`
	Memory     string `json:"memory"`
	Port       int    `json:"port"`
	State      string `json:"state"`
	OnlineMode bool   `json:"online_mode"`
	MaxPlayers int    `json:"max_players"`
	// Host es la IP del tailnet para mostrar ip:puerto (vacío si no se sabe).
	Host string `json:"host,omitempty"`
	// PlayersOnline es -1 cuando no se pudo medir (servidor parado).
	PlayersOnline int `json:"players_online"`
	// MemUsedMB y MemLimitMB son las métricas del contenedor (0 si no corre).
	MemUsedMB  float64 `json:"mem_used_mb,omitempty"`
	MemLimitMB float64 `json:"mem_limit_mb,omitempty"`
	// Idle describe el auto-apagado configurado (RF-8).
	IdleEnabled bool          `json:"idle_enabled"`
	IdleTimeout time.Duration `json:"idle_timeout,omitempty"`
	// StoppedBy es quién detuvo el contenedor ("idle", "user", "crash").
	StoppedBy string `json:"stopped_by,omitempty"`
}

// Detail es la ficha de una instancia.
type Detail struct {
	Spec  *instance.Spec `json:"spec"`
	State string         `json:"state"`
	// Host es la IP del tailnet para la tarjeta de conexión.
	Host string `json:"host,omitempty"`
}

// Lifecycle gestiona el ciclo de vida.
type Lifecycle interface {
	Start(ctx context.Context, slug string) error
	Stop(ctx context.Context, slug string) error
	Close(ctx context.Context, slug string) error
	Restart(ctx context.Context, slug string) error
	// Delete elimina el contenedor y la carpeta de la instancia (P-03).
	Delete(ctx context.Context, slug string) error
}

// Backups gestiona las copias de seguridad.
type Backups interface {
	List(ctx context.Context, slug string) ([]backup.Info, error)
	Create(ctx context.Context, slug string) error
	Restore(ctx context.Context, slug, name string) error
	Prune(ctx context.Context, slug string, keep int) ([]string, error)
}

// Ports comprueba puertos.
type Ports interface {
	Check(ctx context.Context, port int) (ports.Result, error)
}

// DockerState consulta y repara Docker.
type DockerState interface {
	Status(ctx context.Context) (dockerx.Diagnosis, error)
	Ensure(ctx context.Context) (dockerx.Diagnosis, error)
}

// IdleCtrl gestiona el auto-apagado por inactividad.
type IdleCtrl interface {
	Config(ctx context.Context, slug string) (idle.Config, error)
	Set(ctx context.Context, slug string, cfg idle.Config) error
	// WatcherActive indica si hay un vigilante (TUI o demonio) latiendo.
	WatcherActive() bool
	// Watch corre el vigilante dentro del proceso hasta que ctx se cancele,
	// publicando cada evento en emit (RF-8 §6). Devuelve idle.ErrStandby si ya
	// hay otro vigilante (p. ej. el demonio) latiendo.
	Watch(ctx context.Context, emit func(events.Event)) error
}

// Catalog consulta las versiones disponibles de un motor (RF-5).
type Catalog interface {
	Versions(ctx context.Context, engine string, includeUnstable bool) ([]engines.Version, error)
}

// Creator crea instancias con el núcleo compartido (provision).
type Creator interface {
	Create(ctx context.Context, req provision.Request) (slug string, err error)
}

// Services es la fachada que la TUI conoce.
type Services struct {
	MaxParallel  int
	MaxInstances int
	DataDir      string
	Repo         string
	Engines      []engines.Engine
	Instances    Instances
	Lifecycle    Lifecycle
	Backups      Backups
	Ports        Ports
	DockerState  DockerState
	Idle         IdleCtrl
	Catalog      Catalog
	Creator      Creator
	Access       Access
	Worlds       Worlds
}

// New construye los adaptadores reales sobre los paquetes de dominio.
func New(cfg *config.Config) (*Services, error) {
	if _, err := cfg.EnsureDataDir(); err != nil {
		return nil, err
	}
	m := instance.New(cfg.DataDir, engineNames())
	rt := runtime.New(cfg)
	be := &backup.Backend{DataDir: cfg.DataDir}
	resolver := engines.NewRegistry(engines.DefaultClient())

	rng, err := ports.ParseRange(cfg.Ports.Range)
	if err != nil {
		return nil, err
	}
	mode, err := ports.ParseScanMode(cfg.Ports.TailnetScan)
	if err != nil {
		return nil, err
	}
	checker := &ports.DefaultChecker{
		Prober: ports.NewRealProber(func(ctx context.Context) ([]ports.RegistryPort, error) {
			specs, lerr := m.List()
			if lerr != nil {
				return nil, lerr
			}
			out := make([]ports.RegistryPort, 0, len(specs))
			for _, s := range specs {
				out = append(out, ports.RegistryPort{Instance: s.Slug, Port: s.Port})
			}
			return out, nil
		}),
		TailnetScan: mode,
	}

	alloc := ports.NewAllocator(cfg.DataDir, checker, rng)

	return &Services{
		MaxParallel:  cfg.MaxParallel,
		MaxInstances: cfg.MaxInstances,
		DataDir:      cfg.DataDir,
		Repo:         cfg.Repo,
		Engines:      engines.Order,
		Instances:    &realInstances{m: m, rt: rt},
		Lifecycle:    &realLifecycle{rt: rt, m: m},
		Backups:      &realBackups{be: be},
		Ports:        &allocPorts{a: alloc},
		DockerState:  &realDocker{cfg: cfg},
		Idle:         &realIdle{cfg: cfg, m: m, rt: rt, be: be},
		Catalog:      &realCatalog{cache: engines.NewCache(cfg.DataDir, resolver)},
		Creator:      &realCreator{cfg: cfg, m: m, rt: rt, resolver: resolver, alloc: alloc},
		Access:       &realAccess{m: m, rt: rt},
		Worlds:       &realWorlds{cfg: cfg, m: m, rt: rt, wm: &worlds.Manager{DataDir: cfg.DataDir}},
	}, nil
}

// realIdle gestiona la configuración de auto-apagado de una instancia y puede
// correr el vigilante dentro del proceso de la TUI (RF-8).
type realIdle struct {
	cfg *config.Config
	m   *instance.Manager
	rt  *runtime.Runtime
	be  *backup.Backend
}

func (r *realIdle) Config(_ context.Context, slug string) (idle.Config, error) {
	spec, err := r.m.Load(slug)
	if err != nil {
		return idle.Config{}, err
	}
	cfg := spec.Idle.Normalized()
	if cfg.Timeout <= 0 {
		if d, derr := idle.ParseDuration(r.cfg.Idle.DefaultTimeout); derr == nil && d > 0 {
			cfg.Timeout = d
		}
	}
	return cfg, nil
}

func (r *realIdle) Set(_ context.Context, slug string, cfg idle.Config) error {
	spec, err := r.m.Load(slug)
	if err != nil {
		return err
	}
	cfg = cfg.Normalized()
	if verr := cfg.Validate(); verr != nil {
		return verr
	}
	spec.Idle = cfg
	return r.m.Save(spec)
}

// WatcherActive indica si el demonio de auto-apagado está latiendo.
func (r *realIdle) WatcherActive() bool {
	return idle.WatcherActive(filepath.Join(r.cfg.DataDir, "idle.lock"))
}

// Watch corre el vigilante de inactividad en el proceso de la TUI (RF-8): toma
// el bloqueo (si el demonio ya vigila devuelve idle.ErrStandby), sondea las
// instancias con auto-apagado activo y publica cada evento de dominio en emit.
// Se detiene cuando ctx se cancela.
func (r *realIdle) Watch(ctx context.Context, emit func(events.Event)) error {
	if r.rt == nil || r.be == nil {
		return nil
	}
	lock, err := idle.Acquire(filepath.Join(r.cfg.DataDir, "idle.lock"))
	if err != nil {
		return err // incluye idle.ErrStandby
	}
	defer func() { _ = lock.Release() }()

	bus := events.New()
	defer bus.Close()
	sub, unsubscribe := bus.Subscribe(64)
	defer unsubscribe()

	w := idle.NewWatcher()
	w.Bus = bus
	w.Targets = r.idleTargets
	w.Counter = r.idleCounter()
	w.Controller = &runtimeIdleController{rt: r.rt, be: r.be}
	w.Announce = func(ctx context.Context, instance, message string) error {
		return r.rt.Console(ctx, instance, "say "+message)
	}
	w.Record = func(instance string, phase idle.Phase, _ idle.Decision) {
		if phase == idle.PhaseStopped {
			r.markStopped(instance)
		}
	}

	// Bomba de eventos: reenvía el bus al llamante.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-sub:
				if !ok {
					return
				}
				if emit != nil {
					emit(ev)
				}
			}
		}
	}()

	ticker := time.NewTicker(r.pollInterval())
	defer ticker.Stop()
	w.TickOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			_ = lock.Touch()
			w.TickOnce(ctx)
		}
	}
}

// idleTargets lista las instancias con auto-apagado activo que están corriendo.
func (r *realIdle) idleTargets(ctx context.Context) ([]idle.Target, error) {
	specs, err := r.m.List()
	if err != nil {
		return nil, err
	}
	var out []idle.Target
	for _, s := range specs {
		if !s.Idle.Enabled {
			continue
		}
		state, _ := r.rt.State(ctx, s.Slug)
		var started time.Time
		if s.StoppedAt != "" {
			if t, perr := time.Parse(time.RFC3339, s.StoppedAt); perr == nil {
				started = t
			}
		}
		out = append(out, idle.Target{
			Instance: s.Slug, Config: s.Idle, Running: state == "running", StartedAt: started,
		})
	}
	return out, nil
}

// idleCounter mide jugadores por Server List Ping y, como respaldo, con RCON.
func (r *realIdle) idleCounter() idle.Counter {
	slp := &idle.SLPCounter{
		Timeout: 2 * time.Second,
		Endpoint: func(instanceName string) (string, int, bool) {
			spec, err := r.m.Load(instanceName)
			if err != nil || spec.Port <= 0 {
				return "", 0, false
			}
			host := ""
			if spec.Connection != nil {
				host = spec.Connection.Host
			}
			if host == "" {
				pctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				host, _ = tailscale.ResolveIPv4(pctx)
			}
			if host == "" {
				return "", 0, false
			}
			return host, spec.Port, true
		},
	}
	docker := &idle.DockerCounter{Container: func(instanceName string) string {
		return r.rt.ContainerName(instanceName)
	}}
	return idle.ChainCounter{slp, docker}
}

// pollInterval elige el intervalo de sondeo del vigilante: el mínimo de las
// instancias (por defecto 15 s), con un suelo de 5 s.
func (r *realIdle) pollInterval() time.Duration {
	const def = 15 * time.Second
	specs, err := r.m.List()
	if err != nil {
		return def
	}
	best := time.Duration(0)
	for _, s := range specs {
		p := s.Idle.PollInterval
		if !s.Idle.Enabled || p <= 0 {
			continue
		}
		if best == 0 || p < best {
			best = p
		}
	}
	if best < 5*time.Second {
		if best == 0 {
			return def
		}
		return 5 * time.Second
	}
	return best
}

// markStopped deja rastro del apagado por inactividad en el registro.
func (r *realIdle) markStopped(slug string) {
	spec, err := r.m.Load(slug)
	if err != nil {
		return
	}
	spec.StoppedBy = "idle"
	spec.StoppedAt = time.Now().UTC().Format(time.RFC3339)
	_ = r.m.Save(spec)
}

// runtimeIdleController ejecuta la parada limpia sobre el runtime real.
type runtimeIdleController struct {
	rt *runtime.Runtime
	be *backup.Backend
}

func (c *runtimeIdleController) Running(ctx context.Context, instance string) (bool, error) {
	st, err := c.rt.State(ctx, instance)
	return st == "running", err
}

func (c *runtimeIdleController) Flush(ctx context.Context, instance string) error {
	return c.rt.Console(ctx, instance, "save-all flush")
}

func (c *runtimeIdleController) Stop(ctx context.Context, instance string) error {
	return c.rt.Stop(ctx, instance)
}

func (c *runtimeIdleController) Backup(ctx context.Context, instance string) error {
	_, err := c.be.Create(instance, "")
	return err
}

// realCatalog consulta las versiones con caché y respaldo embebido (RF-5).
type realCatalog struct{ cache *engines.Cache }

func (c *realCatalog) Versions(ctx context.Context, engine string, includeUnstable bool) ([]engines.Version, error) {
	list, _, err := c.cache.Versions(ctx, engine, engines.VersionOpts{IncludeUnstable: includeUnstable})
	return list, err
}

// realCreator crea instancias con el núcleo compartido.
type realCreator struct {
	cfg      *config.Config
	m        *instance.Manager
	rt       *runtime.Runtime
	resolver provision.Resolver
	alloc    *ports.Allocator
}

func (c *realCreator) Create(ctx context.Context, req provision.Request) (string, error) {
	res, err := provision.Create(ctx, provision.Deps{
		Cfg: c.cfg, Manager: c.m, Resolver: c.resolver, Runtime: c.rt, Ports: c.alloc,
	}, req)
	if err != nil {
		if res != nil && res.Spec != nil {
			return res.Spec.Slug, err
		}
		return "", err
	}
	return res.Spec.Slug, nil
}

type realInstances struct {
	m  *instance.Manager
	rt *runtime.Runtime
}

func (r *realInstances) Rows(ctx context.Context) ([]Row, error) {
	specs, err := r.m.List()
	if err != nil {
		return nil, err
	}
	host, _ := r.rt.HostIP(ctx)
	out := make([]Row, len(specs))
	var wg sync.WaitGroup
	for i, s := range specs {
		wg.Add(1)
		go func(i int, s *instance.Spec) {
			defer wg.Done()
			out[i] = r.row(ctx, s, host)
		}(i, s)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

func (r *realInstances) row(ctx context.Context, s *instance.Spec, host string) Row {
	state, _ := r.rt.State(ctx, s.Slug)
	row := Row{
		Slug: s.Slug, Name: s.Name, Engine: s.Engine, Version: s.Version,
		Memory: s.Memory, Port: s.Port, State: state,
		OnlineMode: s.OnlineMode, MaxPlayers: s.MaxPlayers,
		Host: host, PlayersOnline: -1,
		IdleEnabled: s.Idle.Enabled, IdleTimeout: s.Idle.Timeout,
		StoppedBy: s.StoppedBy,
	}
	if state == "running" {
		if cpu, used, limit, serr := tps.ContainerStats(ctx, r.rt.ContainerName(s.Slug)); serr == nil {
			_ = cpu
			row.MemUsedMB, row.MemLimitMB = used, limit
		}
		if host != "" && s.Port > 0 {
			pctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			if st, perr := idle.Ping(pctx, host, s.Port, 500*time.Millisecond); perr == nil {
				row.PlayersOnline = st.Players.Online
				if st.Players.Max > 0 {
					row.MaxPlayers = st.Players.Max
				}
			}
			cancel()
		}
	}
	return row
}

func (r *realInstances) Detail(ctx context.Context, slug string) (*Detail, error) {
	spec, err := r.m.Load(slug)
	if err != nil {
		return nil, err
	}
	state, _ := r.rt.State(ctx, slug)
	host, _ := r.rt.HostIP(ctx)
	return &Detail{Spec: spec, State: state, Host: host}, nil
}

func (r *realInstances) Logs(ctx context.Context, slug string, lines int) (string, error) {
	return r.rt.Logs(ctx, slug, lines)
}

func (r *realInstances) Console(ctx context.Context, slug, command string) error {
	return r.rt.Console(ctx, slug, command)
}

func (r *realInstances) Save(_ context.Context, spec *instance.Spec) error {
	return r.m.Save(spec)
}

type realLifecycle struct {
	rt *runtime.Runtime
	m  *instance.Manager
}

func (l *realLifecycle) Start(ctx context.Context, slug string) error {
	spec, err := l.m.Load(slug)
	if err != nil {
		return err
	}
	return l.rt.Start(ctx, spec)
}

func (l *realLifecycle) Stop(ctx context.Context, slug string) error {
	return l.rt.Stop(ctx, slug)
}

func (l *realLifecycle) Close(ctx context.Context, slug string) error {
	return l.rt.Close(ctx, slug)
}

func (l *realLifecycle) Restart(ctx context.Context, slug string) error {
	return l.rt.Restart(ctx, slug)
}

// Delete quita el contenedor y borra la carpeta de la instancia (P-03).
func (l *realLifecycle) Delete(ctx context.Context, slug string) error {
	if l.rt != nil {
		_ = l.rt.Down(ctx, slug)
	}
	return l.m.Remove(slug)
}

type realBackups struct{ be *backup.Backend }

func (b *realBackups) List(ctx context.Context, slug string) ([]backup.Info, error) {
	return b.be.List(slug)
}

func (b *realBackups) Create(ctx context.Context, slug string) error {
	_, err := b.be.Create(slug, "")
	return err
}

func (b *realBackups) Restore(ctx context.Context, slug, name string) error {
	// Restaurar siempre crea un backup previo (P-03).
	if _, err := b.be.Create(slug, "pre-restore"); err != nil {
		return err
	}
	return b.be.Restore(slug, name)
}

func (b *realBackups) Prune(ctx context.Context, slug string, keep int) ([]string, error) {
	return b.be.Prune(slug, keep)
}

type realDocker struct{ cfg *config.Config }

func (*realDocker) Status(ctx context.Context) (dockerx.Diagnosis, error) {
	return dockerx.Diagnose(ctx, dockerx.DefaultEnv()), nil
}

// Ensure intenta reparar Docker y espera a que responda (RF-6).
func (d *realDocker) Ensure(ctx context.Context) (dockerx.Diagnosis, error) {
	timeout := 120 * time.Second
	if d.cfg != nil {
		if dur, err := time.ParseDuration(d.cfg.Docker.EnsureTimeout); err == nil && dur > 0 {
			timeout = dur
		}
	}
	res := dockerx.Ensure(ctx, dockerx.DefaultEnv(), dockerx.Options{Timeout: timeout})
	return res.Diagnosis, nil
}

// allocPorts adapta *ports.Allocator a la interfaz Ports de la fachada.
type allocPorts struct{ a *ports.Allocator }

func (p *allocPorts) Check(ctx context.Context, port int) (ports.Result, error) {
	return p.a.Check(ctx, netip.Addr{}, port)
}
