# F4 — TUI: panel multi-instancia

> Estado del plan: **implementado** (F4). La TUI ya está en `internal/tui` y se
> lanza con `badomens tui`.

## Objetivo

Ofrecer al operador un **panel interactivo en el terminal** para supervisar y
gestionar las instancias de badomens sin escribir comandos ni abrir la web. La
TUI es un cliente adicional: **no sustituye ni rompe el contrato JSON** del CLI
(Emiria/Jamcord siguen consumiendo el binario con `--json`).

## Alcance

| Pantalla | Contenido |
|---|---|
| **Lista** | todas las instancias: slug, nombre, motor, versión, RAM, puerto, estado y salud (opcional). Selección con `↑/↓` y acciones por tecla |
| **Detalle** | configuración completa de una instancia (`inspect`) + estado + métricas (CPU/RAM/mundo/backups) |
| **Logs** | tail de logs con scroll (viewport) y recarga manual/auto |
| **Consola** | enviar comandos a la consola del servidor (`mc-send-to-console`) |
| **Backups** | listar, crear y podar copias de seguridad |
| **Ayuda** | resumen de teclas |

## Diseño

- **Librería**: `charmbracelet/bubbletea` v1.3 + `lipgloss` (estilo) + `bubbles`
  (`viewport` para logs, `textinput` para la consola).
- **Sin subprocesos**: la TUI importa directamente los paquetes internos
  (`config`, `instance`, `runtime`, `backup`, `tps`, `engines`) en lugar de
  ejecutar `badomens` como proceso. Todo el estado llega por el mismo backend.
- **Backend como interfaz** (`Backend`): el modelo bubbletea solo conoce la
  interfaz; una implementación real usa Docker/Compose y una de prueba (fake)
  sirve para los tests unitarios sin Docker.
- **Operaciones asíncronas**: cada acción (start/stop/close/restart/backup/
  console) corre en una goroutine y devuelve un mensaje con el resultado; la UI
  muestra "en curso…" mientras tanto y sigue respondiendo.
- **Refresh**: `tea.Tick` de 5 s actualiza la lista (estados en paralelo) sin
  bloquear la terminal. La salud TPS (`doctor tps`) es cara y solo se calcula
  con la tecla `d`.

## Fases del plan

| Fase | Contenido | Estado |
|---|---|---|
| T0 | dependencias (bubbletea/lipgloss/bubbles) | ✅ |
| T1 | `internal/tui`: backend, tipos, modelo, vistas, estilos | ✅ |
| T2 | comando `badomens tui` (TTY) + registro en manifiesto `commands` | ✅ |
| T3 | tests unitarios del modelo con backend fake (sin Docker) | ✅ |
| T4 | verificación E2E/visual con Docker real | ✅ (e2e PASS, visual QA PASS 6/6) |
| T5 | docs: README, AGENTS.md, CHANGELOG, skill | ✅ |

## Contrato y reglas respetadas

- La TUI solo se puede lanzar en una terminal (`isTTY`); en otro caso devuelve
  el error tipado `error_usage` respetando el contrato del binario.
- Las operaciones críticas (start sobre el límite de paralelos) reutilizan la
  misma lógica del CLI: `runtime.RunningCount` contra `cfg.MaxParallel`.
- El flag `--json` no aplica al modo interactivo; el resto de comandos
  conservan su contrato sin cambios.
- No se usa shell: todo proceso externo pasa por `internal/execx`.

## Riesgos y mitigaciones

| Riesgo | Mitigación |
|---|---|
| Terminal no compatible (Windows sin conpty) | bubbletea soporta Windows Terminal; si falla, el error se muestra y se cae al CLI |
| Operaciones largas que bloqueen la UI | todas las operaciones son asíncronas (goroutine → mensaje) |
| Salud TPS cara si se repite | solo bajo demanda (tecla `d`) y cacheada hasta el siguiente refresco |
| Contrato JSON roto | la TUI no toca la salida del binario; `go test ./...` + `make check` lo verifican |