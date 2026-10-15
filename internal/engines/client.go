package engines

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MrZudro/badomens/internal/version"
)

// Códigos estables del transporte HTTP de catálogos. Son la clave que consumen
// los demás componentes (CLI, TUI, MCP) para decidir su política: la decisión
// NUNCA se toma mirando el texto de un error ni el cuerpo de la respuesta.
const (
	// CodeTimeout: se agotó el tiempo (conexión, cabeceras, cuerpo o límite total).
	CodeTimeout = "timeout"
	// CodeRateLimited: el proveedor respondió 429 (límite de peticiones).
	CodeRateLimited = "rate_limited"
	// CodeUpstreamUnavailable: fallo de red o respuesta 5xx del proveedor.
	CodeUpstreamUnavailable = "upstream_unavailable"
	// CodeInvalidResponse: respuesta no utilizable (estado inesperado, formato
	// distinto del esperado, documento malformado o cancelación del llamante).
	CodeInvalidResponse = "invalid_response"
	// CodeSchemaMismatch: la respuesta es sintácticamente correcta pero no encaja
	// con el esquema esperado (documento truncado, tipos distintos…).
	CodeSchemaMismatch = "schema_mismatch"
	// CodeNotFound: el recurso no existe (404).
	CodeNotFound = "not_found"
	// CodeResponseTooLarge: el cuerpo supera el límite de lectura configurado.
	CodeResponseTooLarge = "response_too_large"
	// CodeCanceled: el llamante canceló el contexto. No se reintenta.
	CodeCanceled = "canceled"
)

// Errores centinela del transporte: permiten clasificar sin mirar el texto
// (errors.Is(err, ErrRateLimited)). El código estable es la fuente de verdad.
var (
	ErrTimeout             = errors.New(CodeTimeout)
	ErrRateLimited         = errors.New(CodeRateLimited)
	ErrUpstreamUnavailable = errors.New(CodeUpstreamUnavailable)
	ErrInvalidResponse     = errors.New(CodeInvalidResponse)
	ErrSchemaMismatch      = errors.New(CodeSchemaMismatch)
	ErrNotFound            = errors.New(CodeNotFound)
	ErrResponseTooLarge    = errors.New(CodeResponseTooLarge)
	ErrCanceled            = errors.New(CodeCanceled)
)

// sentinelCodes relaciona cada centinela con su código estable.
var sentinelCodes = map[error]string{
	ErrTimeout:             CodeTimeout,
	ErrRateLimited:         CodeRateLimited,
	ErrUpstreamUnavailable: CodeUpstreamUnavailable,
	ErrInvalidResponse:     CodeInvalidResponse,
	ErrSchemaMismatch:      CodeSchemaMismatch,
	ErrNotFound:            CodeNotFound,
	ErrResponseTooLarge:    CodeResponseTooLarge,
	ErrCanceled:            CodeCanceled,
}

// errBodyTooLarge marca que el cuerpo superó el límite durante la lectura.
var errBodyTooLarge = errors.New("el cuerpo supera el límite de lectura")

// Valores por defecto del transporte. Un Client construido a mano (literal de
// struct con los campos a cero) también los recibe: los campos a cero se
// resuelven a estos valores en el momento de usarlos.
const (
	// DefaultTimeout es el límite total por petición, esperas incluidas.
	DefaultTimeout = 15 * time.Second
	// DefaultDialTimeout acota el establecimiento de la conexión TCP.
	DefaultDialTimeout = 5 * time.Second
	// DefaultTLSHandshakeTimeout acota el handshake TLS.
	DefaultTLSHandshakeTimeout = 5 * time.Second
	// DefaultResponseHeaderTimeout acota la espera de las cabeceras de respuesta.
	DefaultResponseHeaderTimeout = 10 * time.Second
	// DefaultBodyTimeout acota la lectura completa del cuerpo.
	DefaultBodyTimeout = 10 * time.Second
	// DefaultMaxAttempts es el número de intentos, incluido el primero.
	DefaultMaxAttempts = 3
	// DefaultBaseBackoff es el retraso del primer reintento (se duplica).
	DefaultBaseBackoff = 200 * time.Millisecond
	// DefaultMaxBackoff es la cota del backoff exponencial.
	DefaultMaxBackoff = 5 * time.Second
	// DefaultMaxWait acota la espera de un único reintento (Retry-After incluido).
	DefaultMaxWait = 30 * time.Second
	// DefaultMaxJSONBytes es el límite de lectura de respuestas JSON.
	DefaultMaxJSONBytes int64 = 8 << 20
	// DefaultMaxXMLBytes es el límite de lectura de respuestas XML.
	DefaultMaxXMLBytes int64 = 16 << 20
	// UserAgentName es el nombre del cliente en la cabecera User-Agent.
	UserAgentName = "badomens"
	// UserAgentContact es la URL de contacto publicada en el User-Agent.
	UserAgentContact = "https://github.com/MrZudro/badomens"
)

