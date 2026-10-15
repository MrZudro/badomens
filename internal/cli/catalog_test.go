package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/output"
	"github.com/spf13/cobra"
)

// catalogCacheDoc es el documento de caché mínimo que escriben las pruebas.
type catalogCacheDoc struct {
	Engine    string            `json:"engine"`
	Provider  string            `json:"provider,omitempty"`
	Fetched   time.Time         `json:"fetched"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
	Schema    string            `json:"schema,omitempty"`
	Validated bool              `json:"validated,omitempty"`
	Versions  []engines.Version `json:"versions"`
}

// catalogTestEnv prepara un Env aislado con la salida en memoria.
func catalogTestEnv(t *testing.T) (*Env, *bytes.Buffer, string) {
	t.Helper()
	dataDir := t.TempDir()
	var out, errBuf bytes.Buffer
	env := &Env{
		stdout:  &out,
		stderr:  &errBuf,
		cfg:     &config.Config{DataDir: dataDir},
		printer: output.New(&out, &errBuf, output.JSON),
	}
	return env, &out, dataDir
}

// seedCatalog escribe un documento de caché válido para un motor (sin red).
func seedCatalog(t *testing.T, dataDir, engine string, fetched time.Time, versions []engines.Version) {
	t.Helper()
	dir := filepath.Join(dataDir, "versions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(catalogCacheDoc{
		Engine: engine, Provider: engine, Fetched: fetched, UpdatedAt: fetched,
		Schema: "engines.catalog/1", Validated: true, Versions: versions,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, engine+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// runCatalog ejecuta `catalog <sub> ...` y devuelve el documento JSON impreso.
func runCatalog(t *testing.T, env *Env, out *bytes.Buffer, args ...string) (error, map[string]any) {
	t.Helper()
	out.Reset()
	root := &cobra.Command{Use: "root", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newCatalogCmd(env))
	root.SetArgs(args)
	err := root.Execute()
	var doc map[string]any
	if out.Len() > 0 {
		if uerr := json.Unmarshal(out.Bytes(), &doc); uerr != nil {
			t.Fatalf("la salida no es un documento JSON válido: %v\n%s", uerr, out.String())
		}
		for _, key := range []string{"ok", "exit_code", "action", "message", "timestamp"} {
			if _, ok := doc[key]; !ok {
				t.Errorf("falta la clave estable %q en el documento: %v", key, doc)
			}
		}
	}
	return err, doc
}

// runEngine ejecuta `engine <sub> ...`.
func runEngine(t *testing.T, env *Env, out *bytes.Buffer, args ...string) (error, map[string]any) {
	t.Helper()
	out.Reset()
	root := &cobra.Command{Use: "root", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newEngineCmd(env))
	root.SetArgs(args)
	err := root.Execute()
	var doc map[string]any
	if out.Len() > 0 {
		if uerr := json.Unmarshal(out.Bytes(), &doc); uerr != nil {
			t.Fatalf("la salida no es un documento JSON válido: %v\n%s", uerr, out.String())
		}
	}
	return err, doc
}

func cachedVersions() []engines.Version {
	return []engines.Version{
		{Version: "1.21.1", Stable: true, Channel: engines.ChannelRelease, JavaMajor: 21, JavaStatus: engines.JavaStatusInferred, Compatibility: engines.CompatibilityCompatible, Compatible: true},
		{Version: "26.3", Stable: true, Channel: engines.ChannelRelease, JavaMajor: 25, JavaStatus: engines.JavaStatusVerified, Compatibility: engines.CompatibilityCompatible, Compatible: true, Recommended: true, Build: "190"},
		{Version: "26.2", Stable: false, Channel: engines.ChannelBeta, JavaMajor: 25, JavaStatus: engines.JavaStatusInferred, Compatibility: engines.CompatibilityCompatible, Compatible: true},
	}
}

// TestCatalogStatusFrescaYVencida: el comando informa del estado sin tocar la red
// y expone la antigüedad legible.
func TestCatalogStatusFrescaYVencida(t *testing.T) {
	env, out, dataDir := catalogTestEnv(t)
	seedCatalog(t, dataDir, "paper", time.Now(), cachedVersions())

	_, doc := runCatalog(t, env, out, "catalog", "status", "paper")
	if doc["ok"] != true {
		t.Fatalf("documento inesperado: %v", doc)
	}
	data, _ := doc["data"].(map[string]any)
	catalogs, _ := data["catalogs"].([]any)
	if len(catalogs) != 1 {
		t.Fatalf("catalogs = %v", data["catalogs"])
	}
	first, _ := catalogs[0].(map[string]any)
	if first["state"] != "fresh" || first["usable"] != true {
		t.Fatalf("estado = %v", first)
	}
	if first["versions"] != float64(3) || first["recommended"] != "26.3" {
		t.Fatalf("catálogo mal resumido: %v", first)
	}
	if first["validated"] != true || first["schema"] != "engines.catalog/1" {
		t.Fatalf("falta el estado de validación: %v", first)
	}

	// Vencida: sigue siendo utilizable y la antigüedad es legible.
	seedCatalog(t, dataDir, "paper", time.Now().Add(-7*time.Hour), cachedVersions())
	_, doc = runCatalog(t, env, out, "catalog", "status", "paper")
	data, _ = doc["data"].(map[string]any)
	catalogs, _ = data["catalogs"].([]any)
	first, _ = catalogs[0].(map[string]any)
	if first["state"] != "stale" || first["usable"] != true {
		t.Fatalf("quiero vencida pero utilizable: %v", first)
	}
	if age, _ := first["age"].(string); age == "" {
		t.Fatalf("quiero antigüedad legible: %v", first)
	}
}

// TestCatalogStatusInvalidaYDesconocida: una caché corrupta es inválida (aviso,
// exit 1) y un motor inexistente es un error de uso (exit 2).
func TestCatalogStatusInvalidaYDesconocida(t *testing.T) {
	env, out, dataDir := catalogTestEnv(t)
	dir := filepath.Join(dataDir, "versions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "paper.json"), []byte("{no es json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, doc := runCatalog(t, env, out, "catalog", "status", "paper"); doc["exit_code"] != float64(1) {
		t.Fatalf("una caché inválida debe avisar (exit 1): %v", doc)
	}

	if _, doc := runCatalog(t, env, out, "catalog", "status", "noexiste"); doc["exit_code"] != float64(2) {
		t.Fatalf("un motor inexistente debe fallar (exit 2): %v", doc)
	}
}

// TestEngineListYVersionsCompartenCache (Fase 4): `engine list <motor>` ya no
// consulta el proveedor sin caché; usa la MISMA ruta que `engine versions`.
func TestEngineListYVersionsCompartenCache(t *testing.T) {
	env, out, dataDir := catalogTestEnv(t)
	seedCatalog(t, dataDir, "paper", time.Now(), cachedVersions())

	// engine versions: solo releases (el canal beta se filtra) y con el modelo
	// normalizado.
	_, doc := runEngine(t, env, out, "engine", "versions", "paper")
	data, _ := doc["data"].(map[string]any)
	if doc["action"] != "engine versions" || data["stale"] != false {
		t.Fatalf("documento inesperado: %v", doc)
	}
	versions, _ := data["versions"].([]any)
	if len(versions) != 2 {
		t.Fatalf("quiero 2 releases, tengo %d: %v", len(versions), versions)
	}
	last, _ := versions[len(versions)-1].(map[string]any)
	if last["id"] != "26.3" || last["recommended"] != true {
		t.Fatalf("la última debe ser la recomendada 26.3: %v", last)
	}
	for _, key := range []string{"compatibility", "java_status", "compatible"} {
		if _, ok := last[key]; !ok {
			t.Fatalf("falta el campo normalizado %q: %v", key, last)
		}
	}
	if last["java_major"] != float64(25) || last["java_status"] != "verified" {
		t.Fatalf("Java de 26.3 = %v/%v", last["java_major"], last["java_status"])
	}
	if catalog, _ := data["catalog"].(map[string]any); catalog["state"] != "fresh" {
		t.Fatalf("quiero el estado del catálogo en el documento: %v", data["catalog"])
	}

	// engine list: MISMA fuente de caché y mismo orden (data sigue siendo una
	// lista de cadenas: contrato anterior intacto).
	_, doc = runEngine(t, env, out, "engine", "list", "paper")
	list, ok := doc["data"].([]any)
	if !ok {
		t.Fatalf("engine list debe seguir devolviendo una lista de versiones: %v", doc["data"])
	}
	if len(list) != 2 || list[0] != "1.21.1" || list[1] != "26.3" {
		t.Fatalf("lista = %v; quiero [1.21.1 26.3]", list)
	}

	// --all incluye el canal beta.
	_, doc = runEngine(t, env, out, "engine", "list", "paper", "--all")
	list, _ = doc["data"].([]any)
	if len(list) != 3 {
		t.Fatalf("con --all quiero 3, tengo %v", list)
	}

	// --limit toma las N MÁS RECIENTES (el catálogo va ascendente).
	_, doc = runEngine(t, env, out, "engine", "list", "paper", "--limit", "1")
	list, _ = doc["data"].([]any)
	if len(list) != 1 || list[0] != "26.3" {
		t.Fatalf("--limit 1 = %v; quiero [26.3]", list)
	}
}
