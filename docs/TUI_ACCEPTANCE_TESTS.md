# badomens — matriz de aceptación y regresión de la TUI

Usar esta matriz en los tests de `internal/tui` (y en el harness PTY/snapshot de integración si está disponible). Las pruebas de interacción deben ejecutarse contra `App` + `Router` + pantalla/servicio usados por `Run()`, no exclusivamente contra el modelo legacy.

## Flujos funcionales críticos

| ID | Escenario / pasos | Resultado esperado |
|---|---|---|
| KEY-01 | Abrir pestaña Jugadores y pulsar `A` | Aparece el prompt de añadir whitelist; el foco pasa al input. No hay operación de dominio aún. |
| KEY-02 | Dentro del prompt, escribir `N` y luego `n` | Ambos caracteres se insertan en el texto. No se crea ninguna instancia ni se abre otro diálogo. |
| KEY-03 | Dentro del prompt, escribir un nombre válido y pulsar Enter | Se invoca exactamente una operación `AddWhitelist` para el slug activo y el nombre validado. |
| KEY-04 | Dentro del prompt, pulsar Esc | Se cierra el prompt, no se ejecuta operación y se vuelve a la pestaña Jugadores con foco correcto. |
| KEY-05 | Desde Home, usar el atajo de crear instancia | Solo desde Home, sin input/modal activo, se abre el flujo de creación. Preferible `Ctrl+N`. |
| KEY-06 | Consola activa con input enfocado: escribir `N`, `A`, `q`, `?` | Se escriben caracteres; no cambian pantallas ni ejecutan comandos globales. |
| KEY-07 | Consola: Enter con campo vacío | No envía un comando vacío ni muestra éxito engañoso. El foco sigue definido según la UX acordada. |
| KEY-08 | Consola: escribir comando y Enter | Se envía una vez a la instancia actual; estado y respuesta aparecen sin mover el footer. |
| KEY-09 | Overlay de ayuda/paleta abierto, pulsar letras y Esc | Solo el overlay recibe eventos; Esc lo cierra y restaura el foco previo. |
| KEY-10 | Cambiar rápidamente de instancia A a B mientras A carga datos | Una respuesta tardía de A no reemplaza datos de B. |

## Whitelist / consistencia

| ID | Escenario / pasos | Resultado esperado |
|---|---|---|
| ACL-01 | Estado live reporta whitelist activada y tres usuarios | UI muestra activada y exactamente esos usuarios, con fuente/tiempo de verificación. |
| ACL-02 | `Spec.Whitelist` guardada difiere del servidor | UI separa configuración guardada del estado real; no los presenta como idénticos. |
| ACL-03 | Añadir jugador válido | Tras ejecutar, se realiza read-back; la lista cambia solo tras confirmación/lectura verificada. |
| ACL-04 | Añadir jugador falla | Se conserva el estado previo y se muestra error útil; no aparece éxito ni se agrega optimísticamente. |
| ACL-05 | El comando se envía, pero la verificación posterior falla | Mostrar `enviado, pendiente de verificar` o estado parcial, nunca `verificado`. |
| ACL-06 | Servidor detenido/RCON no disponible | Estado `no verificable`; conservar configuración persistida etiquetada. Permitir reintentar. |
| ACL-07 | Añadir jugador repetido | Operación idempotente o mensaje claro de que ya existe; sin duplicados visuales. |
| ACL-08 | Quitar jugador y refrescar | La salida del servidor/config queda consistente y el usuario desaparece tras confirmación. |
| ACL-09 | Activar/desactivar whitelist | Estado visual se actualiza solo con resultado confirmado; no confundir enabled con la lista de nombres. |
| ACL-10 | Jugador vacío, espacios o caracteres inválidos | Validación local; no se emite comando al servidor. |

## Layout / snapshots

Probar por lo menos `80×24`, `100×30`, `132×40` y `60×18` si el terminal mínimo soportado lo permite.

| ID | Escenario | Aserciones automáticas |
|---|---|---|
| UI-01 | Inicio vacío y poblado | Sin overflow; acción primaria visible; selección identificable. |
| UI-02 | Nueve pestañas del detalle | Pestaña activa completa; las restantes son navegables; no hay truncado silencioso que esconda las últimas. |
| UI-03 | Consola con 0, 1, 200 y 2.000 líneas | Buffer acotado; viewport estable; header/tabs/footer ocupan las mismas regiones. |
| UI-04 | Consola con línea de log de 500+ caracteres | No desplaza footer ni amplía el terminal; truncado con `…` o scroll horizontal explícito. |
| UI-05 | Formulario en tamaño compacto | Input, valor, error y teclas Enter/Esc siguen visibles. |
| UI-06 | Cargar/error/éxito en Acceso | Estados se distinguen por texto/etiqueta, no solo por color. |
| UI-07 | Resize mientras la consola está activa | Se recalculan viewport e input; no se recorta la barra inferior. |
| UI-08 | ANSI, emojis y acentos en nombres/logs | Anchos medidos por celdas visuales; no partir secuencias ANSI ni desalinear columnas. |
| UI-09 | Consola fuera de su pestaña | Ninguna línea cruda de consola visible; Inicio muestra solo indicadores/eventos designados. |
| UI-10 | Barra de estado y acciones | Siempre visibles en su región fija, independientemente del largo de los logs. |

## Concurrencia / rendimiento

| ID | Escenario | Resultado esperado |
|---|---|---|
| PERF-01 | Navegar 20 instancias rápidamente con servicio lento simulado | Debounce/cancelación reduce llamadas; la UI sigue procesando teclas. |
| PERF-02 | Mantener la app abierta 10 minutos con polling simulado | No se acumulan ticks, goroutines ni consultas solapadas sin límite. |
| PERF-03 | Acción lenta de start/backup | Spinner/progreso visible; permite navegación segura o explica por qué una acción está deshabilitada. |
| PERF-04 | Servicio bloqueado hasta timeout | La pantalla no se congela; se muestra error recuperable y se libera el estado busy. |
| PERF-05 | Benchmark de View en listas/logs grandes | Guardar baseline; detectar regresiones significativas. Evitar umbrales frágiles por máquina en CI compartida. |

## Comandos de verificación

Adaptar al `Makefile` real y registrar la salida:

```bash
gofmt -w <archivos-modificados.go>
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

Snapshot/imagen de referencia:

1. Generar cada vista mediante la función de snapshot disponible (`--snapshot`, `--view`, `--width`, `--height`) o un PTY de integración.
2. Normalizar ANSI para goldens de texto, pero conservar capturas reales para revisión de composición.
3. Comparar contra golden aprobado; revisar el diff antes de actualizarlo.
4. Pasar PNG/capturas a un modelo de visión solo después de generarlas desde el código probado.
5. Guardar las capturas junto al artefacto de CI/PR. Registrar por separado pruebas que requieren Docker, Minecraft, RCON o Tailscale.

## Definición de terminado

- Todos los casos `KEY-*` y `ACL-*` pasan con fake determinista y prueban la ruta de producción.
- Todos los casos `UI-*` pasan en los tamaños obligatorios; los goldens tienen revisión humana/visual.
- `go test ./...`, `go test -race ./...`, `go vet ./...` y `go build ./...` pasan o se registran los bloqueos exactos.
- No existe ninguna afirmación de éxito no confirmada para acciones de acceso.
- El informe final incluye resultados reales; el test visual no reemplaza la suite de teclado/estado.
