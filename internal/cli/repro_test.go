package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/output"
	"github.com/spf13/cobra"
)

// humanEnv prepara un Env aislado en modo humano (el manifiesto se imprime tal
// cual por stdout).
func humanEnv(t *testing.T) (*Env, *bytes.Buffer, string) {
	t.Helper()
	dataDir := t.TempDir()
	var out, errBuf bytes.Buffer
	env := &Env{
		stdout:  &out,
		stderr:  &errBuf,
		cfg:     &config.Config{DataDir: dataDir},
		printer: output.New(&out, &errBuf, output.Human),
	}
	return env, &out, dataDir
}

// runRepro ejecuta `repro ...` y devuelve la salida y, si es un documento JSON,
// el documento deserializado.
func runRepro(t *testing.T, env *Env, out *bytes.Buffer, args ...string) (map[string]any, string) {
	t.Helper()
	out.Reset()
	root := &cobra.Command{Use: "root", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newReproCmd(env))
	root.SetArgs(args)
	_ = root.Execute()
	raw := out.String()
	doc := map[string]any{}
	if strings.HasPrefix(strings.TrimSpace(raw), "{") {
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			t.Fatalf("la salida no es un documento JSON válido: %v\n%s", err, raw)
		}
	}
	return doc, raw
}

// runDoctorPreflight ejecuta `doctor preflight ...`.
func runDoctorPreflight(t *testing.T, env *Env, out *bytes.Buffer, args ...string) map[string]any {
	t.Helper()
	out.Reset()
	root := &cobra.Command{Use: "root", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newDoctorPreflightCmd(env))
	root.SetArgs(args)
	_ = root.Execute()
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("preflight no es JSON: %v\n%s", err, out.String())
	}
	return doc
}

// seedPaperInstance registra una instancia de Paper con pins exactos.
func seedPaperInstance(t *testing.T, dataDir, version, build string) {
	t.Helper()
	m := instance.New(dataDir, engineNames())
	spec := &instance.Spec{
		Slug: "demo", Name: "demo", Engine: "paper", Version: version,
		Pins: map[string]string{"PAPER_BUILD": build}, Memory: "4G", Port: 25565,
		MaxPlayers: 20, CreatedAt: instance.Stamp(),
	}
	if err := m.Save(spec); err != nil {
		t.Fatalf("no se pudo preparar la instancia: %v", err)
	}
}

// TestReproExportImportRecreaLaInstancia (plan 12.5) comprueba el ciclo completo
// exportar → importar con los pines exactos y sin consultar la red.
func TestReproExportImportRecreaLaInstancia(t *testing.T) {
	env, out, dataDir := catalogTestEnv(t)
	seedPaperInstance(t, dataDir, "1.21.1", "123")
	path := filepath.Join(t.TempDir(), "demo.json")

	doc, _ := runRepro(t, env, out, "repro", "export", "demo", "--output", path)
	if doc["exit_code"] != float64(0) || doc["ok"] != true {
		t.Fatalf("export = %v", doc)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("no se escribió el manifiesto: %v", err)
	}
	manifest := doc["data"].(map[string]any)["manifest"].(map[string]any)
	if manifest["engine"] != "paper" || manifest["version"] != "1.21.1" {
		t.Fatalf("manifiesto = %v", manifest)
	}
	pins, _ := manifest["pins"].(map[string]any)
	if pins["PAPER_BUILD"] != "123" {
		t.Fatalf("pins = %v; quiero el build exacto", pins)
	}

	doc, _ = runRepro(t, env, out, "repro", "import", path, "--name", "copia")
	if doc["exit_code"] != float64(0) {
		t.Fatalf("import = %v", doc)
	}
	data := doc["data"].(map[string]any)
	if data["instance"] != "copia" || data["engine"] != "paper" || data["version"] != "1.21.1" {
		t.Fatalf("import = %v", data)
	}
	loaded, err := instance.New(dataDir, engineNames()).Load("copia")
	if err != nil {
		t.Fatalf("la instancia importada no se puede cargar: %v", err)
	}
	if loaded.Pins["PAPER_BUILD"] != "123" || loaded.Version != "1.21.1" {
		t.Fatalf("instancia importada = %+v", loaded)
	}

	// Sin --replace no se sobrescribe una instancia existente.
	doc, _ = runRepro(t, env, out, "repro", "import", path, "--name", "copia")
	if doc["exit_code"] != float64(2) {
		t.Fatalf("sin --replace debería fallar: %v", doc)
	}
	if errObj, _ := doc["error"].(map[string]any); errObj["code"] != "error_usage" {
		t.Fatalf("error = %v", doc["error"])
	}
	// Con --replace sí.
	doc, _ = runRepro(t, env, out, "repro", "import", path, "--name", "copia", "--replace")
	if doc["exit_code"] != float64(0) {
		t.Fatalf("con --replace debería aplicarse: %v", doc)
	}
}

