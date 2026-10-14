---
name: badomens
description: Gestiona los servidores de Minecraft locales (Bad Omens) con el CLI badomens. Úsala para consultar el estado del entorno, la capacidad del equipo y si hay actualizaciones, o para preparar el arranque de un servidor (Paper, Fabric, Forge, NeoForge, Vanilla). Ejecuta siempre con --json y delega en el binario; nunca edites compose ni config a mano.
---

# Skill delgada — badomens (servidores de Minecraft)

badomens es un binario único que gestiona instancias de servidor Minecraft en
Docker Compose, publicadas solo en la red privada (Tailscale). No edites los
archivos de configuración a mano: usa siempre el CLI.

## Contrato

Cada comando imprime **un único documento JSON** en stdout y sale con 0 (OK),
1 (aviso) o 2 (error). Claves: `ok`, `exit_code`, `action`, `message`, `data`,
`error`, `timestamp`. Ante un fallo, `error.code` es estable.

## Comandos frecuentes

```bash
badomens --json doctor            # diagnóstico: Docker, Tailscale, config
badomens --json capacity          # CPU/RAM del equipo y plan de instancias
badomens --json update            # ¿hay versión nueva? (--apply la descarga y reemplaza)
badomens --json commands          # manifiesto de comandos
badomens --json config            # configuración resuelta
badomens --json uninstall         # desinstalación (--dry-run para previsualizar)
badomens --json connect --instance <slug>    # información de conexión (dirección, MagicDNS)
badomens --json ports check 25565 # puerto libre (registro, docker, host, tailnet)
badomens --json engine versions paper        # versiones del motor (RF-5)
badomens --json docker status     # estado de Docker (daemon, compose, contexto)
badomens --json docker ensure     # intenta reparar Docker y espera a que responda
badomens --json tailscale status  # estado de la red privada
badomens --json tailscale ensure  # intenta levantar la red privada
badomens --json --instance <slug> idle status  # auto-apagado por inactividad
badomens --json --instance <slug> idle set --timeout 30m   # configurar auto-apagado
badomens --json idle install-service           # vigilante como servicio del SO
badomens --json setup --explain memory         # ayuda larga de un campo
```

## Herramientas MCP

`badomens_version`, `badomens_capacity`, `badomens_doctor`, `badomens_update_check`,
`badomens_connection`, `badomens_ports_check`, `badomens_engine_versions`,
`badomens_docker_status`, `badomens_tailscale_status`, `badomens_idle` (alias
`badomens_idle_status`).

## Reglas al ayudar al usuario

1. Usa siempre `--json`.
2. Si algo falla, ejecuta primero `badomens --json doctor`.
3. `doctor` con avisos devuelve exit 1 y `error.code: "warn"`: no es un fallo
   grave; informa y sigue.
4. Antes de operaciones destructivas (borrar instancias, cambiar de motor,
   restaurar backups), confirma con el usuario y ofrece un backup.
5. 🔒 No pongas IPs reales, seeds ni tokens en archivos versionados; usa
   ejemplos genéricos.

> La integración con Emiria/Jamcord es la F3. La F0 dejó listo el contrato y el
> servidor MCP.

## TUI (humanos)

`badomens tui` abre un panel interactivo con Home (lista + detalle + actividad),
detalle de instancia por pestañas (Resumen, Consola, Jugadores, Backups, Mundos,
Plugins/Mods, Red, Auto-apagado, Ajustes), asistente de creación con selector de
versión por motor, pantalla "Salud del sistema" (`H`) y paleta de comandos
(`Ctrl+P`). Es interactivo: los agentes deben seguir usando los comandos con
`--json`.

Para capturas y pruebas existe:
`--snapshot --view list|detail|logs|backups|players|net|idle|settings|health|wizard|palette|help`
con `--width`/`--height`, y `--plain` para un resumen lineal (accesibilidad).
La apertura comprueba actualizaciones (una vez cada 12 h) y pregunta antes de
cambiar nada.
