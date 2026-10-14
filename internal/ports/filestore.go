package ports

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// fileDoc es el documento persistido en disco.
type fileDoc struct {
	Version      int                    `json:"version"`
	Reservations map[string]Reservation `json:"reservations"`
}

// MemoryStore es una implementación en memoria de Store (para pruebas).
type MemoryStore struct {
	mu   sync.Mutex
	data map[string]Reservation
}

// NewMemoryStore crea un almacén en memoria.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: map[string]Reservation{}}
}

// Update ejecuta fn bajo exclusión mutua.
func (m *MemoryStore) Update(_ context.Context, fn func(map[string]Reservation) (map[string]Reservation, error)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur := map[string]Reservation{}
	for k, v := range m.data {
		cur[k] = v
	}
	next, err := fn(cur)
	if err != nil {
		return err
	}
	if next == nil {
		next = map[string]Reservation{}
	}
	m.data = next
	return nil
}

// Snapshot devuelve una copia de las reservas.
func (m *MemoryStore) Snapshot(_ context.Context) ([]Reservation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Reservation, 0, len(m.data))
	for _, r := range m.data {
		out = append(out, r)
	}
	return out, nil
}

// FileStore persiste las reservas en un JSON dentro del data dir y serializa el
// acceso con un archivo de bloqueo creado de forma exclusiva (portable).
type FileStore struct {
	// Path es la ruta del JSON de reservas.
	Path string
	// LockTimeout limita la espera por el bloqueo (por defecto 15 s).
	LockTimeout time.Duration
	// StaleAfter considera roto un bloqueo más antiguo que esto (por defecto 60 s).
	StaleAfter time.Duration
}

// NewFileStore crea un FileStore en <dataDir>/ports.json.
func NewFileStore(dataDir string) *FileStore {
	return &FileStore{Path: filepath.Join(dataDir, "ports.json")}
}

func (s *FileStore) lockTimeout() time.Duration {
	if s.LockTimeout > 0 {
		return s.LockTimeout
	}
	return 15 * time.Second
}

func (s *FileStore) staleAfter() time.Duration {
	if s.StaleAfter > 0 {
		return s.StaleAfter
	}
	return 60 * time.Second
}

// Update adquiere el bloqueo, lee el estado, ejecuta fn y guarda el resultado
// de forma atómica (temporal + rename).
func (s *FileStore) Update(ctx context.Context, fn func(map[string]Reservation) (map[string]Reservation, error)) error {
	unlock, err := s.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	cur, err := s.read()
	if err != nil {
		return err
	}
	next, err := fn(cur)
	if err != nil {
		return err
	}
	if next == nil {
		next = map[string]Reservation{}
	}
	return s.write(next)
}

// Snapshot devuelve una copia de las reservas de disco.
func (s *FileStore) Snapshot(ctx context.Context) ([]Reservation, error) {
	unlock, err := s.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	cur, err := s.read()
	if err != nil {
		return nil, err
	}
	out := make([]Reservation, 0, len(cur))
	for _, r := range cur {
		out = append(out, r)
	}
	return out, nil
}

func (s *FileStore) read() (map[string]Reservation, error) {
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]Reservation{}, nil
		}
		return nil, err
	}
	var doc fileDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		// Documento ilegible: se parte de cero (no se pierde nada crítico).
		return map[string]Reservation{}, nil
	}
	if doc.Reservations == nil {
		return map[string]Reservation{}, nil
	}
	return doc.Reservations, nil
}

func (s *FileStore) write(data map[string]Reservation) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	doc := fileDoc{Version: 1, Reservations: data}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// lock crea el archivo de bloqueo con O_EXCL, reintentando hasta el timeout. Si
// el bloqueo existente es más viejo que StaleAfter, se considera huérfano y se
// retira. Devuelve la función de liberación.
//
// En Windows, crear/borrar el lock en carrera puede devolver "Acceso denegado"
// de forma transitoria; por eso cualquier error que no sea de directorio se
// reintenta hasta el timeout.
func (s *FileStore) lock(ctx context.Context) (func(), error) {
	lockPath := s.Path + ".lock"
	deadline := time.Now().Add(s.lockTimeout())
	var lastErr error
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if errors.Is(err, fs.ErrExist) {
			if info, serr := os.Stat(lockPath); serr == nil && time.Since(info.ModTime()) > s.staleAfter() {
				_ = os.Remove(lockPath)
				continue
			}
		} else {
			// Puede ser el directorio inexistente o un acceso denegado
			// transitorio (Windows): se asegura el directorio y se reintenta.
			_ = os.MkdirAll(filepath.Dir(lockPath), 0o700)
			lastErr = err
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return nil, fmt.Errorf("no se pudo obtener el bloqueo de puertos (%s): %w", lockPath, lastErr)
			}
			return nil, fmt.Errorf("no se pudo obtener el bloqueo de puertos (%s); otro proceso lo retiene", lockPath)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}
