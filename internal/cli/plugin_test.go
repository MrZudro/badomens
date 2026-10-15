package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/output"
	"github.com/spf13/cobra"
)

// jarDemo construye un JAR mÃ­nimo vÃ¡lido en memoria.
func jarDemo(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct{ name, data string }{
		{"META-INF/MANIFEST.MF", "Manifest-Version: 1.0\n"},
		{"plugin.yml", "name: Demo\nversion: 1.0\n"},
	} {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatalf("create %s: %v", e.name, err)
		}
		if _, err := w.Write([]byte(e.data)); err != nil {
			t.Fatalf("write %s: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// pluginTestEnv prepara un Env aislado (data dir temporal, salida a memoria) con
// una instancia paper ya registrada, y devuelve tambiÃ©n el buffer de salida.
func pluginTestEnv(t *testing.T) (*Env, *bytes.Buffer, string) {
	t.Helper()
	dataDir := t.TempDir()
	cfg := &config.Config{DataDir: dataDir}
	m := instance.New(dataDir, engineNames())
	if err := m.Save(&instance.Spec{
		Slug: "demo", Name: "demo", Engine: "paper", Version: "1.21.4",
		Memory: "2G", Port: 25565, MaxPlayers: 10, Difficulty: "normal", Gamemode: "survival",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var out, errBuf bytes.Buffer
	env := &Env{
		stdout:       &out,
		stderr:       &errBuf,
		cfg:          cfg,
		instanceFlag: "demo",
		printer:      output.New(&out, &errBuf, output.JSON),
	}
	return env, &out, dataDir
}

// runPluginAdd ejecuta el subcomando `plugin add` con los argumentos dados.
func runPluginAdd(t *testing.T, env *Env, args ...string) error {
	t.Helper()
	cmd := newPluginAddCmd(env)
	root := &cobra.Command{Use: "root", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(cmd)
	root.SetArgs(append([]string{"add"}, args...))
	return root.Execute()
}

// parseDoc convierte la salida JSON en un mapa y comprueba las claves estables
// del contrato.
func parseDoc(t *testing.T, out *bytes.Buffer) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("la salida no es un documento JSON vÃ¡lido: %v\n%s", err, out.String())
	}
	for _, key := range []string{"ok", "exit_code", "action", "message", "timestamp"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("falta la clave estable %q en el documento: %v", key, doc)
		}
	}
	if doc["action"] != "plugin add" {
		t.Errorf("action = %v", doc["action"])
	}
	return doc
}

// assertSinTemporales comprueba que no queda ningÃºn archivo parcial.
func assertSinTemporales(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".badomens-") || strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("quedÃ³ un archivo temporal/parcial: %s", e.Name())
		}
	}
}

