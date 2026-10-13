# Changelog

Todas las novedades relevantes de badomens se documentan aquí. El formato sigue
[Keep a Changelog](https://keepachangelog.com/es/1.1.0/) y el versionado es
semántico.

## [Unreleased]

### Añadido

- **F4 — TUI (panel multi-instancia)**
  - `badomens tui`: panel interactivo en el terminal con `bubbletea` + `lipgloss`
    + `bubbles`. Pantallas: lista (estado en paralelo, selección por teclado),
    detalle completo de una instancia con métricas, logs con scroll (auto-refresh),
    consola (envío de comandos) y backups (crear/podar). Teclas en `?`.
  - Acciones asíncronas (start/stop/close/restart/backup/console) que no
    bloquean la UI; contadores de paralelas/max_instancias en la cabecera;
    salud TPS bajo demanda (`d`).
  - La TUI importa los paquetes internos directamente (no levanta subprocesos)
    y **no toca el contrato JSON** del CLI.
  - Modos para automatización: `--snapshot --view list|detail|logs|backups|help`
    (con `-i <slug>`) y `--force` para salidas no terminales.
  - Tests unitarios del modelo con backend fake (sin Docker).

- **F3 — Integración con Emiria (Jamcord)**
  - Jamcord gana un cliente (`internal/minecraft`) que ejecuta el binario
    `badomens --json` y registra herramientas `badomens_*` (activado solo si el
    binario está disponible, patrón SeedMap).
  - Comando `jamcord mc <subcomando>` (passthrough con el contrato JSON),
    bot de Discord `/mc` (solo lista de permitidos) y MCP con modo
    `--read-only`.
  - La documentación de la integración vive en
    `docs/integracion-jamcord.md` (badomens) y `docs/minecraft-badomens.md`
    (Jamcord).

- **F2 — Doctor TPS y métricas**
  - `badomens doctor tps [--instance]`: mide TPS y MSPT por consola
    (Paper/Spigot), CPU y RAM con `docker stats`, y cuenta eventos de retraso
    ("Can't keep up!") de los logs. Clasifica cada instancia (good/degraded/
    critical), devuelve sugerencias y pone el código de salida agregado
    (0/1/2).
  - `badomens metrics`: CPU/RAM del contenedor, tamaño del mundo y estado de
    backups de una instancia.

- **F1 — Multi-instancia**
  - Registro local de hasta 5 instancias (`setup`, `instance create/list/inspect/remove`)
    y ciclo de vida con Docker Compose por instancia (`start`, `stop`, `close`,
    `restart`), con límite configurable de instancias en paralelo (3 por defecto)
    y detección de capacidad del host.
  - Asistente `setup` interactivo y desatendido: motor, versión, RAM, dificultad,
    modo, máximo de jugadores (recomendado), seed, crackeado/premium, whitelist,
    ops, MOTD, icono y plugins/mods.
  - Catálogos en línea de Paper, Fabric, Forge, NeoForge y Vanilla
    (`engine list/use/current`).
  - Biblioteca compartida de mundos (`world save/use/export/import`) con zips
    y manifiestos verificables.
  - Backups por instancia (`backup create/list/restore/prune`) con sha256.
  - Plugins/mods por motor (`plugin add/list/remove`) con aviso de plugins
    exigentes (riesgo de TPS).
  - Operaciones stop-first: `world`/`backup` detienen la instancia y la
    relanzan si estaba corriendo.
  - Apagado graceful (`close`) que para por consola y evita el relanzado por
    `restart: unless-stopped`.
  - Comandos `logs`, `console`, `access` (whitelist/ops) y `resolve-ip`.

- **F0 — Fundación**
  - Binario único `badomens` en Go con contrato JSON estable
    (`ok`, `exit_code`, `action`, `message`, `data`, `error`, `timestamp`).
  - Comandos `version`, `doctor`, `capacity`, `config`, `update`, `commands`.
  - Detección de capacidad del host (CPU/RAM) a través de Docker.
  - `doctor`: Docker, Docker Compose, Tailscale y coherencia de recursos.
  - `update --check`: comparación semver contra las releases de GitHub.
  - Servidor MCP por stdio (`mcp serve`) sobre un registro único de herramientas,
    con modo `--read-only`.
  - CI (Linux/macOS/Windows), GoReleaser y releases con binarios y checksums.