// DefaultUserAgent compone el User-Agent con el nombre y la versión REALES del
// binario. Es el único sitio del proyecto que compone esta cabecera: una sola
// fuente, tomada de internal/version.
func DefaultUserAgent() string {
	return UserAgentName + "/" + version.Version + " (+" + UserAgentContact + ")"
}

// TransportError es el error tipado del transporte. Lleva un código estable
// (comparable con errors.Is y los centinelas Err*) y los metadatos necesarios
// para diagnosticar sin exponer cuerpos de respuesta ni credenciales.
type TransportError struct {
	// Code es el código estable (CodeTimeout, CodeRateLimited…).
	Code string
	// Provider identifica el proveedor (paper, fabric…) cuando se conoce.
	Provider string
	// Operation identifica la operación lógica (list_versions, resolve_build…).
	Operation string
	// URL es la URL de origen sin credenciales ni parámetros sensibles.
	URL string
	// HTTPStatus es el estado HTTP; 0 si no hubo respuesta.
	HTTPStatus int
	// Retryable indica si el fallo es transitorio y se puede reintentar.
	Retryable bool
	// Attempts es el número de intentos realizados.
	Attempts int
	// RetryAfter es la espera pedida por el servidor (cabecera Retry-After).
	RetryAfter time.Duration
	// Err es la causa subyacente (error de red, de decodificación…). Nunca un
	// cuerpo de respuesta.
	Err error
}

// Error describe el fallo sin exponer cuerpos ni credenciales.
func (e *TransportError) Error() string {
	var b strings.Builder
	b.WriteString("consulta")
	if e.Provider != "" {
		fmt.Fprintf(&b, " de %s", e.Provider)
	}
	if e.Operation != "" {
		fmt.Fprintf(&b, " %s", e.Operation)
	}
	if e.URL != "" {
		fmt.Fprintf(&b, " a %s", e.URL)
	}
	b.WriteString(": ")
	b.WriteString(e.Code)
	if e.HTTPStatus != 0 {
		fmt.Fprintf(&b, " (HTTP %d)", e.HTTPStatus)
	}
	if e.Attempts > 0 {
		fmt.Fprintf(&b, " tras %d intento(s)", e.Attempts)
	}
	if e.Err != nil {
		fmt.Fprintf(&b, ": %v", e.Err)
	}
	return b.String()
}

// Unwrap permite llegar a la causa subyacente con errors.Is/errors.As.
func (e *TransportError) Unwrap() error { return e.Err }

// Is hace que errors.Is(err, ErrTimeout) funcione para todos los centinelas,
// sin depender del texto del mensaje.
func (e *TransportError) Is(target error) bool {
	if typed, ok := target.(*TransportError); ok {
		return typed.Code == e.Code
	}
	code, ok := sentinelCodes[target]
	return ok && code == e.Code
}

// CodeOf devuelve el código estable de un error del transporte ("" si el error
// no procede del transporte).
func CodeOf(err error) string {
	var typed *TransportError
	if errors.As(err, &typed) {
		return typed.Code
	}
	return ""
}

// IsRetryable informa si un error del transporte es transitorio.
func IsRetryable(err error) bool {
	var typed *TransportError
	if errors.As(err, &typed) {
		return typed.Retryable
	}
	return false
}

