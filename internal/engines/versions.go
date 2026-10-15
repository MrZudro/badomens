package engines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Channel es el canal de publicación de una versión.
type Channel string

// Canales posibles.
const (
	ChannelRelease  Channel = "release"
	ChannelSnapshot Channel = "snapshot"
	ChannelBeta     Channel = "beta"
	ChannelAlpha    Channel = "alpha"
)

// String devuelve el identificador estable del canal.
func (c Channel) String() string { return string(c) }

// VersionOpts controla la consulta del catálogo.
type VersionOpts struct {
	// IncludeUnstable incluye snapshots/betas (ocultos por defecto).
	IncludeUnstable bool
}

// Catalog consulta las versiones disponibles y compatibles de un motor.
type Catalog interface {
	Versions(ctx context.Context, engine string, opts VersionOpts) ([]Version, error)
}

// Umbrales DOCUMENTADOS de la detección de reducciones anómalas (AC-07). Se
// aplican a todos los motores; NeoForge añade además su propio baseline de
// versiones publicadas (SeedCatalog), que es más fino porque cuenta lo que el
// proveedor publicó y no solo lo que resultó utilizable.
const (
	// catalogMinBaseline: solo se juzga una caída cuando el catálogo previo
	// validado tenía al menos tantas versiones (evita falsos positivos en
	// catálogos pequeños o recién nacidos).
	catalogMinBaseline = 20
	// catalogDropRatio: el catálogo nuevo debe conservar al menos esta fracción
	// del previo; por debajo, la respuesta se considera anómala y NO sobrescribe.
	catalogDropRatio = 0.5
	// catalogSchemaVersion identifica el esquema del documento de caché.
	catalogSchemaVersion = "engines.catalog/1"
)

// CacheValidators son los validadores condicionales (ETag / Last-Modified) que
// se envían al proveedor para ahorrar ancho de banda.
type CacheValidators struct {
	ETag         string
	LastModified string
}

// ConditionalProvider lo implementan los proveedores cuyo catálogo se obtiene en
// UNA sola petición y que, por tanto, pueden aprovechar ETag/Last-Modified (304 =
// sin cambios). Los catálogos que se componen de varias peticiones (Forge,
// NeoForge) no lo implementan.
type ConditionalProvider interface {
	ListAllWithMeta(ctx context.Context, v CacheValidators) ([]Version, Meta, error)
}

// catalogProvider lo implementa el proveedor cuyo catálogo se construye con
// lógica propia y expone metadatos de fiabilidad (NeoForge).
type catalogProvider interface {
	Catalog(ctx context.Context) (NeoForgeCatalog, error)
}

// warningProvider expone las advertencias de la última consulta del catálogo.
type warningProvider interface {
	Warnings() []CatalogWarning
}

// seedableCatalog permite sembrar el último catálogo validado (por ejemplo, el
// recuperado de la caché en disco) para que la detección de caídas funcione ya
// en la primera consulta del proceso (NeoForge).
type seedableCatalog interface {
	SeedCatalog(versions []Version, publishedCount int)
}

// allVersionsProvider devuelve también los canales inestables.
type allVersionsProvider interface {
	ListAllVersions(ctx context.Context) ([]Version, error)
}

// Cache guarda en disco las versiones de cada motor (TTL por defecto 6 h) y
// sirve datos vencidos si no hay red (stale-while-revalidate). Es segura para uso
// concurrente y deduplica las solicitudes simultáneas del mismo catálogo.
type Cache struct {
	// Dir es la raíz donde se guarda <dir>/versions/<motor>.json.
	Dir string
	// TTL es el tiempo de vida de la caché.
	TTL time.Duration
	// Registry resuelve el proveedor de cada motor.
	Registry *Registry
	// Now es el reloj inyectable.
	Now func() time.Time

	mu       sync.Mutex
	inflight map[string]*loadCall
	warnings map[string]string
}

