# Contrato JSON de badomens

Cada comando imprime **un único documento JSON** en stdout (con `--json`) y el
código de salida del proceso resume el estado.

```json
{
  "ok": true,
  "exit_code": 0,
  "action": "doctor",
  "message": "doctor terminó sin problemas",
  "data": { "...": "resultado estructurado, opcional" },
  "phases": [ { "name": "engine", "status": "ok", "detail": "Paper" } ],
  "error": null,
  "timestamp": "2026-01-01T00:00:00Z"
}
```

## Claves estables

| Clave | Tipo | Descripción |
|---|---|---|
| `ok` | bool | `true` si el comando terminó sin error (puede haber avisos). |
| `exit_code` | int | Código de salida del proceso: 0, 1 o 2. |
| `action` | string | Acción ejecutada (`doctor`, `update`, `instance`…). |
| `message` | string | Resumen legible para humanos y para el LLM. |
| `data` | object/array | Resultado estructurado. Opcional. |
| `phases` | array | Etapas de operaciones largas (setup, start…). Opcional. |
| `error` | object | `{code, message, hint}`. Presente solo si `ok=false`. |
| `timestamp` | string | Momento (UTC, RFC3339). |

## Códigos de salida

| Código | Significado |
|---|---|
| `0` | Éxito. |
| `1` | Aviso: el comando terminó, pero hay algo que revisar (`error.code: "warn"`). |
| `2` | Error. El detalle está en `error.code` y `error.message`. |

## Códigos de error (`error.code`)

| Código | Significado |
|---|---|
| `error_general` | Error general no clasificado. |
| `error_usage` | Uso inválido: flags, argumentos o configuración. |
| `error_not_found` | El recurso pedido no existe (instancia, mundo…). |
| `error_dependency_missing` | Falta una dependencia externa (Docker, Tailscale). |
| `error_capacity_exceeded` | La operación excede la capacidad del host. |
| `error_unavailable` | Servicio de red no disponible. |
| `warn` | No es un fallo: el comando terminó con avisos. |

## Estabilidad

Estos nombres y códigos no cambian entre releases menores. Emiria/Jamcord se
apoyará en ellos; cualquier cambio incompatible se versiona y se documenta en
`CHANGELOG.md`.

> **Nota de versionado (v0.1.0-rc.1+):** se añadieron acciones nuevas sin tocar
> las existentes: `uninstall` (con `removed`/`kept`/`warnings` en `data`) y
> `update` con `--apply` (añade `from`, `to` y `restart_required` al documento).
> El preflight de la TUI y el check de actualización al abrirla son interactivos
> y **no** forman parte del contrato: los agentes siguen usando `doctor --json`
> y `update --json`.

## Adiciones de la TUI v2 (espec)

> Cambios **solo aditivos**: el conjunto de claves previo del documento no cambia.

### `error.details` (nuevo, aditivo)

El objeto `error` admite ahora `details` estructurados que **jamás sustituyen** a
`code`. La clave estable más usada es `reason` (string):

```json
{
  "error": {
    "code": "error_capacity_exceeded",
    "message": "no hay puertos libres en el rango 25565-25664",
    "hint": "amplía ports.range en la configuración o libera un puerto",
    "details": { "reason": "port_range_exhausted" }
  }
}
```

Valores estables de `details.reason`:

| Momento | `reason` |
|---|---|
| Asignación de puertos: rango agotado | `port_range_exhausted` |
| Docker no instalado | `docker_not_installed` |
| Daemon de Docker apagado | `docker_daemon_down` |
| Docker arrancando | `docker_starting` |
| Permiso denegado sobre el socket | `docker_permission_denied` |
| Falta Compose | `compose_missing` |
| Contexto de Docker inalcanzable | `docker_wrong_context` |
| Virtualización/WSL2 no disponible | `virtualization_off` |

### Comandos y acciones nuevas

