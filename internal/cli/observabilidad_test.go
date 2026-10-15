package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/spf13/cobra"
)

// manifestVanilla es un manifiesto mínimo pero VÁLIDO para el adaptador de
// Vanilla (id, type y releaseTime), suficiente para una sonda 200.
const manifestVanilla = `{"latest":{"release":"1.21.1","snapshot":"1.22"},
"versions":[{"id":"1.21.1","type":"release","releaseTime":"2026-08-01T00:00:00+00:00"}]}`

// runDoctorAPI ejecuta `doctor api ...` y devuelve el documento JSON.
func runDoctorAPI(t *testing.T, env *Env, out *bytes.Buffer, args ...string) (map[string]any, error) {
	t.Helper()
	out.Reset()
	root := &cobra.Command{Use: "root", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newDoctorAPICmd(env))
	root.SetArgs(args)
	err := root.Execute()
	var doc map[string]any
	if out.Len() > 0 {
		if uerr := json.Unmarshal(out.Bytes(), &doc); uerr != nil {
			t.Fatalf("la salida no es un documento JSON válido: %v\n%s", uerr, out.String())
		}
		for _, key := range []string{"ok", "exit_code", "action", "message", "timestamp"} {
			if _, ok := doc[key]; !ok {
				t.Errorf("falta la clave estable %q: %v", key, doc)
			}
		}
	}
	return doc, err
}

// probeDoc extrae el informe del primer proveedor del documento.
func probeDoc(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	data, ok := doc["data"].(map[string]any)
	if !ok {
		t.Fatalf("falta data: %v", doc)
	}
	list, ok := data["providers"].([]any)
	if !ok || len(list) == 0 {
		t.Fatalf("faltan proveedores: %v", data)
	}
	entry, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("proveedor inválido: %v", list[0])
	}
	probe, ok := entry["probe"].(map[string]any)
	if !ok {
		t.Fatalf("falta probe: %v", entry)
	}
	return probe
}

// TestDoctorAPIInformeCompleto: la sonda publica conectividad, HTTP, tiempo,
// formato, contrato, esquema, versiones y el estado de la caché (AC-12).
func TestDoctorAPIInformeCompleto(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(manifestVanilla))
	}))
	defer srv.Close()

	env, out, _ := catalogTestEnv(t)
	doc, _ := runDoctorAPI(t, env, out, "api", "vanilla", "--base", "vanilla="+srv.URL+"/manifest.json")
	if doc["exit_code"] != float64(0) || doc["ok"] != true {
		t.Fatalf("documento = %v", doc)
	}
	probe := probeDoc(t, doc)
	if probe["status"] != "ok" || probe["reachable"] != true || probe["schema_ok"] != true {
		t.Fatalf("probe = %v", probe)
	}
	if probe["http_status"] != float64(http.StatusOK) || probe["format"] != "json" {
		t.Fatalf("http/formato = %v", probe)
	}
	if probe["contract"] != "version_manifest_v2" || probe["schema"] != "engines.catalog/1" {
		t.Fatalf("contrato/esquema = %v", probe)
	}
	if probe["versions"] != float64(1) {
		t.Fatalf("versiones = %v", probe["versions"])
	}
	if _, ok := probe["duration_ms"].(float64); !ok {
		t.Fatalf("falta duration_ms: %v", probe)
	}
	// El estado de la caché viaja aparte (sin tocar la red).
	data := doc["data"].(map[string]any)
	entry := data["providers"].([]any)[0].(map[string]any)
	cache, ok := entry["cache"].(map[string]any)
	if !ok || cache["state"] == "" {
		t.Fatalf("falta el estado de caché: %v", entry)
	}
	if summary, _ := data["summary"].(map[string]any); summary["ok"] != float64(1) {
		t.Fatalf("resumen = %v", data["summary"])
	}
}

