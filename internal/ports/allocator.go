package ports

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
)

// Reservation es un puerto apartado para una instancia que aún no existe (o que
// se está creando), de forma que dos creadores concurrentes no colisionen.
type Reservation struct {
	Instance string    `json:"instance"`
	Port     int       `json:"port"`
	At       time.Time `json:"at"`
	Expires  time.Time `json:"expires,omitempty"`
}

// Expired indica si la reserva caducó en el instante dado.
func (r Reservation) Expired(now time.Time) bool {
	return !r.Expires.IsZero() && now.After(r.Expires)
}

// Store persiste las reservas con exclusión mutua. Update ejecuta fn bajo el
// bloqueo y guarda el mapa devuelto; Snapshot devuelve una copia de las reservas.
type Store interface {
	Update(ctx context.Context, fn func(map[string]Reservation) (map[string]Reservation, error)) error
	Snapshot(ctx context.Context) ([]Reservation, error)
}

// Allocator elige y reserva puertos libres sobre un Checker.
type Allocator struct {
	// Checker comprueba la disponibilidad real.
	Checker Checker
	// Range es el rango de puertos donde buscar.
	Range Range
	// Store persiste las reservas. Si es nil, se usa uno en memoria.
	Store Store
	// TTL es cuánto dura una reserva sin confirmar (por defecto 10 min).
	TTL time.Duration
	// Now permite inyectar un reloj en pruebas.
	Now func() time.Time
}

// NewAllocator construye un asignador con reservas en disco bajo dataDir.
func NewAllocator(dataDir string, checker Checker, rng Range) *Allocator {
	if rng.To == 0 {
		rng = DefaultRange
	}
	return &Allocator{
		Checker: checker,
		Range:   rng,
		Store:   NewFileStore(dataDir),
	}
}

func (a *Allocator) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Allocator) ttl() time.Duration {
	if a.TTL > 0 {
		return a.TTL
	}
	return 10 * time.Minute
}

func (a *Allocator) store() Store {
	if a.Store == nil {
		a.Store = NewMemoryStore()
	}
	return a.Store
}

// Reserve elige el primer puerto libre del rango (empezando por preferred) y lo
// deja reservado de forma atómica. Es idempotente: si la instancia ya tiene una
// reserva viva, devuelve la misma.
func (a *Allocator) Reserve(ctx context.Context, instance string, preferred int) (Reservation, error) {
	var out Reservation
	err := a.store().Update(ctx, func(cur map[string]Reservation) (map[string]Reservation, error) {
		now := a.now()
		for k, r := range cur {
			if k != instance && r.Expired(now) {
				delete(cur, k)
			}
		}
		if r, ok := cur[instance]; ok && r.Port != 0 && !r.Expired(now) {
			out = r
			return cur, nil
		}
		taken := map[int]bool{}
		for _, r := range cur {
			if r.Port != 0 {
				taken[r.Port] = true
			}
		}
		for _, p := range a.Range.candidates(preferred) {
			if taken[p] {
				continue
			}
			res, cerr := a.Checker.Check(ctx, netip.Addr{}, p)
			if cerr != nil {
				return nil, cerr
			}
			if res.Free {
				out = Reservation{Instance: instance, Port: p, At: now, Expires: now.Add(a.ttl())}
				cur[instance] = out
				return cur, nil
			}
		}
		return nil, capacityRangeExhausted(a.Range)
	})
	if err != nil {
		return Reservation{}, err
	}
	return out, nil
}

// Release elimina la reserva de una instancia (tras crearla o si falla).
func (a *Allocator) Release(ctx context.Context, instance string) error {
	return a.store().Update(ctx, func(cur map[string]Reservation) (map[string]Reservation, error) {
		delete(cur, instance)
		return cur, nil
	})
}

// Reservations devuelve las reservas vivas ordenadas por puerto.
func (a *Allocator) Reservations(ctx context.Context) ([]Reservation, error) {
	rs, err := a.store().Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	now := a.now()
	out := rs[:0]
	for _, r := range rs {
		if !r.Expired(now) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out, nil
}

// Suggest devuelve el primer puerto usable desde from, sin reservarlo.
func (a *Allocator) Suggest(ctx context.Context, from int) (int, error) {
	rs, err := a.store().Snapshot(ctx)
	if err != nil {
		return 0, err
	}
	now := a.now()
	taken := map[int]bool{}
	for _, r := range rs {
		if !r.Expired(now) {
			taken[r.Port] = true
		}
	}
	for _, p := range a.Range.candidates(from) {
		if taken[p] {
			continue
		}
		res, cerr := a.Checker.Check(ctx, netip.Addr{}, p)
		if cerr != nil {
			return 0, cerr
		}
		if res.Free {
			return p, nil
		}
	}
	return 0, capacityRangeExhausted(a.Range)
}

// Check comprueba un puerto sumando las reservas pendientes y propone una
// alternativa si está ocupado.
func (a *Allocator) Check(ctx context.Context, bind netip.Addr, port int) (Result, error) {
	res, err := a.Checker.Check(ctx, bind, port)
	if err != nil {
		return Result{}, err
	}
	now := a.now()
	if rs, serr := a.store().Snapshot(ctx); serr == nil {
		for _, r := range rs {
			if r.Port == port && !r.Expired(now) {
				res.Conflicts = append(res.Conflicts, Conflict{
					Port:     port,
					Severity: SeverityBlock,
					Owner:    Owner{Kind: OwnerReserved, Name: r.Instance, Detail: "reserva pendiente de «" + r.Instance + "»"},
				})
				res.Free = false
			}
		}
	}
	if !res.Free {
		if s, serr := a.Suggest(ctx, port+1); serr == nil {
			res.Suggested = s
		}
	}
	return res, nil
}

// capacityRangeExhausted construye el error tipado de rango agotado con el
// motivo estable en error.details.reason.
func capacityRangeExhausted(rng Range) error {
	return errx.Capacity(
		fmt.Sprintf("no hay puertos libres en el rango %d-%d", rng.From, rng.To),
		"amplía ports.range en la configuración o libera un puerto",
	).WithDetail("reason", "port_range_exhausted")
}
