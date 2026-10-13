# 📜 Changelog

Todas las novedades relevantes de **badomens** se documentan aquí ✨.
El formato sigue [Keep a Changelog](https://keepachangelog.com/es/1.1.0/) y el
versionado es [semántico](https://semver.org/lang/es/).

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

[v0.1.0-rc.0]: https://github.com/MrZudro/badomens/releases/tag/v0.1.0-rc.0