// TestPluginAddInstalaYActualizaRegistro comprueba el flujo completo: archivo en
// destino, temporal eliminado y registro persistente actualizado UNA sola vez.
func TestPluginAddInstalaYActualizaRegistro(t *testing.T) {
	env, out, dataDir := pluginTestEnv(t)
	jar := jarDemo(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(jar)
	}))
	t.Cleanup(srv.Close)

	if err := runPluginAdd(t, env, srv.URL+"/demo-1.0.jar", "--allow-http", "--allow-loopback"); err != nil {
		t.Fatalf("plugin add: %v", err)
	}
	doc := parseDoc(t, out)
	if doc["ok"] != true || doc["exit_code"] != float64(0) {
		t.Errorf("documento inesperado: %v", doc)
	}
	if _, hasErr := doc["error"]; hasErr {
		t.Errorf("no deberÃ­a haber error: %v", doc["error"])
	}
	data, _ := doc["data"].(map[string]any)
	if data == nil {
		t.Fatalf("falta data: %v", doc)
	}
	if data["added"] != "demo-1.0.jar" {
		t.Errorf("data.added = %v", data["added"])
	}
	if data["checksum_verified"] != false {
		t.Errorf("checksum_verified = %v", data["checksum_verified"])
	}
	if w, _ := data["security_warning"].(string); !strings.Contains(w, "terceros") {
		t.Errorf("falta el aviso sobre cÃ³digo de terceros: %v", data["security_warning"])
	}
	if msg, _ := doc["message"].(string); !strings.Contains(msg, "demo-1.0.jar instalado en demo/") {
		t.Errorf("message inesperado: %v", doc["message"])
	}

	pluginsDir := filepath.Join(dataDir, "instances", "demo", "plugins")
	if _, err := os.Stat(filepath.Join(pluginsDir, "demo-1.0.jar")); err != nil {
		t.Errorf("el archivo no estÃ¡ en el destino: %v", err)
	}
	assertSinTemporales(t, pluginsDir)

	m := instance.New(dataDir, engineNames())
	spec, err := m.Load("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(spec.Plugins) != 1 || spec.Plugins[0] != "demo-1.0.jar" {
		t.Fatalf("registro inesperado: %v", spec.Plugins)
	}

	// Segunda ejecuciÃ³n: el archivo ya existe, asÃ­ que falla SIN duplicar el
	// registro ni sobrescribir nada.
	out.Reset()
	if err := runPluginAdd(t, env, srv.URL+"/demo-1.0.jar", "--allow-http", "--allow-loopback"); err == nil {
		t.Error("se esperaba error al reinstalar el mismo archivo")
	}
	doc = parseDoc(t, out)
	if doc["ok"] != false || doc["exit_code"] != float64(2) {
		t.Errorf("documento inesperado: %v", doc)
	}
	errInfo, _ := doc["error"].(map[string]any)
	if errInfo == nil {
		t.Fatalf("falta error en el documento: %v", doc)
	}
	if errInfo["code"] != "error_usage" {
		t.Errorf("error.code = %v", errInfo["code"])
	}
	details, _ := errInfo["details"].(map[string]any)
	if details["reason"] != "destino_existente" {
		t.Errorf("details.reason = %v", details["reason"])
	}
	spec, err = m.Load("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(spec.Plugins) != 1 {
		t.Errorf("el registro se duplicÃ³: %v", spec.Plugins)
	}
	assertSinTemporales(t, pluginsDir)
}

// TestPluginAddFallaSinRegistroNiArchivo: un fallo de descarga no deja plugin
// parcial ni registro desactualizado.
func TestPluginAddFallaSinRegistroNiArchivo(t *testing.T) {
	cases := []struct {
		name   string
		code   int
		url    string
		reason string
	}{
		{name: "http 500", code: http.StatusInternalServerError, reason: "http_error"},
		{name: "http 404", code: http.StatusNotFound, reason: "http_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, out, dataDir := pluginTestEnv(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
			}))
			t.Cleanup(srv.Close)

			err := runPluginAdd(t, env, srv.URL+"/demo.jar", "--allow-http", "--allow-loopback")
			if err == nil {
				t.Fatal("se esperaba error")
			}
			doc := parseDoc(t, out)
			if doc["exit_code"] != float64(2) || doc["ok"] != false {
				t.Errorf("documento inesperado: %v", doc)
			}
			errInfo, _ := doc["error"].(map[string]any)
			if errInfo["code"] != "error_unavailable" {
				t.Errorf("error.code = %v", errInfo["code"])
			}
			details, _ := errInfo["details"].(map[string]any)
			if details["reason"] != tc.reason {
				t.Errorf("details.reason = %v", details["reason"])
			}
			if got := details["http_status"]; got != float64(tc.code) {
				t.Errorf("details.http_status = %v", got)
			}

			pluginsDir := filepath.Join(dataDir, "instances", "demo", "plugins")
			if _, serr := os.Stat(filepath.Join(pluginsDir, "demo.jar")); serr == nil {
				t.Error("no deberÃ­a existir el archivo destino")
			}
			assertSinTemporales(t, pluginsDir)
			m := instance.New(dataDir, engineNames())
			spec, lerr := m.Load("demo")
			if lerr != nil {
				t.Fatalf("Load: %v", lerr)
			}
			if len(spec.Plugins) != 0 {
				t.Errorf("el registro no deberÃ­a cambiar: %v", spec.Plugins)
			}
		})
	}
}

