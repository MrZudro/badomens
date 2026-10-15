package svc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/backup"
)

// writeFile escribe un archivo creando sus carpetas.
func writeBackupTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readFile lee un archivo (vacío si no existe).
func readBackupTestFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- ruta de prueba dentro de t.TempDir()
	if err != nil {
		return ""
	}
	return string(raw)
}

// TestBackupsRestaurarValidaChecksum (BK-02/§13): la restauración valida el
// sha256 ANTES de modificar los datos: un backup dañado o sin checksum no se
// aplica y server/ queda intacto.
func TestBackupsRestaurarValidaChecksum(t *testing.T) {
	dir := t.TempDir()
	be := &backup.Backend{DataDir: dir}
	server := filepath.Join(dir, "instances", "alpha", "server")
	world := filepath.Join(server, "world", "level.dat")
	writeBackupTestFile(t, world, "mundo original")
	if _, err := be.Create("alpha", "copia"); err != nil {
		t.Fatalf("crear backup: %v", err)
	}
	zipPath := filepath.Join(dir, "instances", "alpha", "backups", "copia.zip")
	rb := &realBackups{be: be}
	ctx := context.Background()

	// Restauración correcta: los datos vuelven al contenido del backup.
	writeBackupTestFile(t, world, "estado modificado")
	if err := rb.Restore(ctx, "alpha", "copia"); err != nil {
		t.Fatalf("restaurar un backup íntegro: %v", err)
	}
	if got := readBackupTestFile(t, world); got != "mundo original" {
		t.Fatalf("tras restaurar el mundo debe ser el del backup, tengo %q", got)
	}

	// Backup DAÑADO: el checksum ya no coincide.
	pre := filepath.Join(dir, "instances", "alpha", "backups", "pre-restore.zip")
	if err := os.Remove(pre); err != nil {
		t.Fatal(err)
	}
	writeBackupTestFile(t, world, "no se debe tocar")
	writeBackupTestFile(t, zipPath, "contenido alterado")
	err := rb.Restore(ctx, "alpha", "copia")
	if err == nil {
		t.Fatal("un backup cuyo checksum no coincide no debe restaurarse")
	}
	if !strings.Contains(err.Error(), "dañado") || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("el error debe explicar el checksum, tengo %q", err.Error())
	}
	if got := readBackupTestFile(t, world); got != "no se debe tocar" {
		t.Fatalf("no debe tocarse server/ si la validación falla, tengo %q", got)
	}
	if readBackupTestFile(t, pre) != "" {
		t.Fatal("no debe crearse el backup previo si la validación falla antes")
	}

	// Backup SIN checksum: no se puede verificar, así que no se aplica.
	if err := os.Remove(zipPath + ".sha256"); err != nil {
		t.Fatal(err)
	}
	err = rb.Restore(ctx, "alpha", "copia")
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("sin checksum no debe restaurarse, tengo %v", err)
	}
	if got := readBackupTestFile(t, world); got != "no se debe tocar" {
		t.Fatalf("server/ debe seguir intacto, tengo %q", got)
	}
}
