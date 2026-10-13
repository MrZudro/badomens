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
| `engine versions <motor> [--all]` | `engine versions` | `engine`, `stale`, `versions[]` (RF-5) |
| `docker status` | `docker status` | `state`, `reason`, `message`, `detail`, `platform`, `server_version`, `compose_version`, `context` (RF-6) |
| `docker ensure [--wait N] [--print-script]` | `docker ensure` | `ready`, `state`, `reason`, `attempts[]`, `pending[]`; con `--print-script` → `script` |
| `tailscale status` | `tailscale status` | `self`, `peers[]`, `peers_online` (RF-6) |
| `tailscale ensure` | `tailscale ensure` | `ip` |
| `-i <n> idle set --timeout 30m [...]` | `idle set` | `idle` (RF-8) |
| `-i <n> idle status` | `idle status` | `idle`, `running` |
| `-i <n> idle disable` | `idle disable` | `idle` |
| `idle daemon` | `idle daemon` | — |
| `setup --explain <campo>` | `setup explain` | `field` (RF-4) |

### `instance inspect`, `instance start` y `setup`

- `instance inspect` y `setup` añaden `connection` (RF-2), `idle` (RF-8),
  `stopped_by` y `stopped_at` a `data`.
- `instance start` añade `connection` a `data` (la IP de la tailnet se recalcula
  en cada arranque).

### Herramientas MCP nuevas

`badomens_connection`, `badomens_ports_check`, `badomens_engine_versions`,
`badomens_docker_status`, `badomens_tailscale_status`, `badomens_idle_status`.
