package engines

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MrZudro/badomens/internal/errx"
)

// Este archivo implementa el SERVICIO DE CATÁLOGO unificado (Fase 5, T-01): el
// ÚNICO caso de uso por el que pasan CLI, TUI y MCP. La interfaz Provider sigue
// siendo el LÍMITE EXTERNO de cada proveedor; aquí viven el orden, la caché, la
// selección, la validación de pins obligatorios (AC-06) y la presentación
// normalizada.
//
// Regla de oro: ningún consumidor llama a `provider.Resolve` directamente. Si
// quieres una versión resuelta, pide `CatalogService.ResolveVersion`.

// Names devuelve los nombres de motor en el orden estable de exposición.
func Names() []string {
	out := make([]string, 0, len(Order))
	for _, e := range Order {
		out = append(out, e.Name)
	}
	return out
}

// EngineOf valida un nombre de motor y devuelve el motor. Un nombre desconocido
// es un error de USO tipado: el servicio nunca resuelve un motor "por defecto".
func EngineOf(name string) (Engine, error) {
	e, ok := ByName(name)
	if !ok {
		return Engine{}, errx.Usage("motor desconocido "+strings.TrimSpace(name),
			"usa uno de: "+strings.Join(Names(), ", "))
	}
	return e, nil
}

// isCancellation indica si un error es una cancelación del llamante (o del
// límite de tiempo de su contexto) y no un fallo del catálogo.
func isCancellation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return CodeOf(err) == CodeCanceled
}

// NormalizeError traduce un error del catálogo (transporte o contenido) a un
// error tipado de errx, para que CLI, TUI y MCP reciban el MISMO código estable
// ante el mismo fallo (T-01). El código original del transporte se conserva en
// `error.details.catalog_code` y el texto saneado en `error.details.detail`.
//
// Una CANCELACIÓN no se envuelve: se devuelve como context.Canceled (o
// DeadlineExceeded), de forma que el consumidor la detecte con errors.Is y
// DESCARTE el trabajo obsoleto sin mostrar ninguna alarma espuria (§7).
func NormalizeError(engine, operation string, err error) error {
	return NormalizeErrorWithCatalog(engine, operation, err, CatalogStatus{})
}

// NormalizeErrorWithCatalog es NormalizeError añadiendo el ESTADO DE CACHÉ al
// diagnóstico estructurado (plan 12.3 · AC-12): el objeto
// error.details.diagnostic lleva provider, operation, status, http_status,
// cache_state, resolution_complete y retryable, además de la última fecha de
// consulta del catálogo y si los datos proceden de caché. Es ADITIVO.
func NormalizeErrorWithCatalog(engine, operation string, err error, cache CatalogStatus) error {
	if err == nil {
		return nil
	}
	if isCancellation(err) {
		if errors.Is(err, context.DeadlineExceeded) {
			return context.DeadlineExceeded
		}
		return context.Canceled
	}
	code := CodeOf(err)
	if code == "" {
		return errx.Wrap(errx.CodeGeneral, err, "fallo del catálogo de "+engine, "")
	}
	kind, message, hint := errx.CodeGeneral, "", ""
	switch code {
	case CodeNotFound:
		kind = errx.CodeNotFound
		message = "no se encontró en el catálogo de " + engine
		hint = "consulta las versiones disponibles con 'badomens engine versions " + engine + "'"
	case CodeTimeout, CodeRateLimited, CodeUpstreamUnavailable:
		kind = errx.CodeUnavailable
		message = "el catálogo de " + engine + " no está disponible (" + code + ")"
		hint = "revisa la conexión y vuelve a intentarlo"
	case CodeResponseTooLarge, CodeInvalidResponse, CodeSchemaMismatch:
		kind = errx.CodeUnavailable
		message = "la respuesta del catálogo de " + engine + " no es utilizable (" + code + ")"
		hint = "vuelve a intentarlo; si persiste, informa del fallo"
	default:
		message = "fallo del catálogo de " + engine + " (" + code + ")"
	}
	wrapped := errx.Wrap(kind, err, message, hint).
		WithDetail("engine", engine).
		WithDetail("catalog_code", code).
		WithDetail("detail", err.Error()).
		WithDetail("diagnostic", Diagnose(engine, operation, err, cache))
	if operation != "" {
		wrapped.WithDetail("operation", operation)
	}
	return wrapped
}

