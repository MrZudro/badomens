package engines

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// probeFakeProvider es un proveedor de prueba que OBSERVA el transporte real: su
// única petición pasa por Client (códigos, reintentos, límites) y su parser
// valida el esquema, de modo que la sonda se prueba de verdad y no contra un
// doble que no hace HTTP.
type probeFakeProvider struct {
	engine Engine
	client *Client
	base   string
}

func (p *probeFakeProvider) Engine() Engine { return p.engine }

func (p *probeFakeProvider) ListVersions(ctx context.Context) ([]Version, error) {
	var doc struct {
		Versions []string `json:"versions"`
	}
	if err := p.client.GetJSON(ctx, p.base, &doc); err != nil {
		return nil, err
	}
	if len(doc.Versions) == 0 {
		return nil, newProviderError(CodeSchemaMismatch, p.engine, "list_versions", p.base,
			errors.New("el documento no trae versiones"))
	}
	out := make([]Version, 0, len(doc.Versions))
	for _, v := range doc.Versions {
		out = append(out, Version{Version: v})
	}
	return out, nil
}

func (p *probeFakeProvider) Resolve(context.Context, string) (Resolved, error) {
	return Resolved{}, errors.New("no usado")
}

// probeTestProvider construye el proveedor de prueba y el cliente del transporte.
func probeTestProvider(t *testing.T, handler http.HandlerFunc) (*probeFakeProvider, *Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := staticTestClient() // 1 intento: determinista y rápido
	return &probeFakeProvider{engine: Fabric, client: client, base: srv.URL + "/catalog"}, client
}