// Body es el cuerpo binario de una respuesta de catálogo.
type Body = []byte

// Meta describe la respuesta HTTP más allá del cuerpo: estado, validadores de
// caché y origen. Es la entrada de la caché condicional (ETag/Last-Modified).
type Meta struct {
	// URL es la URL consultada sin credenciales ni parámetros sensibles.
	URL string
	// Status es el código de estado HTTP.
	Status int
	// ETag y LastModified son los validadores devueltos por el servidor.
	ETag         string
	LastModified string
	// ContentType es la cabecera Content-Type tal cual llegó.
	ContentType string
	// NotModified indica que el servidor respondió 304 (sin cambios).
	NotModified bool
	// Attempts es el número de intentos consumidos.
	Attempts int
	// FetchedAt es el momento en que se obtuvo la respuesta.
	FetchedAt time.Time
}

// ContentType clasifica el formato esperado de una respuesta de catálogo.
type ContentType int

// Formatos de respuesta admitidos.
const (
	// ContentAny no valida la cabecera Content-Type.
	ContentAny ContentType = iota
	// ContentJSON espera JSON (application/json, */*+json, text/plain).
	ContentJSON
	// ContentXML espera XML (application/xml, text/xml, */*+xml, text/plain).
	ContentXML
)

// String devuelve el identificador legible del formato.
func (k ContentType) String() string {
	switch k {
	case ContentJSON:
		return "json"
	case ContentXML:
		return "xml"
	default:
		return "any"
	}
}

// RequestOpts ajusta una petición concreta: trazabilidad y validadores de caché.
type RequestOpts struct {
	// Provider identifica el proveedor para los diagnósticos (paper, fabric…).
	Provider string
	// Operation identifica la operación lógica (list_versions, resolve_build…).
	Operation string
	// Expected valida el formato de la respuesta. En GetJSON/GetXML se rellena
	// solo si el llamante lo deja en ContentAny.
	Expected ContentType
	// IfNoneMatch envía If-None-Match (ETag guardado).
	IfNoneMatch string
	// IfModifiedSince envía If-Modified-Since (Last-Modified guardado).
	IfModifiedSince string
	// MaxBytes sobreescribe el límite de lectura del cuerpo (0 = el del cliente).
	MaxBytes int64
}

// LogRecord es una entrada de diagnóstico del transporte. Nunca contiene
// cuerpos de respuesta, tokens ni URLs con credenciales.
type LogRecord struct {
	// Provider y Operation identifican el origen lógico de la consulta.
	Provider  string
	Operation string
	// URL es la URL de origen saneada.
	URL string
	// HTTPStatus es el estado; 0 si no hubo respuesta.
	HTTPStatus int
	// Attempt es el número de intento (1 = primero).
	Attempt int
	// Code es el código tipado del fallo; vacío si el intento fue correcto.
	Code string
	// Retryable indica si el fallo admitía reintento.
	Retryable bool
	// RetryIn es la espera antes del siguiente intento; 0 si no se reintenta.
	RetryIn time.Duration
	// NotModified indica que la respuesta fue 304.
	NotModified bool
}

// Logger recibe los diagnósticos del transporte. Puede ser nil.
type Logger func(LogRecord)

