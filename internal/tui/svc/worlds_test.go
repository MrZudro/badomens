package svc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/worlds"
)

// worldsFixture construye un realWorlds real (registro de instancias +
// biblioteca de mundos) sobre un directorio temporal y una instancia mínima.
// Se deja rt a nil para que los tests no llamen a Docker.
func worldsFixture(t *testing.T) (*realWorlds, *instance.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	m := instance.New(dir, []string{"paper"})
	spec := &instance.Spec{
		Slug: "demo", Name: "Demo", Engine: "paper", Version: "1.21.6",
		Memory: "4G", Port: 25565, MaxPlayers: 20, Seed: "vieja",
	}
	if err := m.Save(spec); err != nil {
		t.Fatalf("guardar instancia: %v", err)
	}
	rw := &realWorlds{cfg: &config.Config{DataDir: dir}, m: m, wm: &worlds.Manager{DataDir: dir}}
	return rw, m, dir
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("crear carpeta de %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("escribir %s: %v", path, err)
	}
}

// makeZip crea un zip con un único level.dat y lo devuelve. El nombre del
// archivo determina el nombre del mundo cuando no se indica uno.
func makeZip(t *testing.T, dir, name, data string) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "src")
	writeFile(t, filepath.Join(src, "level.dat"), data)
	out := filepath.Join(dir, name+".zip")
	if err := worlds.ZipDir(src, out, map[string]string{"name": name}); err != nil {
		t.Fatalf("crear zip %s: %v", name, err)
	}
	return out
}

func TestRealWorldsRegenerateBorraMundosYFijaSemilla(t *testing.T) {
	rw, m, dir := worldsFixture(t)
	server := filepath.Join(dir, "instances", "demo", "server")
	for _, w := range []string{"world", "world_nether", "world_the_end"} {
		writeFile(t, filepath.Join(server, w, "level.dat"), "nivel viejo")
	}
	// Hermano que NO debe borrarse.
	writeFile(t, filepath.Join(server, "server.properties"), "motd=hola")

	ctx := context.Background()
	if err := rw.Regenerate(ctx, "demo", "  abc123  "); err != nil {
		t.Fatalf("regenerar: %v", err)
	}
	for _, w := range []string{"world", "world_nether", "world_the_end"} {
		if _, err := os.Stat(filepath.Join(server, w)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("el mundo %s debería haberse borrado (err=%v)", w, err)
		}
	}
	if _, err := os.Stat(filepath.Join(server, "server.properties")); err != nil {
		t.Fatalf("no debe borrar archivos hermanos: %v", err)
	}
	spec, err := m.Load("demo")
	if err != nil {
		t.Fatalf("recargar instancia: %v", err)
	}
	if spec.Seed != "abc123" {
		t.Fatalf("semilla guardada = %q, quiero abc123 (recortada)", spec.Seed)
	}

	// Semilla vacía = aleatoria, y volver a regenerar sin mundos en disco no falla.
	if err := rw.Regenerate(ctx, "demo", "   "); err != nil {
		t.Fatalf("regenerar con semilla aleatoria: %v", err)
	}
	spec, err = m.Load("demo")
	if err != nil {
		t.Fatalf("recargar instancia: %v", err)
	}
	if spec.Seed != "" {
		t.Fatalf("semilla aleatoria debe quedar vacía, quedó %q", spec.Seed)
	}
}

func TestRealWorldsRegenerateInstanciaInexistente(t *testing.T) {
	rw, _, _ := worldsFixture(t)
	if err := rw.Regenerate(context.Background(), "nope", "x"); err == nil {
		t.Fatal("una instancia inexistente debe fallar")
	}
}

