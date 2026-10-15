package backup

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/errx"
)

// seedServer crea el árbol server/ de una instancia de prueba.
func seedServer(t *testing.T, dataDir, slug string) string {
	t.Helper()
	server := filepath.Join(dataDir, "instances", slug, "server")
	if err := os.MkdirAll(filepath.Join(server, "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(server, "server.properties"), []byte("motd=hola\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(server, "world", "level.dat"), []byte("datos del mundo"), 0o644); err != nil {
		t.Fatal(err)
	}
	return server
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no se pudo leer %s: %v", path, err)
	}
	return string(raw)
}

// TestRestoreVerificaChecksumAntesDeTocarDatos es el control central de 12.4:
// un backup alterado se rechaza SIN modificar un solo byte de los datos.
func TestRestoreVerificaChecksumAntesDeTocarDatos(t *testing.T) {
	dataDir := t.TempDir()
	server := seedServer(t, dataDir, "demo")
	b := &Backend{DataDir: dataDir}
	info, err := b.Create("demo", "copia")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// El mundo cambia DESPUÉS del backup: restaurar debería traer el original.
	if err := os.WriteFile(filepath.Join(server, "server.properties"), []byte("motd=nuevo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// El archivo se corrompe (mismo .sha256): la restauración debe abortar.
	f, err := os.OpenFile(info.Path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("basura")); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	err = b.Restore("demo", "copia")
	if err == nil {
		t.Fatal("un backup alterado no puede restaurarse")
	}
	if reason, _ := errx.Details(err)["reason"].(string); reason != "checksum_no_coincide" {
		t.Fatalf("reason = %q (%v)", reason, err)
	}
	if got := readFile(t, filepath.Join(server, "server.properties")); got != "motd=nuevo\n" {
		t.Fatalf("los datos actuales se modificaron pese al checksum inválido: %q", got)
	}
	if _, statErr := os.Stat(filepath.Join(dataDir, "instances", "demo", ".restore-tmp")); statErr == nil {
		t.Fatal("quedó un directorio temporal tras el rechazo")
	}
}

// TestRestoreSinChecksumSeRechaza: sin .sha256 no hay verificación posible.
func TestRestoreSinChecksumSeRechaza(t *testing.T) {
	dataDir := t.TempDir()
	seedServer(t, dataDir, "demo")
	b := &Backend{DataDir: dataDir}
	info, err := b.Create("demo", "copia")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := os.Remove(info.Path + ".sha256"); err != nil {
		t.Fatal(err)
	}
	if err := b.Restore("demo", "copia"); err == nil {
		t.Fatal("sin checksum la restauración debe rechazarse")
	}
}

// TestRestoreZipCorruptoNoTocaLosDatos: un zip ilegible (pero con checksum
// coherente) falla ANTES de sustituir el directorio.
func TestRestoreZipCorruptoNoTocaLosDatos(t *testing.T) {
	dataDir := t.TempDir()
	server := seedServer(t, dataDir, "demo")
	b := &Backend{DataDir: dataDir}
	info, err := b.Create("demo", "copia")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Se reescribe el zip como basura no-ZIP y se recalcula el checksum para que
	// la verificación pase y el fallo ocurra al extraer.
	if err := os.WriteFile(info.Path, []byte("no soy un zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum, err := fileSHA256(info.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(info.Path+".sha256", []byte(sum), 0o600); err != nil {
		t.Fatal(err)
	}
	err = b.Restore("demo", "copia")
	if err == nil {
		t.Fatal("un zip corrupto no puede restaurarse")
	}
	if !strings.Contains(err.Error(), "no se pudo extraer") {
		t.Fatalf("mensaje = %v", err)
	}
	if got := readFile(t, filepath.Join(server, "world", "level.dat")); got != "datos del mundo" {
		t.Fatalf("los datos se modificaron: %q", got)
	}
}

func TestRestoreExitosoYRecuperable(t *testing.T) {
	dataDir := t.TempDir()
	server := seedServer(t, dataDir, "demo")
	b := &Backend{DataDir: dataDir}
	if _, err := b.Create("demo", "copia"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(server, "server.properties"), []byte("motd=nuevo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(server, "basura.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := b.Restore("demo", "copia"); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := readFile(t, filepath.Join(server, "server.properties")); got != "motd=hola\n" {
		t.Fatalf("no se restauró el contenido: %q", got)
	}
	if _, err := os.Stat(filepath.Join(server, "basura.txt")); err == nil {
		t.Fatal("el contenido posterior al backup debería haberse sustituido")
	}
	for _, leftover := range []string{".restore-tmp", ".restore-old"} {
		if _, err := os.Stat(filepath.Join(dataDir, "instances", "demo", leftover)); err == nil {
			t.Fatalf("quedó %s", leftover)
		}
	}
}

func TestNombresInvalidosRechazados(t *testing.T) {
	dataDir := t.TempDir()
	seedServer(t, dataDir, "demo")
	b := &Backend{DataDir: dataDir}
	// El validador es ESTRICTO (no normaliza): un nombre con espacios al
	// principio o al final tampoco es un identificador limpio.
	for _, name := range []string{"../evil", "a/b", `a\b`, "..", "copia/../otra", "CON", "fin.", ".oculto", "con espacio ", "CON.txt"} {
		if err := ValidateName(name); err == nil {
			t.Fatalf("%q debería rechazarse", name)
		}
	}
	// Create/Restore recortan los espacios exteriores antes de validar, así que
	// se comprueban con nombres inválidos incluso recortados.
	for _, name := range []string{"../evil", "a/b", `a\b`, "..", "copia/../otra", "CON", "fin.", ".oculto"} {
		if _, err := b.Create("demo", name); err == nil {
			t.Fatalf("Create(%q) debería fallar", name)
		}
	}
	// Restore nunca acepta un nombre vacío: no hay "backup por defecto" que
	// restaurar (en Create, vacío significa marca de tiempo).
	for _, name := range []string{"../evil", "a/b", "..", ""} {
		if err := b.Restore("demo", name); err == nil {
			t.Fatalf("Restore(%q) debería fallar", name)
		}
	}
	for _, name := range []string{"copia", "antes-de-actualizar", "20261014T120000", "v1.2"} {
		if err := ValidateName(name); err != nil {
			t.Fatalf("%q debería aceptarse: %v", name, err)
		}
	}
}

// TestSlugInvalidoNoEscapaDelDataDir: el slug es un identificador, no una ruta.
func TestSlugInvalidoNoEscapaDelDataDir(t *testing.T) {
	dataDir := t.TempDir()
	b := &Backend{DataDir: dataDir}
	for _, slug := range []string{"../fuera", "demo/../..", "de mo", "Demo", ""} {
		if _, err := b.Create(slug, "copia"); err == nil {
			t.Fatalf("Create(%q) debería fallar", slug)
		}
		if err := b.Restore(slug, "copia"); err == nil {
			t.Fatalf("Restore(%q) debería fallar", slug)
		}
		if _, err := b.List(slug); err == nil {
			t.Fatalf("List(%q) debería fallar", slug)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dataDir), "fuera")); err == nil {
		t.Fatal("se creó algo fuera del data dir")
	}
}

// TestCreateLimiteDeTamano: el límite de origen se aplica ANTES de empaquetar.
func TestCreateLimiteDeTamano(t *testing.T) {
	dataDir := t.TempDir()
	seedServer(t, dataDir, "demo")
	original := MaxSourceBytes
	MaxSourceBytes = 4
	defer func() { MaxSourceBytes = original }()

	b := &Backend{DataDir: dataDir}
	_, err := b.Create("demo", "copia")
	if err == nil {
		t.Fatal("se debía rechazar por tamaño")
	}
	if !errx.Is(err, errx.CodeCapacity) {
		t.Fatalf("código = %v", err)
	}
	if reason, _ := errx.Details(err)["reason"].(string); reason != "limite_excedido" {
		t.Fatalf("reason = %q", reason)
	}
}

// TestCreatePermisos: la carpeta de backups es 0700 y el zip y su checksum 0600
// (comprobable en sistemas POSIX; en Windows los bits no representan las ACL).
func TestCreatePermisos(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows no expone los permisos POSIX en os.FileMode")
	}
	dataDir := t.TempDir()
	seedServer(t, dataDir, "demo")
	b := &Backend{DataDir: dataDir}
	info, err := b.Create("demo", "copia")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	dir, err := os.Stat(b.Dir("demo"))
	if err != nil {
		t.Fatal(err)
	}
	if dir.Mode().Perm() != 0o700 {
		t.Fatalf("permisos de la carpeta de backups = %o", dir.Mode().Perm())
	}
	for _, path := range []string{info.Path, info.Path + ".sha256"} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("permisos de %s = %o", filepath.Base(path), fi.Mode().Perm())
		}
	}
}

// TestCreateNoDejaTemporalesNiSobrescribe: el zip final solo aparece cuando ya
// está verificado, y nunca se pisa una copia existente.
func TestCreateNoDejaTemporalesNiSobrescribe(t *testing.T) {
	dataDir := t.TempDir()
	seedServer(t, dataDir, "demo")
	b := &Backend{DataDir: dataDir}
	info, err := b.Create("demo", "copia")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := b.Create("demo", "copia"); err == nil {
		t.Fatal("no se debe sobrescribir una copia existente")
	}
	entries, err := os.ReadDir(b.Dir("demo"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Fatalf("quedó un temporal: %s", e.Name())
		}
	}
	if err := b.Verify("demo", "copia"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got := readFile(t, info.Path+".sha256"); got != info.Sha256 {
		t.Fatalf("checksum guardado = %q; quiero %q", got, info.Sha256)
	}
}