// Client es el transporte HTTP común de los catálogos de motores:
// timeouts diferenciados (conexión, TLS, cabeceras y cuerpo) con un límite
// total por context.Context, reintentos acotados con backoff exponencial y
// jitter, respeto de Retry-After, límite de tamaño aplicado durante la lectura,
// validación de estado/formato/esquema y errores tipados con códigos estables.
//
// Es seguro para uso concurrente. No se debe copiar después de usarlo: se usa
// siempre como *Client.
type Client struct {
	// HTTP es el cliente subyacente. Si es nil, o su Transport es nil, se usa un
	// transporte propio con timeouts diferenciados.
	HTTP *http.Client
	// Timeout es el límite TOTAL por petición (reintentos y esperas incluidos).
	// Se aplica con context.WithTimeout envolviendo el contexto del llamante.
	Timeout time.Duration
	// UserAgent identifica las peticiones. DefaultClient usa DefaultUserAgent.
	UserAgent string

	// DialTimeout acota el establecimiento de la conexión TCP.
	DialTimeout time.Duration
	// TLSHandshakeTimeout acota el handshake TLS.
	TLSHandshakeTimeout time.Duration
	// ResponseHeaderTimeout acota la espera de las cabeceras de respuesta.
	ResponseHeaderTimeout time.Duration
	// BodyTimeout acota la lectura completa del cuerpo tras las cabeceras.
	BodyTimeout time.Duration

	// MaxAttempts es el número máximo de intentos, incluido el primero.
	MaxAttempts int
	// MaxBytes es el límite de lectura del cuerpo; 0 usa el valor por defecto
	// según el formato (JSON u XML).
	MaxBytes int64
	// BaseBackoff es el retraso del primer reintento; se duplica en cada uno.
	BaseBackoff time.Duration
	// MaxBackoff es la cota del backoff exponencial.
	MaxBackoff time.Duration
	// MaxWait acota la espera de un reintento, Retry-After incluido (por defecto
	// DefaultMaxWait). Un valor negativo la deja sin tope.
	MaxWait time.Duration

	// Log recibe un registro de diagnóstico por intento. Puede ser nil.
	Log Logger

	// Sleep espera entre reintentos. Es inyectable para pruebas deterministas;
	// si es nil se usa un temporizador que respeta el contexto.
	Sleep func(ctx context.Context, d time.Duration) error
	// Now es el reloj, usado para interpretar Retry-After con fecha HTTP.
	Now func() time.Time

	mu  sync.Mutex
	hc  *http.Client
	rnd *rand.Rand
}

// DefaultClient devuelve el transporte por defecto de los catálogos: 15 s de
// límite total, 3 intentos, límites de 8 MiB (JSON) y 16 MiB (XML) y
// User-Agent con la versión real del binario.
func DefaultClient() *Client {
	c := &Client{
		Timeout:   DefaultTimeout,
		UserAgent: DefaultUserAgent(),
	}
	c.HTTP = &http.Client{Transport: c.newTransport()}
	return c
}

// SeedRand fija la semilla del jitter de reintentos para pruebas deterministas.
func (c *Client) SeedRand(seed int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rnd = rand.New(rand.NewSource(seed)) // #nosec G404 -- jitter de reintentos, no criptográfico
}

// Get lee el cuerpo de una URL. Mantiene la firma histórica del cliente; para
// obtener validadores de caché usa GetWithMeta.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	body, _, err := c.GetWithMeta(ctx, url, RequestOpts{})
	return body, err
}

// GetWithMeta lee el cuerpo y devuelve además los metadatos de la respuesta
// (estado, ETag, Last-Modified…). Un 304 NO es un error: se devuelve
// Meta.NotModified=true con cuerpo vacío. Si se envían IfNoneMatch o
// IfModifiedSince, la respuesta 304 significa "sin cambios".
func (c *Client) GetWithMeta(ctx context.Context, url string, opts RequestOpts) (Body, Meta, error) {
	return c.fetch(ctx, url, opts)
}

// GetJSON decodifica JSON validando estado, formato y esquema. Mantiene la
// firma histórica del cliente.
func (c *Client) GetJSON(ctx context.Context, url string, out any) error {
	_, err := c.GetJSONWithMeta(ctx, url, out, RequestOpts{})
	return err
}

// GetJSONWithMeta decodifica JSON y expone los metadatos de la respuesta. Con
// 304 devuelve NotModified=true sin tocar `out` y sin error.
func (c *Client) GetJSONWithMeta(ctx context.Context, url string, out any, opts RequestOpts) (Meta, error) {
	if opts.Expected == ContentAny {
		opts.Expected = ContentJSON
	}
	return c.decodeWithMeta(ctx, url, out, opts, decodeJSON)
}

// GetXML decodifica XML validando estado, formato y esquema.
func (c *Client) GetXML(ctx context.Context, url string, out any) error {
	_, err := c.GetXMLWithMeta(ctx, url, out, RequestOpts{})
	return err
}

