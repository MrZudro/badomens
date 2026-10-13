# 📜 Changelog

Todas las novedades relevantes de **badomens** se documentan aquí ✨.
El formato sigue [Keep a Changelog](https://keepachangelog.com/es/1.1.0/) y el
versionado es [semántico](https://semver.org/lang/es/).

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

[v0.1.0-rc.1]: https://github.com/MrZudro/badomens/releases/tag/v0.1.0-rc.1
[v0.1.0-rc.0]: https://github.com/MrZudro/badomens/releases/tag/v0.1.0-rc.0