// TestPluginAddRechazaSSRSinArchivo: un destino local no crea nada.
func TestPluginAddRechazaSSRSinArchivo(t *testing.T) {
	env, out, dataDir := pluginTestEnv(t)
	err := runPluginAdd(t, env, "http://169.254.169.254/latest/meta-data/plugin.jar", "--allow-http")
	if err == nil {
		t.Fatal("se esperaba rechazo por SSRF")
	}
	doc := parseDoc(t, out)
	errInfo, _ := doc["error"].(map[string]any)
	details, _ := errInfo["details"].(map[string]any)
	if details["reason"] != "destino_no_permitido" {
		t.Errorf("details.reason = %v", details["reason"])
	}
	if entries, serr := os.ReadDir(filepath.Join(dataDir, "instances", "demo", "plugins")); serr == nil && len(entries) != 0 {
		t.Errorf("no deberÃ­a haberse creado nada: %v", entries)
	}
}

// TestPluginAddValidaFlags: checksum y tamaÃ±o se validan antes de la red.
func TestPluginAddValidaFlags(t *testing.T) {
	env, out, _ := pluginTestEnv(t)
	if err := runPluginAdd(t, env, "https://ejemplo.invalid/demo.jar", "--sha256", "abc"); err == nil {
		t.Error("se esperaba error por checksum invÃ¡lido")
	}
	doc := parseDoc(t, out)
	errInfo, _ := doc["error"].(map[string]any)
	details, _ := errInfo["details"].(map[string]any)
	if details["reason"] != "checksum_invalido" {
		t.Errorf("details.reason = %v", details["reason"])
	}

	out.Reset()
	if err := runPluginAdd(t, env, "https://ejemplo.invalid/demo.jar", "--max-size", "muchisimo"); err == nil {
		t.Error("se esperaba error por tamaÃ±o invÃ¡lido")
	}
	doc = parseDoc(t, out)
	errInfo, _ = doc["error"].(map[string]any)
	details, _ = errInfo["details"].(map[string]any)
	if details["reason"] != "limite_invalido" {
		t.Errorf("details.reason = %v", details["reason"])
	}
}

// TestParseSizeYParseSHA256 cubre los ayudantes de flags.
func TestParseSizeYParseSHA256(t *testing.T) {
	sizes := map[string]int64{"": 0, "1024": 1024, "32K": 32 << 10, "32KiB": 32 << 10, "64M": 64 << 20, "1G": 1 << 30}
	for raw, want := range sizes {
		got, err := parseSize(raw)
		if err != nil || got != want {
			t.Errorf("parseSize(%q) = %d, %v; se esperaba %d", raw, got, err, want)
		}
	}
	for _, raw := range []string{"0", "-5", "abc", "12X"} {
		if _, err := parseSize(raw); err == nil {
			t.Errorf("parseSize(%q): se esperaba error", raw)
		}
	}
	if sum, err := parseSHA256("  SHA256:" + strings.Repeat("AB", 32) + " "); err != nil || sum != strings.Repeat("ab", 32) {
		t.Errorf("parseSHA256 con prefijo = %q, %v", sum, err)
	}
	if _, err := parseSHA256(strings.Repeat("z", 64)); err == nil {
		t.Error("se esperaba error con caracteres no hexadecimales")
	}
	if sum, err := parseSHA256(""); err != nil || sum != "" {
		t.Errorf("parseSHA256 vacÃ­o = %q, %v", sum, err)
	}
}