func TestRealWorldsLibraryListaLaBiblioteca(t *testing.T) {
	rw, _, dir := worldsFixture(t)
	ctx := context.Background()

	list, err := rw.Library(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("biblioteca vacía = %v err=%v", list, err)
	}
	for _, n := range []string{"survival", "arena"} {
		if err := os.MkdirAll(filepath.Join(dir, "worlds", n), 0o700); err != nil {
			t.Fatalf("crear mundo %s: %v", n, err)
		}
	}
	// Un archivo suelto en la biblioteca no debe listarse como mundo.
	writeFile(t, filepath.Join(dir, "worlds", "notas.txt"), "no soy un mundo")

	list, err = rw.Library(ctx)
	if err != nil {
		t.Fatalf("biblioteca: %v", err)
	}
	if len(list) != 2 || list[0] != "arena" || list[1] != "survival" {
		t.Fatalf("biblioteca = %v, quiero [arena survival]", list)
	}
}

func TestRealWorldsImportZipDespliegaMundoActivo(t *testing.T) {
	rw, m, dir := worldsFixture(t)
	ctx := context.Background()
	archive := makeZip(t, dir, "isla", "datos nuevos")

	if err := rw.ImportZip(ctx, "demo", archive, "Isla Perdida"); err != nil {
		t.Fatalf("importar: %v", err)
	}
	list, err := rw.Library(ctx)
	if err != nil {
		t.Fatalf("biblioteca: %v", err)
	}
	if len(list) != 1 || list[0] != "isla-perdida" {
		t.Fatalf("biblioteca = %v, quiero [isla-perdida]", list)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "instances", "demo", "server", "world", "level.dat")) // #nosec G304 -- fichero de un t.TempDir() de la prueba
	if err != nil || string(raw) != "datos nuevos" {
		t.Fatalf("mundo activo = %q err=%v", raw, err)
	}
	spec, err := m.Load("demo")
	if err != nil {
		t.Fatalf("recargar instancia: %v", err)
	}
	if spec.Seed != "vieja" {
		t.Fatalf("importar un mundo no debe tocar la semilla, quedó %q", spec.Seed)
	}

	// Sin nombre: se deriva del archivo (minúsculas y guiones bajos intactos).
	other := makeZip(t, dir, "Mi_Mundo", "otro")
	if err := rw.ImportZip(ctx, "demo", other, "   "); err != nil {
		t.Fatalf("importar sin nombre: %v", err)
	}
	list, err = rw.Library(ctx)
	if err != nil {
		t.Fatalf("biblioteca: %v", err)
	}
	if len(list) != 2 || list[0] != "isla-perdida" || list[1] != "mi_mundo" {
		t.Fatalf("biblioteca = %v, quiero [isla-perdida mi_mundo]", list)
	}
}

func TestRealWorldsImportZipErrores(t *testing.T) {
	rw, _, dir := worldsFixture(t)
	ctx := context.Background()
	archive := makeZip(t, dir, "isla", "datos")

	// Nombre ni indicado ni derivable del archivo.
	if err := rw.ImportZip(ctx, "demo", "", ""); err == nil {
		t.Fatal("sin nombre ni archivo utilizable debe fallar")
	}
	// Zip inexistente.
	if err := rw.ImportZip(ctx, "demo", filepath.Join(dir, "no-existe.zip"), "x"); err == nil {
		t.Fatal("un archivo inexistente debe fallar")
	}
	// Instancia inexistente.
	if err := rw.ImportZip(ctx, "nope", archive, "x"); err == nil {
		t.Fatal("una instancia inexistente debe fallar")
	}
	// Mundo repetido en la biblioteca.
	if err := rw.ImportZip(ctx, "demo", archive, "isla"); err != nil {
		t.Fatalf("primera importación: %v", err)
	}
	if err := rw.ImportZip(ctx, "demo", archive, "isla"); err == nil {
		t.Fatal("importar dos veces el mismo mundo debe fallar")
	}
}