// NewCache construye una caché sobre el registro.
func NewCache(dir string, reg *Registry) *Cache {
	c := &Cache{Dir: filepath.Join(dir, "versions"), Registry: reg, TTL: 6 * time.Hour}
	if c.Registry == nil {
		c.Registry = NewRegistry(DefaultClient())
	}
	return c
}

func (c *Cache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// loadCall es una consulta de catálogo en curso compartida por N llamantes.
type loadCall struct {
	done   chan struct{}
	result loadResult
}

// loadResult es el resultado interno de una consulta de catálogo.
type loadResult struct {
	// versions es el catálogo normalizado (sin filtrar por canal).
	versions []Version
	// stale indica que se sirven datos de caché vencida o del catálogo embebido.
	stale bool
	// warning describe la incertidumbre que afecta al resultado.
	warning string
	// cause es el fallo de la consulta que motivó el respaldo (nil si no hubo).
	cause error
	// err es un fallo fatal: no hay ningún dato que servir.
	err error
}

// cachedDoc es el documento en disco. Los campos añadidos en la Fase 4 son
// opcionales: un documento antiguo (solo engine/fetched/versions) se sigue
// leyendo y se migra al normalizarlo.
type cachedDoc struct {
	Engine         string    `json:"engine"`
	Provider       string    `json:"provider,omitempty"`
	Fetched        time.Time `json:"fetched"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
	Schema         string    `json:"schema,omitempty"`
	Validated      bool      `json:"validated,omitempty"`
	ETag           string    `json:"etag,omitempty"`
	LastModified   string    `json:"last_modified,omitempty"`
	PublishedCount int       `json:"published_count,omitempty"`
	Warning        string    `json:"warning,omitempty"`
	Versions       []Version `json:"versions"`
}

// Versions devuelve las versiones de un motor. stale=true indica que se usaron
// datos de caché vencida o del catálogo embebido (el proveedor falló y no había
// caché fresca). Las filas servidas así llevan Stale=true y Warning.
func (c *Cache) Versions(ctx context.Context, engine string, opts VersionOpts) ([]Version, bool, error) {
	res := c.load(ctx, engine, "list", false)
	if res.err != nil {
		return nil, false, res.err
	}
	out := Filter(res.versions, opts)
	if res.stale {
		out = markStale(out, res.warning)
	}
	return out, res.stale, nil
}

// Refresh fuerza una consulta al proveedor (ignora el TTL), valida antes de
// escribir y conserva el último catálogo válido si la petición o la validación
// fallan. Devuelve error si la consulta no se pudo completar, aunque el catálogo
// previo siga disponible (consúltalo con Status).
func (c *Cache) Refresh(ctx context.Context, engine string) ([]Version, CatalogStatus, error) {
	res := c.load(ctx, engine, "refresh", true)
	status := c.Status(engine)
	if res.err != nil {
		return nil, status, res.err
	}
	if res.cause != nil {
		return res.versions, status, res.cause
	}
	return res.versions, status, nil
}

// load deduplica las solicitudes simultáneas del mismo catálogo (una sola
// petición para N llamadas concurrentes) con un mutex y un canal de espera.
func (c *Cache) load(ctx context.Context, engine, kind string, force bool) loadResult {
	if ctx == nil {
		ctx = context.Background()
	}
	engine = strings.ToLower(strings.TrimSpace(engine))
	key := kind + ":" + engine

	c.mu.Lock()
	if c.inflight == nil {
		c.inflight = make(map[string]*loadCall)
	}
	if call, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		select {
		case <-call.done:
			return call.result
		case <-ctx.Done():
			return loadResult{err: ctx.Err()}
		}
	}
	call := &loadCall{done: make(chan struct{})}
	c.inflight[key] = call
	c.mu.Unlock()

	// La consulta NO se ata al contexto de un único llamante: si el primero
	// cancela, los demás seguirían esperando datos que nadie va a traer. El
	// límite de tiempo real lo aplica el propio transporte HTTP.
	res := c.loadUncached(context.WithoutCancel(ctx), engine, force)

	c.mu.Lock()
	call.result = res
	delete(c.inflight, key)
	if res.warning != "" {
		if c.warnings == nil {
			c.warnings = make(map[string]string)
		}
		c.warnings[engine] = res.warning
	} else {
		delete(c.warnings, engine)
	}
	c.mu.Unlock()
	close(call.done)
	return res
}

func (c *Cache) path(engine string) string {
	return filepath.Join(c.Dir, strings.ToLower(strings.TrimSpace(engine))+".json")
}

// loadUncached es la ruta real de lectura: caché fresca → proveedor → respaldo.
func (c *Cache) loadUncached(ctx context.Context, engine string, force bool) loadResult {
	now := c.now()
	path := c.path(engine)

	doc, docErr := c.readCache(path)
	if docErr != nil {
		// Documento ilegible o corrupto: se ignora y se intenta el proveedor.
		// Nunca se sirve una caché inválida como si fuera buena.
		doc = nil
	}
	if doc != nil {
		// Normaliza (y migra) el documento leído: canal, Java, compatibilidad,
		// orden y recomendada. Un documento anterior a la Fase 3 trae
		// `compatible: true` sin el campo trivalente y se migra aquí.
		doc.Versions = Enrich(engine, doc.Versions)
		// La procedencia de las filas servidas desde disco se rellena aquí: el
		// consumidor (CLI/TUI/MCP) necesita saber de dónde salió cada dato.
		for i := range doc.Versions {
			if doc.Versions[i].Source == "" {
				doc.Versions[i].Source = engine
			}
			if doc.Versions[i].FetchedAt.IsZero() {
				doc.Versions[i].FetchedAt = doc.Fetched
			}
		}
		c.seedBaseline(engine, doc)
	}
	if !force && doc != nil && len(doc.Versions) > 0 && doc.Fetched.After(now.Add(-c.TTL)) {
		return loadResult{versions: doc.Versions}
	}

	provider, perr := c.Registry.Provider(engine)
	if perr != nil {
		return c.fallback(doc, engine, perr)
	}

	validators := CacheValidators{}
	if doc != nil {
		validators = CacheValidators{ETag: doc.ETag, LastModified: doc.LastModified}
	}
	fetched, ferr := fetchCatalog(ctx, provider, validators)
	if ferr != nil {
		return c.fallback(doc, engine, ferr)
	}
	if fetched.meta.NotModified && doc != nil {
		// 304: sin cambios. Se refresca la FECHA sin reescribir los datos.
		touched := *doc
		touched.Fetched, touched.UpdatedAt = now, now
		_ = c.writeCacheDoc(path, touched)
		return loadResult{versions: doc.Versions}
	}

	enriched := Enrich(engine, fetched.versions)
	for i := range enriched {
		enriched[i].Source = engine
		enriched[i].FetchedAt = now
	}
	if verr := validateCatalog(enriched); verr != nil {
		return c.fallback(doc, engine, verr)
	}
	warning := firstWarning(fetched.warnings)
	if c.anomalousReduction(doc, len(enriched)) {
		w := fmt.Sprintf("reducción anómala del catálogo de %s: %d versiones frente a %d del último validado; se conserva el catálogo previo y no se sobrescribe",
			engine, len(enriched), len(doc.Versions))
		if warning != "" {
			w = warning + " · " + w
		}
		return loadResult{versions: doc.Versions, stale: true, warning: w}
	}
	newDoc := cachedDoc{
		Engine:         engine,
		Provider:       engine,
		Fetched:        now,
		UpdatedAt:      now,
		Schema:         catalogSchemaVersion,
		Validated:      true,
		ETag:           fetched.meta.ETag,
		LastModified:   fetched.meta.LastModified,
		PublishedCount: fetched.published,
		Warning:        warning,
		Versions:       enriched,
	}
	if werr := c.writeCacheDoc(path, newDoc); werr != nil {
		// El catálogo es válido aunque no se pueda persistir: se sirve igual.
		return loadResult{versions: enriched, warning: warning}
	}
	return loadResult{versions: enriched, warning: warning}
}

// fetchResult es lo que devuelve una consulta al proveedor.
type fetchResult struct {
	versions  []Version
	published int
	warnings  []string
	meta      Meta
}

// fetchCatalog consulta el proveedor por la vía que corresponda, respetando el
// contrato de cada uno. Si el proveedor implementa ConditionalProvider se usa esa
// vía SIEMPRE (aunque no haya validadores todavía): así se capturan el ETag y el
// Last-Modified de la primera respuesta y la siguiente consulta puede ser
// condicional.
func fetchCatalog(ctx context.Context, provider Provider, validators CacheValidators) (fetchResult, error) {
	if cp, ok := provider.(ConditionalProvider); ok {
		list, meta, err := cp.ListAllWithMeta(ctx, validators)
		if err != nil {
			return fetchResult{}, err
		}
		return fetchResult{versions: list, published: len(list), meta: meta}, nil
	}
	if np, ok := provider.(catalogProvider); ok {
		cat, err := np.Catalog(ctx)
		if err != nil {
			return fetchResult{}, err
		}
		res := fetchResult{versions: cat.Versions, published: cat.Published}
		if wp, ok := provider.(warningProvider); ok {
			res.warnings = warningMessages(wp.Warnings())
		}
		return res, nil
	}
	if all, ok := provider.(allVersionsProvider); ok {
		list, err := all.ListAllVersions(ctx)
		if err != nil {
			return fetchResult{}, err
		}
		return fetchResult{versions: list, published: len(list)}, nil
	}
	list, err := provider.ListVersions(ctx)
	if err != nil {
		return fetchResult{}, err
	}
	res := fetchResult{versions: list, published: len(list)}
	if wp, ok := provider.(warningProvider); ok {
		res.warnings = warningMessages(wp.Warnings())
	}
	return res, nil
}

// warningMessages extrae los textos legibles de las advertencias.
func warningMessages(list []CatalogWarning) []string {
	out := make([]string, 0, len(list))
	for _, w := range list {
		if msg := strings.TrimSpace(w.Message); msg != "" {
			out = append(out, msg)
		}
	}
	return out
}

// firstWarning devuelve el primer aviso legible ("" si no hay).
func firstWarning(list []string) string {
	for _, w := range list {
		if w = strings.TrimSpace(w); w != "" {
			return w
		}
	}
	return ""
}

// seedBaseline siembra el último catálogo validado del proveedor a partir del
// documento en disco, para que la detección de caídas funcione desde la primera
// consulta del proceso (NeoForge).
func (c *Cache) seedBaseline(engine string, doc *cachedDoc) {
	if doc == nil || len(doc.Versions) == 0 {
		return
	}
	provider, err := c.Registry.Provider(engine)
	if err != nil {
		return
	}
	sp, ok := provider.(seedableCatalog)
	if !ok {
		return
	}
	count := doc.PublishedCount
	if count <= 0 {
		count = len(doc.Versions)
	}
	sp.SeedCatalog(doc.Versions, count)
}

// anomalousReduction indica si el catálogo nuevo es una reducción anómala
// respecto al último validado. Regla genérica documentada: con un baseline de al
// menos catalogMinBaseline versiones, una respuesta que conserva menos del
// catalogDropRatio del catálogo previo es sospechosa (caso real: NeoForge
// publicó 3 versiones donde había más de 1800).
func (c *Cache) anomalousReduction(doc *cachedDoc, current int) bool {
	if doc == nil || len(doc.Versions) < catalogMinBaseline {
		return false
	}
	return float64(current) < float64(len(doc.Versions))*catalogDropRatio
}

// validateCatalog valida el catálogo ANTES de escribirlo: no vacío, con
// identificadores presentes y sin repeticiones.
func validateCatalog(list []Version) error {
	if len(list) == 0 {
		return fmt.Errorf("el catálogo quedó vacío: no se sobrescribe el previo")
	}
	seen := make(map[string]struct{}, len(list))
	for _, v := range list {
		id := strings.TrimSpace(v.Version)
		if id == "" {
			return fmt.Errorf("hay una entrada de catálogo sin identificador de versión")
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("el catálogo repite la versión %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

// fallback sirve el último catálogo válido (caché vencida) o el embebido, y
// conserva la causa del fallo como advertencia. Sin ninguno de los dos, el error
// se propaga.
func (c *Cache) fallback(doc *cachedDoc, engine string, cause error) loadResult {
	if doc != nil && len(doc.Versions) > 0 {
		return loadResult{
			versions: doc.Versions,
			stale:    true,
			warning:  "catálogo de caché vencida: " + cause.Error(),
			cause:    cause,
		}
	}
	if emb, ok := EmbeddedVersions(engine); ok {
		return loadResult{
			versions: emb,
			stale:    true,
			warning:  "catálogo embebido (sin conexión con el proveedor): " + cause.Error(),
			cause:    cause,
		}
	}
	return loadResult{err: cause}
}

// readCache lee el documento de caché. Un documento ausente devuelve (nil, nil).
func (c *Cache) readCache(path string) (*cachedDoc, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- ruta del catálogo dentro del data dir
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var doc cachedDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// writeCacheDoc escribe el documento de forma ATÓMICA: archivo temporal, fsync y
// rename. Una interrupción nunca deja un documento a medias.
func (c *Cache) writeCacheDoc(path string, doc cachedDoc) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if doc.Schema == "" {
		doc.Schema = catalogSchemaVersion
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- fichero temporal junto al catálogo, dentro del data dir
	if err != nil {
		return err
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Filter aplica el filtro de canales estables (los inestables se ocultan salvo
// que se pidan). Siempre devuelve una copia: el llamante puede marcarla (stale,
// warning) sin tocar el catálogo en memoria.
func Filter(list []Version, opts VersionOpts) []Version {
	if opts.IncludeUnstable {
		out := make([]Version, len(list))
		copy(out, list)
		return out
	}
	out := make([]Version, 0, len(list))
	for _, v := range list {
		if v.Channel == ChannelRelease || v.Channel == "" {
			out = append(out, v)
		}
	}
	return out
}

// markStale etiqueta las filas servidas desde caché vencida o catálogo embebido.
func markStale(list []Version, warning string) []Version {
	for i := range list {
		list[i].Stale = true
		if warning != "" && list[i].Warning == "" {
			list[i].Warning = warning
		}
	}
	return list
}

// Enrich normaliza una lista de versiones de un motor y aplica el algoritmo de
// la sección 4.3: canal, Java (con su origen), compatibilidad trivalente,
// agrupación Minecraft↔build de Forge/NeoForge, ORDEN según el esquema del motor
// y, por ÚLTIMO, cálculo de la recomendada.
//
// El orden de respuesta del proveedor no influye en el resultado. Es idempotente.
func Enrich(engine string, list []Version) []Version {
	out := make([]Version, 0, len(list))
	for _, v := range list {
		if strings.TrimSpace(v.Version) == "" {
			// Identificador obligatorio ausente: se descarta la fila inservible.
			continue
		}
		out = append(out, v)
	}
	if engine == "forge" || engine == "neoforge" {
		out = normalizeMaven(engine, out)
	}
	for i := range out {
		normalizeChannel(&out[i])
		normalizeJava(&out[i])
		out[i].Compatibility = NormalizeCompatibility(out[i].Compatibility, out[i].Compatible)
		out[i].Compatible = out[i].Compatibility.IsCompatible()
	}
	sortVersions(engine, out)
	markRecommended(out)
	return out
}
