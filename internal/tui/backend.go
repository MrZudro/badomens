package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/manuelserver/badomens/internal/backup"
	"github.com/manuelserver/badomens/internal/config"
	"github.com/manuelserver/badomens/internal/engines"
	"github.com/manuelserver/badomens/internal/errx"
	"github.com/manuelserver/badomens/internal/instance"
	"github.com/manuelserver/badomens/internal/runtime"
	"github.com/manuelserver/badomens/internal/tps"
)

// Backend es la frontera entre la TUI y el mundo real. El modelo bubbletea solo
// conoce esta interfaz: así los tests unitarios usan un fake sin Docker.
type Backend interface {
	// Config devuelve los límites de paralelas e instancias registradas.
	Config() (maxParallel, maxInstances int)
	// ListRows devuelve filas (spec + estado) y cuántas están corriendo.
	ListRows(ctx context.Context) (rows []Row, running int, err error)
	// Health analiza el TPS/MSPT de las instancias corriendo. Lento: solo bajo demanda.
	Health(ctx context.Context) (map[string]string, error)
	// Detail devuelve la ficha completa de una instancia.
	Detail(ctx context.Context, slug string) (*Detail, error)
	// Logs devuelve las últimas lines líneas de logs.
	Logs(ctx context.Context, slug string, lines int) (string, error)
	// Backups lista los backups de una instancia.
	Backups(ctx context.Context, slug string) ([]backup.Info, error)
	Start(ctx context.Context, slug string, force bool) error
	Stop(ctx context.Context, slug string) error
	Close(ctx context.Context, slug string) error
	Restart(ctx context.Context, slug string) error
	CreateBackup(ctx context.Context, slug string) error
	PruneBackups(ctx context.Context, slug string, keep int) ([]string, error)
	Console(ctx context.Context, slug, command string) error
}

// realBackend implementa Backend sobre los paquetes internos de badomens.
type realBackend struct {
	cfg *config.Config
	rt  *runtime.Runtime
	m   *instance.Manager
	be  *backup.Backend
}

// newBackend construye el backend real preparando el data dir.
func newBackend(cfg *config.Config) (*realBackend, error) {
	if _, err := cfg.EnsureDataDir(); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(engines.Order))
	for _, e := range engines.Order {
		names = append(names, e.Name)
	}
	return &realBackend{
		cfg: cfg,
		rt:  runtime.New(cfg),
		m:   instance.New(cfg.DataDir, names),
		be:  &backup.Backend{DataDir: cfg.DataDir},
	}, nil
}

func (b *realBackend) Config() (int, int) { return b.cfg.MaxParallel, b.cfg.MaxInstances }

// ListRows lee el registro y consulta el estado de cada contenedor en paralelo
// (el CLI lo hace en serie; aquí el panel refresca cada pocos segundos).
func (b *realBackend) ListRows(ctx context.Context) ([]Row, int, error) {
	specs, err := b.m.List()
	if err != nil {
		return nil, 0, err
	}
	rows := make([]Row, len(specs))
	running := 0
	var wg sync.WaitGroup
	for i, s := range specs {
		wg.Add(1)
		go func(i int, s *instance.Spec) {
			defer wg.Done()
			state, _ := b.rt.State(ctx, s.Slug) // best-effort, igual que el CLI
			rows[i] = Row{
				Slug: s.Slug, Name: s.Name, Engine: s.Engine, Version: s.Version,
				Memory: s.Memory, CPU: s.CPU, Port: s.Port, State: state,
				Gamemode: s.Gamemode, OnlineMode: s.OnlineMode, MaxPlayers: s.MaxPlayers,
			}
		}(i, s)
	}
	wg.Wait()
	sort.Slice(rows, func(i, j int) bool { return rows[i].Slug < rows[j].Slug })
	for _, r := range rows {
		if r.State == "running" {
			running++
		}
	}
	return rows, running, nil
}