// TestDoctorAPIMatrizDeEstados: cada código HTTP se traduce al mismo código
// estable y a la misma categoría que el catálogo.
func TestDoctorAPIMatrizDeEstados(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantStatus string
		wantReason string
		wantExit   float64
		retryable  bool
	}{
		{"404", http.StatusNotFound, `{"error":"no"}`, "not_found", "not_found", 1, false},
		{"500", http.StatusInternalServerError, `{"error":"boom"}`, "upstream_unavailable", "network", 1, true},
		{"429", http.StatusTooManyRequests, `{"error":"lento"}`, "rate_limited", "network", 1, true},
		{"esquema distinto", http.StatusOK, `{"otra":"cosa"}`, "schema_mismatch", "invalid_response", 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			env, out, _ := catalogTestEnv(t)
			doc, _ := runDoctorAPI(t, env, out, "api", "vanilla", "--base", "vanilla="+srv.URL, "--timeout", "5")
			if doc["exit_code"] != tc.wantExit {
				t.Fatalf("exit_code = %v; quiero %v (%v)", doc["exit_code"], tc.wantExit, doc["message"])
			}
			probe := probeDoc(t, doc)
			if probe["status"] != tc.wantStatus || probe["reason"] != tc.wantReason {
				t.Fatalf("probe = %v; quiero %s/%s", probe, tc.wantStatus, tc.wantReason)
			}
			if probe["retryable"] != tc.retryable {
				t.Fatalf("retryable = %v; quiero %v", probe["retryable"], tc.retryable)
			}
			if probe["reachable"] != true {
				t.Fatalf("hubo respuesta HTTP: %v", probe)
			}
		})
	}
}

// TestDoctorAPIBaseInvalida: un --base mal formado es un error de uso.
func TestDoctorAPIBaseInvalida(t *testing.T) {
	env, out, _ := catalogTestEnv(t)
	doc, _ := runDoctorAPI(t, env, out, "api", "vanilla", "--base", "sin-igual")
	if doc["exit_code"] != float64(2) {
		t.Fatalf("exit_code = %v", doc["exit_code"])
	}
	errObj, _ := doc["error"].(map[string]any)
	if errObj["code"] != "error_usage" {
		t.Fatalf("error = %v", errObj)
	}
}

// TestDoctorAPINoSacaSecretos: la URL publicada va saneada (sin credenciales ni
// tokens) incluso si el operador pasa un --base con ellos.
func TestDoctorAPINoSacaSecretos(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(manifestVanilla))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	env, out, _ := catalogTestEnv(t)
	raw := "http://usuario:clave@" + host + "/manifest.json?token=secreto"
	doc, _ := runDoctorAPI(t, env, out, "api", "vanilla", "--base", "vanilla="+raw)
	serialized, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"clave", "secreto", "usuario"} {
		if strings.Contains(string(serialized), secret) {
			t.Fatalf("el documento filtra %q: %s", secret, serialized)
		}
	}
	if !strings.Contains(string(serialized), "***") {
		t.Fatalf("la URL publicada debería ir saneada: %s", serialized)
	}
}

// TestEngineUseFalloExponeDiagnostico: un fallo de catálogo llega al documento
// con el objeto de diagnóstico completo en error.details.
func TestEngineUseFalloExponeDiagnostico(t *testing.T) {
	env, out, _, _ := paridadEnv(t)
	paridadSeedInstance(t, env.cfg.DataDir)

	doc, _ := runEngine(t, env, out, "engine", "use", "demo", "paper", "--version", "9.9")
	if doc["exit_code"] != float64(2) {
		t.Fatalf("exit_code = %v (%v)", doc["exit_code"], doc["message"])
	}
	errObj, ok := doc["error"].(map[string]any)
	if !ok {
		t.Fatalf("falta error: %v", doc)
	}
	if errObj["code"] != "error_not_found" {
		t.Fatalf("código = %v", errObj["code"])
	}
	details, ok := errObj["details"].(map[string]any)
	if !ok {
		t.Fatalf("faltan detalles: %v", errObj)
	}
	if details["catalog_code"] != engines.CodeNotFound || details["operation"] != "resolve" {
		t.Fatalf("detalles = %v", details)
	}
	diag, ok := details["diagnostic"].(map[string]any)
	if !ok {
		t.Fatalf("falta el diagnóstico estructurado: %v", details)
	}
	if diag["provider"] != "paper" || diag["operation"] != "resolve" {
		t.Fatalf("diagnóstico = %v", diag)
	}
	if diag["status"] != engines.CodeNotFound || diag["reason"] != engines.ReasonNotFound {
		t.Fatalf("status/reason = %v", diag)
	}
	if diag["resolution_complete"] != false || diag["retryable"] != false {
		t.Fatalf("resolution_complete/retryable = %v", diag)
	}
	if diag["cache_state"] == "" || diag["cache_state"] == nil {
		t.Fatalf("falta cache_state: %v", diag)
	}
	if msg, _ := diag["message"].(string); !strings.Contains(msg, "no publica") {
		t.Fatalf("mensaje humano = %q", msg)
	}
}