// GetXMLWithMeta decodifica XML y expone los metadatos de la respuesta. Con 304
// devuelve NotModified=true sin tocar `out` y sin error.
func (c *Client) GetXMLWithMeta(ctx context.Context, url string, out any, opts RequestOpts) (Meta, error) {
	if opts.Expected == ContentAny {
		opts.Expected = ContentXML
	}
	return c.decodeWithMeta(ctx, url, out, opts, decodeXML)
}

// decoder decodifica un cuerpo crudo sobre `out`.
type decoder func(raw []byte, out any) error

// decodeWithMeta aplica el decodificador del formato tras validar la respuesta.
func (c *Client) decodeWithMeta(ctx context.Context, url string, out any, opts RequestOpts, decode decoder) (Meta, error) {
	body, meta, err := c.fetch(ctx, url, opts)
	if err != nil {
		return meta, err
	}
	if meta.NotModified || out == nil {
		return meta, nil
	}
	if err := decode(body, out); err != nil {
		return meta, c.newError(decodeCode(err), opts, meta.URL, meta.Status, false, meta.Attempts, err)
	}
	return meta, nil
}

// fetch ejecuta la petición con, como máximo, MaxAttempts intentos.
func (c *Client) fetch(ctx context.Context, rawURL string, opts RequestOpts) (Body, Meta, error) {
	safe := sanitizeURL(rawURL)
	runCtx := ctx
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	limit := opts.MaxBytes
	if limit <= 0 {
		limit = c.limitFor(opts.Expected)
	}
	attempts := c.maxAttempts()
	var lastMeta Meta
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		body, meta, err := c.attempt(runCtx, rawURL, safe, opts, limit, attempt)
		c.logAttempt(opts, meta, err, 0)
		if err == nil {
			return body, meta, nil
		}
		lastMeta, lastErr = meta, err
		var typed *TransportError
		if !errors.As(err, &typed) || !typed.Retryable || attempt >= attempts {
			break
		}
		if runCtx.Err() != nil {
			// Se agotó el límite total o el llamante canceló: no hay tiempo útil.
			break
		}
		wait := c.backoff(attempt, typed.RetryAfter)
		c.logAttempt(opts, meta, err, wait)
		if serr := c.sleep(runCtx, wait); serr != nil {
			break
		}
	}
	return nil, lastMeta, lastErr
}

// attempt ejecuta un único intento y lo clasifica en errores tipados.
func (c *Client) attempt(ctx context.Context, rawURL, safe string, opts RequestOpts, limit int64, attempt int) (Body, Meta, error) {
	meta := Meta{URL: safe, Attempts: attempt}
	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, meta, c.newError(CodeInvalidResponse, opts, safe, 0, false, attempt, sanitizeCause(err, safe))
	}
	req.Header.Set("User-Agent", c.userAgent())
	if accept := acceptHeader(opts.Expected); accept != "" {
		req.Header.Set("Accept", accept)
	}
	if opts.IfNoneMatch != "" {
		req.Header.Set("If-None-Match", opts.IfNoneMatch)
	}
	if opts.IfModifiedSince != "" {
		req.Header.Set("If-Modified-Since", opts.IfModifiedSince)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		code, retryable := classifyNetErr(err, ctx)
		return nil, meta, c.newError(code, opts, safe, 0, retryable, attempt, sanitizeCause(err, safe))
	}
	meta.Status = resp.StatusCode
	meta.ETag = resp.Header.Get("ETag")
	meta.LastModified = resp.Header.Get("Last-Modified")
	meta.ContentType = resp.Header.Get("Content-Type")
	meta.FetchedAt = c.now()

	if resp.StatusCode == http.StatusNotModified {
		drainBody(resp.Body)
		meta.NotModified = true
		return nil, meta, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		code, retryable := classifyStatus(resp.StatusCode)
		typed := c.newError(code, opts, safe, resp.StatusCode, retryable, attempt,
			fmt.Errorf("estado HTTP %d", resp.StatusCode))
		if retryable {
			if wait, ok := parseRetryAfter(resp.Header.Get("Retry-After"), c.now()); ok {
				typed.RetryAfter = wait
			}
		}
		drainBody(resp.Body)
		return nil, meta, typed
	}
	if err := checkContentType(meta.ContentType, opts.Expected); err != nil {
		drainBody(resp.Body)
		return nil, meta, c.newError(CodeInvalidResponse, opts, safe, resp.StatusCode, false, attempt, err)
	}

	// La lectura se acota en tamaño (limit) y en tiempo (BodyTimeout): si el
	// servidor deja de enviar, se cancela la petición y el fallo es un timeout.
	stop := time.AfterFunc(c.bodyTimeout(), cancel)
	body, rerr := readLimited(resp.Body, limit)
	stop.Stop()
	_ = resp.Body.Close()
	if rerr != nil {
		if errors.Is(rerr, errBodyTooLarge) {
			return nil, meta, c.newError(CodeResponseTooLarge, opts, safe, resp.StatusCode, false, attempt, rerr)
		}
		code, retryable := classifyNetErr(rerr, ctx)
		return nil, meta, c.newError(code, opts, safe, resp.StatusCode, retryable, attempt, rerr)
	}
	return body, meta, nil
}

