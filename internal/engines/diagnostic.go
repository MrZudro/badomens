package engines

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
)

// ---------------------------------------------------------------------------
// Observabilidad del catálogo (plan 12.3 · AC-12)
// ---------------------------------------------------------------------------
//
// Cada fallo de catálogo publica un objeto de DIAGNÓSTICO estructurado con, como
// mínimo: provider, operation, status, http_status, cache_state,
// resolution_complete y retryable. Se construye SIEMPRE desde las fuentes que ya
// existen —el error tipado del transporte (TransportError / CodeOf) y el estado
// de caché (CatalogStatus)—, nunca desde un canal paralelo.
//
// El objeto se ADJUNTA al documento JSON en `error.details.diagnostic`: es
// aditivo y no rompe el contrato (`ok`, `exit_code`, `action`, `message`,
// `data`, `phases`, `error`, `timestamp` siguen intactos).

// Categorías legibles de un fallo (campo `reason` del diagnóstico). Distinguen
// sin ambigüedad un problema de red, una respuesta inválida, una versión
// inexistente y una incompatibilidad real.
const (
	// ReasonNetwork: no se pudo completar la comunicación (DNS, conexión, TLS,
	// timeout, 429 o 5xx del proveedor). Es un fallo del CAMINO, no del contrato.
	ReasonNetwork = "network"
	// ReasonInvalidResponse: hubo respuesta, pero no es utilizable (formato o
	// esquema distintos de los esperados, o cuerpo demasiado grande).
	ReasonInvalidResponse = "invalid_response"
	// ReasonNotFound: la respuesta es válida y dice que el recurso no existe
	// (por ejemplo, una versión de Minecraft inexistente).
	ReasonNotFound = "not_found"
	// ReasonIncompatible: la combinación comprobada es incompatible de verdad
	// (no es un fallo de red ni de contrato).
	ReasonIncompatible = "incompatible"
	// ReasonCanceled: el llamante canceló la consulta; no es un fallo del catálogo.
	ReasonCanceled = "canceled"
	// ReasonUnknown: no se pudo clasificar (no procede del transporte).
	ReasonUnknown = "unknown"
)

// Diagnostic es el objeto de diagnóstico de un fallo de catálogo. Todos los
// campos son ADITIVOS respecto al contrato JSON existente.
type Diagnostic struct {
	// Engine es el motor normalizado (paper, fabric…).
	Engine string `json:"engine,omitempty"`
	// Provider es el proveedor que sirvió la consulta.
	Provider string `json:"provider,omitempty"`
	// Operation identifica la operación lógica (list, resolve, refresh…).
	Operation string `json:"operation,omitempty"`
	// Status es el código ESTABLE del fallo (timeout, not_found, schema_mismatch…).
	Status string `json:"status"`
	// Reason es la categoría legible (network, invalid_response, not_found,
	// incompatible, canceled, unknown).
	Reason string `json:"reason"`
	// HTTPStatus es el estado HTTP; 0 si no hubo respuesta.
	HTTPStatus int `json:"http_status,omitempty"`
	// CacheState es el estado del catálogo (fresh, stale, invalid, unavailable).
	CacheState string `json:"cache_state"`
	// CacheSource indica de dónde saldría el catálogo (cache, embedded, none).
	CacheSource string `json:"cache_source,omitempty"`
	// CacheUsable indica que, pese al fallo, hay datos utilizables en caché.
	CacheUsable bool `json:"cache_usable"`
	// LastFetchedAt es cuándo se consultó el catálogo por última vez.
	LastFetchedAt *time.Time `json:"last_fetched_at,omitempty"`
	// CacheAge es la antigüedad legible del catálogo.
	CacheAge string `json:"cache_age,omitempty"`
	// ResolutionComplete indica si la operación produjo una resolución completa y
	// verificable. En un diagnóstico de FALLO es siempre false: si fuera true no
	// habría error. Se publica para que un agente decida sin leer el texto.
	ResolutionComplete bool `json:"resolution_complete"`
	// Retryable indica si el fallo es transitorio y admite reintento.
	Retryable bool `json:"retryable"`
	// Attempts es el número de intentos realizados.
	Attempts int `json:"attempts,omitempty"`
	// RetryAfterSeconds es la espera pedida por el servidor (Retry-After).
	RetryAfterSeconds int `json:"retry_after_seconds,omitempty"`
	// URL es la URL de origen saneada (sin credenciales ni parámetros sensibles).
	URL string `json:"url,omitempty"`
	// Message es el mensaje apto para humanos.
	Message string `json:"message"`
}

// ReasonFor traduce un código estable del transporte a su categoría legible.
func ReasonFor(code string) string {
	switch code {
	case CodeTimeout, CodeRateLimited, CodeUpstreamUnavailable:
		return ReasonNetwork
	case CodeInvalidResponse, CodeSchemaMismatch, CodeResponseTooLarge:
		return ReasonInvalidResponse
	case CodeNotFound:
		return ReasonNotFound
	case CodeCanceled:
		return ReasonCanceled
	}
	return ReasonUnknown
}

