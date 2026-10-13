# 🧱 badomens

**Gestor de varios servidores de Minecraft en un solo binario.** Eliges motor
(Paper, Fabric, Forge, NeoForge o Vanilla), versión, recursos, mundo y seed, y
badomens levanta cada servidor en Docker Compose, aislado y publicado solo en tu
red privada (Tailscale).

> 🔒 **Solo para ti y tu gente:** los puertos se publican únicamente en la IP del
> tailnet, nunca en todas las interfaces.

Esta es la reescritura en **Go** del proyecto Bad Omens (antes en Python). El
objetivo es un binario precompilado, sin interfaz web, **CLI primero** y TUI
después, y **operable desde Emiria** (el asistente Jamcord) por CLI, bot de
Discord, agente de IA y MCP.

---

## Estado

Este repositorio está en construcción por fases. Lo que ya funciona:

| Fase | Contenido | Estado |
|---|---|---|
| **F0** Fundación | estructura, contrato JSON, `version`, `doctor`, `capacity`, `update`, MCP, CI y releases | ✅ |
| **F1** Multi-instancia | `instance` (hasta 5, 3 en paralelo), `setup`, motores, mundos, backups, plugins, planificador | ✅ |
| **F2** Doctor TPS + updates | `doctor tps`, `metrics`, `update --check` | ✅ |
| **F3** Integración Emiria | `jamcord mc`, herramientas `badomens_*`, bot `/mc` | ✅ |
| **F4** TUI | panel multi-instancia | ⏳ |
| **F5** Distribución | instalador de un comando (`scripts/install.sh`/`.ps1`) + releases precompiladas | ✅ `installer`, TUI pendiente |

> La especificación del MVP vive aparte. Este README describe lo que ya existe y
> lo que viene.

---

## Requisitos

| Herramienta | Para qué | Cómo instalarla |
|---|---|---|
| **Docker + Compose** | corre cada servidor en un contenedor | [docker.com](https://www.docker.com/) |
| **Tailscale** | red privada entre tú y tus amigos | [tailscale.com](https://tailscale.com/) |

badomens es un único binario: no necesitas Go ni Python para usarlo.

---

## Instalación

Descarga el binario de la [última release](https://github.com/manuelserver/badomens/releases)
para tu sistema, o usa el instalador de un comando:

```bash
# macOS / Linux
curl -fsSL https://github.com/manuelserver/badomens/releases/latest/download/install.sh | sh

# Windows (PowerShell)
irm https://github.com/manuelserver/badomens/releases/latest/download/install.ps1 | iex
```

Los instaladores descargan el binario, **verifican el checksum** contra
`checksums.txt` y lo dejan listo (`/usr/local/bin/badomens` o
`%LOCALAPPDATA%\Programs\badomens`).

O compílalo desde el código (necesitas Go 1.27+):

```bash
go install github.com/manuelserver/badomens/cmd/badomens@latest
```

---

## Inicio rápido

```bash
badomens doctor                 # ¿está todo listo? Docker, Tailscale, config
badomens capacity               # cuánta CPU/RAM tienes y cuántas instancias caben
badomens config --init          # crea la configuración global

# Crea y arranca una instancia (interactivo si no pasas flags)
badomens setup --name "Survival" --engine paper --memory 6G --no-start
badomens -i survival start
badomens -i survival logs
badomens -i survival backup create
badomens instance close survival
```

Puedes tener hasta 5 instancias registradas y 3 corriendo en paralelo (ajustable
en `config.yaml`). Cada una tiene su mundo, motor, versión y puerto. Todos los
comandos aceptan `--json` para una salida estable.

---

## Contrato de salida

Cada comando imprime **un único documento JSON** en stdout:

```json
{
  "ok": true,
  "exit_code": 0,
  "action": "doctor",
  "message": "doctor terminó sin problemas",
  "data": { "checks": [ ... ] },
  "timestamp": "2026-01-01T00:00:00Z"
}
```

- En caso de error: `ok: false` y `error: {code, message, hint}`.
- Códigos de salida: **0** éxito · **1** aviso · **2** error.
- Los códigos de error (`error.code`) son estables: `error_usage`,
  `error_not_found`, `error_dependency_missing`, `error_capacity_exceeded`,
  `error_unavailable`, `error_general`.

Este contrato es lo que consume Emiria/Jamcord.

---

## Comandos

| Comando | Función |
|---|---|
| `badomens setup` | asistente interactivo/desatendido de configuración |
| `badomens instance list/inspect/create` | registro de instancias |
| `badomens instance start/stop/close/restart/remove` | ciclo de vida (start respeta el límite de paralelos) |
| `badomens engine list/current/use` | catálogo y selección de motor/versión |
| `badomens world list/save/use/export/import` | biblioteca compartida de mundos |
| `badomens backup create/list/restore/prune` | copias de seguridad por instancia |
| `badomens access whitelist-add/…/ops-add/…` | whitelist y operadores vía consola |
| `badomens plugin add/list/remove` | plugins/mods según motor (con aviso de TPS) |
| `badomens logs` / `badomens console <cmd>` | logs y consola de una instancia |
| `badomens resolve-ip` | IP de publicación del host |
| `badomens doctor tps` | diagnóstico de TPS/MSPT de las instancias |
| `badomens metrics` | métricas de una instancia (CPU/RAM/mundo/backups) |
| `badomens version` | versión del binario |
| `badomens doctor` | diagnóstico del entorno |
| `badomens capacity` | CPU/RAM del equipo y plan de instancias |
| `badomens config [--init]` | configuración global |
| `badomens update [--check]` | ¿hay una versión nueva? |
| `badomens mcp serve [--read-only]` | servidor MCP por stdio |
| `badomens commands` | manifiesto de comandos (JSON) |

---

## Integración con Emiria (Jamcord)

badomens es autónomo. Jamcord lo consume ejecutando el binario y leyendo su JSON
estable, igual que hace con SeedMap. Así Emiria puede gestionar los servidores
por lenguaje natural (Discord/agente), por `/mc` y por MCP. Detalle en
`docs/integracion-jamcord.md`.

---

## Desarrollo

```bash
make build     # compila bin/badomens
make test      # pruebas unitarias
make lint      # golangci-lint
make run       # ejecuta en desarrollo
```

---

## Licencia

MIT. Ver `LICENSE`, `CONTRIBUTING.md` y `SECURITY.md`. Proyecto no afiliado a
Mojang, Microsoft, PaperMC ni a los demás proyectos de servidores mencionados.