// CatalogService es el caso de uso ÚNICO de catálogo (T-01). Envuelve la caché
// persistente y el registro de proveedores y expone operaciones de alto nivel
// con errores normalizados y contexto cancelable.
type CatalogService struct {
	cache *Cache
}

// NewCatalogService construye el servicio sobre una caché. Con cache == nil se
// construye una caché por defecto (datos en memoria/relativos) para no dejar el
// servicio a medio construir.
func NewCatalogService(cache *Cache) *CatalogService {
	if cache == nil {
		cache = NewCache("", nil)
	}
	return &CatalogService{cache: cache}
}

// Cache devuelve la caché subyacente (estado, diagnóstico y pruebas).
func (s *CatalogService) Cache() *Cache { return s.cache }

// registry devuelve el registro de proveedores de la caché, construyendo el
// registro por defecto si la caché se creó a mano sin uno.
func (s *CatalogService) registry() *Registry {
	if s.cache.Registry == nil {
		s.cache.Registry = NewRegistry(DefaultClient())
	}
	return s.cache.Registry
}

// Filters acota una consulta de versiones. El cero (Filters{}) equivale al
// contrato histórico: solo canales estables, sin límite.
type Filters struct {
	// IncludeUnstable incluye snapshots/betas.
	IncludeUnstable bool
	// Channel, si no está vacío, deja solo ese canal.
	Channel Channel
	// Limit, si es > 0, deja las N versiones MÁS RECIENTES.
	Limit int
}

// ListResult es el catálogo normalizado con su estado y su incertidumbre.
type ListResult struct {
	// Engine es el motor normalizado.
	Engine string
	// Versions es la lista filtrada, en orden ascendente (antiguo → nuevo).
	Versions []Version
	// Stale indica que la lista procede de caché vencida o del catálogo embebido.
	Stale bool
	// Status es el estado consultable del catálogo (sin tocar la red).
	Status CatalogStatus
	// Warning describe la incertidumbre de la lista ("" si no hay).
	Warning string
}

// ListVersions devuelve las versiones de un motor por la ruta ÚNICA de caché,
// validación y normalización. Un motor desconocido es un error de uso; un
// catálogo inservible, un error tipado con el código normalizado.
func (s *CatalogService) ListVersions(ctx context.Context, engine string, f Filters) (ListResult, error) {
	e, err := EngineOf(engine)
	if err != nil {
		return ListResult{Engine: strings.ToLower(strings.TrimSpace(engine))}, err
	}
	if cerr := ctxErr(ctx); cerr != nil {
		return ListResult{Engine: e.Name, Status: s.cache.Status(e.Name)}, cerr
	}
	versions, stale, verr := s.cache.Versions(ctx, e.Name, VersionOpts{IncludeUnstable: f.IncludeUnstable})
	status := s.cache.Status(e.Name)
	if verr != nil {
		return ListResult{Engine: e.Name, Status: status, Warning: status.Warning},
			NormalizeErrorWithCatalog(e.Name, "list", verr, status)
	}
	if cerr := ctxErr(ctx); cerr != nil {
		// Respuesta tardía de una consulta ya cancelada: no se entrega.
		return ListResult{Engine: e.Name, Status: status}, cerr
	}
	return ListResult{
		Engine:   e.Name,
		Versions: filterCatalog(versions, f),
		Stale:    stale,
		Status:   status,
		Warning:  status.Warning,
	}, nil
}

