package engines

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
)

// El objeto de diagnóstico (plan 12.3 · AC-12) debe aportar, como mínimo:
// provider, operation, status, http_status, cache_state, resolution_complete y
// retryable; distinguir red / respuesta inválida / versión inexistente /
// incompatibilidad real; y no filtrar cuerpos, tokens ni URLs con credenciales.

func TestDiagnoseObjetoCompletoConCache(t *testing.T) {
	fetched := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	status := CatalogStatus{
		Engine: "paper", State: CatalogStale, Source: "cache",
		Provider: "paper", FetchedAt: fetched, Age: "hace 7 h",
	}
	err := &TransportError{
		Code: CodeUpstreamUnavailable, Provider: "paper", Operation: "resolve_build",
		URL:        "https://fill.papermc.io/v3/projects/paper/versions/1.21.1/builds",
		HTTPStatus: http.StatusServiceUnavailable, Retryable: true, Attempts: 3,
		Err: errors.New("estado HTTP 503"),
	}

	d := Diagnose("paper", "resolve_build", err, status)
	if d.Provider != "paper" || d.Engine != "paper" || d.Operation != "resolve_build" {
		t.Fatalf("identificación = %+v", d)
	}
	if d.Status != CodeUpstreamUnavailable || d.Reason != ReasonNetwork {
		t.Fatalf("status/reason = %q/%q", d.Status, d.Reason)
	}
	if d.HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("http_status = %d", d.HTTPStatus)
	}
	if d.CacheState != string(CatalogStale) || d.CacheSource != "cache" {
		t.Fatalf("caché = %q / %q", d.CacheState, d.CacheSource)
	}
	if d.ResolutionComplete {
		t.Fatal("un fallo nunca es una resolución completa")
	}
	if !d.Retryable || d.Attempts != 3 {
		t.Fatalf("retryable/attempts = %v/%d", d.Retryable, d.Attempts)
	}
	if d.LastFetchedAt == nil || !d.LastFetchedAt.Equal(fetched) || d.CacheAge != "hace 7 h" {
		t.Fatalf("última consulta = %v / %q", d.LastFetchedAt, d.CacheAge)
	}
	if !d.CacheUsable {
		t.Fatal("una caché vencida sigue siendo utilizable")
	}
	if d.Message == "" {
		t.Fatal("falta el mensaje legible")
	}
}

func TestDiagnoseDistingueRedRespuestaVersionEIncompatibilidad(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"red", &TransportError{Code: CodeTimeout}, ReasonNetwork},
		{"rate limited", &TransportError{Code: CodeRateLimited}, ReasonNetwork},
		{"upstream", &TransportError{Code: CodeUpstreamUnavailable}, ReasonNetwork},
		{"respuesta inválida", &TransportError{Code: CodeInvalidResponse}, ReasonInvalidResponse},
		{"esquema", &TransportError{Code: CodeSchemaMismatch}, ReasonInvalidResponse},
		{"demasiado grande", &TransportError{Code: CodeResponseTooLarge}, ReasonInvalidResponse},
		{"versión inexistente", &TransportError{Code: CodeNotFound}, ReasonNotFound},
		{"cancelado", &TransportError{Code: CodeCanceled}, ReasonCanceled},
		{"desconocido", errors.New("vaya"), ReasonUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Diagnose("paper", "list", tc.err, CatalogStatus{}).Reason; got != tc.want {
				t.Fatalf("reason = %q; quiero %q", got, tc.want)
			}
		})
	}

	d := DiagnoseIncompatible("paper", "compat", "el catálogo no publica Minecraft 9.9", CatalogStatus{State: CatalogFresh})
	if d.Status != ReasonIncompatible || d.Reason != ReasonIncompatible || d.Retryable {
		t.Fatalf("incompatibilidad = %+v", d)
	}
	if !strings.Contains(d.Message, "incompatible") {
		t.Fatalf("mensaje = %q", d.Message)
	}
}

func TestDiagnoseNoFiltraCredenciales(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	host := strings.TrimPrefix(srv.URL, "http://")
	srv.Close() // puerto cerrado: el fallo de conexión es inmediato

	raw := "http://usuario:clave@" + host + "/catalogo?token=secreto&v=1"
	c := newTestTransport(t)
	_, _, err := c.GetWithMeta(t.Context(), raw, RequestOpts{Provider: "paper", Operation: "list_versions"})
	if err == nil {
		t.Fatal("se esperaba un fallo de conexión")
	}
	d := Diagnose("paper", "list_versions", err, CatalogStatus{})
	for _, secret := range []string{"clave", "secreto", "usuario"} {
		if strings.Contains(d.URL, secret) || strings.Contains(d.Message, secret) {
			t.Fatalf("el diagnóstico filtra %q: %q / %q", secret, d.URL, d.Message)
		}
	}
	if CodeOf(err) != CodeUpstreamUnavailable {
		t.Fatalf("código = %q", CodeOf(err))
	}
}

func TestNormalizeErrorAdjuntaDiagnostico(t *testing.T) {
	status := CatalogStatus{Engine: "fabric", State: CatalogFresh, Source: "cache", Provider: "fabric"}
	cause := &TransportError{
		Code: CodeNotFound, Provider: "fabric", Operation: "list_versions",
		URL: "https://meta.fabricmc.net/v2/versions/loader/9.9", HTTPStatus: http.StatusNotFound,
	}
	err := NormalizeErrorWithCatalog("fabric", "list_versions", cause, status)
	if err == nil {
		t.Fatal("se esperaba error")
	}
	if !errx.Is(err, errx.CodeNotFound) {
		t.Fatalf("código errx = %v", err)
	}
	details := errx.Details(err)
	if details == nil {
		t.Fatal("faltan detalles")
	}
	if details["catalog_code"] != CodeNotFound || details["engine"] != "fabric" {
		t.Fatalf("detalles = %v", details)
	}
	d, ok := DiagnosticOf(err)
	if !ok {
		t.Fatalf("no se pudo extraer el diagnóstico de %v", details)
	}
	if d.Provider != "fabric" || d.Status != CodeNotFound || d.CacheState != string(CatalogFresh) {
		t.Fatalf("diagnóstico = %+v", d)
	}
	if _, ok := DiagnosticOf(errors.New("otro")); ok {
		t.Fatal("un error ajeno no lleva diagnóstico")
	}
}

// TestNormalizeErrorSinDiagnosticoNoRompeMantieneLaRutaHistorica comprueba que
// la firma antigua sigue funcionando (aditivo).
func TestNormalizeErrorSinDiagnosticoNoRompeMantieneLaRutaHistorica(t *testing.T) {
	cause := &TransportError{Code: CodeTimeout, Provider: "paper", Operation: "list"}
	err := NormalizeError("paper", "list", cause)
	if !errx.Is(err, errx.CodeUnavailable) {
		t.Fatalf("código = %v", err)
	}
	d, ok := DiagnosticOf(err)
	if !ok || d.Status != CodeTimeout {
		t.Fatalf("diagnóstico = %+v (%v)", d, ok)
	}
	if d.CacheState != "unknown" {
		t.Fatalf("cache_state sin dato = %q", d.CacheState)
	}
}