| Comando | Acción (`action`) | Campos nuevos en `data` |
|---|---|---|
| `-i <n> connect [--copy]` | `connect` | `connection` (objeto RF-2), `player_steps`, `invitation` |
| `ports check <puerto>` | `ports check` | `ports`, `free`, `suggested`, `conflicts[]` (RF-3) |
| `ports list` | `ports list` | `instances[]`, `reservations[]` |
| `engine versions <motor> [--all]` | `engine versions` | `engine`, `stale`, `versions[]`, `warnings[]` (RF-5) |
| `docker status` | `docker status` | `state`, `reason`, `message`, `detail`, `platform`, `server_version`, `compose_version`, `context` (RF-6) |
| `docker ensure [--wait N] [--print-script]` | `docker ensure` | `ready`, `state`, `reason`, `attempts[]`, `pending[]`; con `--print-script` → `script` |
| `tailscale status` | `tailscale status` | `self`, `peers[]`, `peers_online` (RF-6) |
| `tailscale ensure` | `tailscale ensure` | `ip` |
| `-i <n> idle set --timeout 30m [...]` | `idle set` | `idle` (RF-8) |
| `-i <n> idle status` | `idle status` | `idle`, `running` |
| `-i <n> idle disable` | `idle disable` | `idle` |
| `idle daemon` | `idle daemon` | — |
| `idle install-service [--dry-run]` | `idle install-service` | `os`, `exe`, `path`, `unit` |
| `idle uninstall-service` | `idle uninstall-service` | `path` |
| `setup --explain <campo>` | `setup explain` | `field` (RF-4) |

> `connect`, `instance inspect` y `instance start` aceptan la instancia como
> argumento posicional **o** con el flag global `-i <slug>`.

### `instance list` con Docker caído

Si el daemon no está disponible, `instance list` devuelve `exit_code: 1`
(aviso) con `error.code: warn` y `state` vacío en cada instancia, en lugar de
silenciar el fallo. `instance start` hace un *preflight* y devuelve
`error_dependency_missing` con `details.reason` (`docker_daemon_down`,
`docker_not_installed`, …); si el puerto está ocupado usa
`error_capacity_exceeded` con `details.reason: port_in_use`.

### `instance inspect`, `instance start` y `setup`

- `instance inspect` y `setup` añaden `connection` (RF-2), `idle` (RF-8),
  `stopped_by` y `stopped_at` a `data`.
- `instance start` añade `connection` a `data` (la IP de la tailnet se recalcula
  en cada arranque).

### Herramientas MCP nuevas

`badomens_connection`, `badomens_ports_check`, `badomens_engine_versions`,
`badomens_docker_status`, `badomens_tailscale_status`, `badomens_idle_status` y
`badomens_idle` (alias de la anterior).

## Modelo normalizado de catálogo y compatibilidad

> Cambios **solo aditivos**: las claves estables (`ok`, `exit_code`, `action`,
> `message`, `data`, `phases`, `error`, `timestamp`) **no** cambian y todo campo
> nuevo se añade dentro de `data` o de `error`. Los documentos de caché y el
> catálogo embebido anteriores se **migran** al leerse (ver «Migración de
> documentos anteriores»), así que un consumidor antiguo sigue funcionando.

Los comandos de catálogo y de resolución (`engine versions`, `engine use`,
`engine compat`, `setup` con resolución) y las herramientas MCP
`badomens_engine_versions` y `badomens_engine_resolve` publican el MISMO modelo
normalizado: comparten la caché, la validación, la normalización y el orden, de
modo que los mismos campos llegan con los mismos valores por CLI, TUI y MCP
(`engine list <motor>` usa esa misma caché y orden, pero su `data` sigue siendo
solo la lista de identificadores de versión).

### `compatibility` (nuevo) y `compatible` (semántica nueva)

`compatibility` es el estado **trivalente** de compatibilidad y es el campo que
hay que mirar:

| Valor | Significado |
|---|---|
| `compatible` | La fuente oficial del motor publica la versión y, cuando el motor exige un pin (`PAPER_BUILD`, `FABRIC_LOADER_VERSION`…), este se ha confirmado. |
| `incompatible` | Se ha **comprobado** que el conjunto no se puede usar (por ejemplo, la imagen Docker usa un Java inferior al requerido). |
| `unknown` | No hay dato suficiente para confirmarlo. |

