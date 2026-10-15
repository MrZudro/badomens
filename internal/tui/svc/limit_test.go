package svc

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// PERF-07 (Fase 12.2): el semáforo acota la concurrencia de operaciones externas:
// nunca hay más de N en vuelo a la vez.
func TestSemAcotaConcurrencia(t *testing.T) {
	const limit = 3
	const total = 30
	s := newSem(limit)
	ctx := context.Background()
	var inFlight, maxSeen int32
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.acquire(ctx); err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			defer s.release()
			cur := atomic.AddInt32(&inFlight, 1)
			for {
				old := atomic.LoadInt32(&maxSeen)
				if cur <= old || atomic.CompareAndSwapInt32(&maxSeen, old, cur) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			atomic.AddInt32(&inFlight, -1)
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&maxSeen); got > limit {
		t.Fatalf("el semáforo debe limitar a %d operaciones a la vez, superó con %d", limit, got)
	}
	if got := atomic.LoadInt32(&maxSeen); got < 2 {
		t.Fatalf("el semáforo debe permitir concurrencia real (>=2), tengo %d", got)
	}
}

// El semáforo respeta la cancelación del contexto al esperar un cupo.
func TestSemCancelaEnEspera(t *testing.T) {
	s := newSem(1)
	if err := s.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.acquire(ctx); err == nil {
		t.Fatal("con el cupo tomado y el contexto cancelado, acquire debe fallar")
	}
	s.release()
}
