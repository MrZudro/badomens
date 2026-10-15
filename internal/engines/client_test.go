package engines

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MrZudro/badomens/internal/version"
)

// testTransport envuelve un *Client determinista: timeouts cortos, backoff
// mínimo, jitter con semilla fija y "esperas" registradas en lugar de dormir.
type testTransport struct {
	*Client
	mu     sync.Mutex
	sleeps []time.Duration
}

func newTestTransport(t *testing.T) *testTransport {
	t.Helper()
	c := DefaultClient()
	c.Timeout = 5 * time.Second
	c.DialTimeout = 500 * time.Millisecond
	c.TLSHandshakeTimeout = time.Second
	c.ResponseHeaderTimeout = 300 * time.Millisecond
	c.BodyTimeout = time.Second
	c.BaseBackoff = 10 * time.Millisecond
	c.MaxBackoff = 50 * time.Millisecond
	c.HTTP = &http.Client{Transport: c.newTransport()}
	c.SeedRand(1)
	tt := &testTransport{Client: c}
	c.Sleep = func(_ context.Context, d time.Duration) error {
		tt.mu.Lock()
		defer tt.mu.Unlock()
		tt.sleeps = append(tt.sleeps, d)
		return nil
	}
	return tt
}

// rebuild vuelve a construir el transporte tras cambiar sus timeouts.
func (tt *testTransport) rebuild() {
	tt.HTTP = &http.Client{Transport: tt.newTransport()}
}

// waited devuelve las esperas registradas entre reintentos.
func (tt *testTransport) waited() []time.Duration {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return append([]time.Duration(nil), tt.sleeps...)
}

// asTransportError exige que el error sea del transporte y lo devuelve.
func asTransportError(t *testing.T, err error) *TransportError {
	t.Helper()
	var typed *TransportError
	if !errors.As(err, &typed) {
		t.Fatalf("quiero *TransportError, tengo %v", err)
	}
	return typed
}

func TestClientHTTP200ConValidadores(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	var gotUA, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		mu.Lock()
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2026 07:28:00 GMT")
		_, _ = w.Write([]byte(`{"ok":true,"n":2}`))
	}))
	defer srv.Close()

	c := newTestTransport(t)
	var out struct {
		OK bool `json:"ok"`
		N  int  `json:"n"`
	}
	meta, err := c.GetJSONWithMeta(context.Background(), srv.URL, &out,
		RequestOpts{Provider: "paper", Operation: "list_versions"})
	if err != nil {
		t.Fatalf("200: %v", err)
	}
	if !out.OK || out.N != 2 {
		t.Fatalf("cuerpo decodificado = %+v", out)
	}
	if meta.Status != http.StatusOK || meta.NotModified {
		t.Fatalf("meta = %+v", meta)
	}
	if meta.ETag != `"v1"` || meta.LastModified == "" {
		t.Fatalf("validadores = %+v", meta)
	}
	if meta.Attempts != 1 || calls.Load() != 1 {
		t.Fatalf("intentos = %d, llamadas = %d", meta.Attempts, calls.Load())
	}
	if waits := c.waited(); len(waits) != 0 {
		t.Fatalf("no se esperaba ninguna espera: %v", waits)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotUA != DefaultUserAgent() {
		t.Fatalf("User-Agent = %q", gotUA)
	}
	if !strings.Contains(gotUA, version.Version) || gotUA == "badomens/0" {
		t.Fatalf("el User-Agent debe llevar la versión real: %q", gotUA)
	}
	if !strings.Contains(gotAccept, "json") {
		t.Fatalf("Accept = %q", gotAccept)
	}
}

