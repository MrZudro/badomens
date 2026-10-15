# Servicio de catálogo unificado (Fase 5)

Este documento describe el **servicio de catálogo** de badomens: el único caso
de uso por el que pasan CLI, TUI y MCP para consultar versiones, resolver pines y
comprobar compatibilidad. Cumple las reglas transversales del plan:

| Regla | Cómo se cumple aquí |
| --- | --- |
| **T-01** una sola ruta de resolución | `engines.CatalogService` es el único punto de entrada; ningún consumidor llama a `provider.Resolve`. |
| **T-02** sin éxito parcial | La resolución se valida con `Resolved.Validate(engine)`: sin todos los pines obligatorios no hay resultado. |
| **T-03** compatibilidad trivalente | `compatible` / `incompatible` / `unknown`; un dato desconocido **nunca** se convierte en `compatible` ni en Java 8. |
| **T-04** selección por valor | La instancia guarda motor + versión + pines exactos, nunca un índice ni `latest`. |
| **T-05/AC-09** contrato JSON aditivo | Se conservan `ok`, `exit_code`, `action`, `message`, `data`, `phases`, `error`, `timestamp`; solo se añaden campos. |
| **T-08/AC-10** sin éxito prematuro | `engine use` y `provision` informan de `outcome`: `applied`, `partial` o `failed`, con sus `phases`. |
| **AC-06** pins completos | Se validan antes de escribir la configuración y antes de arrancar. |

## 1. Diagrama de dependencias

```
  CLI (cobra)                 TUI (bubbletea)                 MCP (stdio)
  internal/cli                internal/tui                    internal/tools + internal/cli
  ├ engine list|versions      ├ screen_wizard (lista)         ├ badomens_engine_versions
  ├ engine use|compat         ├ screen_instance (cambio ver.) ├ badomens_engine_resolve
  ├ catalog status|refresh    └ svc.Catalog (fachada)         ├ badomens_catalog_status
  └ provision (crear)                                         └ badomens_engine_compat
             │                          │                                │
             └──────────────┬───────────┴────────────────┬───────────────┘
                            ▼                            ▼
                  internal/engines.CatalogService  (casos de uso, T-01)
                            │
                            ▼
                  internal/engines.Cache  (caché en disco, TTL, ETag,
                  validación antes de escribir, deduplicación, fallback)
                            │
                            ▼
                  internal/engines.Registry → engines.Provider  (límite externo)
                            │
                            ▼
                  internal/engines.Client  (transporte HTTP común, reintentos,
                  errores tipados, User-Agent, límites de tamaño)
                            │
                            ▼
                  Catálogos oficiales (Fill v3, Mojang, Fabric, Forge, NeoForge)
```

Nadie salta la flecha del medio: si un consumidor necesita una versión resuelta
pide `CatalogService.ResolveVersion`, y si necesita la lista pide
`CatalogService.ListVersions`. La caché, el orden, la selección, la validación de
pins y la presentación normalizada son responsabilidad del servicio.

## 2. API pública

```go
// Construcción
func NewCatalogService(cache *Cache) *CatalogService
func (s *CatalogService) Cache() *Cache

// Casos de uso (todos aceptan context.Context y se cancelan con él)
func (s *CatalogService) ListVersions(ctx context.Context, engine string, f Filters) (ListResult, error)
func (s *CatalogService) ResolveVersion(ctx context.Context, engine, mcVersion string) (Resolved, error)
func (s *CatalogService) GetCatalogStatus(ctx context.Context, engine string) (CatalogStatus, error)
func (s *CatalogService) RefreshCatalog(ctx context.Context, engine string) (RefreshResult, error)
func (s *CatalogService) CheckCompatibility(ctx context.Context, req CompatibilityRequest) (CompatibilityResult, error)

// Tipos de apoyo
type Filters struct { IncludeUnstable bool; Channel Channel; Limit int }
type ListResult struct { Engine string; Versions []Version; Stale bool; Status CatalogStatus; Warning string }
type RefreshResult struct { Engine string; Versions []Version; Status CatalogStatus }
type CompatibilityRequest struct {
    Engine, MinecraftVersion, LoaderVersion, Build string
    Pins map[string]string
    JavaMajor int; JavaStatus string
    DockerImage string
    Plugins []PluginRequirement
    Fill bool // completa con el servicio los datos que falten
}
type CompatibilityResult struct {
    Engine string; Compatibility Compatibility; Reason, Warning string
    MinecraftVersion, LoaderVersion, Build string
    JavaMajor int; JavaStatus, DockerImage string
    Pins map[string]string
    Plugins []PluginCompatibility
}
type PluginRequirement struct { Name string; Loaders, MinecraftVersions []string }

// Helpers normalizadores compartidos
func EngineOf(name string) (Engine, error)   // motor desconocido = error de uso
func Names() []string
func NormalizeError(engine, operation string, err error) error
```

Adaptaciones para minimizar cambios incompatibles:

- `Registry.Resolve(ctx, motor, versión)` se mantiene intacto (resuelve, valida
  los pines obligatorios y normaliza): es la ruta que usa internamente
  `CatalogService.ResolveVersion`, que añade contexto, caché de comprobación y
  errores normalizados.