// Diagnose construye el diagnóstico de un fallo de catálogo a partir del error
// tipado del transporte (TransportError / CodeOf) y del estado de caché
// (CatalogStatus). No inventa un canal paralelo: reutiliza lo que ya existe.
func Diagnose(engine, operation string, err error, cache CatalogStatus) Diagnostic {
	d := Diagnostic{
		Engine:             engine,
		Provider:           engine,
		Operation:          operation,
		Status:             CodeInvalidResponse,
		Reason:             ReasonUnknown,
		CacheState:         "unknown",
		CacheUsable:        cache.Usable(),
		ResolutionComplete: false,
	}
	if cache.State != "" {
		d.CacheState = string(cache.State)
	}
	d.CacheSource = cache.Source
	if cache.Provider != "" {
		d.Provider = cache.Provider
	}
	if !cache.FetchedAt.IsZero() {
		fetched := cache.FetchedAt
		d.LastFetchedAt = &fetched
		d.CacheAge = cache.Age
	}
	var typed *TransportError
	switch {
	case errors.As(err, &typed):
		if strings.TrimSpace(typed.Provider) != "" {
			d.Provider = typed.Provider
		}
		if strings.TrimSpace(typed.Operation) != "" {
			d.Operation = typed.Operation
		}
		if strings.TrimSpace(typed.Code) != "" {
			d.Status = typed.Code
			d.Reason = ReasonFor(typed.Code)
		}
		d.HTTPStatus = typed.HTTPStatus
		d.Retryable = typed.Retryable
		d.Attempts = typed.Attempts
		d.URL = typed.URL
		if typed.RetryAfter > 0 {
			d.RetryAfterSeconds = int(typed.RetryAfter / time.Second)
		}
	default:
		if code := CodeOf(err); code != "" {
			d.Status = code
			d.Reason = ReasonFor(code)
			d.Retryable = IsRetryable(err)
		}
	}
	d.Message = DiagnosticMessage(d)
	return d
}

// DiagnoseIncompatible construye el diagnóstico de una incompatibilidad REAL
// (la comprobación se hizo y el veredicto es `incompatible`): no es un fallo de
// red ni de contrato, así que nunca es reintentable.
func DiagnoseIncompatible(engine, operation, reason string, cache CatalogStatus) Diagnostic {
	d := Diagnose(engine, operation, nil, cache)
	d.Status = ReasonIncompatible
	d.Reason = ReasonIncompatible
	d.Retryable = false
	d.HTTPStatus = 0
	if strings.TrimSpace(reason) != "" {
		d.Message = "combinación incompatible: " + reason
		return d
	}
	d.Message = "la combinación comprobada es incompatible"
	return d
}

// DiagnosticMessage redacta el mensaje apto para humanos del diagnóstico,
// distinguiendo red, respuesta inválida, versión inexistente e incompatibilidad.
func DiagnosticMessage(d Diagnostic) string {
	var b strings.Builder
	switch d.Reason {
	case ReasonNetwork:
		b.WriteString("no se pudo contactar con el catálogo de " + d.Engine)
	case ReasonInvalidResponse:
		b.WriteString("la respuesta del catálogo de " + d.Engine + " no es utilizable")
	case ReasonNotFound:
		b.WriteString("el catálogo de " + d.Engine + " no publica lo pedido")
	case ReasonIncompatible:
		b.WriteString("la combinación comprobada es incompatible")
	case ReasonCanceled:
		b.WriteString("consulta del catálogo de " + d.Engine + " cancelada")
	default:
		b.WriteString("fallo del catálogo de " + d.Engine)
	}
	if d.Status != "" {
		b.WriteString(" (" + d.Status)
		if d.HTTPStatus != 0 {
			fmt.Fprintf(&b, ", HTTP %d", d.HTTPStatus)
		}
		b.WriteString(")")
	}
	if d.Retryable {
		b.WriteString("; transitorio, se puede reintentar")
	}
	switch {
	case d.CacheUsable && d.CacheAge != "":
		b.WriteString("; datos de caché utilizables (" + d.CacheAge + ")")
	case d.CacheUsable:
		b.WriteString("; datos de caché utilizables")
	default:
		b.WriteString("; sin datos de caché utilizables")
	}
	return b.String()
}

// DiagnosticOf extrae el diagnóstico adjunto a un error tipado del catálogo
// (false si el error no lo lleva).
func DiagnosticOf(err error) (Diagnostic, bool) {
	details := errx.Details(err)
	if details == nil {
		return Diagnostic{}, false
	}
	switch v := details["diagnostic"].(type) {
	case Diagnostic:
		return v, true
	case *Diagnostic:
		if v != nil {
			return *v, true
		}
	}
	return Diagnostic{}, false
}