func TestProbeReportaConectividadFormatoYEsquema(t *testing.T) {
	provider, client := probeTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"versions":["1.21.1"]}`))
	})
	rep := probeProvider(context.Background(), provider, client, ProbeOptions{})
	if !rep.OK() || rep.Status != "ok" {
		t.Fatalf("informe = %+v", rep)
	}
	if !rep.Reachable || !rep.SchemaOK || rep.HTTPStatus != http.StatusOK {
		t.Fatalf("conectividad/esquema = %+v", rep)
	}
	if rep.Format != "json" || rep.Versions != 1 {
		t.Fatalf("formato/versiones = %+v", rep)
	}
	if rep.Contract != "" || rep.Schema != catalogSchemaVersion {
		t.Fatalf("contrato/esquema = %q / %q", rep.Contract, rep.Schema)
	}
	if rep.Message == "" || rep.Reason != "" {
		t.Fatalf("mensaje/reason = %q / %q", rep.Message, rep.Reason)
	}
}

func TestProbeMatrizHTTP(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantStatus string
		wantReason string
		retryable  bool
		httpStatus int
		reachable  bool
	}{
		{"200 ok", http.StatusOK, `{"versions":["1.21.1"]}`, "ok", "", false, http.StatusOK, true},
		{"404", http.StatusNotFound, `{"error":"no"}`, CodeNotFound, ReasonNotFound, false, http.StatusNotFound, true},
		{"429", http.StatusTooManyRequests, `{"error":"lento"}`, CodeRateLimited, ReasonNetwork, true, http.StatusTooManyRequests, true},
		{"500", http.StatusInternalServerError, `{"error":"boom"}`, CodeUpstreamUnavailable, ReasonNetwork, true, http.StatusInternalServerError, true},
		{"esquema distinto", http.StatusOK, `{"otraCosa":true}`, CodeSchemaMismatch, ReasonInvalidResponse, false, http.StatusOK, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider, client := probeTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			rep := probeProvider(context.Background(), provider, client, ProbeOptions{})
			if rep.Status != tc.wantStatus {
				t.Fatalf("status = %q; quiero %q (%+v)", rep.Status, tc.wantStatus, rep)
			}
			if rep.Reason != tc.wantReason {
				t.Fatalf("reason = %q; quiero %q", rep.Reason, tc.wantReason)
			}
			if rep.Retryable != tc.retryable {
				t.Fatalf("retryable = %v; quiero %v", rep.Retryable, tc.retryable)
			}
			if rep.HTTPStatus != tc.httpStatus {
				t.Fatalf("http_status = %d; quiero %d", rep.HTTPStatus, tc.httpStatus)
			}
			if rep.Reachable != tc.reachable {
				t.Fatalf("reachable = %v; quiero %v", rep.Reachable, tc.reachable)
			}
			if rep.Message == "" {
				t.Fatal("falta el mensaje legible")
			}
		})
	}
}

func TestProbeTimeout(t *testing.T) {
	provider, client := probeTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	})
	client.Timeout = 150 * time.Millisecond

	rep := probeProvider(context.Background(), provider, client, ProbeOptions{})
	if rep.Status != CodeTimeout || rep.Reason != ReasonNetwork {
		t.Fatalf("timeout = %+v", rep)
	}
	if rep.Reachable || rep.HTTPStatus != 0 {
		t.Fatalf("un timeout no tiene respuesta HTTP: %+v", rep)
	}
	if rep.DurationMS < 0 {
		t.Fatalf("duration_ms = %d", rep.DurationMS)
	}
}

// TestProbeReintentaYCuentaIntentos comprueba que la sonda observa el número de
// intentos del transporte (política única, sin canal paralelo).
func TestProbeReintentaYCuentaIntentos(t *testing.T) {
	provider, client := probeTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	client.MaxAttempts = 3
	client.Sleep = func(context.Context, time.Duration) error { return nil } // sin esperas reales

	rep := probeProvider(context.Background(), provider, client, ProbeOptions{})
	if rep.Status != CodeUpstreamUnavailable || !rep.Retryable {
		t.Fatalf("informe = %+v", rep)
	}
	if rep.Attempts != 3 {
		t.Fatalf("intentos = %d; quiero 3", rep.Attempts)
	}
}

func TestProbeMotorDesconocido(t *testing.T) {
	rep := Probe(context.Background(), "noexiste", ProbeOptions{})
	if rep.Status != "unknown_engine" || rep.OK() {
		t.Fatalf("informe = %+v", rep)
	}
	if !strings.Contains(rep.Message, "motor desconocido") {
		t.Fatalf("mensaje = %q", rep.Message)
	}
}

func TestProbeRegistroPorBasesYContratos(t *testing.T) {
	reg := probeRegistry(staticTestClient(), nil)
	for _, name := range Names() {
		p, err := reg.Provider(name)
		if err != nil {
			t.Fatalf("sin proveedor para %s: %v", name, err)
		}
		if providerContract(p) == "" {
			t.Fatalf("el proveedor %s no declara su contrato", name)
		}
	}
	bases := map[string]string{"paper": "https://ejemplo.test/fill"}
	p, _ := probeRegistry(staticTestClient(), bases).Provider("paper")
	if bp, ok := p.(baseProvider); !ok || bp.Base() != "https://ejemplo.test/fill" {
		t.Fatalf("la base inyectada se ignoró: %v", p)
	}
}

func TestFormatOf(t *testing.T) {
	cases := map[string]string{
		"application/json; charset=utf-8": "json",
		"application/vnd.api+json":        "json",
		"application/xml":                 "xml",
		"text/xml; charset=utf-8":         "xml",
		"text/html":                       "unknown",
	}
	for header, want := range cases {
		if got := formatOf(header, ""); got != want {
			t.Fatalf("formatOf(%q) = %q; quiero %q", header, got, want)
		}
	}
	if got := formatOf("", "maven-metadata/xml-1"); got != "xml" {
		t.Fatalf("contrato XML sin cabecera = %q", got)
	}
	if got := formatOf("", "fill/v3"); got != "json" {
		t.Fatalf("contrato JSON sin cabecera = %q", got)
	}
	if got := formatOf("", ""); got != "unknown" {
		t.Fatalf("sin datos = %q", got)
	}
}