// TestAcceptHeaderUsaRangosValidos: los rangos de medios deben ser tipo/subtipo
// (RFC 9110 §12.5.1). Formas como `*/*+json` no son válidas y hacen que algunos
// servidores (Spring/Hangar) respondan 406.
func TestAcceptHeaderUsaRangosValidos(t *testing.T) {
	for _, kind := range []ContentType{ContentJSON, ContentXML} {
		header := acceptHeader(kind)
		if header == "" {
			t.Fatalf("Accept vacío para %v", kind)
		}
		for _, parte := range strings.Split(header, ",") {
			rango := strings.TrimSpace(strings.SplitN(parte, ";", 2)[0])
			tipo, sub, ok := strings.Cut(rango, "/")
			token := func(s string) bool {
				if s == "*" {
					return true
				}
				return s != "" && !strings.ContainsAny(s, "*+")
			}
			if !ok || !token(tipo) || !token(sub) {
				t.Errorf("rango de medios inválido %q en %q", rango, header)
			}
		}
	}
}

func TestClientHTTPCompatibilidadHistorica(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/manifest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestTransport(t)
	var decoded struct {
		OK bool `json:"ok"`
	}
	if err := c.GetJSON(context.Background(), srv.URL+"/manifest", &decoded); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if !decoded.OK {
		t.Fatal("GetJSON no decodificó el cuerpo")
	}
	raw, err := c.Get(context.Background(), srv.URL+"/manifest")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(string(raw), `"ok":true`) {
		t.Fatalf("Get = %q", raw)
	}
	if _, err := c.Get(context.Background(), srv.URL+"/no-existe"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("404: %v", err)
	}
}

