# Auditoría previa — TUI v2 (Fase 0.1)

> Documento de la Fase 0.1 de `SPEC-tui-v2.md`. Antes de crear paquetes nuevos
> se revisó el código actual del repositorio para decidir qué se **reutiliza**,
> qué se **reemplaza** y qué **deuda** existe. Toda afirmación aquí apunta a
> archivos y símbolos reales del repo en el momento de la auditoría.

## 1. Qué existe hoy

### 1.1 TUI actual (`internal/tui/`)

| Archivo | Contenido | Veredicto |
|---|---|---|
| `app.go` | `Run(cfg, Options)` (bubbletea, `WithAltScreen`), `RenderSnapshot` para capturas y pruebas | **Reutilizar**: la entrada sigue siendo válida; se ampliará con `Options` (tema, animaciones). |
| `backend.go` | Interfaz `Backend` (una sola fachada grande) + `realBackend` sobre `config`/`instance`/`runtime`/`backup`/`tps`/`provision` | **Reemplazar por `svc.Services`** (interfaces pequeñas). El `Backend` mezcla lectura y escritura y no es mockeable por capacidad. `realBackend` es una referencia útil para implementar los adaptadores. |
| `model.go` | Un único `Model` con `mode` (enum) en vez de una pila de pantallas; `Update`/`View`/`handleKey` con `switch` por modo | **Reemplazar** por el router + `Screen` de la spec (§3.2). Lógica reutilizable: carga asíncrona por `tea.Cmd`, `busy` por slug, refresco por `tea.Tick`. |
| `views.go` | Render de lista/detalle/logs/backups/ayuda | **Reescribir** con `theme`/`layout`/`components`. Se conservan utilidades puras probadas. |
| `create.go` | Asistente `N` (formulario por pasos secuencial) con llamada a `provision.Create` | **Reemplazar** por `wizard` (pasos + `help` + picker de versión). El formulario actual es un buen esqueleto y su integración con `provision.Request` se mantiene. |
| `styles.go` | Paleta y estilos lipgloss | **Reemplazar** por `internal/tui/theme` (4 temas, sin color, ASCII). |
| `types.go` | `Row`, `Metrics`, `Detail`, `op`, `stateLabel` | **Migrar**: `Row`/`Detail` sirven de base; `stateLabel` pasa al catálogo i18n. |
| `model_test.go`, `views_test.go` | Tests con `fakeBackend` (sin Docker) | **Conservar el patrón** (fakes); los tests se reescribirán sobre `svc`. |

Utilidades puras ya probadas y reutilizables tal cual (se mueven a
`internal/tui/layout` o `components`):

- `wrapLogs`, `stripANSI`, `pinsLabel` (probadas en `views_test.go`).
- El patrón de `bodyHeight`, `moveCursor`, `busy[slug]` y `tea.Tick` de refresco.

### 1.2 Núcleo de creación: `internal/provision`

- `provision.Request` / `provision.Create` es **el único camino de creación**
  (lo usan `cli/setup.go` y `tui/backend.go`). Se mantiene.
- Campos actuales del `Request`: `Name, Engine, Version, Memory, CPU,
  Difficulty, Gamemode, MaxPlayers, Seed, Online, WhitelistEnabled, Whitelist,
  Ops, Motd, Icon, Plugins, Start`.
- El puerto se calcula con `instance.Manager.NextPort(25565)` (solo mira el
  registro local). **No detecta** puertos ocupados por contenedores ajenos,
  procesos del host ni peers del tailnet → aquí entra `internal/ports` (RF-3).
- No persiste información de conexión → aquí entra `internal/connection` (RF-2).

### 1.3 Motores: `internal/engines`

- `Engine` (Name/Label/TypeValue/PluginDir/DepVar), `Order`, `ByName`,
  `Version{Version,Stable,Detail}`, `Resolved{Version,Pins,Detail}`.
- `Provider{Engine,ListVersions,Resolve}` y `Registry`.
- Proveedores: `paper.go` (API v2 PaperMC), `fabric.go` (FabricMC meta),
  `forgeProvider`/`neoforgeProvider` (maven-metadata **JSON**), `vanillaProvider`
  (Mojang `version_manifest_v2.json`).
- **Lo que falta para RF-5:** `ListVersions` devuelve solo `Version` (string);
  no hay `Channel`, `ReleasedAt`, `JavaMajor`, `Build`, `Compatible` ni caché ni
  respaldo embebido. Se ampliará con un `Versions(ctx, engine, opts)` aditivo sin
  romper `ListVersions`/`Resolve` (los usa `engine list` y `provision`).

### 1.4 Runtime y red

- `internal/runtime`: genera `compose.yml` (plantilla con pin de puerto
  `"{{.HostIP}}:{{.Port}}:25565"`), ciclo de vida (`Start/Stop/Close/Restart/
  Down/State/Logs/Console`), `RunningCount`. `HostIP` usa
  `tailscale.ResolveIPv4` (o `127.0.0.1` si Tailscale está desactivado).
