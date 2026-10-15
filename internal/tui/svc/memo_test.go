package svc

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MrZudro/badomens/internal/engines"
)

// contandoCatalog cuenta cuántas veces se consulta el catálogo por dentro, para
// comprobar que la caché en memoria sirve las lecturas repetidas.
type contandoCatalog struct {
	versions, resolve, status, refresh int32
}

func (c *contandoCatalog) Versions(context.Context, string, bool) (engines.ListResult, error) {
	atomic.AddInt32(&c.versions, 1)
	return engines.ListResult{Engine: "paper", Versions: []engines.Version{{Version: "1.21.4"}}}, nil
}

func (c *contandoCatalog) Resolve(_ context.Context, engine, version string) (engines.Resolved, error) {
	atomic.AddInt32(&c.resolve, 1)
	return engines.Resolved{Version: version, Pins: map[string]string{"PAPER_BUILD": "1"}, Source: engine}, nil
}

func (c *contandoCatalog) Status(context.Context, string) (engines.CatalogStatus, error) {
	atomic.AddInt32(&c.status, 1)
	return engines.CatalogStatus{Engine: "paper"}, nil
}

func (c *contandoCatalog) Refresh(context.Context, string) (engines.RefreshResult, error) {
	atomic.AddInt32(&c.refresh, 1)
	return engines.RefreshResult{Engine: "paper"}, nil
}

// PERF-06 (Fase 12.2): la caché en MEMORIA sirve lecturas repetidas sin volver a
// tocar el servicio; el TTL la caduca y Refresh la invalida.
func TestMemoCatalogLecturasRepetidas(t *testing.T) {
	inner := &contandoCatalog{}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	m := NewMemoCatalog(inner, 2*time.Second, clock)
	ctx := context.Background()

	// Versions: la segunda lectura no consulta al servicio.
	if _, err := m.Versions(ctx, "paper", false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Versions(ctx, "paper", false); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&inner.versions); got != 1 {
		t.Fatalf("Versions repetidas deben servirse de memoria, tengo %d consultas", got)
	}
	// Otro filtro es una clave distinta: sí consulta.
	if _, err := m.Versions(ctx, "paper", true); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&inner.versions); got != 2 {
		t.Fatalf("includeUnstable distinto debe ser otra entrada, tengo %d", got)
	}

	// Resolve y Status también se memorizan.
	_, _ = m.Resolve(ctx, "paper", "1.21.4")
	_, _ = m.Resolve(ctx, "paper", "1.21.4")
	if got := atomic.LoadInt32(&inner.resolve); got != 1 {
		t.Fatalf("Resolve repetido debe servirse de memoria, tengo %d", got)
	}
	_, _ = m.Status(ctx, "paper")
	_, _ = m.Status(ctx, "paper")
	if got := atomic.LoadInt32(&inner.status); got != 1 {
		t.Fatalf("Status repetido debe servirse de memoria, tengo %d", got)
	}

	// Refresh fuerza la consulta e invalida el motor: la siguiente Versions
	// vuelve a preguntar.
	if _, err := m.Refresh(ctx, "paper"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&inner.refresh); got != 1 {
		t.Fatalf("Refresh debe consultar al proveedor, tengo %d", got)
	}
	if _, err := m.Versions(ctx, "paper", false); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&inner.versions); got != 3 {
		t.Fatalf("tras Refresh la Versions debe volver a consultar, tengo %d", got)
	}

	// Pasado el TTL, la caché en memoria caduca.
	now = now.Add(3 * time.Second)
	if _, err := m.Versions(ctx, "paper", false); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&inner.versions); got != 4 {
		t.Fatalf("pasado el TTL debe volver a consultar, tengo %d", got)
	}
}