func TestClientHTTP304SinCambios(t *testing.T) {
	var mu sync.Mutex
	var gotINM, gotIMS string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotINM = r.Header.Get("If-None-Match")
		gotIMS = r.Header.Get("If-Modified-Since")
		mu.Unlock()
		if r.Header.Get("If-None-Match") != "" {
			w.Header().Set("ETag", `"v1"`)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"x":1}`))
	}))
	defer srv.Close()

	opts := RequestOpts{
		Provider:        "fabric",
		Operation:       "list_versions",
		IfNoneMatch:     `"v1"`,
		IfModifiedSince: "Wed, 21 Oct 2026 07:28:00 GMT",
	}
	c := newTestTransport(t)
	body, meta, err := c.GetWithMeta(context.Background(), srv.URL, opts)
	if err != nil {
		t.Fatalf("un 304 no es un error: %v", err)
	}
	if !meta.NotModified || meta.Status != http.StatusNotModified {
		t.Fatalf("meta = %+v", meta)
	}
	if len(body) != 0 {
		t.Fatalf("cuerpo = %q", body)
	}
	if meta.ETag != `"v1"` || meta.Attempts != 1 {
		t.Fatalf("meta = %+v", meta)
	}
	mu.Lock()
	if gotINM != `"v1"` || gotIMS == "" {
		t.Fatalf("validadores enviados = %q / %q", gotINM, gotIMS)
	}
	mu.Unlock()

	// Con 304, GetJSONWithMeta no toca `out` ni devuelve error.
	type payload struct {
		X int `json:"x"`
	}
	out := payload{X: 7}
	jmeta, err := c.GetJSONWithMeta(context.Background(), srv.URL, &out, opts)
	if err != nil {
		t.Fatalf("GetJSONWithMeta 304: %v", err)
	}
	if !jmeta.NotModified || out.X != 7 {
		t.Fatalf("un 304 no debe tocar out: meta=%+v out=%+v", jmeta, out)
	}
}

func TestClientHTTPErroresPermanentes(t *testing.T) {
	cases := []struct {
		status   int
		code     string
		sentinel error
	}{
		{http.StatusBadRequest, CodeInvalidResponse, ErrInvalidResponse},
		{http.StatusUnauthorized, CodeInvalidResponse, ErrInvalidResponse},
		{http.StatusForbidden, CodeInvalidResponse, ErrInvalidResponse},
		{http.StatusNotFound, CodeNotFound, ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				http.Error(w, "no", tc.status)
			}))
			defer srv.Close()

			c := newTestTransport(t)
			_, meta, err := c.GetWithMeta(context.Background(), srv.URL,
				RequestOpts{Provider: "paper", Operation: "list_versions"})
			if err == nil {
				t.Fatal("se esperaba error")
			}
			if CodeOf(err) != tc.code || !errors.Is(err, tc.sentinel) {
				t.Fatalf("err = %v (código %q)", err, CodeOf(err))
			}
			typed := asTransportError(t, err)
			if typed.Retryable || typed.HTTPStatus != tc.status || typed.Attempts != 1 {
				t.Fatalf("typed = %+v", typed)
			}
			if typed.Provider != "paper" || typed.Operation != "list_versions" {
				t.Fatalf("trazabilidad = %+v", typed)
			}
			if calls.Load() != 1 || meta.Attempts != 1 {
				t.Fatalf("no debe reintentar: llamadas=%d intentos=%d", calls.Load(), meta.Attempts)
			}
			if waits := c.waited(); len(waits) != 0 {
				t.Fatalf("esperas = %v", waits)
			}
		})
	}
}

func TestClientNoReintentaAunqueElTextoParezcaTransitorio(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "timeout: rate limited, reintenta con backoff", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := newTestTransport(t)
	_, _, err := c.GetWithMeta(context.Background(), srv.URL, RequestOpts{})
	typed := asTransportError(t, err)
	if typed.Code != CodeInvalidResponse || typed.Retryable {
		t.Fatalf("la política no puede depender del texto: %+v", typed)
	}
	if calls.Load() != 1 {
		t.Fatalf("llamadas = %d; un 400 no se reintenta", calls.Load())
	}
}

func TestClientRetry429ConRetryAfterSegundos(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "2")
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newTestTransport(t)
	_, meta, err := c.GetWithMeta(context.Background(), srv.URL, RequestOpts{})
	if err == nil {
		t.Fatal("se esperaba error")
	}
	typed := asTransportError(t, err)
	if typed.Code != CodeRateLimited || !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	if !typed.Retryable || typed.HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("typed = %+v", typed)
	}
	if typed.RetryAfter != 2*time.Second {
		t.Fatalf("Retry-After = %v", typed.RetryAfter)
	}
	if typed.Attempts != DefaultMaxAttempts || meta.Attempts != DefaultMaxAttempts {
		t.Fatalf("intentos = %d / %d", typed.Attempts, meta.Attempts)
	}
	if calls.Load() != DefaultMaxAttempts {
		t.Fatalf("llamadas = %d", calls.Load())
	}
	waits := c.waited()
	if len(waits) != DefaultMaxAttempts-1 {
		t.Fatalf("esperas = %v", waits)
	}
	for _, w := range waits {
		if w != 2*time.Second {
			t.Fatalf("debe respetar Retry-After: %v", w)
		}
	}
}

func TestClientRetry429SinRetryAfter(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newTestTransport(t)
	_, _, err := c.GetWithMeta(context.Background(), srv.URL, RequestOpts{})
	typed := asTransportError(t, err)
	if typed.Code != CodeRateLimited || typed.RetryAfter != 0 || !typed.Retryable {
		t.Fatalf("typed = %+v", typed)
	}
	if calls.Load() != DefaultMaxAttempts {
		t.Fatalf("llamadas = %d", calls.Load())
	}
	waits := c.waited()
	if len(waits) != DefaultMaxAttempts-1 {
		t.Fatalf("esperas = %v", waits)
	}
	for _, w := range waits {
		if w <= 0 || w > c.MaxBackoff {
			t.Fatalf("backoff fuera de rango: %v", w)
		}
	}
}

func TestClientRetryAfterFechaHTTP(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", time.Now().Add(3*time.Second).UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newTestTransport(t)
	c.MaxAttempts = 2
	_, _, err := c.GetWithMeta(context.Background(), srv.URL, RequestOpts{})
	typed := asTransportError(t, err)
	if typed.Code != CodeUpstreamUnavailable {
		t.Fatalf("err = %v", err)
	}
	if typed.RetryAfter < time.Second || typed.RetryAfter > 3*time.Second {
		t.Fatalf("Retry-After = %v", typed.RetryAfter)
	}
	if calls.Load() != 2 {
		t.Fatalf("llamadas = %d", calls.Load())
	}
	waits := c.waited()
	if len(waits) != 1 {
		t.Fatalf("esperas = %v", waits)
	}
	if waits[0] < time.Second || waits[0] > 3*time.Second {
		t.Fatalf("espera = %v", waits[0])
	}
}

func TestClientRetry5xx(t *testing.T) {
	for _, status := range []int{
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
			}))
			defer srv.Close()

			c := newTestTransport(t)
			_, meta, err := c.GetWithMeta(context.Background(), srv.URL, RequestOpts{})
			typed := asTransportError(t, err)
			if typed.Code != CodeUpstreamUnavailable || !errors.Is(err, ErrUpstreamUnavailable) {
				t.Fatalf("err = %v", err)
			}
			if !typed.Retryable || typed.HTTPStatus != status {
				t.Fatalf("typed = %+v", typed)
			}
			if typed.Attempts != DefaultMaxAttempts || meta.Attempts != DefaultMaxAttempts || calls.Load() != DefaultMaxAttempts {
				t.Fatalf("intentos = %d, meta = %d, llamadas = %d", typed.Attempts, meta.Attempts, calls.Load())
			}
			if waits := c.waited(); len(waits) != DefaultMaxAttempts-1 {
				t.Fatalf("esperas = %v", waits)
			}
		})
	}
}

func TestClientRetrySeRecuperaTras5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestTransport(t)
	var out struct {
		OK bool `json:"ok"`
	}
	meta, err := c.GetJSONWithMeta(context.Background(), srv.URL, &out, RequestOpts{})
	if err != nil {
		t.Fatalf("debería recuperarse: %v", err)
	}
	if !out.OK || meta.Attempts != 3 || calls.Load() != 3 {
		t.Fatalf("out=%+v meta=%+v llamadas=%d", out, meta, calls.Load())
	}
	if waits := c.waited(); len(waits) != 2 {
		t.Fatalf("esperas = %v", waits)
	}
}

func TestClientRetryJitterDeterminista(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	first := newTestTransport(t)
	if _, _, err := first.GetWithMeta(context.Background(), srv.URL, RequestOpts{}); err == nil {
		t.Fatal("se esperaba error")
	}
	second := newTestTransport(t)
	if _, _, err := second.GetWithMeta(context.Background(), srv.URL, RequestOpts{}); err == nil {
		t.Fatal("se esperaba error")
	}
	a, b := first.waited(), second.waited()
	if len(a) != 2 || len(b) != 2 {
		t.Fatalf("esperas = %v / %v", a, b)
	}
	if a[0] != b[0] || a[1] != b[1] {
		t.Fatalf("misma semilla debe dar las mismas esperas: %v / %v", a, b)
	}
	if a[0] >= a[1] {
		t.Fatalf("el backoff debe crecer: %v", a)
	}
	for _, w := range a {
		if w <= 0 || w > first.MaxBackoff {
			t.Fatalf("espera fuera de rango: %v", w)
		}
	}
}

func TestClientMaxAttemptsConfigurable(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	uno := newTestTransport(t)
	uno.MaxAttempts = 1
	_, meta, err := uno.GetWithMeta(context.Background(), srv.URL, RequestOpts{})
	typed := asTransportError(t, err)
	if typed.Attempts != 1 || meta.Attempts != 1 || calls.Load() != 1 {
		t.Fatalf("intentos = %d / %d / llamadas = %d", typed.Attempts, meta.Attempts, calls.Load())
	}
	if waits := uno.waited(); len(waits) != 0 {
		t.Fatalf("esperas = %v", waits)
	}

	antes := calls.Load()
	cinco := newTestTransport(t)
	cinco.MaxAttempts = 5
	_, _, err = cinco.GetWithMeta(context.Background(), srv.URL, RequestOpts{})
	typed = asTransportError(t, err)
	if typed.Attempts != 5 || calls.Load()-antes != 5 {
		t.Fatalf("intentos = %d, llamadas nuevas = %d", typed.Attempts, calls.Load()-antes)
	}
	if waits := cinco.waited(); len(waits) != 4 {
		t.Fatalf("esperas = %v", waits)
	}
}

func TestClientTransportTimeoutDeServidor(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// Nunca responde: el cliente debe abortar por ResponseHeaderTimeout.
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer srv.Close()

	c := newTestTransport(t)
	c.ResponseHeaderTimeout = 40 * time.Millisecond
	c.rebuild()

	_, meta, err := c.GetWithMeta(context.Background(), srv.URL, RequestOpts{Provider: "vanilla"})
	if err == nil {
		t.Fatal("se esperaba timeout")
	}
	typed := asTransportError(t, err)
	if typed.Code != CodeTimeout || !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
	if !typed.Retryable {
		t.Fatal("un timeout es transitorio")
	}
	if typed.Attempts != DefaultMaxAttempts || meta.Attempts != DefaultMaxAttempts || calls.Load() != DefaultMaxAttempts {
		t.Fatalf("intentos = %d, meta = %d, llamadas = %d", typed.Attempts, meta.Attempts, calls.Load())
	}
	if waits := c.waited(); len(waits) != DefaultMaxAttempts-1 {
		t.Fatalf("esperas = %v", waits)
	}
}

func TestClientTransportContextoCanceladoPorElCliente(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-started:
		default:
			close(started)
		}
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()

	c := newTestTransport(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-started
		cancel()
	}()

	_, meta, err := c.GetWithMeta(ctx, srv.URL, RequestOpts{})
	if err == nil {
		t.Fatal("se esperaba error")
	}
	if !errors.Is(err, ErrCanceled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	typed := asTransportError(t, err)
	if typed.Code != CodeCanceled || typed.Retryable {
		t.Fatalf("una cancelación del llamante no se reintenta: %+v", typed)
	}
	if typed.Attempts != 1 || meta.Attempts != 1 || calls.Load() != 1 {
		t.Fatalf("intentos = %d, meta = %d, llamadas = %d", typed.Attempts, meta.Attempts, calls.Load())
	}
	if waits := c.waited(); len(waits) != 0 {
		t.Fatalf("esperas = %v", waits)
	}
}

func TestClientTransportLimiteTotalDelContexto(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()

	c := newTestTransport(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()

	_, meta, err := c.GetWithMeta(ctx, srv.URL, RequestOpts{})
	if err == nil {
		t.Fatal("se esperaba timeout")
	}
	typed := asTransportError(t, err)
	if typed.Code != CodeTimeout || typed.Retryable {
		t.Fatalf("agotado el límite total, no se reintenta: %+v", typed)
	}
	if typed.Attempts != 1 || meta.Attempts != 1 || calls.Load() != 1 {
		t.Fatalf("intentos = %d, meta = %d, llamadas = %d", typed.Attempts, meta.Attempts, calls.Load())
	}
	if waits := c.waited(); len(waits) != 0 {
		t.Fatalf("esperas = %v", waits)
	}
}

func TestClientTransportLimiteDeTamano(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":"` + strings.Repeat("a", 4096) + `"}`))
	}))
	defer srv.Close()

	c := newTestTransport(t)
	_, meta, err := c.GetWithMeta(context.Background(), srv.URL, RequestOpts{MaxBytes: 1024})
	if err == nil {
		t.Fatal("se esperaba error de tamaño")
	}
	if !errors.Is(err, ErrResponseTooLarge) || CodeOf(err) != CodeResponseTooLarge {
		t.Fatalf("err = %v", err)
	}
	typed := asTransportError(t, err)
	if typed.Retryable || typed.Attempts != 1 || meta.Attempts != 1 || calls.Load() != 1 {
		t.Fatalf("typed=%+v meta=%+v llamadas=%d", typed, meta, calls.Load())
	}
	if waits := c.waited(); len(waits) != 0 {
		t.Fatalf("esperas = %v", waits)
	}

	// El límite también se puede fijar en el cliente.
	otro := newTestTransport(t)
	otro.MaxBytes = 512
	var out map[string]any
	if _, err := otro.GetJSONWithMeta(context.Background(), srv.URL, &out, RequestOpts{}); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("límite del cliente: %v", err)
	}
}