func TestNormalizeWorldName(t *testing.T) {
	cases := map[string]string{
		"Isla Perdida":  "isla-perdida",
		"  MI_MUNDO  ":  "mi_mundo",
		"a/b\\c":        "abc",
		"ñandú Ñ":       "nandu-n",
		"...":           "",
		"":              "",
		"solo-valido-_": "solo-valido",
	}
	for in, want := range cases {
		if got := normalizeWorldName(in); got != want {
			t.Errorf("normalizeWorldName(%q) = %q, quiero %q", in, got, want)
		}
	}
	// La derivación del nombre debe ser portable: da igual el separador de ruta
	// (Windows «\» o POSIX «/»), el nombre resultante es el mismo.
	for _, in := range []string{`C:\datos\mi mundo.zip`, "/datos/mi mundo.zip", "mi mundo.zip"} {
		if got := worldNameFromArchive(in); got != "mi-mundo" {
			t.Errorf("worldNameFromArchive(%q) = %q, quiero mi-mundo", in, got)
		}
	}
}

func TestFakeWorldsRegistraYLlamaSinDocker(t *testing.T) {
	s := NewFake([]*instance.Spec{fakeSpec("alpha")})
	fw, ok := s.Worlds.(*FakeWorlds)
	if !ok {
		t.Fatalf("Worlds falso es %T, quiero *FakeWorlds", s.Worlds)
	}
	ctx := context.Background()

	if list, err := fw.Library(ctx); err != nil || len(list) != 0 {
		t.Fatalf("biblioteca falsa = %v err=%v", list, err)
	}
	if err := fw.Regenerate(ctx, "alpha", "semilla-1"); err != nil {
		t.Fatalf("regenerar en el fake: %v", err)
	}
	if err := fw.ImportZip(ctx, "alpha", `C:\tmp\mundo.zip`, "Mi Mundo"); err != nil {
		t.Fatalf("importar en el fake: %v", err)
	}

	calls := fw.Calls()
	if len(calls) != 3 {
		t.Fatalf("llamadas = %+v, quiero 3", calls)
	}
	if calls[0] != (WorldCall{Op: "library"}) {
		t.Fatalf("primera llamada = %+v", calls[0])
	}
	if calls[1] != (WorldCall{Op: "regenerate", Slug: "alpha", Seed: "semilla-1"}) {
		t.Fatalf("segunda llamada = %+v", calls[1])
	}
	if calls[2] != (WorldCall{Op: "import", Slug: "alpha", Archive: `C:\tmp\mundo.zip`, Name: "Mi Mundo"}) {
		t.Fatalf("tercera llamada = %+v", calls[2])
	}
	if got := fw.LibraryNames(); len(got) != 1 || got[0] != "Mi Mundo" {
		t.Fatalf("biblioteca tras importar = %v", got)
	}
	if err := fw.Regenerate(ctx, "desconocida", ""); err == nil {
		t.Fatal("un slug fuera del registro debe fallar en el fake")
	}
	fw.Reset()
	if len(fw.Calls()) != 0 {
		t.Fatal("Reset debe limpiar las llamadas")
	}
}

func TestFakeWorldsOpciones(t *testing.T) {
	s := NewFakeWith([]*instance.Spec{fakeSpec("alpha")}, FakeOptions{
		IdleWatcherActive: true,
		WorldLibrary:      []string{"survival", "arena"},
	})
	fw := s.Worlds.(*FakeWorlds)
	list, err := fw.Library(context.Background())
	if err != nil {
		t.Fatalf("biblioteca: %v", err)
	}
	if len(list) != 2 || list[0] != "arena" || list[1] != "survival" {
		t.Fatalf("biblioteca falsa = %v, quiero ordenada [arena survival]", list)
	}

	boom := errors.New("no se pudo importar")
	bad := NewFakeWith([]*instance.Spec{fakeSpec("alpha")},
		FakeOptions{IdleWatcherActive: true, WorldErr: boom})
	bfw := bad.Worlds.(*FakeWorlds)
	ctx := context.Background()
	if _, err := bfw.Library(ctx); !errors.Is(err, boom) {
		t.Fatalf("Library con WorldErr = %v", err)
	}
	if err := bfw.Regenerate(ctx, "alpha", "x"); !errors.Is(err, boom) {
		t.Fatalf("Regenerate con WorldErr = %v", err)
	}
	if err := bfw.ImportZip(ctx, "alpha", "a.zip", "n"); !errors.Is(err, boom) {
		t.Fatalf("ImportZip con WorldErr = %v", err)
	}
}
