package engines

import (
	"fmt"
	"strings"
	"time"
)

// CatalogState es el estado explícito de un catálogo de motor (Fase 4).
type CatalogState string

// Estados posibles del catálogo.
const (
	// CatalogFresh: el catálogo está en disco y dentro del TTL.
	CatalogFresh CatalogState = "fresh"
	// CatalogStale: vencido pero utilizable (se puede mostrar etiquetado).
	CatalogStale CatalogState = "stale"
	// CatalogInvalid: el documento existe pero no es legible o está vacío.
	CatalogInvalid CatalogState = "invalid"
	// CatalogUnavailable: no hay catálogo (ni caché ni embebido).
	CatalogUnavailable CatalogState = "unavailable"
)

// CatalogStatus es el estado CONSULTABLE de un catálogo, sin tocar la red.
type CatalogStatus struct {
	// Engine es el motor.
	Engine string `json:"engine"`
	// State es el estado explícito.
	State CatalogState `json:"state"`
	// Source indica de dónde saldría el catálogo: cache, embedded o none.
	Source string `json:"source"`
	// Provider es el proveedor que lo generó.
	Provider string `json:"provider,omitempty"`
	// FetchedAt y UpdatedAt son la fecha de la consulta y de la última validación.
	FetchedAt time.Time `json:"fetched_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	// AgeSeconds y Age son la antigüedad en segundos y en formato legible.
	AgeSeconds int64  `json:"age_seconds,omitempty"`
	Age        string `json:"age,omitempty"`
	// TTL es el tiempo de vida configurado.
	TTL string `json:"ttl"`
	// Versions es el número de versiones disponibles.
	Versions int `json:"versions"`
	// Schema es el esquema del documento en disco.
	Schema string `json:"schema,omitempty"`
	// Validated indica que el documento superó la validación antes de escribirse.
	Validated bool `json:"validated"`
	// Recommended es la versión recomendada actual del catálogo.
	Recommended string `json:"recommended,omitempty"`
	// Warning describe la incertidumbre (reducción anómala, caché vencida…).
	Warning string `json:"warning,omitempty"`
}

// Usable indica que el estado permite operar (con o sin etiqueta de antigüedad).
// Un catálogo "no disponible" no permite listar ni resolver.
func (s CatalogStatus) Usable() bool {
	return s.State == CatalogFresh || s.State == CatalogStale
}

// Status devuelve el estado del catálogo de un motor SIN tocar la red.
func (c *Cache) Status(engine string) CatalogStatus {
	engine = strings.ToLower(strings.TrimSpace(engine))
	st := CatalogStatus{Engine: engine, State: CatalogUnavailable, Source: "none", TTL: c.TTL.String()}
	now := c.now()

	doc, err := c.readCache(c.path(engine))
	switch {
	case err != nil:
		st.State = CatalogInvalid
		st.Source = "cache"
		st.Warning = "documento de caché ilegible: " + err.Error()
		return st
	case doc == nil || len(doc.Versions) == 0:
		if emb, ok := EmbeddedVersions(engine); ok {
			st.State = CatalogStale
			st.Source = "embedded"
			st.Versions = len(emb)
			st.Age = "sin caché en disco"
			st.Warning = "sin caché en disco: se dispone del catálogo embebido, que puede estar desactualizado"
			return st
		}
		st.Warning = "no hay catálogo en caché para " + engine
		return st
	}

	versions := Enrich(engine, doc.Versions)
	if len(versions) == 0 {
		st.State = CatalogInvalid
		st.Source = "cache"
		st.Warning = "el documento de caché no contiene ninguna versión utilizable"
		return st
	}
	st.Source = "cache"
	st.Provider = doc.Provider
	if st.Provider == "" {
		st.Provider = engine
	}
	st.Schema = doc.Schema
	st.Validated = doc.Validated
	st.FetchedAt = doc.Fetched
	st.UpdatedAt = doc.UpdatedAt
	st.Versions = len(versions)
	for _, v := range versions {
		if v.Recommended {
			st.Recommended = v.Version
			break
		}
	}
	age := now.Sub(doc.Fetched)
	if age < 0 {
		age = 0
	}
	st.AgeSeconds = int64(age / time.Second)
	st.Age = HumanAge(age)
	st.State = CatalogFresh
	if doc.Fetched.Before(now.Add(-c.TTL)) {
		st.State = CatalogStale
	}
	st.Warning = c.Warning(engine)
	return st
}

// Warning devuelve la última advertencia asociada al catálogo ("" si no hay).
func (c *Cache) Warning(engine string) string {
	engine = strings.ToLower(strings.TrimSpace(engine))
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.warnings[engine]
}

// HumanAge redacta una antigüedad de forma legible.
func HumanAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "hace unos segundos"
	case d < time.Hour:
		return fmt.Sprintf("hace %d min", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("hace %d h", int(d/time.Hour))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("hace %d días", int(d/(24*time.Hour)))
	default:
		return fmt.Sprintf("hace %d meses", int(d/(30*24*time.Hour)))
	}
}