// filterCatalog aplica los filtros de canal y de límite sobre el catálogo ya
// normalizado y ordenado ascendentemente (el límite toma las MÁS recientes).
func filterCatalog(list []Version, f Filters) []Version {
	out := list
	if f.Channel != "" {
		kept := make([]Version, 0, len(out))
		for _, v := range out {
			if v.Channel == f.Channel {
				kept = append(kept, v)
			}
		}
		out = kept
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[len(out)-f.Limit:]
	}
	return out
}

// ResolveVersion resuelve un motor (y una versión de Minecraft pedida; vacía =
// última estable) a los PINES finales, con la MISMA ruta para CLI, TUI y MCP:
// delega en el registro (que valida el invariante de pins obligatorios, AC-06)
// y normaliza el error.
func (s *CatalogService) ResolveVersion(ctx context.Context, engine, mcVersion string) (Resolved, error) {
	e, err := EngineOf(engine)
	if err != nil {
		return Resolved{}, err
	}
	if cerr := ctxErr(ctx); cerr != nil {
		return Resolved{}, cerr
	}
	// Pre-validación con el catálogo del disco: si la versión pedida no existe
	// se falla con un error tipado SIN tocar la red (siempre que el catálogo sea
	// utilizable). Un catálogo inservible no bloquea: el proveedor decidirá.
	if v := strings.TrimSpace(mcVersion); v != "" {
		if list, lerr := s.ListVersions(ctx, e.Name, Filters{IncludeUnstable: true}); lerr == nil && len(list.Versions) > 0 {
			if !hasVersion(list.Versions, v) {
				return Resolved{}, NormalizeErrorWithCatalog(e.Name, "resolve", catalogContentError(
					e.Name, CodeNotFound, "resolve", "", Meta{},
					fmt.Errorf("la versión %q no existe para el motor %s", v, e.Name)), list.Status)
			}
		}
	}
	res, rerr := s.registry().Resolve(ctx, e.Name, mcVersion)
	if rerr != nil {
		return Resolved{}, NormalizeErrorWithCatalog(e.Name, "resolve", rerr, s.cache.Status(e.Name))
	}
	if cerr := ctxErr(ctx); cerr != nil {
		return Resolved{}, cerr
	}
	// La fila del catálogo tiene PRIORIDAD cuando VERIFICA el requisito de Java
	// (Fill v3 declara java.version.minimum): el dato verificado gana a la regla
	// versionada (AC-05) y el servicio lo propaga a todos sus consumidores.
	if res.JavaStatus != JavaStatusVerified {
		if major, ok := s.verifiedJava(ctx, e.Name, res.Version); ok {
			res.JavaMajor, res.JavaStatus = major, JavaStatusVerified
			if tag := JavaTag(major); tag != "" {
				res.Image = imageWithTag(res.Image, tag)
			}
		}
	}
	return res, nil
}

// verifiedJava devuelve el requisito de Java que el catálogo VERIFICA para una
// versión de Minecraft ((0, false) si el proveedor no lo declara).
func (s *CatalogService) verifiedJava(ctx context.Context, engine, mc string) (int, bool) {
	list, err := s.ListVersions(ctx, engine, Filters{IncludeUnstable: true})
	if err != nil {
		return 0, false
	}
	for _, v := range list.Versions {
		if v.Version == mc && v.JavaMajor > 0 && v.JavaStatus == JavaStatusVerified {
			return v.JavaMajor, true
		}
	}
	return 0, false
}

// imageWithTag cambia la etiqueta de una imagen Docker conservando el
// repositorio (si no hay imagen previa se usa la base oficial de itzg).
func imageWithTag(image, tag string) string {
	repo := "itzg/minecraft-server"
	if strings.TrimSpace(image) != "" {
		repo = image
		if i := strings.Index(repo, ":"); i >= 0 {
			repo = repo[:i]
		}
	}
	return repo + ":" + tag
}

