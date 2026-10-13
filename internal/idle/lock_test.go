package idle

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLockExclusivoYRecuperacion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watch.lock")

	l1, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := l1.Touch(); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if _, err := Acquire(path); err != ErrStandby {
		t.Fatalf("segundo vigilante: quiero ErrStandby, tengo %v", err)
	}
	if err := l1.Release(); err != nil {
		t.Fatal(err)
	}
	l2, err := Acquire(path)
	if err != nil {
		t.Fatalf("tras liberar debe poderse adquirir: %v", err)
	}
	_ = l2.Release()
}

func TestLockHuérfanoSeRecupera(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watch.lock")
	l1, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	// Forzar que el lock pare huérfano (viejo) para que se recupere.
	old := time.Now().Add(-lockStaleAfter - time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	l2, err := Acquire(path)
	if err != nil {
		t.Fatalf("un lock huérfano debe recuperarse: %v", err)
	}
	_ = l1.Release()
	_ = l2.Release()
}
