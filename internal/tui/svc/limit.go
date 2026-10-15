package svc

import "context"

// defaultExternalConcurrency acota cuántas consultas externas (estado por
// instancia, métricas del contenedor, Server List Ping) se lanzan a la vez. Sin
// límite, abrir la TUI con muchas instancias dispara N procesos/consultas en
// paralelo y satura el equipo (Fase 12.2).
const defaultExternalConcurrency = 4

// sem es un semáforo contador: acota la concurrencia de operaciones externas.
type sem chan struct{}

func newSem(n int) *sem {
	if n < 1 {
		n = 1
	}
	s := make(sem, n)
	return &s
}

// acquire toma un cupo (o devuelve el error del contexto si se cancela antes).
func (s *sem) acquire(ctx context.Context) error {
	select {
	case *s <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release devuelve un cupo.
func (s *sem) release() {
	select {
	case <-*s:
	default:
	}
}