**Regla transversal: un dato desconocido NUNCA se convierte en `compatible`.**

`compatible` (booleano) sigue existiendo y ahora se **deriva** de `compatibility`
(`compatible: true` ⇔ `compatibility: "compatible"`). Consecuencia importante
para el consumidor: **`compatible: false` ya no significa «incompatible»**, puede
ser `unknown`. Antes la normalización rellenaba `compatible: true` a ciegas; ya
no. Si solo se mira el booleano, hay que tratar `false` como «revisa
`compatibility` (y `reason`/`incompatible_why`)».

### `java_major` y `java_status` (nuevos, aditivos)

| Campo | Tipo | Descripción |
|---|---|---|
| `java_major` | int | Java requerido (8, 16, 17, 21, 25…). **`0` = desconocido**: ya **no se asume Java 8**. |
| `java_status` | string | ORIGEN del dato: `verified` (lo declara el proveedor —p. ej. Paper Fill v3— o el propio llamante), `inferred` (regla versionada) o `unknown`. |

Reglas versionadas que producen `java_status: "inferred"`:

| Versión de Minecraft | Java |
|---|---|
| `26.1+` | 25 |
| `1.20.5`–`1.21.x` | 21 |
| `1.18`–`1.20.4` | 17 |
| `1.17` | 16 |
| `≤ 1.16.x` | 8 |

Una versión que no encaja en ninguna regla deja `java_major: 0` y
`java_status: "unknown"`. En ese caso la imagen Docker **conserva el `fallback`
sin cambios**: nunca se degrada la imagen por una suposición. El campo `image` de
la resolución informa de la imagen que realmente se va a usar.

### Campos del modelo normalizado

`engine versions` publica en `data`: `engine`, `stale`, `catalog`, `versions[]` y
`warnings[]` (solo cuando `stale` es `true`). Cada elemento de `versions[]`:

| Campo | Tipo | Descripción |
|---|---|---|
| `id` | string | Versión **de Minecraft** (nunca la del loader); es el identificador estable de la fila. |
| `channel` | string | Canal de publicación: `release`, `snapshot`, `beta` o `alpha`. |
| `released_at` | string | Fecha de publicación (UTC, RFC3339). |
| `recommended` | bool | Última versión estable que el motor soporta. |
| `java_major` | int | Java requerido (`0` = desconocido). |
| `java_status` | string | Origen del dato de Java: `verified` / `inferred` / `unknown`. |
| `build` | string | Build/artefacto concreto (Paper build, versión de Forge/NeoForge). |
| `loader_version` | string | Versión exacta del loader (Fabric). |
| `detail` | string | Nota legible adicional (build, loader). |
| `compatibility` | string | Estado trivalente (ver arriba). |
| `compatible` | bool | Derivado de `compatibility`. |
| `incompatible_why` | string | Motivo por el que la fila no se puede seleccionar (vacío si no procede). |
| `source` | string | Proveedor del que procede el dato. |
| `fetched_at` | string | Momento de obtención del catálogo. |
| `stale` | bool | La fila procede de caché vencida o del catálogo embebido. |
| `warning` | string | Restricción o incertidumbre que afecta a la fila. |

La **resolución** (`engine use` y `badomens_engine_resolve`) publica en `data`
los mismos nombres: `engine`, `version`, `pins`, `build`, `loader_version`,
`java_major`, `java_status`, `compatibility`, `source`, `stale`, `catalog`, más
`image`, `fetched_at` y `warning` cuando proceden. En la resolución, `stale`
indica que el estado del catálogo **no era utilizable** (`invalid`/`unavailable`)
en el momento de resolver; `engine use` añade además `instance`, `outcome`
(`applied`/`partial`/`failed`) y, en el caso parcial, `follow_up`.

