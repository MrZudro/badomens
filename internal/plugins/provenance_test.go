package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MrZudro/badomens/internal/instance"
)

// provSpec es la instancia de las pruebas de procedencia.
func provSpec() *instance.Spec { return &instance.Spec{Slug: "demo", Engine: "paper"} }

// TestProvenanceRoundTrip: guardar y leer conserva proyecto, versión, checksum y
// procedencia.
func TestProvenanceRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	store := NewProvenance(dataDir)
	spec := provSpec()

	reg := NewRegister()
	reg.Upsert(Record{
		Name:        "demo-5.5.0.jar",
		Version:     "5.5.0",
		SHA256:      "AA00",
		SHA512:      "BB00",
		SizeBytes:   1234,
		Loader:      "paper",
		GameVersion: "1.21.1",
		InstalledAt: "2025-06-01T00:00:00Z",
		Origin: Origin{
			Source: "modrinth", ProjectID: "P1OZGk5p", ProjectSlug: "viaversion",
			ProjectName: "ViaVersion", VersionID: "VvREL5x1", Channel: "release", Kind: "plugin",
		},
	})
	if err := store.Save(spec, reg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "instances", "demo", ProvenanceFile)); err != nil {
		t.Fatalf("no se escribió el registro: %v", err)
	}

	got, err := NewProvenance(dataDir).Load(spec)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.SchemaVersion != ProvenanceSchemaVersion || got.Len() != 1 {
		t.Fatalf("registro = %+v", got)
	}
	rec := got.Records[0]
	if rec.Version != "5.5.0" || rec.SHA256 != "aa00" || rec.SHA512 != "bb00" {
		t.Errorf("checksums/versión = %+v", rec)
	}
	if rec.Origin.ProjectSlug != "viaversion" || !rec.Origin.Known() {
		t.Errorf("procedencia = %+v", rec.Origin)
	}
	if rec.Loader != "paper" || rec.GameVersion != "1.21.1" || rec.SizeBytes != 1234 {
		t.Errorf("entorno/tamaño = %+v", rec)
	}
}

// TestProvenanceUpsertNoDuplica: una instalación deja exactamente una entrada.
func TestProvenanceUpsertNoDuplica(t *testing.T) {
	reg := NewRegister()
	reg.Upsert(Record{Name: "a.jar", Version: "1.0"})
	reg.Upsert(Record{Name: "a.jar", Version: "2.0"})
	reg.Upsert(Record{Name: "b.jar", Version: "1.0"})
	if reg.Len() != 2 {
		t.Fatalf("entradas = %d, quiero 2", reg.Len())
	}
	rec, ok := reg.Get("A.JAR")
	if !ok || rec.Version != "2.0" {
		t.Errorf("Get = %+v (%v)", rec, ok)
	}
	if !reg.Remove("a.jar") || reg.Len() != 1 {
		t.Errorf("Remove falló: %+v", reg.Records)
	}
	if reg.Remove("noexiste") {
		t.Error("Remove de un nombre inexistente debe devolver false")
	}
}

