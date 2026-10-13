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
badomens --json update            # ¿hay versión nueva?
badomens --json commands          # manifiesto de comandos
badomens --json config            # configuración resuelta
```

## Reglas al ayudar al usuario

1. Usa siempre `--json`.
2. Si algo falla, ejecuta primero `badomens --json doctor`.
3. `doctor` con avisos devuelve exit 1 y `error.code: "warn"`: no es un fallo
   grave; informa y sigue.
4. Antes de operaciones destructivas (borrar instancias, cambiar de motor,
   restaurar backups), confirma con el usuario y ofrece un backup.
5. 🔒 No pongas IPs reales, seeds ni tokens en archivos versionados; usa
   ejemplos genéricos.

> En F0 solo hay comandos de lectura. La gestión de instancias llega en F1 y la
> integración con Emiria en F3.