func TestClientTransportJSONTruncado(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"versions":["1.21"`))
	}))
	defer srv.Close()

	c := newTestTransport(t)
	var out map[string]any
	_, err := c.GetJSONWithMeta(context.Background(), srv.URL, &out, RequestOpts{})
	if !errors.Is(err, ErrSchemaMismatch) || CodeOf(err) != CodeSchemaMismatch {
		t.Fatalf("err = %v", err)
	}
	typed := asTransportError(t, err)
	if typed.Retryable || typed.Attempts != 1 || typed.HTTPStatus != http.StatusOK {
		t.Fatalf("typed = %+v", typed)
	}
}

func TestClientTransportJSONInvalido(t *testing.T) {
	cases := map[string]string{
		"sintaxis": `{"versions": [}`,
		"vacio":    ``,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()

			c := newTestTransport(t)
			var out map[string]any
			_, err := c.GetJSONWithMeta(context.Background(), srv.URL, &out, RequestOpts{})
			if !errors.Is(err, ErrInvalidResponse) || CodeOf(err) != CodeInvalidResponse {
				t.Fatalf("err = %v", err)
			}
			if typed := asTransportError(t, err); typed.Retryable {
				t.Fatalf("un documento ilegible no se reintenta: %+v", typed)
			}
		})
	}
}

func TestClientTransportContentTypeIncorrecto(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestTransport(t)
	var out map[string]any
	_, err := c.GetJSONWithMeta(context.Background(), srv.URL, &out, RequestOpts{})
	if !errors.Is(err, ErrInvalidResponse) || CodeOf(err) != CodeInvalidResponse {
		t.Fatalf("err = %v", err)
	}
	typed := asTransportError(t, err)
	if typed.Retryable || typed.Attempts != 1 || calls.Load() != 1 {
		t.Fatalf("typed=%+v llamadas=%d", typed, calls.Load())
	}
}

func TestClientTransportContentTypeTextoPlanoAceptado(t *testing.T) {
	// httptest sirve el JSON sniffado como text/plain: la relajación documentada
	// lo acepta y el decodificador sigue siendo la última palabra.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestTransport(t)
	var out struct {
		OK bool `json:"ok"`
	}
	if _, err := c.GetJSONWithMeta(context.Background(), srv.URL, &out, RequestOpts{}); err != nil {
		t.Fatalf("text/plain debería aceptarse: %v", err)
	}
	if !out.OK {
		t.Fatal("no se decodificó el cuerpo")
	}
}

func TestClientHTTPXML(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/malo" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<metadata><version>21.1.0</version></metadata>`))
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(`<metadata><version>21.1.0</version></metadata>`))
	}))
	defer srv.Close()

	c := newTestTransport(t)
	var out struct {
		Version string `xml:"version"`
	}
	if err := c.GetXML(context.Background(), srv.URL+"/ok", &out); err != nil {
		t.Fatalf("XML: %v", err)
	}
	if out.Version != "21.1.0" {
		t.Fatalf("out = %+v", out)
	}

	var malo struct {
		Version string `xml:"version"`
	}
	if _, err := c.GetXMLWithMeta(context.Background(), srv.URL+"/malo", &malo, RequestOpts{}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("content-type incorrecto para XML: %v", err)
	}
}

