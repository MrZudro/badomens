package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateListRestore(t *testing.T) {
	dir := t.TempDir()
	b := &Backend{DataDir: dir}
	slug := "prueba"

	// Mundo inicial
	server := filepath.Join(dir, "instances", slug, "server", "world")
	if err := os.MkdirAll(server, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(server, "level.dat"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := b.Create(slug, "one")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if info.Sha256 == "" || info.Size == 0 {
		t.Fatalf("info incompleta: %+v", info)
	}

	list, err := b.List(slug)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}

	// Cambiar el mundo y restaurar
	if err := os.WriteFile(filepath.Join(server, "level.dat"), []byte("v2-cambiado"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := b.Restore(slug, "one"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(server, "level.dat"))
	if err != nil || string(raw) != "v1" {
		t.Fatalf("tras restaurar level.dat = %q, %v (quiero v1)", raw, err)
	}

	removed, err := b.Prune(slug, 0)
	if err != nil || len(removed) != 1 {
		t.Fatalf("prune = %v, %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(b.Dir(slug), "one.zip")); !os.IsNotExist(err) {
		t.Fatalf("el zip debía desaparecer: %v", err)
	}
}

func TestRestoreUnknown(t *testing.T) {
	b := &Backend{DataDir: t.TempDir()}
	if err := b.Restore("x", "no-existe"); err == nil {
		t.Fatal("restaurar un backup inexistente debe fallar")
	}
}
