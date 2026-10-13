# AGENTS.md — Guía para agentes de IA

Referencia rápida para cualquier agente (o persona) que trabaje en este
repositorio. Léela antes de modificar código o ejecutar comandos.

## Qué es esto

**badomens** es un binario único en Go que gestiona **varios servidores de
Minecraft** en Docker Compose (`itzg/minecraft-server`) + Tailscale. Está en
fases: F0 (fundación) está completa; F1 (multi-instancia), F2 (doctor TPS),
F3 (integración con Emiria/Jamcord), F4 (TUI) y F5 (distribución) están
pendientes (ver `README.md`).

## Comandos esenciales

```bash
go build ./...                 # compilar
go test ./...                  # pruebas
go run ./cmd/badomens doctor   # diagnóstico
go run ./cmd/badomens capacity # capacidad del equipo
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
internal/cli/            comandos cobra (root, doctor, update, mcp, …)
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
```

## Reglas

- **No uses shell**: los procesos externos se lanzan con argumentos separados
  (`internal/execx`).
- Cambios incompatibles al contrato JSON se versionan y documentan.
- Los datos locales (instancias, mundos, backups, `.env`) **no** se suben.
- Comentarios y docs en español. `gofmt -w .` antes de commitear.
- En Windows, compila las pruebas con `-ldflags=-w` si Smart App Control
  bloquea los binarios recién compilados (`make test` ya lo hace).