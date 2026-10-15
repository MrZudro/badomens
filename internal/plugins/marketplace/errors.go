package marketplace

import (
	"errors"
	"strings"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
)

// Motivos estables que acompañan a los errores de marketplace en
// `error.details.reason`. No cambian entre releases: los consumidores comparan
// contra ellos, nunca contra el texto del mensaje.
const (
	// ReasonNotFound: el recurso no existe en el marketplace (404).
	ReasonNotFound = "no_encontrado"
	// ReasonRed: fallo de red, DNS o timeout al hablar con el proveedor.
	ReasonRed = "red"
	// ReasonUpstream: el proveedor respondió con un error propio (5xx o 429).
	ReasonUpstream = "proveedor_no_disponible"
	// ReasonRespuestaInvalida: la respuesta no se puede interpretar.
	ReasonRespuestaInvalida = "respuesta_invalida"
	// ReasonRespuestaExcesiva: la respuesta supera el límite de lectura.
	ReasonRespuestaExcesiva = "respuesta_excesiva"
)

// WrapTransport traduce el error tipado del transporte HTTP de catálogos
// (internal/engines) a un error de badomens con código estable, sin filtrar
// cuerpos de respuesta ni credenciales y conservando la clasificación: 404 es
// not_found, y 429/5xx/red/timeout son unavailable (reintentables o no, según el
// transporte).
//
// Los adaptadores NO construyen clientes HTTP propios: usan el transporte común,
// que ya aplica reintentos con backoff, límite de tamaño durante la lectura,
// validación de formato y errores tipados.
func WrapTransport(src SourceID, operation string, err error) error {
	if err == nil {
		return nil
	}
	var typed *engines.TransportError
	if !errors.As(err, &typed) {
		return errx.Wrap(errx.CodeUnavailable, err, "no se pudo consultar "+string(src),
			"comprueba tu conexión y vuelve a intentarlo").
			WithDetail("reason", ReasonRed).
			WithDetail("provider", string(src)).
			WithDetail("operation", operation)
	}
	base := errx.New(errx.CodeUnavailable, "fallo al consultar "+string(src),
		"comprueba tu conexión y vuelve a intentarlo").
		WithDetail("provider", string(src)).
		WithDetail("operation", operation).
		WithDetail("upstream_code", typed.Code).
		WithDetail("retryable", typed.Retryable)
	if typed.HTTPStatus != 0 {
		base = base.WithDetail("http_status", typed.HTTPStatus)
	}
	switch typed.Code {
	case engines.CodeNotFound:
		base.Code = errx.CodeNotFound
		base.Message = "el recurso no existe en " + string(src)
		base.Hint = "comprueba el nombre o el identificador del proyecto"
		return base.WithDetail("reason", ReasonNotFound)
	case engines.CodeTimeout:
		base.Message = "se agotó el tiempo al consultar " + string(src)
		return base.WithDetail("reason", ReasonRed)
	case engines.CodeRateLimited:
		base.Message = string(src) + " está limitando las peticiones (429)"
		return base.WithDetail("reason", ReasonUpstream)
	case engines.CodeUpstreamUnavailable:
		base.Message = string(src) + " no está disponible"
		return base.WithDetail("reason", ReasonUpstream)
	case engines.CodeResponseTooLarge:
		base.Message = "la respuesta de " + string(src) + " es demasiado grande"
		base.Hint = "acota la búsqueda o la paginación"
		return base.WithDetail("reason", ReasonRespuestaExcesiva)
	default:
		base.Message = "respuesta no utilizable de " + string(src)
		base.Hint = "reintenta más tarde o usa otro marketplace"
		return base.WithDetail("reason", ReasonRespuestaInvalida)
	}
}

// InvalidResponse construye el error de respuesta inutilizable: el proveedor
// respondió 200, pero lo que devolvió no encaja con su esquema conocido.
func InvalidResponse(src SourceID, operation, detail string) error {
	e := errx.New(errx.CodeUnavailable,
		"respuesta no utilizable de "+string(src)+": "+detail,
		"el esquema del proveedor puede haber cambiado; reintenta más tarde o usa otro marketplace").
		WithDetail("reason", ReasonRespuestaInvalida).
		WithDetail("provider", string(src)).
		WithDetail("operation", operation)
	return e
}

// NotFound construye el error de recurso inexistente en el marketplace.
func NotFound(src SourceID, what, ref string) error {
	shown := strings.TrimSpace(ref)
	if shown == "" {
		shown = "desconocido"
	}
	return errx.NotFound(
		"no existe "+what+" "+shown+" en "+string(src),
		"revisa el nombre o el identificador, o busca con el subcomando de búsqueda").
		WithDetail("reason", ReasonNotFound).
		WithDetail("provider", string(src)).
		WithDetail("ref", shown)
}
