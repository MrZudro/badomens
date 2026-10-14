// Package svc es la fachada de servicios de la TUI v2 (Fase 0.2): interfaces
// pequeñas que la TUI consume y adaptadores reales sobre los paquetes de
// dominio. Los tests usan fakes (NewFake) sin Docker, red ni tailnet.
package svc

import (
	"context"
	"net/netip"
	"sort"

	"github.com/MrZudro/badomens/internal/backup"
	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/ports"
	"github.com/MrZudro/badomens/internal/provision"
	"github.com/MrZudro/badomens/internal/runtime"
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
}

// Detail es la ficha de una instancia.
type Detail struct {
	Spec  *instance.Spec `json:"spec"`
	State string         `json:"state"`
}

// Lifecycle gestiona el ciclo de vida.
type Lifecycle interface {
	Start(ctx context.Context, slug string) error
	Stop(ctx context.Context, slug string) error
	Close(ctx context.Context, slug string) error
	Restart(ctx context.Context, slug string) error
}

// Backups gestiona las copias de seguridad.
type Backups interface {
	List(ctx context.Context, slug string) ([]backup.Info, error)
	Create(ctx context.Context, slug string) error
}

// Ports comprueba puertos.
type Ports interface {
	Check(ctx context.Context, port int) (ports.Result, error)
}

// DockerState consulta el estado de Docker.
type DockerState interface {
	Status(ctx context.Context) (dockerx.Diagnosis, error)
}

// Creator crea instancias con el núcleo compartido (provision).
type Creator interface {
	Create(ctx context.Context, req provision.Request) (slug string, err error)
}

// Services es la fachada que la TUI conoce.
type Services struct {
	MaxParallel  int
	MaxInstances int
	Engines      []engines.Engine
	Instances    Instances
	Lifecycle    Lifecycle
	Backups      Backups
	Ports        Ports
	DockerState  DockerState
	Creator      Creator
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
		Engines:      engines.Order,
		Instances:    &realInstances{m: m, rt: rt},
		Lifecycle:    &realLifecycle{rt: rt, m: m},
		Backups:      &realBackups{be: be},
		Ports:        &allocPorts{a: alloc},
		DockerState:  &realDocker{},
		Creator:      &realCreator{cfg: cfg, m: m, rt: rt, resolver: resolver, alloc: alloc},
	}, nil
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
	out := make([]Row, 0, len(specs))
	for _, s := range specs {
		state, _ := r.rt.State(ctx, s.Slug)
		out = append(out, Row{
			Slug: s.Slug, Name: s.Name, Engine: s.Engine, Version: s.Version,
			Memory: s.Memory, Port: s.Port, State: state,
			OnlineMode: s.OnlineMode, MaxPlayers: s.MaxPlayers,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

func (r *realInstances) Detail(ctx context.Context, slug string) (*Detail, error) {
	spec, err := r.m.Load(slug)
	if err != nil {
		return nil, err
	}
	state, _ := r.rt.State(ctx, slug)
	return &Detail{Spec: spec, State: state}, nil
}

func (r *realInstances) Logs(ctx context.Context, slug string, lines int) (string, error) {
	return r.rt.Logs(ctx, slug, lines)
}

func (r *realInstances) Console(ctx context.Context, slug, command string) error {
	return r.rt.Console(ctx, slug, command)
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

type realBackups struct{ be *backup.Backend }

func (b *realBackups) List(ctx context.Context, slug string) ([]backup.Info, error) {
	return b.be.List(slug)
}

func (b *realBackups) Create(ctx context.Context, slug string) error {
	_, err := b.be.Create(slug, "")
	return err
}

type realDocker struct{}

func (*realDocker) Status(ctx context.Context) (dockerx.Diagnosis, error) {
	return dockerx.Diagnose(ctx, dockerx.DefaultEnv()), nil
}

// allocPorts adapta *ports.Allocator a la interfaz Ports de la fachada.
type allocPorts struct{ a *ports.Allocator }

func (p *allocPorts) Check(ctx context.Context, port int) (ports.Result, error) {
	return p.a.Check(ctx, netip.Addr{}, port)
}