// RefreshResult es el resultado de forzar el refresco de un catálogo.
type RefreshResult struct {
	// Engine es el motor normalizado.
	Engine string
	// Versions es el catálogo obtenido ("" si el refresco falló).
	Versions []Version
	// Status es el estado del catálogo DESPUÉS del intento.
	Status CatalogStatus
}

// RefreshCatalog fuerza una consulta al proveedor (ignora el TTL), valida antes
// de escribir y conserva el último catálogo válido si algo falla. El estado
// devuelto dice si el catálogo previo sigue disponible.
func (s *CatalogService) RefreshCatalog(ctx context.Context, engine string) (RefreshResult, error) {
	e, err := EngineOf(engine)
	if err != nil {
		return RefreshResult{}, err
	}
	if cerr := ctxErr(ctx); cerr != nil {
		return RefreshResult{Engine: e.Name}, cerr
	}
	versions, status, rerr := s.cache.Refresh(ctx, e.Name)
	out := RefreshResult{Engine: e.Name, Versions: versions, Status: status}
	if rerr != nil {
		return out, NormalizeErrorWithCatalog(e.Name, "refresh", rerr, status)
	}
	if cerr := ctxErr(ctx); cerr != nil {
		return RefreshResult{Engine: e.Name, Status: status}, cerr
	}
	return out, nil
}

// GetCatalogStatus devuelve el estado del catálogo de un motor SIN tocar la red
// (fresca, vencida, inválida o no disponible).
func (s *CatalogService) GetCatalogStatus(ctx context.Context, engine string) (CatalogStatus, error) {
	e, err := EngineOf(engine)
	if err != nil {
		return CatalogStatus{}, err
	}
	if cerr := ctxErr(ctx); cerr != nil {
		return CatalogStatus{}, cerr
	}
	return s.cache.Status(e.Name), nil
}

// ctxErr devuelve el error de cancelación si el contexto ya no es válido. Una
// consulta cancelada NO produce resultado: es la base para que un consumidor
// (p. ej. la TUI al cambiar de instancia) descarte respuestas obsoletas (§7).
func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

// PluginRequirement declara lo que un plugin/mod declara sobre su propia
// compatibilidad. Los campos vacíos significan "no lo declara": nunca se
// interpretan como compatibles (T-03).
type PluginRequirement struct {
	// Name identifica el plugin/mods.
	Name string
	// Loaders son los ecosistemas declarados (paper, bukkit, spigot, fabric,
	// forge, neoforge).
	Loaders []string
	// MinecraftVersions son las versiones de Minecraft declaradas como
	// compatibles (vacío = no las declara).
	MinecraftVersions []string
}

// PluginCompatibility es el veredicto trivalente sobre UN plugin.
type PluginCompatibility struct {
	// Name es el plugin evaluado.
	Name string `json:"name"`
	// Compatibility es el estado trivalente.
	Compatibility Compatibility `json:"compatibility"`
	// Reason explica la incompatibilidad o la incertidumbre ("" si compatible).
	Reason string `json:"reason,omitempty"`
}

// CompatibilityRequest describe el conjunto completo a validar: Minecraft,
// motor, loader/build, Java, imagen Docker y plugins/mods.
type CompatibilityRequest struct {
	// Engine es el motor pedido (obligatorio).
	Engine string
	// MinecraftVersion es la versión real del juego (obligatoria).
	MinecraftVersion string
	// LoaderVersion es la versión exacta del loader (Fabric), si aplica.
	LoaderVersion string
	// Build es el build exacto del motor, si aplica.
	Build string
	// Pins son los pines obligatorios ya resueltos (claves de .env).
	Pins map[string]string
	// JavaMajor es el Java requerido (0 = desconocido) y JavaStatus su origen.
	JavaMajor  int
	JavaStatus string
	// DockerImage es la imagen final que se va a usar (opcional).
	DockerImage string
	// Plugins son los plugins/mods a cruzar (opcional).
	Plugins []PluginRequirement
	// Fill pide completar con el servicio los datos que falten (resolviendo la
	// versión). Requiere red; si falla, lo que falte sigue siendo `unknown`.
	Fill bool
}