// TestProvenanceMigracionAditiva: los registros antiguos (solo el nombre, o un
// esquema anterior) se siguen leyendo, y un nombre sin procedencia se devuelve
// como origen desconocido en lugar de inventarla.
func TestProvenanceMigracionAditiva(t *testing.T) {
	dataDir := t.TempDir()
	spec := provSpec()
	dir := filepath.Join(dataDir, "instances", "demo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Registro de un esquema anterior: solo el nombre, sin versión ni checksum.
	legacy := `{"schema_version":0,"records":[{"name":"antiguo-1.0.jar"}]}`
	if err := os.WriteFile(filepath.Join(dir, ProvenanceFile), []byte(legacy), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	reg, err := NewProvenance(dataDir).Load(spec)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reg.SchemaVersion != ProvenanceSchemaVersion {
		t.Errorf("SchemaVersion = %d", reg.SchemaVersion)
	}
	rec, ok := reg.Get("antiguo-1.0.jar")
	if !ok {
		t.Fatal("el registro antiguo debe seguir leyéndose")
	}
	if rec.Version != "" || rec.SHA256 != "" {
		t.Errorf("no debe inventarse versión ni checksum: %+v", rec)
	}
	if rec.Origin.Source != originUnknown || rec.Origin.Known() {
		t.Errorf("procedencia = %+v", rec.Origin)
	}

	// La lista de la instancia manda: lo instalado sin entrada de procedencia se
	// devuelve igualmente, marcado como desconocido.
	spec.Plugins = []string{"antiguo-1.0.jar", "nuevo-2.0.jar"}
	merged := reg.Merge(spec.Plugins)
	if len(merged) != 2 {
		t.Fatalf("merge = %+v", merged)
	}
	if merged[0].Name != "antiguo-1.0.jar" || merged[1].Name != "nuevo-2.0.jar" {
		t.Errorf("orden/nombres = %+v", merged)
	}
	for _, rec := range merged {
		if rec.Origin.Source != originUnknown {
			t.Errorf("un nombre sin registro debe quedar como desconocido: %+v", rec)
		}
	}
}

// TestProvenanceSanitiza: el registro no debe poder apuntar fuera de la carpeta
// de plugins ni contener duplicados.
func TestProvenanceSanitiza(t *testing.T) {
	dataDir := t.TempDir()
	spec := provSpec()
	dir := filepath.Join(dataDir, "instances", "demo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	raw := `{"schema_version":1,"records":[
        {"name":"../evil.jar","origin":{"source":"url"}},
        {"name":"sub/dir.jar","origin":{"source":"url"}},
        {"name":"back\\slash.jar","origin":{"source":"url"}},
        {"name":"","origin":{"source":"url"}},
        {"name":"ok.jar","origin":{"source":""}},
        {"name":"OK.jar","origin":{"source":"modrinth"}}
    ]}`
	if err := os.WriteFile(filepath.Join(dir, ProvenanceFile), []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	reg, err := NewProvenance(dataDir).Load(spec)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reg.Len() != 1 || reg.Records[0].Name != "ok.jar" {
		t.Fatalf("registro saneado = %+v", reg.Records)
	}
	if reg.Records[0].Origin.Source != originUnknown {
		t.Errorf("un origen vacío debe normalizarse a desconocido: %+v", reg.Records[0].Origin)
	}
}

// TestProvenanceSinArchivoYCorrupto: la ausencia no es un error; un archivo
// ilegible sí, porque perder la procedencia en silencio sería peor.
func TestProvenanceSinArchivoYCorrupto(t *testing.T) {
	dataDir := t.TempDir()
	spec := provSpec()
	store := NewProvenance(dataDir)

	reg, err := store.Load(spec)
	if err != nil {
		t.Fatalf("Load sin archivo: %v", err)
	}
	if reg.Len() != 0 || reg.SchemaVersion != ProvenanceSchemaVersion {
		t.Errorf("registro vacío = %+v", reg)
	}

	dir := filepath.Join(dataDir, "instances", "demo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ProvenanceFile), []byte("{no soy json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := store.Load(spec); err == nil {
		t.Error("un registro corrupto debe ser un error explícito")
	}
}

// TestProvenanceOrphans: las entradas cuyo archivo ya no está se detectan.
func TestProvenanceOrphans(t *testing.T) {
	reg := NewRegister()
	reg.Upsert(Record{Name: "vivo.jar", Origin: Origin{Source: "modrinth"}})
	reg.Upsert(Record{Name: "huerfano.jar", Origin: Origin{Source: "modrinth"}})
	orphans := reg.Orphans([]string{"vivo.jar"})
	if len(orphans) != 1 || orphans[0].Name != "huerfano.jar" {
		t.Errorf("huérfanos = %+v", orphans)
	}
	if got := reg.Names(); len(got) != 2 || got[0] != "huerfano.jar" {
		t.Errorf("Names = %v", got)
	}
}

// TestProvenanceSaveAtómicoDejaJSONLegible comprueba que el archivo escrito es
// JSON válido y no queda ningún temporal.
func TestProvenanceSaveDejaJSONLegible(t *testing.T) {
	dataDir := t.TempDir()
	spec := provSpec()
	store := NewProvenance(dataDir)
	reg := NewRegister()
	reg.Upsert(Record{Name: "demo.jar", Version: "1.0", Origin: Origin{Source: "hangar"}})
	if err := store.Save(spec, reg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	dir := filepath.Join(dataDir, "instances", "demo")
	raw, err := os.ReadFile(filepath.Join(dir, ProvenanceFile))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var decoded Register
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("el registro no es JSON válido: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != ProvenanceFile {
			t.Errorf("quedó un archivo inesperado: %s", e.Name())
		}
	}
}
