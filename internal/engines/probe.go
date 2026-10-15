package engines

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Sonda de proveedores (plan 12.3 · AC-12 · `badomens doctor api`)
// ---------------------------------------------------------------------------
//
// La sonda consulta el catálogo REAL de un motor por la ruta que ya existe —el
// mismo `Client` (transporte único) y el mismo adaptador/parser de cada
// proveedor (T-06)— y observa lo que el transporte registre (URL, código HTTP,
// formato y reintentos). NO crea un cliente HTTP, un parser ni una ruta
// paralelos: si mañana cambia el contrato de un proveedor, la sonda lo detecta
// exactamente igual que el catálogo.

// ProbeOptions ajusta una sonda.
type ProbeOptions struct {
	// Bases sobreescribe la raíz del endpoint de un motor (claves paper, fabric,
	// forge, neoforge, vanilla). Vacío = endpoint oficial. Existe para las
	// pruebas con httptest.
	Bases map[string]string
	// Timeout es el límite total por motor (0 = el del transporte).
	Timeout time.Duration
	// UserAgent sobreescribe la cabecera (vacío = la del binario).
	UserAgent string
}

// ProbeReport es el informe de la sonda de un proveedor: conectividad, código
// HTTP, tiempo, formato de la respuesta, contrato, esquema y número de versiones.
// Todos los campos son ADITIVOS en el documento del comando.
type ProbeReport struct {
	// Engine y Provider identifican el motor y su adaptador.
	Engine   string `json:"engine"`
	Provider string `json:"provider"`
	// Endpoint es la URL raíz consultada (saneada).
	Endpoint string `json:"endpoint,omitempty"`
	// Contract es la versión del contrato upstream que consume el adaptador.
	Contract string `json:"contract,omitempty"`
	// Schema es el esquema del documento de catálogo en caché.
	Schema string `json:"schema,omitempty"`
	// Reachable indica que hubo respuesta HTTP (aunque fuera de error).
	Reachable bool `json:"reachable"`
	// HTTPStatus es el código HTTP del último intento; 0 si no hubo respuesta.
	HTTPStatus int `json:"http_status,omitempty"`
	// DurationMS es el tiempo total de la consulta (reintentos incluidos).
	DurationMS int64 `json:"duration_ms"`
	// ContentType es la cabecera Content-Type observada.
	ContentType string `json:"content_type,omitempty"`
	// Format es el formato detectado (json, xml, unknown).
	Format string `json:"format,omitempty"`
	// SchemaOK indica que la respuesta encajó con el esquema esperado.
	SchemaOK bool `json:"schema_ok"`
	// Versions es el número de versiones obtenidas.
	Versions int `json:"versions"`
	// Status es "ok" o un código estable del transporte (timeout, not_found…).
	Status string `json:"status"`
	// Reason es la categoría legible (network, invalid_response, not_found…).
	Reason string `json:"reason,omitempty"`
	// Retryable indica si el fallo es transitorio.
	Retryable bool `json:"retryable"`
	// Attempts es el número de intentos del último intento observado.
	Attempts int `json:"attempts,omitempty"`
	// Message es el mensaje apto para humanos.
	Message string `json:"message"`
}

// OK indica que la sonda se completó y la respuesta respetó el contrato.
func (r ProbeReport) OK() bool { return r.Status == "ok" }

// contractProvider declara la versión del contrato upstream que consume el
// adaptador. Es METADATA DE OBSERVABILIDAD: no altera la resolución.
type contractProvider interface {
	ContractVersion() string
}

// baseProvider expone la raíz del endpoint del adaptador.
type baseProvider interface {
	Base() string
}

// Versiones de contrato declaradas por cada adaptador. Se declaran aquí (y no en
// la lógica de resolución) porque solo las consume el diagnóstico.
func (p *paperProvider) ContractVersion() string    { return "fill/v3" }
func (p *fabricProvider) ContractVersion() string   { return "fabric-meta/v2" }
func (p *forgeProvider) ContractVersion() string    { return "promotions+maven/1" }
func (p *neoforgeProvider) ContractVersion() string { return "maven-metadata/xml-1" }
func (p *vanillaProvider) ContractVersion() string  { return "version_manifest_v2" }

// probeRecorder acumula el último registro del transporte.
type probeRecorder struct {
	mu   sync.Mutex
	last LogRecord
	seen bool
}

func (r *probeRecorder) record(rec LogRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last = rec
	r.seen = true
}

func (r *probeRecorder) snapshot() (LogRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last, r.seen
}