// TestPluginAddRechazaLoopbackPorDefecto es la prueba de regresión de API-07:
// antes CUALQUIER URL se descargaba (incluido http://127.0.0.1); ahora el
// destino local se rechaza salvo --allow-loopback explícito.
func TestPluginAddRechazaLoopbackPorDefecto(t *testing.T) {
	env, out, dataDir := pluginTestEnv(t)
	jar := jarDemo(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(jar)
	}))
	t.Cleanup(srv.Close)
	pluginsDir := filepath.Join(dataDir, "instances", "demo", "plugins")

	if err := runPluginAdd(t, env, srv.URL+"/demo.jar", "--allow-http"); err == nil {
		t.Fatal("se esperaba rechazo del destino loopback por defecto")
	}
	doc := parseDoc(t, out)
	errInfo, _ := doc["error"].(map[string]any)
	details, _ := errInfo["details"].(map[string]any)
	if details["reason"] != "destino_no_permitido" {
		t.Errorf("details.reason = %v", details["reason"])
	}
	if _, serr := os.Stat(filepath.Join(pluginsDir, "demo.jar")); serr == nil {
		t.Error("no debería existir el archivo destino")
	}

	// Con la política explícita se instala.
	out.Reset()
	if err := runPluginAdd(t, env, srv.URL+"/demo.jar", "--allow-http", "--allow-loopback"); err != nil {
		t.Fatalf("con --allow-loopback se esperaba éxito: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pluginsDir, "demo.jar")); err != nil {
		t.Errorf("el archivo no está en el destino: %v", err)
	}
}

// TestPluginAddLimiteDeDescarga: `--max-size` aborta la descarga sin dejar
// archivo (antes el límite era de 256 MiB y el archivo se escribía completo).
func TestPluginAddLimiteDeDescarga(t *testing.T) {
	env, out, dataDir := pluginTestEnv(t)
	jar := jarDemo(t)
	jar = append(jar, bytes.Repeat([]byte("Z"), 4096)...) // deja de ser un JAR válido
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(jar)
	}))
	t.Cleanup(srv.Close)

	if err := runPluginAdd(t, env, srv.URL+"/demo.jar", "--allow-http", "--allow-loopback", "--max-size", "1K"); err == nil {
		t.Fatal("se esperaba error por superar el límite")
	}
	doc := parseDoc(t, out)
	errInfo, _ := doc["error"].(map[string]any)
	if errInfo["code"] != "error_capacity_exceeded" {
		t.Errorf("error.code = %v", errInfo["code"])
	}
	pluginsDir := filepath.Join(dataDir, "instances", "demo", "plugins")
	if _, serr := os.Stat(filepath.Join(pluginsDir, "demo.jar")); serr == nil {
		t.Error("no debería existir el archivo destino")
	}
	assertSinTemporales(t, pluginsDir)
}

// TestPluginAddNoSobrescribe: un archivo existente no se sustituye en silencio
// (antes sí se sobrescribía con os.WriteFile).
func TestPluginAddNoSobrescribe(t *testing.T) {
	env, out, dataDir := pluginTestEnv(t)
	pluginsDir := filepath.Join(dataDir, "instances", "demo", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	destino := filepath.Join(pluginsDir, "demo.jar")
	if err := os.WriteFile(destino, []byte("antiguo"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "demo.jar")
	if err := os.WriteFile(src, jarDemo(t), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runPluginAdd(t, env, src); err == nil {
		t.Fatal("se esperaba error por archivo existente")
	}
	doc := parseDoc(t, out)
	errInfo, _ := doc["error"].(map[string]any)
	details, _ := errInfo["details"].(map[string]any)
	if details["reason"] != "destino_existente" {
		t.Errorf("details.reason = %v", details["reason"])
	}
	if raw, rerr := os.ReadFile(destino); rerr != nil || string(raw) != "antiguo" {
		t.Errorf("el archivo existente se alteró: %q (%v)", raw, rerr)
	}
	assertSinTemporales(t, pluginsDir)

	// Con --on-conflict=rename se conserva el antiguo y se instala el nuevo.
	out.Reset()
	if err := runPluginAdd(t, env, src, "--on-conflict", "rename"); err != nil {
		t.Fatalf("con --on-conflict=rename se esperaba éxito: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pluginsDir, "demo-2.jar")); err != nil {
		t.Errorf("no se creó demo-2.jar: %v", err)
	}
	if raw, _ := os.ReadFile(destino); string(raw) != "antiguo" {
		t.Errorf("el archivo existente se alteró: %q", raw)
	}
	m := instance.New(dataDir, engineNames())
	spec, lerr := m.Load("demo")
	if lerr != nil {
		t.Fatalf("Load: %v", lerr)
	}
	if len(spec.Plugins) != 1 || spec.Plugins[0] != "demo-2.jar" {
		t.Errorf("registro inesperado: %v", spec.Plugins)
	}
}