- `provision.Resolver` cambió de `Provider(name)` a
  `ResolveVersion(ctx, motor, versión)`: `*engines.CatalogService` la cumple
  directamente, y `provision` deja de hablar con proveedores.
- `NewRegistryFrom(providers ...Provider)` construye registros con proveedores
  propios (pruebas de integración y fachadas controladas).
- `ResolveVersion` hereda de la fila del catálogo el requisito de Java
  **verificado** (Fill v3 declara `java.version.minimum`) cuando el proveedor lo
  publica: el dato verificado gana a la regla versionada (AC-05) y la imagen
  Docker coherente se recalcula con esa etiqueta.
- Los pines ausentes se derivan del `build`/`loader` declarados
  (`withDerivedPins`), así que el consumidor no necesita conocer el nombre de la
  variable de `.env` de la imagen.
- La TUI amplió su fachada `svc.Catalog` con `Resolve` y `Status`
  (`svc.NewCatalog(service)`).

## 3. Errores normalizados

`NormalizeError` traduce cualquier fallo del catálogo a un error tipado de
`internal/errx`, de modo que las tres fachadas reciban el MISMO código estable:

| Código del transporte | Código de `error.code` | Pista |
| --- | --- | --- |
| `not_found` | `error_not_found` | consulta `engine versions <motor>` |
| `timeout`, `rate_limited`, `upstream_unavailable` | `error_unavailable` | revisa la conexión y reintenta |
| `invalid_response`, `schema_mismatch`, `response_too_large` | `error_unavailable` | vuelve a intentarlo |
| motor desconocido (`EngineOf`) | `error_usage` | usa uno de los motores conocidos |
| otros | `error_general` | — |

El código original se conserva en `error.details.catalog_code`, junto a
`engine`, `operation` y `detail` (texto saneado: nunca cuerpos de respuesta ni
credenciales).

### Cancelación

Una cancelación **no** es un fallo del catálogo: `NormalizeError` devuelve
`context.Canceled` (o `context.DeadlineExceeded`) sin envolver, y los casos de
uso comprueban el contexto antes y después de consultar. Consecuencia: una
consulta cancelada **no produce resultado ni error espurio**; el consumidor la
detecta con `errors.Is(err, context.Canceled)` y descarta el trabajo.

```go
if errors.Is(err, context.Canceled) { return } // descartar en silencio
```

## 4. Respuestas tardías: cómo no pisar el estado vigente

El servicio acepta `context.Context` en todas sus operaciones y se cancela con
él, así que la consulta puede abortarse al abandonar una pantalla o al cambiar de
instancia. Además, cada consumidor asincrónico debe **etiquetar la petición y
descartar la respuesta que ya no corresponde**:

1. La TUI (`screen_wizard`) numera cada petición (`wizardScreen.verSeq`) y
   incluye el número en el mensaje (`wizardVersionsMsg.seq`). En `noteVersions`
   una respuesta se aplica **solo** si `msg.seq == w.verSeq` y siguen coincidiendo
   motor y modo de snapshots; si no, se descarta sin tocar el estado (la lista sí
   se cachea bajo su clave `motor|snapshots`, porque es válida para esa clave).
2. Las pantallas que cambian de instancia cancelan el `context` de la consulta
   anterior y comprueban el `slug`/generación del mensaje antes de aplicarlo
   (patrón ya usado por el sondeo de puertos y el detalle de instancia).
3. El servicio nunca "resucita" un trabajo cancelado: `Cache.load` deduplica
   consultas simultáneas, pero el llamante que canceló recibe `ctx.Err()` y la
   respuesta no se le entrega.

Pruebas que lo cubren:
`TestAsistenteDescartaRespuestaTardia` (TUI),
`TestCatalogServiceConsultaCanceladaNoProduceResultado` y
`TestCatalogServiceContextoCanceladoAntesDeConsultar` (servicio).

## 5. Desenlace de operaciones que modifican runtime (AC-10)

`internal/result` define el tipo compartido:

```go
type Outcome string
const ( OutcomeApplied = "applied"; OutcomeFailed = "failed"; OutcomePartial = "partial" )
func PhaseFor(name string, o Outcome) Phase
```

- `applied`: terminó por completo y verificado.
- `failed`: no se aplicó nada; el estado es recuperable tal cual.
- `partial`: se aplicó una parte (p. ej. la configuración quedó escrita y el
  runtime no se pudo reiniciar). Se publica `data.outcome = "partial"`, una fase
  `runtime` con estado `partial` y `data.follow_up` con el reintento exacto.

Dónde se informa:

| Operación | Campos | Fases |
| --- | --- | --- |
| `engine use` | `data.outcome`, `data.catalog`, `data.pins`, `data.compatibility` | `resolve`, `config`, `runtime` |
| `setup` / `instance create` | `data.outcome`, `data.compatibility`, `data.java_major`, `data.java_status`, `data.follow_up` | `created`, `runtime` |
| `catalog refresh` | `data.catalogs[].outcome` (`partial` si el proveedor falló y se conserva el previo) | — |
| TUI (alta y cambio de versión) | `wizardCreatedMsg.outcome`; el aviso distingue "no se pudo crear" de "quedó parcialmente aplicada" | — |