`data.catalog` es el estado consultable del catálogo: `engine`, `state`
(`fresh`/`stale`/`invalid`/`unavailable`), `source` (`cache`/`embedded`/`none`),
`versions`, `ttl`, `validated` (el documento superó la validación antes de
escribirse), `usable`, `age`, `age_seconds` y, cuando existen, `provider`,
`schema`, `fetched_at`, `updated_at`, `recommended` y `warning`.

> `badomens_engine_versions` comparte los campos de `versions[]` y expone
> `warning` (cadena) en el nivel superior, mientras que `engine versions` usa
> `warnings[]` (lista) cuando la lista viene de caché vencida o del embebido.

### `engine compat`

`engine compat <motor>` cruza Minecraft + motor + loader + Java + imagen Docker +
plugins con la compatibilidad trivalente y **publica el veredicto también en el
código de salida**:

| `compatibility` | `ok` | `exit_code` | Documento |
|---|---|---|---|
| `compatible` | `true` | `0` | Éxito. |
| `unknown` | `false` | `1` | Documento con `error` (`error.code: "warn"`) y `message` «compatibilidad desconocida: …». **Desconocido nunca es compatible.** |
| `incompatible` | `false` | `2` | Documento con `error` y el detalle del veredicto en `data`. |

Campos de `data`: `engine`, `compatibility`, `java_major`, `java_status` y,
cuando proceden, `version` (versión de Minecraft), `loader_version`, `build`,
`image`, `pins`, `reason`, `warning` y `plugins[]` (cada uno con `name`,
`compatibility` y `reason`).

### Diagnóstico por proveedor (nuevo, aditivo)

Todo fallo de catálogo añade un objeto estructurado en
`error.details.diagnostic` (nunca sustituye a `error.code`), pensado para que un
agente decida sin leer el texto:

| Campo | Tipo | Descripción |
|---|---|---|
| `engine` | string | Motor normalizado (paper, fabric…). |
| `provider` | string | Proveedor que sirvió la consulta. |
| `operation` | string | Operación lógica: `list`, `resolve`, `refresh`… |
| `status` | string | Código ESTABLE del fallo (`timeout`, `not_found`, `schema_mismatch`…). |
| `reason` | string | Categoría legible: `network`, `invalid_response`, `not_found`, `incompatible`, `canceled`, `unknown`. |
| `http_status` | int | Estado HTTP; `0` si no hubo respuesta. |
| `cache_state` | string | Estado del catálogo: `fresh`, `stale`, `invalid`, `unavailable`. |
| `cache_source` | string | De dónde saldría el catálogo: `cache`, `embedded`, `none`. |
| `cache_usable` | bool | Pese al fallo, hay datos utilizables en caché. |
| `last_fetched_at` | string | Última consulta del catálogo (UTC, RFC3339). |
| `cache_age` | string | Antigüedad legible del catálogo. |
| `resolution_complete` | bool | Si la operación produjo una resolución completa y verificable. En un diagnóstico de fallo es siempre `false`. |
| `retryable` | bool | El fallo es transitorio y admite reintento. |
| `attempts` | int | Intentos realizados (cuando procede). |
| `retry_after_seconds` | int | Espera pedida por el servidor (`Retry-After`). |
| `url` | string | URL de origen saneada (sin credenciales ni parámetros sensibles). |
| `message` | string | Mensaje apto para humanos. |

El error tipado del catálogo conserva además `error.details.catalog_code` (el
código original del transporte), `error.details.engine`,
`error.details.operation` y `error.details.detail` (texto saneado).

### Migración de documentos anteriores

Los documentos de caché y el catálogo embebido escritos antes de estos cambios
se **migran al leerse**, sin intervención del consumidor:

- Un documento que solo traía el booleano `compatible` se traduce a
  `compatibility`: `compatible: true` se conserva como `compatible` verificado
  (los catálogos se generaron a partir de fuentes oficiales) y un estado ausente
  o desconocido pasa a `unknown`, **nunca** a `compatible`.
- Un documento sin `schema` se completa con el esquema actual al reescribirse.
- El canal, el requisito de Java (con su origen), la compatibilidad, el orden
  según el esquema del motor y la marca de recomendada se recalculan en la
  normalización, que es idempotente y no depende del orden del proveedor.