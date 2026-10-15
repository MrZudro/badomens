package worlds

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/MrZudro/badomens/internal/errx"
)

func TestValidateNameRechazaRutas(t *testing.T) {
	for _, name := range []string{"", " ", "..", "../fuera", "a/b", `a\b`, ".oculto", "con espacio ", "x\n", "../../etc"} {
		if err := ValidateName(name); err == nil {
			t.Fatalf("%q debería rechazarse", name)
		} else if !errx.Is(err, errx.CodeUsage) {
			t.Fatalf("%q: código = %v", name, err)
		}
	}
	for _, name := range []string{"arena", "mundo 1", "v1.2", "copia-2026"} {
		if err := ValidateName(name); err != nil {
			t.Fatalf("%q debería aceptarse: %v", name, err)
		}
	}
}

// TestSaveWorldNoEscapaDeLaBiblioteca: un nombre con ruta no puede escribir
// fuera de <data>/worlds.
func TestSaveWorldNoEscapaDeLaBiblioteca(t *testing.T) {
	dataDir := t.TempDir()
	m := &Manager{DataDir: dataDir}
	instDir := filepath.Join(dataDir, "instances", "demo")
	if err := os.MkdirAll(filepath.Join(instDir, "server", "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instDir, "server", "world", "level.dat"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../fuera", "a/b", ".."} {
		if err := m.SaveWorld(instDir, name); err == nil {
			t.Fatalf("SaveWorld(%q) debería fallar", name)
		}
		if err := m.UseWorld(instDir, name); err == nil {
			t.Fatalf("UseWorld(%q) debería fallar", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dataDir, "fuera")); err == nil {
		t.Fatal("se escribió fuera de la biblioteca de mundos")
	}
	if err := m.SaveWorld(instDir, "arena"); err != nil {
		t.Fatalf("SaveWorld válido: %v", err)
	}
	names, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "arena" {
		t.Fatalf("biblioteca = %v", names)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(dataDir, "worlds"))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Fatalf("la biblioteca es accesible por otros: %o", fi.Mode().Perm())
		}
	}
}

// TestImportFromZipCreaLaBibliotecaConPermisosDeDueño cubre la OTRA vía de
// escritura en la biblioteca (importar un zip) en un data dir limpio: la raíz de
// <data>/worlds debe quedar en 0700 (plan 12.4 · mínimos privilegios), sin
// depender de que SaveWorld la haya creado antes.
func TestImportFromZipCreaLaBibliotecaConPermisosDeDueño(t *testing.T) {
	dataDir := t.TempDir()
	m := &Manager{DataDir: dataDir}
	src := filepath.Join(dataDir, "origen", "world")
	if err := os.MkdirAll(filepath.Join(src, "region"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "level.dat"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	zip := filepath.Join(dataDir, "origen.zip")
	if err := ZipDir(filepath.Join(dataDir, "origen"), zip, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.ImportFromZip(zip, "importado"); err != nil {
		t.Fatalf("ImportFromZip: %v", err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(m.LibraryDir())
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Fatalf("la biblioteca es accesible por otros: %o", fi.Mode().Perm())
		}
	}
}
