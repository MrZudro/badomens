# Guía de la TUI v2 de badomens

La TUI (`badomens tui`) es un panel interactivo con el que puedes gestionar
**todo** lo que hace el CLI, sin escribir comandos. Sigue el rediseño descrito en
`SPEC-tui-v2.md`.

## Abrir y salir

```bash
badomens tui                 # abre el panel en tu terminal
badomens tui --theme light   # dark | light | high-contrast | colorblind
badomens tui --ascii         # iconos y bordes solo ASCII ([OK], +-|)
badomens tui --no-animations # movimiento reducido (sin pulso ni transiciones)
badomens tui --plain         # resumen lineal (lectores de pantalla / no-TTY)
```

- `q` o `Ctrl+C` salen; si hay una operación en curso, se pide confirmación.
- `?` abre la hoja de atajos; `Esc` la cierra.
- `Ctrl+P` (o `:`) abre la **paleta de comandos** (búsqueda difusa de todas las
  acciones; se ejecutan al pulsar `Enter`).

## Atajos globales

| Tecla | Acción |
|---|---|
| `↑↓` / `k` `j` | mover por listas y campos |
| `Enter` | abrir / confirmar / editar |
| `Esc` | volver / cerrar / descartar toasts |
| `Tab` / `Shift+Tab` | cambiar de panel, pestaña o paso |
| `n` | nueva instancia (asistente) |
| `H` | Salud del sistema |
| `D` | reparar Docker (cuando está caído) |
| `c` | copiar la dirección de conexión |
| `?` · `Ctrl+P` · `q` | ayuda · comandos · salir |

La barra inferior muestra siempre la ayuda global y la **pista** de la acción
enfocada.

## Home (dashboard)

- Lista de instancias con insignia de estado, nombre, `jugadores/máx`, RAM
  usada/asignada, `ip:puerto` y la cuenta atrás de auto-apagado.
- Cabecera con el recuento `INSTANCIAS (n/5 · r/3 en marcha)`.
- En terminales **anchas** (≥ 110) hay tres zonas: lista · detalle · **actividad**
  (últimas líneas de log). En **compacto** solo la lista; `Enter` abre el detalle.
- Estado vacío: botón `Crear mi primer servidor` y comprobación de requisitos
  (Docker/Tailscale) con su botón de reparación.

## Detalle de instancia

Nueve pestañas (`Tab` / `←` `→`):

1. **Resumen** — motor, memoria, puerto, jugadores, estado y tarjeta "Cómo
   conectarse".
2. **Consola** — logs con filtro (`/`), seguir (`f`), pausa (`p`), envío de
   comandos con autocompletado (`Tab`) e historial (`↑↓`). Los comandos
   peligrosos (`stop`, `op`, `ban`, `whitelist off`…) piden confirmación.
3. **Jugadores** — conectados, lista blanca y operadores; `a` añade a la lista
   blanca y `o` hace operador.
4. **Backups** — `b` crea, `x` restaura (con backup previo automático) y
   `Shift+X` poda.
5. **Mundos** — semilla y enlace al CLI de mundos.
6. **Plugins/Mods** — instalados y enlace al CLI.
7. **Red** — dirección de conexión y estado del puerto; `R` recomprueba.
8. **Auto-apagado** — presets (5 min…2 h, Nunca), backup antes de apagar y
   prueba de detección.
9. **Ajustes** — edición con la misma ayuda del asistente y marca "Requiere
   reiniciar".

## Asistente de nueva instancia

- **Rápida (3 preguntas)**: nombre, motor+versión y memoria.
- **Avanzada (7 pasos)**: Básico · Mundo · Jugadores · Recursos · Red ·
  Automatización · Revisar.
- Cada campo muestra su **descripción corta** siempre; `?` abre la larga.
- El **selector de versión** solo ofrece versiones que el motor publica de
  verdad; la ★ es la recomendada, las incompatibles aparecen atenuadas con su
  motivo y `s` muestra/oculta snapshots.
- **Aviso P-05**: con el modo online desactivado y sin lista blanca, el asistente
  advierte del riesgo de suplantación y ofrece activar la lista blanca (`W`).

## Salud del sistema (`H`)

Filas de Docker, Compose, Tailscale, puertos, disco (avisa < 10 GB, bloquea
< 2 GB), RAM asignada, actualización disponible y límites del plan. Cada fila con
su botón de reparación: `D` Docker, `T` Tailscale, `R` recomprobar.

## Auto-apagado por inactividad

Configúralo por instancia en la pestaña **Auto-apagado**. Para que funcione con
la TUI cerrada, arranca el vigilante como servicio:

```bash
badomens idle daemon                 # vigilante en primer plano
badomens idle install-service        # systemd (Linux) / launchd (macOS) / tarea (Windows)
badomens idle install-service --dry-run   # ver qué se instalaría
```

Si hay auto-apagado configurado y ningún vigilante activo, la TUI avisa.

## Capturas y pruebas (sin terminal)

```bash
badomens tui --snapshot --view list --width 110 --height 30
badomens tui --snapshot --view health --width 200 --height 50 --theme light
```

Vistas: `list`, `detail`, `logs`, `backups`, `players`, `net`, `idle`,
`settings`, `health`, `wizard`, `palette`, `help`.

Los frames de referencia viven en `internal/tui/testdata/golden/` y se regeneran
con `go test ./internal/tui/ -update`.