// TestReproExportImprimeElManifiesto: sin --output el manifiesto va por stdout en
// modo humano, y en modo JSON viaja en `data.manifest`.
func TestReproExportImprimeElManifiesto(t *testing.T) {
	env, out, dataDir := humanEnv(t)
	seedPaperInstance(t, dataDir, "26.3", "190")

	_, raw := runRepro(t, env, out, "repro", "export", "demo")
	if !strings.Contains(raw, "\"schema\": \"badomens.instance/1\"") {
		t.Fatalf("salida humana = %s", raw)
	}
	if !strings.Contains(raw, "\"PAPER_BUILD\": \"190\"") || !strings.Contains(raw, "\"version\": \"26.3\"") {
		t.Fatalf("salida humana sin pins exactos = %s", raw)
	}

	jsonEnv, jsonOut, _ := catalogTestEnv(t)
	seedPaperInstance(t, jsonEnv.cfg.DataDir, "26.3", "190")
	doc, _ := runRepro(t, jsonEnv, jsonOut, "repro", "export", "demo")
	if doc["exit_code"] != float64(0) {
		t.Fatalf("export JSON = %v", doc)
	}
	if _, ok := doc["data"].(map[string]any)["manifest"].(map[string]any); !ok {
		t.Fatalf("en modo JSON el manifiesto debe estar en data.manifest: %v", doc)
	}
}

// TestReproImportManifiestoInvalido: un manifiesto no reproducible se rechaza.
func TestReproImportManifiestoInvalido(t *testing.T) {
	env, out, _ := catalogTestEnv(t)
	path := filepath.Join(t.TempDir(), "malo.json")
	raw := `{"schema":"badomens.instance/1","slug":"demo","engine":"paper","version":"latest","pins":{},"memory":"4G"}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, _ := runRepro(t, env, out, "repro", "import", path)
	if doc["exit_code"] != float64(2) {
		t.Fatalf("un manifiesto con 'latest' debe rechazarse: %v", doc)
	}
	if errObj, _ := doc["error"].(map[string]any); errObj["code"] != "error_usage" {
		t.Fatalf("error = %v", doc["error"])
	}
}

// TestDoctorPreflightVeredictoTrivalente (plan 12.5): compatible → 0,
// incompatible → 2, y el veredicto lleva diagnóstico estructurado.
func TestDoctorPreflightVeredictoTrivalente(t *testing.T) {
	env, out, _, dataDir := paridadEnv(t)
	seedPaperInstance(t, dataDir, "1.21.1", "123")

	doc := runDoctorPreflight(t, env, out, "preflight", "demo")
	if doc["exit_code"] != float64(0) || doc["ok"] != true {
		t.Fatalf("compatible = %v", doc)
	}
	data := doc["data"].(map[string]any)
	if data["compatibility"] != "compatible" || data["instance"] != "demo" {
		t.Fatalf("data = %v", data)
	}
	if data["pins"].(map[string]any)["PAPER_BUILD"] != "123" {
		t.Fatalf("pins = %v", data["pins"])
	}
	if catalog, _ := data["catalog"].(map[string]any); catalog["engine"] != "paper" {
		t.Fatalf("falta el estado del catálogo: %v", data)
	}

	// Una versión con Java conocido que el motor no publica es una
	// incompatibilidad REAL (no un fallo de red): el diagnóstico la marca como
	// tal y el comando sale 2.
	seedPaperInstance(t, dataDir, "1.20.5", "123")
	doc = runDoctorPreflight(t, env, out, "preflight", "demo")
	if doc["exit_code"] != float64(2) {
		t.Fatalf("incompatible debe salir 2: %v", doc)
	}
	errObj := doc["error"].(map[string]any)
	details := errObj["details"].(map[string]any)
	diag := details["diagnostic"].(map[string]any)
	if diag["status"] != "incompatible" || diag["reason"] != "incompatible" {
		t.Fatalf("diagnóstico = %v", diag)
	}
	if diag["retryable"] != false || diag["resolution_complete"] != false {
		t.Fatalf("diagnóstico = %v", diag)
	}
}

// TestDoctorPreflightDesconocidoAvisa: un dato que no se puede comprobar
// (Java desconocido para esa versión) es AVISO, nunca "compatible".
func TestDoctorPreflightDesconocidoAvisa(t *testing.T) {
	env, out, _, dataDir := paridadEnv(t)
	seedPaperInstance(t, dataDir, "27.1", "123") // serie futura: Java desconocido
	doc := runDoctorPreflight(t, env, out, "preflight", "demo")
	if doc["exit_code"] != float64(1) {
		t.Fatalf("unknown debe salir 1: %v", doc)
	}
	data := doc["data"].(map[string]any)
	if data["compatibility"] != "unknown" {
		t.Fatalf("compatibilidad = %v", data["compatibility"])
	}
	if reason, _ := data["reason"].(string); reason == "" {
		t.Fatalf("falta el motivo estructurado: %v", data)
	}
}

// TestDoctorPreflightInstanciaInexistente: sin instancia no hay veredicto.
func TestDoctorPreflightInstanciaInexistente(t *testing.T) {
	env, out, _, _ := paridadEnv(t)
	doc := runDoctorPreflight(t, env, out, "preflight", "no-existe")
	if doc["exit_code"] != float64(2) {
		t.Fatalf("exit_code = %v", doc["exit_code"])
	}
}
