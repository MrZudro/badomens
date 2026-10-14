# Integración con Emiria (Jamcord)

badomens y Jamcord son proyectos independientes. La integración es por
**subproceso**: Jamcord ejecuta el binario `badomens` y lee su contrato JSON
estable. Así cada proyecto tiene su release, su CI y su instalación, y ninguno
depende del otro para funcionar. **Estado: implementada** (F3).

```
Emiria (Jamcord)                        badomens
  ├─ CLI   jamcord mc <sub>              ├─ CLI   badomens <sub> --json
  ├─ Bot   /mc <sub>        ── exec ──▶  ├─ MCP   badomens mcp serve
  ├─ Agente (registro badomens_*)        └─ (F4) TUI
  └─ MCP
```

## Configuración en Jamcord

- `BADOMENS_BIN` (opcional): ruta al binario. Si falta, se busca `badomens` en
  el `PATH`.
- La integración se registra **solo si el binario está disponible**, siguiendo el
  patrón de SeedMap (registro condicional).
- Las herramientas se exponen en el registro único de Jamcord:
  `badomens_instances_list`, `badomens_instance_create`,
  `badomens_instance_start|stop|restart|close`, `badomens_status`,
  `badomens_logs`, `badomens_players`, `badomens_console`,
  `badomens_world_*`, `badomens_backup_*`, `badomens_whitelist_*`,
  `badomens_ops_*`, `badomens_plugin_*`, `badomens_doctor_tps`,
  `badomens_update_check`.
- **Nuevas (TUI v2)**: `badomens_connection` (RF-2), `badomens_ports_check`
  (RF-3), `badomens_engine_versions` (RF-5), `badomens_docker_status` (RF-6),
  `badomens_tailscale_status` (RF-6) y `badomens_idle` (alias
  `badomens_idle_status`, RF-8).
- Las de solo lectura se marcan `ReadOnly` para el modo `mcp serve --read-only`.
- Las acciones destructivas piden confirmación humana en Discord.

## Por qué subproceso y no HTTP

- **Reproducible**: el contrato es el JSON del CLI, no un protocolo propio.
- **Sin proceso encendido**: badomens no necesita un daemon escuchando.
- **Aislado**: un fallo de badomens no tumba a Emiria y viceversa.
- **Simple de instalar**: un binario por lado.

## Estados

Esta integración es la **Fase 3**. La Fase 0 (este repositorio) deja listo el
contrato y el servidor MCP para que el puente sea fino.