// flakyTransport falla los primeros intentos con un error de conexión y luego
// delega en el transporte real: simula una red intermitente sin red real.
type flakyTransport struct {
	inner http.RoundTripper
	fails atomic.Int32
}

func (f *flakyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if f.fails.Add(-1) >= 0 {
		return nil, &netOpError{}
	}
	return f.inner.RoundTrip(req)
}

// netOpError simula un fallo de conexión no temporal (sin timeout).
type netOpError struct{}

func (*netOpError) Error() string   { return "dial tcp: connection refused" }
func (*netOpError) Timeout() bool   { return false }
func (*netOpError) Temporary() bool { return false }

func TestClientTransportReintentaFalloDeConexion(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestTransport(t)
	flaky := &flakyTransport{inner: http.DefaultTransport}
	flaky.fails.Store(2)
	c.HTTP = &http.Client{Transport: flaky}

	var out struct {
		OK bool `json:"ok"`
	}
	meta, err := c.GetJSONWithMeta(context.Background(), srv.URL, &out, RequestOpts{})
	if err != nil {
		t.Fatalf("debería recuperarse del fallo de conexión: %v", err)
	}
	if !out.OK || meta.Attempts != 3 || calls.Load() != 1 {
		t.Fatalf("out=%+v meta=%+v llamadas=%d", out, meta, calls.Load())
	}
	if waits := c.waited(); len(waits) != 2 {
		t.Fatalf("esperas = %v", waits)
	}
}

