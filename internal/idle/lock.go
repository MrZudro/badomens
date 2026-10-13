package idle

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// ErrStandby indica que ya hay un vigilante activo (el segundo queda en espera).
var ErrStandby = errors.New("ya hay un vigilante de inactividad activo")

// lockStaleAfter es cuánto puede estar un pidfile sin latido antes de
// considerarlo huérfano.
const lockStaleAfter = 5 * time.Minute

// Lock es un bloqueo exclusivo basado en un pidfile con latido.
type Lock struct {
	path     string
	released bool
}

// Acquire intenta tomar el bloqueo. Si ya hay uno vivo devuelve ErrStandby.
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	for i := 0; i < 2; i++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			return &Lock{path: path}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		if info, serr := os.Stat(path); serr == nil && time.Since(info.ModTime()) > lockStaleAfter {
			_ = os.Remove(path) // huérfano: se recupera
			continue
		}
		return nil, ErrStandby
	}
	return nil, ErrStandby
}

// Touch actualiza el latido del bloqueo.
func (l *Lock) Touch() error {
	if l == nil || l.released {
		return nil
	}
	now := time.Now()
	return os.Chtimes(l.path, now, now)
}

// Release libera el bloqueo. Es idempotente.
func (l *Lock) Release() error {
	if l == nil || l.released {
		return nil
	}
	l.released = true
	if err := os.Remove(l.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
