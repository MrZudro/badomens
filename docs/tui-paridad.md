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
| `instance list` | `home` (lista de instancias; ya en la TUI actual) |
| `instance inspect` | `screens/instance` (pestañas, Fase 5) |
| `instance create` | `wizard` → crear (mismo núcleo `provision`) |
| `instance start\|stop\|restart` | `home`/`instance` (botones del ciclo de vida) |
| `instance close` | `instance` (apagado con cuidado) |
| `instance remove` | `instance` (modal destructivo con confirmación, Fase 5) |
| `connect` | `connectcard` (tarjeta "Cómo conectarse") |
| `ports check\|list` | `wizard` (validación en vivo) + `screens/health` |
| `engine list\|current\|use\|versions` | `wizard` (picker de motor/versión, Fase 3) + `instance/ajustes` |
| `world list\|save\|use\|export\|import` | `instance/mundos` (Fase 5) |
| `backup create\|list\|restore\|prune` | `instance/backups` (Fase 5) |
| `access whitelist-add\|remove\|ops-add\|remove` | `instance/jugadores` (Fase 5) |
| `plugin add\|list\|remove` | `instance/plugins` (Fase 5) |
| `logs` | `instance/consola` (logs con filtros, Fase 5) |
| `console` | `instance/consola` (línea de comandos con autocompletado, Fase 5) |
| `metrics` | `sparkline` en `home`/`instance` (Fase 5) |
| `resolve-ip` | `connectcard` + `screens/health` |
| `doctor` | `screens/health` (Salud del sistema, `H`) |
| `doctor tps` | `screens/health` + sparklines de TPS |
| `docker status\|ensure` | `screens/health` + modal de reparación de Docker (Fase 1) |
| `tailscale status\|ensure` | `screens/health` + modal de reparación de Tailscale (Fase 1) |
| `idle set\|status\|disable\|daemon` | `instance/auto-apagado` + banner de vigilante (Fase 4) |
| `update` | pantalla Salud (fila "actualización") |
| `uninstall` | `solo-cli` (destructivo; la TUI solo lo enlaza con `--dry-run`) |
| `capacity` | `screens/health` (fila de capacidad) + `wizard` (recomendaciones) |
| `config` | `screens/settings` (ajustes globales, Fase 5) |
| `mcp` | `solo-cli` (servidor por stdio) |
| `tui` | la propia TUI |
| `version` | `solo-cli` (la TUI muestra la versión en la barra de estado) |