// newError construye el error tipado del transporte.
func (c *Client) newError(code string, opts RequestOpts, safeURL string, status int, retryable bool, attempts int, cause error) *TransportError {
	return &TransportError{
		Code:       code,
		Provider:   opts.Provider,
		Operation:  opts.Operation,
		URL:        safeURL,
		HTTPStatus: status,
		Retryable:  retryable,
		Attempts:   attempts,
		Err:        cause,
	}
}

// classifyStatus traduce un estado HTTP a código tipado y reintentabilidad.
// Solo 429 y 500/502/503/504 son transitorios; 400/401/403/404 y el resto son
// permanentes y nunca se reintentan.
func classifyStatus(status int) (string, bool) {
	switch status {
	case http.StatusNotFound:
		return CodeNotFound, false
	case http.StatusTooManyRequests:
		return CodeRateLimited, true
	case http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return CodeUpstreamUnavailable, true
	}
	if status >= 500 {
		return CodeUpstreamUnavailable, false
	}
	return CodeInvalidResponse, false
}

// classifyNetErr clasifica un fallo de red por TIPO de error (nunca por su
// texto) y decide si es reintentable. `parent` es el contexto del llamante: si
// ya se agotó o se canceló, no se reintenta.
func classifyNetErr(err error, parent context.Context) (string, bool) {
	if parent != nil && parent.Err() != nil {
		if errors.Is(parent.Err(), context.DeadlineExceeded) {
			return CodeTimeout, false
		}
		return CodeCanceled, false
	}
	// Fallos de confianza TLS/certificados: permanentes.
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return CodeInvalidResponse, false
	}
	var unknownAuthority x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthority) {
		return CodeInvalidResponse, false
	}
	var hostnameErr x509.HostnameError
	if errors.As(err, &hostnameErr) {
		return CodeInvalidResponse, false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CodeTimeout, true
	}
	if errors.Is(err, context.Canceled) {
		// Cancelación interna (por ejemplo el límite de tiempo del cuerpo).
		return CodeTimeout, true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return CodeTimeout, true
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		// Cuerpo cortado a mitad: transitorio.
		return CodeInvalidResponse, true
	}
	// Conexión rechazada, DNS, reset…: el upstream no está disponible.
	return CodeUpstreamUnavailable, true
}

// decodeCode clasifica un fallo de decodificación por TIPO de error (nunca por
// su texto): documento truncado o tipos distintos => schema_mismatch; sintaxis
// inválida o cuerpo vacío => invalid_response.
func decodeCode(err error) string {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return CodeSchemaMismatch
	}
	var unmarshalErr *xml.UnmarshalError
	if errors.As(err, &unmarshalErr) {
		return CodeSchemaMismatch
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return CodeSchemaMismatch
	}
	return CodeInvalidResponse
}

