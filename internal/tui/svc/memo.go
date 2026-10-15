package svc

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/MrZudro/badomens/internal/engines"
)

// memoCatalog envuelve un Catalog y sirve lecturas REPETIDAS desde una caché en
// MEMORIA con TTL corto, además de la caché persistente del servicio de catálogo.
// No duplica la deduplicación de peticiones simultáneas: eso vive en la caché del
// catálogo (engines.Cache.load); aquí solo se evita repetir la MISMA lectura ya
// servida (volver a una versión/motor ya consultado). Refresh invalida las
// entradas del motor para no servir datos vencidos tras un refresco forzado.
type memoCatalog struct {
	inner Catalog
	ttl   time.Duration
	now   func() time.Time

	mu      sync.Mutex
	entries map[string]memoEntry
}

type memoEntry struct {
	value   any
	expires time.Time
}

// NewMemoCatalog envuelve un catálogo con una caché en memoria de TTL corto.
// ttl <= 0 usa 2 s; now == nil usa time.Now.
func NewMemoCatalog(inner Catalog, ttl time.Duration, now func() time.Time) Catalog {
	if ttl <= 0 {
		ttl = 2 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &memoCatalog{inner: inner, ttl: ttl, now: now, entries: map[string]memoEntry{}}
}

func (m *memoCatalog) get(key string) (any, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[key]
	if !ok {
		return nil, false
	}
	if !m.now().Before(e.expires) {
		delete(m.entries, key)
		return nil, false
	}
	return e.value, true
}

func (m *memoCatalog) put(key string, v any) {
	m.mu.Lock()
	m.entries[key] = memoEntry{value: v, expires: m.now().Add(m.ttl)}
	m.mu.Unlock()
}

// invalidate borra las entradas de un motor (tras un refresco forzado).
func (m *memoCatalog) invalidate(engine string) {
	eng := strings.ToLower(strings.TrimSpace(engine))
	m.mu.Lock()
	for k := range m.entries {
		if parts := strings.SplitN(k, "|", 3); len(parts) >= 2 && parts[1] == eng {
			delete(m.entries, k)
		}
	}
	m.mu.Unlock()
}

func (m *memoCatalog) Versions(ctx context.Context, engine string, includeUnstable bool) (engines.ListResult, error) {
	key := "versions|" + strings.ToLower(strings.TrimSpace(engine)) + "|" + boolKey(includeUnstable)
	if v, ok := m.get(key); ok {
		return v.(engines.ListResult), nil
	}
	res, err := m.inner.Versions(ctx, engine, includeUnstable)
	if err != nil {
		return res, err
	}
	m.put(key, res)
	return res, nil
}

func (m *memoCatalog) Resolve(ctx context.Context, engine, version string) (engines.Resolved, error) {
	key := "resolve|" + strings.ToLower(strings.TrimSpace(engine)) + "|" + strings.TrimSpace(version)
	if v, ok := m.get(key); ok {
		return v.(engines.Resolved), nil
	}
	res, err := m.inner.Resolve(ctx, engine, version)
	if err != nil {
		return res, err
	}
	m.put(key, res)
	return res, nil
}

func (m *memoCatalog) Status(ctx context.Context, engine string) (engines.CatalogStatus, error) {
	key := "status|" + strings.ToLower(strings.TrimSpace(engine))
	if v, ok := m.get(key); ok {
		return v.(engines.CatalogStatus), nil
	}
	res, err := m.inner.Status(ctx, engine)
	if err != nil {
		return res, err
	}
	m.put(key, res)
	return res, nil
}

// Refresh SIEMPRE consulta al proveedor (ignora la caché en memoria) e invalida
// las entradas del motor para que la siguiente lectura no sirva lo viejo.
func (m *memoCatalog) Refresh(ctx context.Context, engine string) (engines.RefreshResult, error) {
	res, err := m.inner.Refresh(ctx, engine)
	m.invalidate(engine)
	return res, err
}

func boolKey(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
