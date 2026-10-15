package instance

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/MrZudro/badomens/internal/errx"
)

func TestValidateSlugRechazaRutasYEspacios(t *testing.T) {
	invalid := []string{
		"", " ", "..", "../fuera", "demo/../..", `demo\..`, "a/b", "de mo",
		"Demo", "DEMO", "demo.", ".demo", "demo\n", "../../etc/passwd",
		"con-espacio ", "áé", "demo@1", string([]byte{0x00}),
	}
	for _, slug := range invalid {
		if err := ValidateSlug(slug); err == nil {
			t.Fatalf("%q debería rechazarse", slug)
		} else if !errx.Is(err, errx.CodeUsage) {
			t.Fatalf("%q: código = %v", slug, err)
		}
	}
	valid := []string{"demo", "ab", "servidor-1", "mc26", "a1b2c3", "12"}
	for _, slug := range valid {
		if err := ValidateSlug(slug); err != nil {
			t.Fatalf("%q debería aceptarse: %v", slug, err)
		}
	}
}

// TestRutasNoEscapanDelDataDir: cargar/guardar/borrar con un slug de ruta no
// puede tocar nada fuera del data dir.
func TestRutasNoEscapanDelDataDir(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	m := New(dataDir, []string{"paper", "vanilla"})

	for _, slug := range []string{"../fuera", "demo/../..", `..\fuera`, "a/b"} {
		if _, err := m.Load(slug); err == nil {
			t.Fatalf("Load(%q) debería fallar", slug)
		}
		if err := m.Remove(slug); err == nil {
			t.Fatalf("Remove(%q) debería fallar", slug)
		}
		if err := m.Save(&Spec{
			Slug: slug, Engine: "vanilla", Version: "1.21.1",
			Memory: "4G", Port: 25565, MaxPlayers: 20,
		}); err == nil {
			t.Fatalf("Save(%q) debería fallar", slug)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "fuera")); err == nil {
		t.Fatal("se escribió fuera del data dir")
	}
	if _, err := os.Stat(filepath.Join(root, "instances")); err == nil {
		t.Fatal("se creó un árbol de instancias fuera del data dir")
	}
}

func paperSpec() Spec {
	return Spec{
		Slug: "demo", Name: "Demo", Engine: "paper", Version: "1.21.1",
		Pins: map[string]string{"PAPER_BUILD": "123"}, Memory: "4G", CPU: 2,
		Port: 25565, Difficulty: "normal", Gamemode: "survival", MaxPlayers: 20,
		OnlineMode: true, Motd: "hola", WhitelistEnabled: true,
		Whitelist: []string{"ana"}, Ops: []string{"ana"}, CreatedAt: Stamp(),
	}
}