// CompatibilityResult es el veredicto trivalente del conjunto, con el detalle
// normalizado que se expone por CLI/TUI/MCP.
type CompatibilityResult struct {
	// Engine es el motor normalizado.
	Engine string `json:"engine"`
	// Compatibility es el estado trivalente del conjunto completo.
	Compatibility Compatibility `json:"compatibility"`
	// Reason explica por qué no es compatible o por qué es incierto.
	Reason string `json:"reason,omitempty"`
	// Warning resume las incertidumbres de los plugins, cuando existan.
	Warning string `json:"warning,omitempty"`
	// MinecraftVersion, LoaderVersion y Build identifican lo validado.
	MinecraftVersion string `json:"minecraft_version,omitempty"`
	LoaderVersion    string `json:"loader_version,omitempty"`
	Build            string `json:"build,omitempty"`
	// JavaMajor y JavaStatus son el requisito de Java y su ORIGEN.
	JavaMajor  int    `json:"java_major,omitempty"`
	JavaStatus string `json:"java_status,omitempty"`
	// DockerImage es la imagen evaluada ("" si no se indicó).
	DockerImage string `json:"docker_image,omitempty"`
	// Pins son los pines obligatorios evaluados.
	Pins map[string]string `json:"pins,omitempty"`
	// Plugins es el veredicto por plugin.
	Plugins []PluginCompatibility `json:"plugins,omitempty"`
}

// CheckCompatibility cruza Minecraft + motor + loader/build + Java + imagen
// Docker + plugins y devuelve el estado TRIVALENTE del conjunto. Un dato que
// falta NUNCA se convierte en `compatible` (T-03): como mucho, `unknown`.
func (s *CatalogService) CheckCompatibility(ctx context.Context, req CompatibilityRequest) (CompatibilityResult, error) {
	e, err := EngineOf(req.Engine)
	if err != nil {
		return CompatibilityResult{}, err
	}
	if cerr := ctxErr(ctx); cerr != nil {
		return CompatibilityResult{}, cerr
	}
	if req.Fill {
		if cerr := s.fillCompatibility(ctx, e, &req); cerr != nil {
			return CompatibilityResult{}, cerr
		}
	}
	// Un Java indicado explícitamente lo DECLARA el llamante: es un dato
	// verificado (el origen "desconocido" solo aplica a lo que no se aporta).
	javaMajor, javaStatus := req.JavaMajor, req.JavaStatus
	if javaMajor > 0 && javaStatus == "" {
		javaStatus = JavaStatusVerified
	}
	// Los pines obligatorios se completan con el build/loader declarados: el
	// consumidor no tiene por qué conocer el nombre de la variable de .env de la
	// imagen. Nunca se INVENTA un valor: solo se rellena lo que falta.
	pins := withDerivedPins(e, req.Pins, req.Build, req.LoaderVersion)
	state, why := CheckCompatibility(CompatibilityInput{
		Engine:           e,
		MinecraftVersion: req.MinecraftVersion,
		LoaderVersion:    req.LoaderVersion,
		Build:            req.Build,
		Pins:             pins,
		JavaMajor:        javaMajor,
		JavaStatus:       javaStatus,
		DockerImage:      req.DockerImage,
	})
	// Cruce con el catálogo del motor: no se puede afirmar que la combinación es
	// compatible si el motor no publica esa versión de Minecraft (T-03). Si el
	// catálogo no se puede consultar, el dato queda DESCONOCIDO.
	if state == CompatibilityCompatible {
		cstate, cwhy, cerr := s.confirmVersion(ctx, e, req.MinecraftVersion)
		if cerr != nil {
			return CompatibilityResult{}, cerr
		}
		state, why = cstate, cwhy
	}
	out := CompatibilityResult{
		Engine:           e.Name,
		Compatibility:    state,
		Reason:           why,
		MinecraftVersion: req.MinecraftVersion,
		LoaderVersion:    req.LoaderVersion,
		Build:            req.Build,
		JavaMajor:        javaMajor,
		JavaStatus:       javaStatus,
		DockerImage:      req.DockerImage,
		Pins:             pins,
	}
	// Los plugins solo pueden EMPEORAR el veredicto: un conflicto explícito es
	// `incompatible` y una duda es `unknown`. Nunca lo mejoran.
	var warnings []string
	for _, p := range req.Plugins {
		pc := pluginCompatibility(e, req.MinecraftVersion, p)
		out.Plugins = append(out.Plugins, pc)
		switch pc.Compatibility {
		case CompatibilityIncompatible:
			if out.Compatibility != CompatibilityIncompatible {
				out.Compatibility = CompatibilityIncompatible
				out.Reason = pc.Reason
			}
		case CompatibilityUnknown:
			if out.Compatibility == CompatibilityCompatible {
				out.Compatibility = CompatibilityUnknown
				out.Reason = pc.Reason
			}
			if pc.Reason != "" {
				warnings = append(warnings, pc.Reason)
			}
		}
	}
	out.Warning = strings.Join(warnings, " · ")
	if cerr := ctxErr(ctx); cerr != nil {
		return CompatibilityResult{}, cerr
	}
	return out, nil
}