// Probe consulta el catálogo real de un motor y describe conectar/contrato.
func Probe(ctx context.Context, engine string, opts ProbeOptions) ProbeReport {
	name := strings.ToLower(strings.TrimSpace(engine))
	rep := ProbeReport{
		Engine:   name,
		Provider: name,
		Schema:   catalogSchemaVersion,
		Status:   CodeInvalidResponse,
		Reason:   ReasonUnknown,
	}
	if _, ok := ByName(name); !ok {
		rep.Status = "unknown_engine"
		rep.Message = "motor desconocido: " + name + " (usa uno de: " + strings.Join(Names(), ", ") + ")"
		return rep
	}
	client := DefaultClient()
	if opts.Timeout > 0 {
		client.Timeout = opts.Timeout
	}
	if opts.UserAgent != "" {
		client.UserAgent = opts.UserAgent
	}
	provider, err := probeRegistry(client, opts.Bases).Provider(name)
	if err != nil {
		rep.Status = "unknown_engine"
		rep.Message = err.Error()
		return rep
	}
	return probeProvider(ctx, provider, client, opts)
}

// probeProvider ejecuta la sonda con el transporte indicado. La sonda es DUEÑA
// del registro de diagnóstico del cliente que recibe (lo usa para observar el
// código HTTP, la URL y el formato); es la puerta que usan las pruebas para
// inyectar un servidor httptest sin cambiar la ruta real.
func probeProvider(ctx context.Context, provider Provider, client *Client, _ ProbeOptions) ProbeReport {
	rep := ProbeReport{
		Engine:   provider.Engine().Name,
		Provider: provider.Engine().Name,
		Schema:   catalogSchemaVersion,
		Status:   CodeInvalidResponse,
		Reason:   ReasonUnknown,
	}
	if client == nil {
		client = DefaultClient()
	}
	recorder := &probeRecorder{}
	client.Log = recorder.record
	rep.Contract = providerContract(provider)
	if bp, ok := provider.(baseProvider); ok {
		rep.Endpoint = sanitizeURL(bp.Base())
	}

	start := time.Now()
	fetched, ferr := fetchCatalog(ctx, provider, CacheValidators{})
	rep.DurationMS = time.Since(start).Milliseconds()
	rep.Versions = len(fetched.versions)
	if rec, ok := recorder.snapshot(); ok {
		rep.ContentType = rec.ContentType
		rep.HTTPStatus = rec.HTTPStatus
		rep.Attempts = rec.Attempt
		if rec.URL != "" {
			rep.Endpoint = rec.URL
		}
	}
	rep.Format = formatOf(rep.ContentType, rep.Contract)

	if ferr == nil {
		rep.Reachable = true
		rep.SchemaOK = true
		rep.Status = "ok"
		rep.Reason = ""
		rep.Retryable = false
		rep.Message = fmt.Sprintf("%d versiones en %d ms", rep.Versions, rep.DurationMS)
		return rep
	}
	rep.Status = CodeOf(ferr)
	if rep.Status == "" {
		rep.Status = CodeInvalidResponse
	}
	rep.Reason = ReasonFor(rep.Status)
	rep.Retryable = IsRetryable(ferr)
	var typed *TransportError
	if errors.As(ferr, &typed) {
		if typed.HTTPStatus != 0 {
			rep.HTTPStatus = typed.HTTPStatus
		}
		if typed.URL != "" {
			rep.Endpoint = typed.URL
		}
	}
	rep.Reachable = rep.HTTPStatus != 0
	rep.Message = DiagnosticMessage(Diagnostic{
		Engine: rep.Engine, Provider: rep.Provider, Operation: "probe",
		Status: rep.Status, Reason: rep.Reason, HTTPStatus: rep.HTTPStatus,
		Retryable: rep.Retryable,
	})
	return rep
}

// probeRegistry construye el registro de proveedores sobre el cliente dado,
// aplicando las raíces inyectadas (pruebas con httptest).
func probeRegistry(client *Client, bases map[string]string) *Registry {
	base := func(name string) string {
		if bases == nil {
			return ""
		}
		return bases[name]
	}
	return NewRegistryFrom(
		&paperProvider{engine: Paper, client: client, base: base(Paper.Name)},
		&fabricProvider{engine: Fabric, client: client, base: base(Fabric.Name)},
		&forgeProvider{engine: Forge, client: client, base: base(Forge.Name)},
		&neoforgeProvider{engine: NeoForge, client: client, base: base(NeoForge.Name)},
		&vanillaProvider{engine: Vanilla, client: client, base: base(Vanilla.Name)},
	)
}

// providerContract devuelve la versión de contrato declarada por un adaptador.
func providerContract(p Provider) string {
	if cp, ok := p.(contractProvider); ok {
		return cp.ContractVersion()
	}
	return ""
}

// formatOf deduce el formato de la respuesta a partir de la cabecera
// Content-Type y, si no la hay, del contrato declarado.
func formatOf(contentType, contract string) string {
	media := mediaType(contentType)
	switch {
	case media == "":
	case strings.HasSuffix(media, "json") || media == "text/json":
		return "json"
	case strings.HasSuffix(media, "xml"):
		return "xml"
	default:
		return "unknown"
	}
	if strings.Contains(contract, "xml") {
		return "xml"
	}
	if contract != "" {
		return "json"
	}
	return "unknown"
}
