# Tabla de paridad CLI ↔ TUI (RF-1)

Regla: *si se puede hacer en la TUI, se puede hacer en el CLI* (y al revés). Un
test recorre `badomens commands --json` y falla si un subcomando no tiene fila en
esta tabla (o no está marcado explícitamente `solo-cli` / `pendiente`).

Leyenda de la columna **TUI**:

- `nombre` → pantalla/acción existente o planificada de la TUI v2.
- `solo-cli` → intencionalmente solo CLI (servidor MCP, versión, etc.).
- `pendiente (Fase N)` → paridad planificada en la fase N de `SPEC-tui-v2.md`.

| Comando CLI | Pantalla/acción TUI |
|---|---|
| `setup` | `wizard` (asistente rápido/avanzado, Fase 3) |
| `instance list` | `home` (lista de instancias con jugadores/RAM/ip:puerto y límites) |
| `instance inspect` | `instance` (pestaña Resumen + tarjeta de conexión) |
| `instance create` | `wizard` → crear (mismo núcleo `provision`) |
| `instance start\|stop\|restart` | `home`/`instance` (botones y teclas `s`/`p`/`r`) |
| `instance close` | `instance` (acción "Detener" que guarda el mundo) |
| `instance remove` | `instance` (modal destructivo con confirmación, Fase 5) |
| `connect` | `connectcard` (tarjeta "Cómo conectarse", tecla `c` copia) |
| `ports check\|list` | `wizard` + `instance/Red` (comprobación del puerto) |
| `engine list\|current\|use\|versions` | `wizard` (picker de motor/versión) + `instance/Ajustes` |
| `world list\|save\|use\|export\|import` | `instance/Mundos` (resumen + CLI enlazado) |
| `backup create\|list\|restore\|prune` | `instance/Backups` (`b` crea, `x` restaura, `X` poda) |
| `access whitelist-add\|remove\|ops-add\|remove` | `instance/Jugadores` (`a` añade, `o` op) |
| `plugin add\|list\|remove` | `instance/Plugins/Mods` (resumen + CLI enlazado) |
| `logs` | `instance/Consola` (logs con filtro `/` y seguir `f`) |
| `console` | `instance/Consola` (autocompletado `Tab`, historial ↑↓, aviso de peligro) |
| `metrics` | `sparkline` en `home`/`instance` (Fase 5) |
| `resolve-ip` | `connectcard` + `screens/health` |
| `doctor` | `health` (Salud del sistema, `H`) |
| `doctor tps` | `health` + sparklines de TPS |
| `docker status\|ensure` | `health` + reparación de Docker (`H` → `D`) |
| `tailscale status\|ensure` | `health` + reparación de Tailscale (`H` → `T`) |
| `idle set\|status\|disable\|daemon\|install-service\|uninstall-service` | `instance/Auto-apagado` + banner de vigilante |
| `update` | pantalla Salud (fila "actualización") |
| `uninstall` | `solo-cli` (destructivo; la TUI solo lo enlaza con `--dry-run`) |
| `capacity` | `screens/health` (fila de capacidad) + `wizard` (recomendaciones) |
| `config` | `screens/settings` (ajustes globales, Fase 5) |
| `mcp` | `solo-cli` (servidor por stdio) |
| `tui` | la propia TUI |
| `version` | `solo-cli` (la TUI muestra la versión en la barra de estado) |