# Observabilidad del catálogo y diagnóstico de proveedores (Fase 12 · 12.3 · AC-12)

Documento de la parte de observabilidad de la Fase 12. Describe **qué se
implementó**, **cómo se verificó** y **qué no se puede garantizar**.

Ámbito: `internal/engines` (diagnóstico y sonda), `internal/doctor` (informe),
`internal/cli/doctor.go`, `internal/cli/doctor_api.go`, `internal/mcp/server.go`.
La accesibilidad (12.1) y el rendimiento de la TUI (12.2) los cubre otro agente.

## 1. Reglas que se cumplen

| Regla (plan 12.3 / spec 8) | Cómo se cumple |
| --- | --- |
| Cada fallo aporta `provider`, `operation`, `status`, `http_status`, `cache_state`, `resolution_complete`, `retryable` | `engines.Diagnostic` (nuevo, `internal/engines/diagnostic.go`), adjunto a `error.details.diagnostic` |
| Mensajes aptos para humanos **y** códigos estables para agentes | `Diagnostic.Message` (humano) + `Diagnostic.Status`/`Reason` (estable) |
| Distinguir red / respuesta inválida / versión inexistente / incompatibilidad real | `ReasonNetwork`, `ReasonInvalidResponse`, `ReasonNotFound`, `ReasonIncompatible` (+ `ReasonCanceled`, `ReasonUnknown`) |
| No guardar cuerpos completos, tokens ni URLs con credenciales | El diagnóstico se construye desde `TransportError` (nunca desde el cuerpo); `URL` va saneada por `sanitizeURL`/`sanitizeCause` del transporte |
| Informar de cuándo se consultó el catálogo y si viene de caché | `Diagnostic.CacheState`, `CacheSource`, `CacheUsable`, `LastFetchedAt`, `CacheAge` + `catalog.*` en todos los comandos de catálogo y chequeos `Catálogo <motor>` en `doctor` |
| `badomens doctor api` | Comando nuevo: conectividad, códigos HTTP, tiempos, formato, contrato, esquema y edad de caché por proveedor |
| Contrato JSON intacto | Todo aditivo: `ok`, `exit_code`, `action`, `message`, `data`, `phases`, `error`, `timestamp` sin cambios |

## 2. El objeto de diagnóstico

`internal/engines/diagnostic.go` define:

```go
type Diagnostic struct {
    Engine, Provider, Operation string
    Status string   // código ESTABLE del transporte (timeout, not_found, schema_mismatch…)
    Reason string   // network | invalid_response | not_found | incompatible | canceled | unknown
    HTTPStatus int  // 0 si no hubo respuesta
    CacheState string   // fresh | stale | invalid | unavailable | unknown
    CacheSource string  // cache | embedded | none
    CacheUsable bool
    LastFetchedAt *time.Time
    CacheAge string
    ResolutionComplete bool
    Retryable bool
    Attempts int
    RetryAfterSeconds int
    URL string          // saneada
    Message string      // apto para humanos
}
```

Se construye con `engines.Diagnose(engine, operation, err, cacheStatus)`, que lee
el error tipado del transporte con `errors.As(*TransportError)` (nunca el texto) y
el estado de caché con `CatalogStatus`. Para una incompatibilidad comprobada se
usa `engines.DiagnoseIncompatible(...)`. `engines.DiagnosticOf(err)` recupera el
diagnóstico adjunto a un error (lo usan las pruebas y cualquier consumidor).

**Dónde aparece.** En el documento JSON del comando, dentro de
`error.details.diagnostic`:

```json
{
  "ok": false,
  "exit_code": 2,
  "action": "engine use",
  "message": "no se encontró en el catálogo de paper",
  "error": {
    "code": "error_not_found",
    "message": "no se encontró en el catálogo de paper",
    "hint": "consulta las versiones disponibles con 'badomens engine versions paper'",
    "details": {
      "catalog_code": "not_found",
      "detail": "consulta de paper resolve: not_found: la versión \"9.9\" no existe para el motor paper",
      "diagnostic": {
        "engine": "paper",
        "provider": "paper",
        "operation": "resolve",
        "status": "not_found",
        "reason": "not_found",
        "cache_state": "fresh",
        "cache_source": "cache",
        "cache_usable": true,
        "last_fetched_at": "2026-10-14T21:36:58-05:00",
        "cache_age": "hace 1 min",
        "resolution_complete": false,
        "retryable": false,
        "message": "el catálogo de paper no publica lo pedido (not_found); datos de caché utilizables (hace 1 min)"
      },
      "engine": "paper",
      "operation": "resolve"
    }
  },
  "timestamp": "2026-10-15T02:38:29Z"
}
```

`NormalizeErrorWithCatalog(engine, operation, err, status)` es el punto por el que
pasan las operaciones del `CatalogService` (`list`, `resolve`, `refresh`).
`NormalizeError` se conserva con su firma histórica y delega en la variante nueva.

## 3. `badomens doctor api`

```bash
badomens doctor api                 # todos los motores
badomens doctor api paper           # solo uno
badomens doctor api --timeout 10 --json
badomens doctor api vanilla --base vanilla=https://espejo.local/manifest.json
```

Por proveedor publica (captura real de `badomens doctor api paper --json`):