// backoff calcula la espera antes del siguiente intento: Retry-After si el
// servidor lo pidió; si no, backoff exponencial con jitter (entre el 50 % y el
// 100 % del valor calculado) acotado por MaxBackoff.
func (c *Client) backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if maxWait := c.maxWait(); maxWait > 0 && retryAfter > maxWait {
			return maxWait
		}
		return retryAfter
	}
	d := c.baseBackoff()
	for i := 1; i < attempt; i++ {
		if d >= c.maxBackoff() {
			break
		}
		d *= 2
	}
	if top := c.maxBackoff(); top > 0 && d > top {
		d = top
	}
	half := d / 2
	return half + time.Duration(c.randFloat()*float64(half))
}

// sleep espera d respetando el contexto. Es inyectable para pruebas.
func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// logAttempt emite el diagnóstico del intento si hay registrador.
func (c *Client) logAttempt(opts RequestOpts, meta Meta, err error, retryIn time.Duration) {
	if c.Log == nil {
		return
	}
	record := LogRecord{
		Provider:    opts.Provider,
		Operation:   opts.Operation,
		URL:         meta.URL,
		HTTPStatus:  meta.Status,
		Attempt:     meta.Attempts,
		RetryIn:     retryIn,
		NotModified: meta.NotModified,
	}
	var typed *TransportError
	if errors.As(err, &typed) {
		record.Code = typed.Code
		record.Retryable = typed.Retryable
	}
	c.Log(record)
}

// httpClient devuelve el cliente HTTP efectivo, creando y memorizando un
// transporte propio si el cliente aportado no trae uno.
func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil && c.HTTP.Transport != nil {
		return c.HTTP
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hc != nil {
		return c.hc
	}
	transport := c.newTransport()
	if c.HTTP == nil {
		c.hc = &http.Client{Transport: transport}
		return c.hc
	}
	copied := *c.HTTP
	copied.Transport = transport
	c.hc = &copied
	return c.hc
}

// newTransport construye el transporte con los timeouts diferenciados.
func (c *Client) newTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: c.dialTimeout(), KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   c.tlsHandshakeTimeout(),
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: c.responseHeaderTimeout(),
	}
}

// limitFor devuelve el límite de lectura por formato.
func (c *Client) limitFor(kind ContentType) int64 {
	if c.MaxBytes > 0 {
		return c.MaxBytes
	}
	if kind == ContentXML {
		return DefaultMaxXMLBytes
	}
	return DefaultMaxJSONBytes
}

func (c *Client) maxAttempts() int {
	if c.MaxAttempts > 0 {
		return c.MaxAttempts
	}
	return DefaultMaxAttempts
}

func (c *Client) dialTimeout() time.Duration {
	if c.DialTimeout > 0 {
		return c.DialTimeout
	}
	return DefaultDialTimeout
}

func (c *Client) tlsHandshakeTimeout() time.Duration {
	if c.TLSHandshakeTimeout > 0 {
		return c.TLSHandshakeTimeout
	}
	return DefaultTLSHandshakeTimeout
}

func (c *Client) responseHeaderTimeout() time.Duration {
	if c.ResponseHeaderTimeout > 0 {
		return c.ResponseHeaderTimeout
	}
	return DefaultResponseHeaderTimeout
}

func (c *Client) bodyTimeout() time.Duration {
	if c.BodyTimeout > 0 {
		return c.BodyTimeout
	}
	return DefaultBodyTimeout
}

func (c *Client) baseBackoff() time.Duration {
	if c.BaseBackoff > 0 {
		return c.BaseBackoff
	}
	return DefaultBaseBackoff
}

func (c *Client) maxBackoff() time.Duration {
	if c.MaxBackoff > 0 {
		return c.MaxBackoff
	}
	return DefaultMaxBackoff
}

func (c *Client) maxWait() time.Duration {
	if c.MaxWait != 0 {
		return c.MaxWait
	}
	return DefaultMaxWait
}

func (c *Client) userAgent() string {
	if c.UserAgent != "" {
		return c.UserAgent
	}
	return DefaultUserAgent()
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// randFloat devuelve el jitter en [0,1) con la fuente inyectable del cliente.
func (c *Client) randFloat() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rnd == nil {
		c.rnd = rand.New(rand.NewSource(time.Now().UnixNano())) // #nosec G404 -- jitter de reintentos, no criptográfico
	}
	return c.rnd.Float64()
}

