# AGENTS.md — Guía para agentes de IA

Referencia rápida para cualquier agente (o persona) que trabaje en este
repositorio. Léela antes de modificar código o ejecutar comandos.

## Qué es esto

**badomens** es un binario único en Go que gestiona **varios servidores de
Minecraft** en Docker Compose (`itzg/minecraft-server`) + Tailscale. Está en
fases: F0–F3 y F5 están completas; **F4 (TUI) se añadió también y está
implementada** (ver `README.md`).

## Comandos esenciales

```bash
go build ./...                 # compilar
go test ./...                  # pruebas
go run ./cmd/badomens doctor   # diagnóstico
go run ./cmd/badomens setup --name demo --engine paper --no-start
go run ./cmd/badomens instance list
go run ./cmd/badomens -i demo start   # arranca la instancia
go run ./cmd/badomens tui             # panel interactivo (TUI) — no usar en CI
go run ./cmd/badomens update --apply  # descarga+verifica+reemplaza el binario
go run ./cmd/badomens uninstall       # desinstala (conserva datos sin --purge)
```

## Contrato (importante)

Cada comando imprime **un único documento JSON** con `--json` y sale con
`0` OK, `1` aviso, `2` error. Claves estables: `ok`, `exit_code`, `action`,
`message`, `data`, `phases`, `error`, `timestamp`. No rompas este contrato:
Emiria/Jamcord y la documentación dependen de él. Detalle en
`docs/contrato-json.md`.

## Estructura

```
cmd/badomens/            entrada del binario
internal/cli/            comandos cobra (root, engine, instance, setup, world,
                         backup, plugin, access, logs/console, doctor, update…)
internal/tools/          registro único de herramientas (MCP)
internal/mcp/            servidor MCP por stdio
internal/doctor/         diagnóstico del entorno
internal/host/           capacidad del equipo (CPU/RAM)
internal/update/         chequeo de releases
internal/config/         configuración global (YAML + env + flags)
internal/execx/          ejecución de procesos sin shell
internal/errx/           errores tipados con código estable
internal/result/         documento de salida (contrato JSON)
internal/output/         impresora humano/JSON
internal/engines/        catálogos Paper/Fabric/Forge/NeoForge/Vanilla
internal/instance/       registro y validación de instancias
internal/runtime/        compose por instancia y ciclo de vida Docker
internal/tailscale/      resolución de la IP del tailnet
internal/worlds/         biblioteca de mundos (zip/manifiestos)
internal/backup/         copias de seguridad (manifest + sha256)
internal/plugins/        instalación de plugins/mods según motor
internal/setup/          recomendaciones del asistente de creación
internal/provision/      núcleo de creación de instancias (CLI + TUI)
internal/tps/            diagnóstico de TPS/MSPT y métricas del contenedor
internal/tui/            panel interactivo multi-instancia (bubbletea)
internal/update/         chequeo de releases + descarga verificada (Apply) + caché
internal/uninstall/      desinstalación del binario y la configuración
```

## Reglas

- **No uses shell**: los procesos externos se lanzan con argumentos separados
  (`internal/execx`).
- Cambios incompatibles al contrato JSON se versionan y documentan.
- Los datos locales (instancias, mundos, backups, `.env`) **no** se suben.
- Comentarios y docs en español. `gofmt -w .` antes de commitear.
- En Windows, compila las pruebas con `-ldflags=-w` si Smart App Control
  bloquea los binarios recién compilados (`make test` ya lo hace).