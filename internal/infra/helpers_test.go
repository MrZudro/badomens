package infra

import (
	"os"
	"path/filepath"
	"time"
)

// writeHeartbeat crea el pidfile del vigilante con latido reciente, simulando un
// daemon vivo sin lanzar procesos.
func writeHeartbeat(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte("12345\n"), 0o600); err != nil {
		return err
	}
	now := time.Now()
	return os.Chtimes(path, now, now)
}