// Health recorre las instancias corriendo con doctor tps y devuelve la
// puntuación por slug. Las detenidas se marcan como "not-running" sin medir.
func (b *realBackend) Health(ctx context.Context) (map[string]string, error) {
	specs, err := b.m.List()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(specs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 2) // como mucho 2 diagnósticos a la vez
	for _, s := range specs {
		wg.Add(1)
		go func(s *instance.Spec) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			if st, _ := b.rt.State(ctx, s.Slug); st != "running" {
				mu.Lock()
				out[s.Slug] = "not-running"
				mu.Unlock()
				return
			}
			rep, err := tps.Collect(ctx, b.rt, s.Slug, s.Engine, tps.Options{})
			mu.Lock()
			if err != nil {
				out[s.Slug] = "error"
			} else if rep.Score == "" {
				out[s.Slug] = "unknown"
			} else {
				out[s.Slug] = rep.Score
			}
			mu.Unlock()
		}(s)
	}
	wg.Wait()
	return out, nil
}

func (b *realBackend) Detail(ctx context.Context, slug string) (*Detail, error) {
	spec, err := b.m.Load(slug)
	if err != nil {
		return nil, err
	}
	state, _ := b.rt.State(ctx, slug)
	d := &Detail{Spec: spec, State: state}
	if ip, err := b.rt.HostIP(ctx); err == nil {
		d.HostIP = ip
	}
	var met *Metrics
	if state == "running" {
		if cpu, used, limit, err := tps.ContainerStats(ctx, b.rt.ContainerName(slug)); err == nil {
			met = &Metrics{CPUPercent: cpu, MemUsedMB: used, MemLimitMB: limit}
		}
	}
	if met == nil {
		met = &Metrics{}
	}
	met.WorldMB = dirSizeMB(filepath.Join(b.cfg.DataDir, "instances", slug, "server", "world"))
	d.Metrics = met
	bks, err := b.be.List(slug)
	if err == nil {
		d.Backups = bks
	}
	return d, nil
}

func (b *realBackend) Logs(ctx context.Context, slug string, lines int) (string, error) {
	return b.rt.Logs(ctx, slug, lines)
}

func (b *realBackend) Backups(ctx context.Context, slug string) ([]backup.Info, error) {
	return b.be.List(slug)
}

func (b *realBackend) Start(ctx context.Context, slug string, force bool) error {
	spec, err := b.m.Load(slug)
	if err != nil {
		return err
	}
	state, err := b.rt.State(ctx, slug)
	if err != nil {
		return err
	}
	if state == "running" {
		return nil // idempotente
	}
	if !force {
		specs, _ := b.m.List()
		n, err := b.rt.RunningCount(ctx, specs)
		if err != nil {
			return err
		}
		if n >= b.cfg.MaxParallel {
			return errx.Capacity(
				fmt.Sprintf("ya hay %d instancias en paralelo (máximo %d)", n, b.cfg.MaxParallel),
				"detén una antes de arrancar otra",
			)
		}
	}
	return b.rt.Start(ctx, spec)
}

func (b *realBackend) Stop(ctx context.Context, slug string) error {
	return b.rt.Stop(ctx, slug)
}

func (b *realBackend) Close(ctx context.Context, slug string) error {
	return b.rt.Close(ctx, slug)
}

func (b *realBackend) Restart(ctx context.Context, slug string) error {
	return b.rt.Restart(ctx, slug)
}

func (b *realBackend) CreateBackup(ctx context.Context, slug string) error {
	_, err := b.be.Create(slug, "")
	return err
}

func (b *realBackend) PruneBackups(ctx context.Context, slug string, keep int) ([]string, error) {
	return b.be.Prune(slug, keep)
}

func (b *realBackend) Console(ctx context.Context, slug, command string) error {
	return b.rt.Console(ctx, slug, command)
}

// dirSizeMB suma el tamaño de un directorio en MiB con un decimal (copia de la
// utilidad del CLI de métricas).
func dirSizeMB(dir string) float64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return float64(total) / (1024 * 1024)
}