func TestClientTransportDiagnosticoSinCredenciales(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	host := strings.TrimPrefix(srv.URL, "http://")
	srv.Close() // puerto cerrado: el fallo de conexión es inmediato

	raw := "http://usuario:clave@" + host + "/catalogo?token=secreto&v=1"
	c := newTestTransport(t)
	var mu sync.Mutex
	var records []LogRecord
	c.Log = func(rec LogRecord) {
		mu.Lock()
		defer mu.Unlock()
		records = append(records, rec)
	}

	_, _, err := c.GetWithMeta(context.Background(), raw, RequestOpts{Provider: "paper", Operation: "list_versions"})
	if err == nil {
		t.Fatal("se esperaba un fallo de conexión")
	}
	typed := asTransportError(t, err)
	if typed.Code != CodeUpstreamUnavailable || typed.Provider != "paper" || typed.Operation != "list_versions" {
		t.Fatalf("typed = %+v", typed)
	}
	if typed.URL == "" || strings.Contains(typed.URL, "clave") || strings.Contains(typed.URL, "secreto") {
		t.Fatalf("URL sin sanear en el error: %q", typed.URL)
	}
	if !strings.Contains(typed.URL, "***") {
		t.Fatalf("el parámetro sensible debe ocultarse: %q", typed.URL)
	}
	if msg := err.Error(); strings.Contains(msg, "clave") || strings.Contains(msg, "secreto") {
		t.Fatalf("el mensaje filtra credenciales: %s", msg)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(records) == 0 {
		t.Fatal("quiero registros de diagnóstico")
	}
	for _, rec := range records {
		if rec.Provider != "paper" || rec.Operation != "list_versions" || rec.URL == "" {
			t.Fatalf("registro incompleto: %+v", rec)
		}
		if strings.Contains(rec.URL, "clave") || strings.Contains(rec.URL, "secreto") {
			t.Fatalf("registro con credenciales: %+v", rec)
		}
	}
	last := records[len(records)-1]
	if last.Code != CodeUpstreamUnavailable || last.Attempt != DefaultMaxAttempts {
		t.Fatalf("último registro = %+v", last)
	}
}

func TestClientSanitizeURLYRetryAfter(t *testing.T) {
	got := sanitizeURL("https://usuario:clave@ejemplo.test/a/b?token=x&v=2")
	if strings.Contains(got, "clave") || strings.Contains(got, "usuario") {
		t.Fatalf("credenciales sin sanear: %q", got)
	}
	if !strings.Contains(got, "***") || !strings.Contains(got, "v=2") {
		t.Fatalf("saneado = %q", got)
	}

	now := time.Date(2026, 10, 21, 7, 0, 0, 0, time.UTC)
	if d, ok := parseRetryAfter("5", now); !ok || d != 5*time.Second {
		t.Fatalf("Retry-After en segundos = %v (%v)", d, ok)
	}
	when := now.Add(4 * time.Second).Format(http.TimeFormat)
	if d, ok := parseRetryAfter(when, now); !ok || d != 4*time.Second {
		t.Fatalf("Retry-After con fecha = %v (%v)", d, ok)
	}
	if d, ok := parseRetryAfter("", now); ok || d != 0 {
		t.Fatalf("Retry-After vacío = %v (%v)", d, ok)
	}
	if d, ok := parseRetryAfter(when, now.Add(10*time.Second)); !ok || d != 0 {
		t.Fatalf("Retry-After en el pasado = %v (%v)", d, ok)
	}
	if _, ok := parseRetryAfter("no-es-una-fecha", now); ok {
		t.Fatal("Retry-After inválido debe ignorarse")
	}
}
