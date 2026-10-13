package ports

import (
	"context"
	"sync"
	"testing"
)

func TestFileStorePersisteYRelee(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	ctx := context.Background()
	if err := store.Update(ctx, func(m map[string]Reservation) (map[string]Reservation, error) {
		m["alpha"] = Reservation{Instance: "alpha", Port: 25565}
		return m, nil
	}); err != nil {
		t.Fatal(err)
	}
	// Otro FileStore sobre el mismo directorio debe ver la reserva (persistencia).
	other := NewFileStore(dir)
	rs, err := other.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].Port != 25565 {
		t.Fatalf("quiero la reserva persistida, tengo %+v", rs)
	}
	if _, err := other.Snapshot(ctx); err != nil {
		t.Fatalf("el bloqueo debe liberarse: %v", err)
	}
}

func TestFileStoreConcurrenteNoPierdeReservas(t *testing.T) {
	// Varios FileStore sobre el mismo directorio simulan procesos distintos.
	dir := t.TempDir()
	ctx := context.Background()
	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := NewFileStore(dir)
			name := "inst" + string(rune('a'+i))
			if err := s.Update(ctx, func(m map[string]Reservation) (map[string]Reservation, error) {
				m[name] = Reservation{Instance: name, Port: 25565 + i}
				return m, nil
			}); err != nil {
				t.Errorf("update %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	rs, err := NewFileStore(dir).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != n {
		t.Fatalf("quiero %d reservas, tengo %d", n, len(rs))
	}
}

func TestMemoryStoreCopiaDefensiva(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	_ = s.Update(ctx, func(m map[string]Reservation) (map[string]Reservation, error) {
		m["a"] = Reservation{Instance: "a", Port: 1}
		return m, nil
	})
	rs, _ := s.Snapshot(ctx)
	rs[0].Port = 999 // mutar la copia no debe afectar al almacén
	again, _ := s.Snapshot(ctx)
	if again[0].Port != 1 {
		t.Fatalf("Snapshot debe devolver una copia: %+v", again)
	}
}
