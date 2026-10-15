package doctor

import (
	"context"
	"strings"

	"github.com/MrZudro/badomens/internal/engines"
)

// ---------------------------------------------------------------------------
// `badomens doctor api` (plan 12.3 · AC-12 · spec 6.4)
// ---------------------------------------------------------------------------
//
// Comprueba, por proveedor: conectividad, códigos HTTP, tiempos, formato de la
// respuesta, edad de caché y versión de contrato. La sonda usa la ruta ÚNICA
// (transporte + adaptador del catálogo) y el estado consultable de la caché, de
// modo que este informe no puede desviarse de lo que ve el resto del programa.

// APIEntry es el informe de UN proveedor: la sonda en vivo y el estado de su
// caché (sin tocar la red).
type APIEntry struct {
	// Engine es el motor sondeado.
	Engine string `json:"engine"`
	// Probe es el resultado de la sonda en vivo (conectividad, HTTP, tiempos).
	Probe engines.ProbeReport `json:"probe"`
	// Cache es el estado del catálogo en caché: fuente, antigüedad, esquema.
	Cache engines.CatalogStatus `json:"cache"`
}

// APIResult es el informe completo del diagnóstico de proveedores.
type APIResult struct {
	// Providers es el informe por proveedor, en orden estable.
	Providers []APIEntry `json:"providers"`
	// OK, Warnings y Errors cuentan el veredicto de cada proveedor.
	OK       int `json:"ok"`
	Warnings int `json:"warnings"`
	Errors   int `json:"errors"`
}

// Healthy indica que ningún proveedor quedó en error.
func (r APIResult) Healthy() bool { return r.Errors == 0 }

// ProbeAPI sondea cada motor indicado. Un proveedor cuyo catálogo en vivo falla
// pero conserva caché utilizable cuenta como AVISO (la herramienta sigue
// funcionando con datos etiquetados); sin caché utilizable es un ERROR.
func ProbeAPI(ctx context.Context, service *engines.CatalogService, names []string, opts engines.ProbeOptions) APIResult {
	var out APIResult
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		entry := APIEntry{Engine: name}
		if service != nil {
			if st, err := service.GetCatalogStatus(ctx, name); err == nil {
				entry.Cache = st
			}
		}
		entry.Probe = engines.Probe(ctx, name, opts)
		out.Providers = append(out.Providers, entry)
		switch {
		case entry.Probe.OK():
			out.OK++
		case entry.Cache.Usable():
			out.Warnings++
		default:
			out.Errors++
		}
	}
	return out
}

// APIHuman redacta el informe de proveedores de forma legible.
func APIHuman(r APIResult) string {
	var b strings.Builder
	for _, p := range r.Providers {
		// El veredicto por defecto es "error": los dos casos de abajo lo
		// sobrescriben cuando corresponde.
		verdict := "error"
		switch {
		case p.Probe.OK():
			verdict = "ok"
		case p.Cache.Usable():
			verdict = "aviso"
		}
		line := "  [" + verdict + "] " + p.Engine
		if p.Probe.Contract != "" {
			line += " (" + p.Probe.Contract + ")"
		}
		line += ": " + p.Probe.Message
		if p.Probe.HTTPStatus != 0 {
			line += " [HTTP " + itoa(p.Probe.HTTPStatus) + "]"
		}
		line += " · " + itoa(int(p.Probe.DurationMS)) + " ms"
		if p.Cache.Age != "" {
			line += " · caché " + string(p.Cache.State) + " (" + p.Cache.Age + ")"
		} else {
			line += " · caché " + string(p.Cache.State)
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
