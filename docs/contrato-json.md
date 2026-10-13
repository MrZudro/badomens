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
