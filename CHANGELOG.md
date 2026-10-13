# 📜 Changelog

Todas las novedades relevantes de **badomens** se documentan aquí ✨.
El formato sigue [Keep a Changelog](https://keepachangelog.com/es/1.1.0/) y el
versionado es [semántico](https://semver.org/lang/es/).

## [Unreleased] — TUI v2 (dominio, paridad CLI/MCP y cimientos)

> 🧱 Primera entrega de la `SPEC-tui-v2.md` (Fases 0–4 parciales): paquetes de
> dominio nuevos (puertos, conexión, Docker, auto-apagado, eventos), paridad
> CLI/MCP para todo lo nuevo y el catálogo de ayudas del asistente.

### ➕ Añadido

- **`internal/ports` (RF-3)**: detección de puertos ocupados en 4 fuentes
  (registro, Docker, bind local, peers de la tailnet), rango configurable
  (`ports.range`), `tailnet_scan: off|warn|block`, reserva atómica con bloqueo
  de archivo, revalidación y sugerencia del siguiente libre. Comandos
  `badomens ports check|list` y tool MCP `badomens_ports_check`.
- **`internal/connection` (RF-2)**: cálculo de la dirección de conexión,
  MagicDNS, loader y pasos para el jugador; se **persiste** en el registro y se
  **recalcula en cada arranque**. Comando `badomens -i <n> connect [--copy]`,
  campo `data.connection` en `instance inspect|start|create` y tool MCP
  `badomens_connection`.
- **`internal/dockerx` (RF-6)**: diagnóstico tipado (8 estados) y plan de
  reparación por plataforma con espera activa; `badomens docker status|ensure
  [--wait] [--print-script]`, scripts de respaldo `scripts/ensure-docker.{sh,ps1}`
  y tool MCP `badomens_docker_status`.
- **`internal/idle` (RF-8)**: máquina de estados con reloj inyectable, detección
  por Server List Ping (y RCON como respaldo), parada limpia y vigilante con
  pidfile (`badomens -i <n> idle set|status|disable`, `badomens idle daemon` y
  tool MCP `badomens_idle_status`).
- **`internal/events`**: bus de eventos de dominio ligero (estado, jugadores,
  auto-apagado, Docker/Tailscale, backups).
- **`internal/engines` (RF-5)**: `Version` enriquecida (canal, fecha, Java,
  recomendada, build), caché en disco con `stale-while-revalidate` y respaldo
  embebido; snapshots de Vanilla vía `engine versions --all`.
- **`internal/tui/help` (RF-4)**: registro único de descripciones de opciones
  (corta ≤ 90 y larga) que alimenta el asistente, los ajustes y
  `badomens setup --explain <campo>`.
- **Cimientos de la TUI v2 (Fase 0.2)**: `internal/tui/theme` (4 temas + test de
  contraste WCAG AA), `internal/tui/layout` (breakpoints y truncado por ancho
  real), `internal/tui/i18n` (catálogo es), `internal/tui/components`
  (`Action` + lint de botones, RF-7), `internal/tui/anim` (tick global sin
  ticks en reposo, §4.3), `internal/tui/svc` (fachada de servicios + fakes) y
  `internal/tui/router.go` (interfaz `Screen` + pila/overlay).
- **Contrato JSON**: `error.details` (aditivo) con `details.reason` estable; el
  resto de cambios nuevos son campos aditivos en `data` (ver
  `docs/contrato-json.md`).
- **Documentación**: `docs/tui-auditoria.md`, `docs/tui-paridad.md` y el test de
  regla que exige paridad CLI↔TUI para cada subcomando.

### 🧹 Pendiente

- Rediseño de la TUI (`theme`, `layout`, `anim`, `components`, `screens`,
  `i18n`) y las fases 1–6 de la spec: la TUI actual queda intacta y funcional.

## [v0.1.0-rc.2] — 2026-10-13

> 🔧 Segunda pre-release con **correcciones de distribución**: el instalador de
> un comando y `update --apply` vuelven a funcionar (ya no dependen de
> `/releases/latest` ni de nombres de asset sin versión), y los instaladores se
> publican como assets de la release.

### 🔧 Correcciones

- **Instalador de un comando**: usa `raw.githubusercontent.com` (las URLs
  `releases/latest` web y API ignoran las pre-releases) y resuelve la versión
  vía `releases?per_page=1`. En Windows añade el directorio al **PATH de
  usuario** automáticamente (y `uninstall.ps1` lo quita al desinstalar).
- **Assets de release**: GoReleaser nombra los assets con la versión
  (`badomens_<v>_<os>_<arch>.<ext>`); los instaladores y `update --apply` ahora
  lo respetan (antes descargaban 404).
- **`update` / `update --apply` / check de la TUI**: consultan el listado de
  releases en vez de `/releases/latest` (que devolvía 404 al haber solo
  pre-releases) y descargan con el nombre de asset correcto.
- **GoReleaser**: `install.sh` e `install.ps1` se publican como assets
  (`extra_files`) para que la URL `…/releases/latest/download/…` funcione
  cuando exista una release **estable**.

## [v0.1.0-rc.1] — 2026-10-13

> 🚀 Segunda **pre-release** de badomens: la TUI crea instancias y avisa de sus
> requisitos, y el binario puede **actualizarse y desinstalarse** desde el CLI.
> Mira [la release](https://github.com/MrZudro/badomens/releases) para descargar
> binarios verificados.

### ➕ Añadido

- **🖥️ TUI**: asistente de creación de instancias con **paridad total** con
  `badomens setup` (tecla `N`): nombre, motor, versión, RAM, CPU, dificultad,
  modo, jugadores, premium, whitelist+usuarios, ops, semilla, MOTD y plugins.
- **⚠️ Requisitos en la TUI**: preflight al abrir (directorios, Docker/Compose,
  Tailscale), banner con lo que falla y pantalla detallada en la ayuda (tecla
  `R`; `--no-preflight` para saltarlo).
- **🔄 `update --apply`**: descarga la última release, **verifica el sha256** y
  reemplaza el binario (con confirmación). Renombre a `.old` en Windows.
- **🔔 Check de actualización al abrir la TUI**: pregunta si actualizar, con
  throttle por caché (12 h) y recordatorio de versión descartada
  (`update_check`, `BADOMENS_UPDATE_CHECK`, `--no-preflight`).
- **🧹 `badomens uninstall`**: binario + configuración (conserva los datos);
  `--purge` borra también instancias/mundos/backups; `--dry-run` previsualiza.
  Scripts `scripts/uninstall.sh` y `scripts/uninstall.ps1`.
- **🧩 `internal/provision`**: núcleo compartido de creación entre `setup` y la
  TUI (una sola validación, sin duplicar lógica).

### 🔧 Otros

- `doctor` expone `Preflight` (subconjunto de comprobaciones para la TUI).
- Documentación actualizada: README, `docs/setup-install.md`, `docs/tui-plan.md`
  y `AGENTS.md`.

## [v0.1.0-rc.0] — 2026-10-13

> 🧱 Primera **pre-release** de badomens: el binario ya cubre todo el plan
> (F0–F5). Mira [la release](https://github.com/MrZudro/badomens/releases) para
> descargar binarios verificados.

### ➕ Añadido

- **🏗️ F0 — Fundación**
  - Binario único en Go con **contrato JSON estable** (`ok`, `exit_code`,
    `action`, `message`, `data`, `error`, `timestamp`).
  - Comandos `version`, `doctor`, `capacity`, `config`, `update`, `commands`.
  - Servidor MCP por stdio y registro único de herramientas, con `--read-only`.
  - CI (Linux/macOS/Windows), GoReleaser y releases con checksums.

- **🧩 F1 — Multi-instancia**
  - Hasta **5 instancias** (`setup`, `instance create/list/inspect/remove`) y
    **3 en paralelo** por defecto.
  - Asistente `setup` interactivo y desatendido (motor, versión, RAM, mundo,
    seed, whitelist, ops, MOTD, icono, plugins…).
  - Motores Paper/Fabric/Forge/NeoForge/Vanilla, biblioteca de mundos, backups
    con sha256, plugins según motor y apagado *graceful* (`close`).
  - Comandos `logs`, `console`, `access` (whitelist/ops) y `resolve-ip`.

- **🩺 F2 — Doctor TPS y métricas**
  - `doctor tps [--instance]`: TPS/MSPT por consola, CPU/RAM por `docker stats`
    y recuento de eventos de retraso. Clasifica good/degraded/critical.
  - `metrics`: CPU/RAM del contenedor, tamaño del mundo y estado de backups.

- **🤖 F3 — Integración con Emiria (Jamcord)**
  - Cliente Jamcord que ejecuta `badomens --json` y registra herramientas
    `badomens_*`; comando `jamcord mc`, bot `/mc` y MCP con `--read-only`.

- **🖥️ F4 — TUI (panel multi-instancia)**
  - `badomens tui`: panel interactivo con lista, detalle, logs, consola,
    backups y ayuda (bubbletea + lipgloss + bubbles).
  - Operaciones asíncronas que no bloquean la UI y salud TPS bajo demanda.
  - `--snapshot --view list|detail|logs|backups|help` y `--force` para pruebas
    sin terminal. La TUI no toca el contrato JSON.

- **📦 F5 — Distribución**
  - Instalador de un comando (`install.sh` / `install.ps1`) con verificación de
    checksum y releases precompiladas para Windows/macOS/Linux (amd64 y arm64).

### 🔧 Otros

- **🚚 Repositorio**: el proyecto se publica ahora en
  [`MrZudro/badomens`](https://github.com/MrZudro/badomens) (módulo Go y URLs
  de instalación actualizados).
- **📖 Documentación**: README y docs reescritos con estilo amigable, emojis y
  Markdown de GitHub (badges, admonitions, diagramas y tablas).
- **🚀 Workflows**: el pipeline de release crea la *prerelease* para etiquetas
  semánticas de pre-lanzamiento (ver `.goreleaser.yaml` y `release.yml`).

## [Unreleased]

### ⏳ Próximos pasos

- Ideas, mejoras y correcciones tras el primer uso en producción. Abre un issue
  en [el repositorio](https://github.com/MrZudro/badomens/issues) 🐛.

---

[v0.1.0-rc.2]: https://github.com/MrZudro/badomens/releases/tag/v0.1.0-rc.2
[v0.1.0-rc.1]: https://github.com/MrZudro/badomens/releases/tag/v0.1.0-rc.1
[v0.1.0-rc.0]: https://github.com/MrZudro/badomens/releases/tag/v0.1.0-rc.0