// readLimited lee como máximo `limit` bytes y falla si hay más.
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = DefaultMaxJSONBytes
	}
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errBodyTooLarge
	}
	return raw, nil
}

// drainBody descarta un resto pequeño del cuerpo y lo cierra. No se guardan
// cuerpos de error completos, solo se libera la conexión.
func drainBody(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 4<<10))
	_ = body.Close()
}

// decodeJSON decodifica un documento JSON.
func decodeJSON(raw []byte, out any) error {
	return json.NewDecoder(bytes.NewReader(raw)).Decode(out)
}

// decodeXML decodifica un documento XML.
func decodeXML(raw []byte, out any) error {
	return xml.NewDecoder(bytes.NewReader(raw)).Decode(out)
}

// acceptHeader devuelve la cabecera Accept del formato esperado.
func acceptHeader(kind ContentType) string {
	switch kind {
	case ContentJSON:
		return "application/json, */*+json;q=0.9"
	case ContentXML:
		return "application/xml, text/xml;q=0.9, */*+xml;q=0.8"
	default:
		return ""
	}
}

// checkContentType valida la cabecera Content-Type contra el formato esperado.
// Se aceptan además la cabecera ausente y `text/plain` (algunos servidores de
// catálogo sirven JSON/XML como texto plano); el contenido se valida igualmente
// al decodificarlo. Cualquier otro tipo explícito es un fallo de formato.
func checkContentType(header string, want ContentType) error {
	if want == ContentAny {
		return nil
	}
	media := mediaType(header)
	if media == "" || media == "text/plain" {
		return nil
	}
	ok := false
	switch want {
	case ContentJSON:
		ok = media == "application/json" || media == "text/json" || strings.HasSuffix(media, "+json")
	case ContentXML:
		ok = media == "application/xml" || media == "text/xml" || strings.HasSuffix(media, "+xml")
	}
	if !ok {
		return fmt.Errorf("content-type %q no es %s", header, want)
	}
	return nil
}

// mediaType recorta los parámetros (charset…) y normaliza a minúsculas.
func mediaType(header string) string {
	if i := strings.IndexByte(header, ';'); i >= 0 {
		header = header[:i]
	}
	return strings.ToLower(strings.TrimSpace(header))
}

// parseRetryAfter interpreta Retry-After en segundos o como fecha HTTP.
func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(value); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	d := when.Sub(now)
	if d < 0 {
		d = 0
	}
	return d, true
}

// sanitizeCause evita que la causa subyacente filtre credenciales: los errores
// de net/http son *url.Error y llevan la URL original (con usuario y
// contraseña). Se sustituye por la URL saneada conservando la causa real.
func sanitizeCause(err error, safeURL string) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		copied := *urlErr
		copied.URL = safeURL
		return &copied
	}
	return err
}

// sanitizeURL quita credenciales y parámetros sensibles de una URL para poder
// registrarla. Si la URL no se puede analizar devuelve "".
func sanitizeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parsed.User = nil
	if parsed.RawQuery != "" {
		parsed.RawQuery = sanitizeQuery(parsed.RawQuery)
	}
	return parsed.String()
}

// sanitizeQuery oculta el valor de los parámetros sensibles conservando el
// resto de la consulta tal cual llegó (sin recodificar).
func sanitizeQuery(rawQuery string) string {
	parts := strings.Split(rawQuery, "&")
	for i, part := range parts {
		key := part
		if j := strings.IndexByte(part, '='); j >= 0 {
			key = part[:j]
		}
		if decoded, err := url.QueryUnescape(key); err == nil {
			key = decoded
		}
		if sensitiveParam(key) {
			parts[i] = key + "=***"
		}
	}
	return strings.Join(parts, "&")
}

// sensitiveParam indica si un parámetro de consulta puede llevar un secreto.
func sensitiveParam(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "token", "access_token", "apikey", "api_key", "key", "password",
		"passwd", "secret", "signature", "sig", "auth", "authorization":
		return true
	}
	return false
}