// fillCompatibility completa con el servicio los datos que falten (pines,
// Java, imagen) resolviendo la versión. Un fallo de resolución NO degrada a
// `compatible`: lo que siga faltando quedará `unknown`.
func (s *CatalogService) fillCompatibility(ctx context.Context, e Engine, req *CompatibilityRequest) error {
	if req.JavaMajor > 0 && req.JavaStatus != JavaStatusUnknown && len(RequiredPins(e)) > 0 && len(req.Pins) > 0 {
		return nil
	}
	resolved, err := s.ResolveVersion(ctx, e.Name, req.MinecraftVersion)
	if err != nil {
		if isCancellation(err) {
			return err
		}
		return nil
	}
	if req.MinecraftVersion == "" {
		req.MinecraftVersion = resolved.Version
	}
	if req.Build == "" {
		req.Build = resolved.Build
	}
	if req.LoaderVersion == "" {
		req.LoaderVersion = resolved.LoaderVersion
	}
	if req.JavaMajor <= 0 {
		req.JavaMajor, req.JavaStatus = resolved.JavaMajor, resolved.JavaStatus
	}
	if req.JavaStatus == "" {
		req.JavaStatus = JavaStatusVerified
	}
	if len(req.Pins) == 0 {
		req.Pins = resolved.Pins
	}
	if req.DockerImage == "" {
		req.DockerImage = resolved.Image
	}
	return nil
}

// confirmVersion cruza la versión pedida con el catálogo del motor: sin esa
// confirmación la compatibilidad no puede afirmarse (nunca se convierte un dato
// no comprobado en `compatible`).
func (s *CatalogService) confirmVersion(ctx context.Context, e Engine, mc string) (Compatibility, string, error) {
	mc = strings.TrimSpace(mc)
	if mc == "" {
		return CompatibilityUnknown, "no se identificó la versión de Minecraft", nil
	}
	list, err := s.ListVersions(ctx, e.Name, Filters{IncludeUnstable: true})
	if err != nil {
		if isCancellation(err) {
			return CompatibilityUnknown, "", err
		}
		return CompatibilityUnknown,
			"no se pudo confirmar en el catálogo de " + e.Name + " que exista Minecraft " + mc, nil
	}
	if len(list.Versions) == 0 {
		return CompatibilityUnknown,
			"el catálogo de " + e.Name + " no tiene versiones: no se puede confirmar Minecraft " + mc, nil
	}
	if !hasVersion(list.Versions, mc) {
		return CompatibilityIncompatible, "el catálogo de " + e.Name + " no publica Minecraft " + mc, nil
	}
	if list.Stale {
		// La versión existe, pero el catálogo viene de caché vencida o embebida.
		return CompatibilityCompatible, "", nil
	}
	return CompatibilityCompatible, "", nil
}