func TestManifestRoundTripReproduceExacto(t *testing.T) {
	dataDir := t.TempDir()
	m := New(dataDir, []string{"paper", "fabric", "forge", "neoforge", "vanilla"})
	spec := paperSpec()
	if err := m.Save(&spec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	mf, err := m.ExportManifest("demo")
	if err != nil {
		t.Fatalf("ExportManifest: %v", err)
	}
	if mf.Schema != ManifestSchema || mf.Engine != "paper" || mf.Version != "1.21.1" {
		t.Fatalf("manifiesto = %+v", mf)
	}
	if mf.Pins["PAPER_BUILD"] != "123" {
		t.Fatalf("pins = %v; quiero el build exacto", mf.Pins)
	}
	if err := mf.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// Recreación en otro slug con los MISMOS pines.
	recreated, err := m.ImportManifest(mf, "copia", false)
	if err != nil {
		t.Fatalf("ImportManifest: %v", err)
	}
	if recreated.Engine != "paper" || recreated.Version != "1.21.1" || recreated.Pins["PAPER_BUILD"] != "123" {
		t.Fatalf("recreación = %+v", recreated)
	}
	loaded, err := m.Load("copia")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Version != spec.Version || loaded.Pins["PAPER_BUILD"] != "123" || loaded.Whitelist[0] != "ana" {
		t.Fatalf("cargada = %+v", loaded)
	}
	// El manifiesto no comparte memoria con el registro.
	mf.Pins["PAPER_BUILD"] = "999"
	if loaded.Pins["PAPER_BUILD"] != "123" {
		t.Fatal("el manifiesto y la instancia comparten el mapa de pins")
	}
}

func TestManifestValidateRechazaNoReproducible(t *testing.T) {
	base := Manifest{
		Schema: ManifestSchema, Slug: "demo", Engine: "paper", Version: "1.21.1",
		Pins: map[string]string{"PAPER_BUILD": "123"}, Memory: "4G",
	}
	cases := map[string]func(m *Manifest){
		"motor desconocido":   func(m *Manifest) { m.Engine = "noexiste" },
		"versión vacía":       func(m *Manifest) { m.Version = "" },
		"versión latest":      func(m *Manifest) { m.Version = "latest" },
		"pin ausente":         func(m *Manifest) { m.Pins = map[string]string{} },
		"pin latest":          func(m *Manifest) { m.Pins = map[string]string{"PAPER_BUILD": "latest"} },
		"memoria inválida":    func(m *Manifest) { m.Memory = "mucha" },
		"memoria vacía":       func(m *Manifest) { m.Memory = "" },
		"fabric sin loader":   func(m *Manifest) { m.Engine = "fabric"; m.Pins = map[string]string{} },
		"vanilla sin pins ok": func(m *Manifest) { m.Engine = "vanilla"; m.Pins = nil },
	}
	for name, mutate := range cases {
		m := base
		m.Pins = map[string]string{"PAPER_BUILD": "123"}
		mutate(&m)
		err := m.Validate()
		if name == "vanilla sin pins ok" {
			if err != nil {
				t.Fatalf("%s: vanilla no necesita pins: %v", name, err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("%s: debería rechazarse", name)
		}
	}
}

func TestImportManifestNoSobrescribeSinPermiso(t *testing.T) {
	dataDir := t.TempDir()
	m := New(dataDir, []string{"paper", "vanilla"})
	spec := paperSpec()
	if err := m.Save(&spec); err != nil {
		t.Fatal(err)
	}
	mf, err := m.ExportManifest("demo")
	if err != nil {
		t.Fatal(err)
	}
	mf.Version = "26.3"
	if _, err := m.ImportManifest(mf, "demo", false); err == nil {
		t.Fatal("no debe sobrescribir sin overwrite")
	}
	loaded, err := m.Load("demo")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != "1.21.1" {
		t.Fatalf("la instancia se modificó: %s", loaded.Version)
	}
	if _, err := m.ImportManifest(mf, "demo", true); err != nil {
		t.Fatalf("con overwrite debería aplicarse: %v", err)
	}
	loaded, _ = m.Load("demo")
	if loaded.Version != "26.3" {
		t.Fatalf("versión = %s; quiero 26.3", loaded.Version)
	}
}

func TestReadManifestYWriteManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.json")
	mf := Manifest{
		Schema: ManifestSchema, Slug: "demo", Engine: "paper", Version: "1.21.1",
		Pins: map[string]string{"PAPER_BUILD": "123"}, Memory: "4G",
	}
	if err := WriteManifest(path, mf); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	got, err := ReadManifest(path)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if got.Engine != "paper" || got.Pins["PAPER_BUILD"] != "123" || got.Schema != ManifestSchema {
		t.Fatalf("manifiesto leído = %+v", got)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("permisos = %o; quiero 600", fi.Mode().Perm())
		}
	}

	bad := filepath.Join(dir, "malo.json")
	if err := os.WriteFile(bad, []byte("{no json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadManifest(bad); err == nil {
		t.Fatal("un JSON inválido debe rechazarse")
	}
	other := filepath.Join(dir, "otro.json")
	if err := os.WriteFile(other, []byte(`{"schema":"badomens.instance/99","slug":"x","engine":"paper","version":"1.21.1","pins":{"PAPER_BUILD":"1"},"memory":"4G"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadManifest(other); err == nil {
		t.Fatal("un esquema desconocido debe rechazarse")
	}
	if _, err := ReadManifest(filepath.Join(dir, "no-existe.json")); !errx.Is(err, errx.CodeNotFound) {
		t.Fatalf("archivo ausente = %v", err)
	}
}

// TestExportManifestRechazaConfiguracionInvalida: no se exporta algo que no se
// puede recrear.
func TestExportManifestRechazaConfiguracionInvalida(t *testing.T) {
	dataDir := t.TempDir()
	m := New(dataDir, []string{"paper", "vanilla"})
	broken := paperSpec()
	broken.Memory = "mucha" // Save no valida el spec completo; la exportación sí
	if err := m.Save(&broken); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ExportManifest("demo"); err == nil {
		t.Fatal("una configuración inválida no es reproducible")
	}
}