`provision.Create` devuelve siempre `Result.Outcome` junto al spec guardado
cuando el fallo es posterior a la escritura, de modo que ningún consumidor
informa de un éxito que no se ha verificado (T-08).

## 6. Checklist de compatibilidad trivalente

`CatalogService.CheckCompatibility` cruza **Minecraft + motor + loader/build +
Java + imagen Docker + plugins**:

- Motor desconocido → `incompatible`.
- Sin versión de Minecraft → `unknown`.
- **La versión se cruza con el catálogo del motor**: si el motor no la publica y
  el resto es compatible, el conjunto es `incompatible`; si el catálogo no se
  puede consultar (sin caché y sin respaldo embebido), el resultado es
  `unknown` con un motivo explícito, nunca `compatible`.
- Java `0`/`unknown` → `unknown` (nunca se asume nada); un `java_major > 0`
  declarado sin origen se publica como `verified` (lo declara el llamante).
- La imagen Docker, si se conoce, debe usar un Java **≥** el requerido; un Java
  inferior es `incompatible`.
- Los pines obligatorios se completan con `build`/`loader` si no se aportan
  (`withDerivedPins`); nunca se inventan valores.
- Los plugins solo pueden EMPEORAR el veredicto: un plugin que declara otro
  ecosistema o que no declara la versión de Minecraft resuelta es
  `incompatible`; un plugin sin metadatos deja el conjunto en `unknown` con
  `warning` estructurado.
- `unknown` + `unknown` = `unknown`; `incompatible` gana siempre.

`Fill: true` completa lo que falte resolviendo la versión (pines, Java, imagen);
si esa consulta falla, lo que falte sigue siendo `unknown` (nunca `compatible`).

`engine compat` traduce el veredicto a códigos de salida estables:

| Estado | `exit_code` | Documento |
| --- | --- | --- |
| `compatible` | 0 | `ok: true` |
| `unknown` | 1 | aviso con `reason` |
| `incompatible` | 2 | error con `reason` |

## 7. Campos normalizados y estado del catálogo

Cada versión y cada resolución publican los mismos campos estructurados
(`compatibility`, `java_major`, `java_status`, `loader_version`, `build`,
`source`, `fetched_at`, `stale`, `warning`), y el estado del catálogo viaja como
objeto `catalog` (`engine`, `state`, `source`, `versions`, `ttl`, `validated`,
`usable`, `age`, `age_seconds`, `provider`, `schema`, `fetched_at`,
`updated_at`, `recommended`, `warning`). MCP no depende de mensajes visuales: el
texto es solo un resumen y todo lo verificable está en `structuredContent`.

Herramientas MCP de catálogo:

| Herramienta | Uso |
| --- | --- |
| `badomens_engine_versions` | lista normalizada + estado del catálogo |
| `badomens_engine_resolve` | versión → pines + campos normalizados |
| `badomens_catalog_status` | estado sin tocar la red (todos o uno) |
| `badomens_engine_compat` | compatibilidad trivalente con plugins |

Comandos CLI equivalentes: `engine list`, `engine versions`, `engine use`,
`engine compat`, `catalog status`, `catalog refresh`.

## 8. Pruebas

- Servicio (`internal/engines/catalog_test.go`): filtros de lista, resolución de
  versión explícita y de última estable, versión inexistente (error tipado, sin
  red si hay catálogo en caché), los cuatro estados del catálogo, `RefreshCatalog`
  forzando la consulta y conservando el catálogo previo, compatibilidad con Java
  verificado/inferido/desconocido, imagen Docker incompatible y plugins,
  derivación de pines desde `build`, `NormalizeError` y cancelación.
- Paridad CLI/TUI/MCP (`internal/cli/paridad_catalogo_test.go`): la misma entrada
  produce el mismo motor, versión y pins por las tres rutas, el mismo error
  normalizado (`error_not_found`), el mismo listado y los mismos campos
  estructurados; contrato JSON aditivo de `engine versions`, `engine list`,
  `catalog status` y `catalog refresh`.
- TUI (`internal/tui/svc_catalogo_test.go`): descarte de respuestas tardías,
  cambio de versión que resuelve versión+pins por el servicio y que no aplica
  nada si la resolución falla.
- `internal/provision`: la creación usa el servicio (pins validados) y clasifica
  el desenlace.

## 9. Notas de integración

- `internal/cli/commands.go` (manifiesto de comandos) y la tabla de paridad
  `internal/cli/paridad_test.go` los mantiene otro subagente: si se quiere que
  `engine compat` aparezca en `badomens commands`, hay que añadirlo a
  `commandManifest()` y a la tabla de paridad.
- La función `versionData` de `internal/cli/engine.go` se renombró a
  `engineVersionData` para no chocar con el `versionData` del catálogo de
  plugins (`internal/cli/plugin_market.go`).
- `-race` no está disponible en este entorno (requiere cgo); la verificación se
  hizo con `gofmt -l`, `go build ./...`, `go vet ./...` y `go test ./... -count=1`.