// hasVersion indica si el catálogo contiene esa versión de Minecraft.
func hasVersion(list []Version, version string) bool {
	for _, v := range list {
		if v.Version == version {
			return true
		}
	}
	return false
}

// withDerivedPins devuelve los pines obligatorios del motor completando los que
// falten a partir del build (Paper/Forge/NeoForge) o de la versión de loader
// (Fabric). Si ya venían todos, se respeta el mapa recibido.
func withDerivedPins(e Engine, pins map[string]string, build, loader string) map[string]string {
	deps := RequiredPins(e)
	if len(deps) == 0 {
		return pins
	}
	out := make(map[string]string, len(deps))
	for k, v := range pins {
		out[k] = v
	}
	for _, dep := range deps {
		if strings.TrimSpace(out[dep]) != "" {
			continue
		}
		switch {
		case strings.TrimSpace(build) != "":
			out[dep] = build
		case strings.TrimSpace(loader) != "":
			out[dep] = loader
		}
	}
	if len(out) == 0 {
		return pins
	}
	return out
}

// pluginCompatibility evalúa UN plugin contra el motor y la versión de
// Minecraft. `unknown` cuando el plugin no declara el dato necesario.
func pluginCompatibility(e Engine, mc string, p PluginRequirement) PluginCompatibility {
	out := PluginCompatibility{Name: p.Name, Compatibility: CompatibilityUnknown}
	if len(p.Loaders) == 0 && len(p.MinecraftVersions) == 0 {
		out.Reason = "el plugin " + p.Name + " no declara motor ni versiones de Minecraft: no se puede confirmar"
		return out
	}
	if len(p.Loaders) > 0 && !loaderAccepted(e, p.Loaders) {
		out.Compatibility = CompatibilityIncompatible
		out.Reason = fmt.Sprintf("el plugin %s declara %s y el motor %s no lo admite",
			p.Name, strings.Join(p.Loaders, "/"), e.Name)
		return out
	}
	if len(p.MinecraftVersions) > 0 {
		if strings.TrimSpace(mc) == "" {
			out.Reason = "no se identificó la versión de Minecraft para comprobar el plugin " + p.Name
			return out
		}
		if !containsFold(p.MinecraftVersions, mc) {
			out.Compatibility = CompatibilityIncompatible
			out.Reason = fmt.Sprintf("el plugin %s no declara compatibilidad con Minecraft %s", p.Name, mc)
			return out
		}
	}
	if len(p.Loaders) == 0 {
		out.Reason = "el plugin " + p.Name + " no declara el motor/loader: no se puede confirmar"
		return out
	}
	if len(p.MinecraftVersions) == 0 {
		out.Reason = "el plugin " + p.Name + " no declara las versiones de Minecraft compatibles: no se puede confirmar"
		return out
	}
	out.Compatibility = CompatibilityCompatible
	return out
}

// engineLoaders devuelve los ecosistemas de plugins/mods que admite un motor.
func engineLoaders(e Engine) []string {
	switch e.Name {
	case Paper.Name:
		return []string{"paper", "bukkit", "spigot"}
	case Fabric.Name:
		return []string{"fabric"}
	case Forge.Name:
		return []string{"forge"}
	case NeoForge.Name:
		return []string{"neoforge"}
	}
	return nil
}

// loaderAccepted indica si alguno de los loaders declarados encaja con el motor.
func loaderAccepted(e Engine, loaders []string) bool {
	accepted := engineLoaders(e)
	if len(accepted) == 0 {
		return false
	}
	for _, l := range loaders {
		for _, a := range accepted {
			if strings.EqualFold(strings.TrimSpace(l), a) {
				return true
			}
		}
	}
	return false
}

// containsFold indica si una lista contiene un valor (comparación sin distinguir
// mayúsculas).
func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}