```json
{
  "engine": "paper",
  "probe": {
    "engine": "paper",
    "provider": "paper",
    "endpoint": "https://fill.papermc.io/v3/projects/paper/versions",
    "contract": "fill/v3",
    "schema": "engines.catalog/1",
    "reachable": true,
    "http_status": 200,
    "duration_ms": 277,
    "content_type": "application/json",
    "format": "json",
    "schema_ok": true,
    "versions": 68,
    "status": "ok",
    "retryable": false,
    "attempts": 1,
    "message": "68 versiones en 277 ms"
  },
  "cache": {
    "engine": "paper",
    "state": "fresh",
    "source": "cache",
    "provider": "paper",
    "fetched_at": "2026-10-14T21:36:58-05:00",
    "updated_at": "2026-10-14T21:36:58-05:00",
    "age_seconds": 96,
    "age": "hace 1 min",
    "ttl": "6h0m0s",
    "versions": 68,
    "schema": "engines.catalog/1",
    "validated": true,
    "recommended": "26.3",
    "usable": true
  }
}
```

Veredicto por proveedor: `ok` si la sonda completó y la respuesta respetó el
contrato; **aviso** si la consulta en vivo falló pero hay caché utilizable (la
herramienta sigue funcionando con datos etiquetados); **error** si no hay datos
utilizables. El resumen (`data.summary`) cuenta cada caso y el código de salida es
0 / 1 / 2.

**Cómo se sondea (sin canales paralelos).** `engines.Probe` construye el registro
de proveedores sobre el **mismo** `Client` (transporte único, con su política de
reintentos, timeouts y límites) y ejecuta la consulta real del catálogo; observa
lo que el transporte registra (`Client.Log` → `LogRecord`, que ahora incluye
`ContentType`). No hay cliente HTTP, parser ni ruta paralelos: si el contrato de un
proveedor cambia, la sonda lo detecta igual que el catálogo. Los adaptadores
declaran su contrato con `ContractVersion()` (`fill/v3`, `fabric-meta/v2`,
`promotions+maven/1`, `maven-metadata/xml-1`, `version_manifest_v2`) y el esquema
del documento de caché es `engines.catalog/1`.

`--base motor=URL` existe para usar un espejo o para las pruebas: solo cambia la
raíz del endpoint del motor indicado.

## 4. Última consulta y procedencia de la caché

- `badomens doctor` añade un chequeo `Catálogo <motor>` por motor: estado
  (`fresh`/`stale`/`invalid`/`unavailable`), número de versiones, aviso y
  antigüedad legible ("consultada hace 3 h"). No toca la red.
- `badomens catalog status` sigue publicando `fetched_at`, `age`, `age_seconds`,
  `source` (`cache`/`embedded`/`none`) y `validated` por motor.
- Cada fila del catálogo (`engine versions`, `engine list`) conserva `source`,
  `fetched_at` y `stale`, y cada fallo publica `cache_state` en su diagnóstico.

## 5. MCP

`tools/call` con error devuelve, además del texto humano, `_meta.code`,
`_meta.hint` y `_meta.details` (con `diagnostic` cuando aplica): un agente decide
con datos estructurados y nunca leyendo el texto. Es aditivo.

## 6. Pruebas

| Prueba | Qué cubre |
| --- | --- |
| `internal/engines/diagnostic_test.go` | Objeto completo con caché; las cuatro categorías (`network`, `invalid_response`, `not_found`, `incompatible`) más `canceled`/`unknown`; ausencia de credenciales/tokens en URL y mensaje; `NormalizeErrorWithCatalog` adjunta el diagnóstico y la firma antigua sigue funcionando |
| `internal/engines/probe_test.go` | Matriz `httptest`: 200, 404, 429, 500, timeout y esquema distinto; conteo de intentos con reintentos; formato/contrato/esquema; motor desconocido |
| `internal/cli/observabilidad_test.go` | `doctor api` completo (formato, contrato, esquema, versión, tiempo, caché, resumen, códigos de salida), `--base` inválido, ausencia de secretos en el documento y el diagnóstico dentro de `error.details` de un comando fallido |

Comandos:

```bash
go test ./internal/engines/ -run 'TestDiagnose|TestProbe|TestNormalizeError' -count=1 -v
go test ./internal/cli/ -run 'TestDoctorAPI|TestEngineUseFallo' -count=1 -v
```

## 7. Lo que NO se puede garantizar

- **Que el proveedor responda**: `doctor api` informa del fallo, no lo evita. Un
  proveedor caído aparece como error (o aviso si hay caché utilizable).
- **Semántica de la versión de contrato**: `contract` declara la versión de
  upstream que el adaptador *consume*, no un número publicado por el proveedor. Si
  el proveedor cambia el esquema sin cambiar de ruta, la sonda lo detecta como
  `schema_mismatch`, pero no puede saber "qué versión" sirve.
- **Tiempos absolutos**: `duration_ms` incluye reintentos y backoff; es una medida
  real de la operación, no un percentil.
- **`resolution_complete` en un diagnóstico es siempre `false`**: se publica
  porque un agente lo lee sin interpretar el texto; que hubiera datos utilizables
  se expresa con `cache_usable`/`cache_state`.
- **Cuerpos de error**: nunca se guardan. Si un proveedor devuelve un mensaje útil
  en el cuerpo, ese detalle se pierde a propósito.
- **`Retry-After`**: se publica en segundos enteros (`retry_after_seconds`), de
  modo que un valor sub-segundo aparece como 0/ausente.
- **Sondear no valida compatibilidad**: `doctor api` comprueba conectividad y
  contrato; la compatibilidad real de una combinación es `engine compat` /
  `doctor preflight`.
- **`-race`** no está disponible en este entorno (requiere cgo).

## 8. Notas de integración

- `internal/cli/commands.go` (manifiesto) y `internal/cli/paridad_test.go` (tabla
  de paridad) incluyen `doctor api` y `doctor preflight`.
- El diagnóstico es **aditivo**: ningún consumidor existente (Emiria/Jamcord,
  scripts) necesita cambios; quien quiera el detalle lee
  `error.details.diagnostic`.
- La sonda es de solo lectura: no escribe catálogo, no resuelve versiones y no
  modifica ninguna instancia.