- **Deuda relevante para RF-3:** `compose.yml` no fija `container_name` único ni
  publica RCON (bien), pero el arranque no revalida el puerto. Convención de
  nombre de contenedor: `badomens-<slug>`; proyecto compose:
  `badomens-<slug>`.
- `internal/tailscale`: solo `ResolveIPv4` y `InRange`. No hay estado
  (`NeedsLogin`, `Stopped`), ni MagicDNS, ni `status --json` → se amplía para
  RF-2/RF-6.

### 1.5 Diagnóstico: `internal/doctor`

- `Check{Name,Status,Message}`, `Report`, `Run` y `Preflight` (sin capacidad).
- `checkDocker` distingue "no está en PATH" de "el daemon no responde", pero
  **no** distingue el resto de estados de RF-6 (`PermissionDenied`,
  `ComposeMissing`, `WrongContext`, `Starting`, `VirtualizationOff`) ni repara.
  `doctor` se mantiene y reutilizará `internal/dockerx` en Fase 1.

### 1.6 Contrato, errores y ejecución

- `internal/result`: documento estable (`ok, exit_code, action, message, data,
  phases, error, timestamp`). **Se mantiene**; solo campos aditivos en `data`.
- `internal/errx`: códigos estables (`error_general`, `error_usage`,
  `error_not_found`, `error_dependency_missing`, `error_capacity_exceeded`,
  `error_unavailable`, `warn`). **No** existe `error.details` en
  `result.ErrorInfo` → se añadirá `details` (aditivo) para `reason`.
- `internal/execx`: ejecución sin shell + `LookPath`. Es la única vía de
  procesos externos; **regla respetada** y a respetar en los scripts de RF-6.
- `internal/cli`: cobra, un comando por archivo, `Env` con `finish(res, human)`,
  `manager()`, `runtime()`, `specFor()`, `reg()`. Se añaden subcomandos nuevos
  siguiendo ese patrón.
- `internal/tools` + `cli/tools.go`: registro MCP único (hoy 4 herramientas de
  solo lectura). Se añaden las nuevas herramientas aquí.

## 2. Decisiones de la Fase 0 (escritas)

1. **Bubble Tea v1 / Lip Gloss v1 / Bubbles v1** (ver `go.mod`): se **mantiene**
   la versión mayor. No se migra a v2. Riesgo #1 de la spec resuelto.
2. **Un solo `svc.Services`** sustituye a `Backend`; los adaptadores reales
   envuelven los paquetes actuales (`instance`, `runtime`, `backup`, `tps`,
   `provision`, y los nuevos `ports`/`dockerx`/`connection`/`idle`).
3. **La lógica de dominio sale de la TUI**: `ports`, `connection`, `dockerx` e
   `idle` viven en paquetes propios y se exponen por CLI y MCP antes de usarse en
   la TUI (regla de paridad).
4. **Nada de shell**: los "scripts de reparación" de RF-6 son un respaldo
   independiente; la implementación en Go sigue usando `internal/execx`.
5. **Migración de datos aditiva**: `instance.Spec` gana campos (`connection`,
   `idle`, `port_reservation`, `stopped_by`, `stopped_at`) con cero-valores
   seguros; los YAML antiguos siguen cargando sin migración destructiva.
6. **Polling**: el `Poller` central (un `docker ps` + un `docker stats` para
   todas) sustituye los `tea.Tick` por pantalla del modelo actual.

## 3. Deuda detectada (a resolver en esta entrega)

| Deuda | Estado actual | Plan |
|---|---|---|
| Puertos ocupados no detectados | `NextPort` solo mira el registro | `internal/ports` con registro + docker + bind + peers |
| Sin información de conexión | no existe | `internal/connection` + `connectcard` |
| Docker sin diagnóstico fino ni reparación | `doctor` básico | `internal/dockerx` + pantalla Salud |
| Sin auto-apagado | no existe | `internal/idle` + vigilante + servicio |
| Versiones sin metadatos/compatibilidad | `ListVersions` devuelve strings | `engines.Versions` + caché + respaldo embebido |
| Sin descripciones de opciones | textos embebidos | `internal/tui/help` (registro único) |
| Sin catálogo i18n | textos en línea | `internal/tui/i18n` |
| Botones sin contrato | teclas sueltas | `components.Action` + lint de etiquetas |
| Contrato JSON sin `error.details` | `ErrorInfo` sin detalle | campo `details` aditivo |

## 4. Correspondencia con las fases de la spec

- **Fase 0:** este documento + `theme`/`layout`/`anim`/`components`/`svc`/
  `events`/`Poller` + Home responsive.
- **Fase 1:** `dockerx`, `tailscale status/ensure`, pantalla Salud, guardia.
- **Fase 2:** `ports`, `connection`, tarjeta de conexión, CLI/MCP.
- **Fase 3:** `help`, `engines.Versions`, picker, asistente, lint de botones.
- **Fase 4:** `idle` (SLP + RCON), vigilante, demonio/servicio, pestaña.
- **Fase 5:** detalle completo, consola, jugadores, backups, paleta, sparklines.
- **Fase 6:** pulido, temas, ratón, i18n, `--plain`, `vhs`.
