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

## Navegación, teclado y ajustes (v1.2.1)

| ID | Escenario / pasos | Resultado esperado |
|---|---|---|
| NAV-01 | `Tab` / `Shift+Tab` en pantallas de primer nivel | Cambia de pantalla: Inicio → Instancias (detalle seleccionado) → Salud → Ayuda → Inicio (y al revés). |
| NAV-02 | `←`/`→` dentro del detalle de una instancia | Cambia de pestaña en cualquier pestaña, **incluida Ajustes**; nunca cambia de pantalla de primer nivel. |
| NAV-03 | `Tab` con el asistente (modo elegido) o la consola con foco | No navega: el formulario/input captura la tecla (la precedencia de foco manda). |
| NAV-04 | `←`/`→` en Ajustes | No actúan "como Enter": no cambian el valor del campo, no abren el selector ni guardan. |
| SET-01 | Ajustes: Enter en Dificultad/Modo/Cuentas/Memoria | Abre el selector; `↑/↓` eligen; Enter aplica y guarda; Esc cancela sin guardar. |
| SET-02 | Ajustes: Enter en un campo de texto (Jugadores/MOTD) | Edita en el campo propio de Ajustes; Enter guarda; Esc cancela y **no** abandona el detalle. |
| SET-03 | Ajustes: cambiar Memoria y pasar a Consola | El campo de Consola aparece **vacío** (no hereda `4G` ni placeholders de otros formularios). |
| SET-04 | Ajustes: alternar Cuentas crackeado ↔ premium | Persiste `online_mode` en el Spec y avisa "Requiere reiniciar". |
| CON-01 | Consola: pulsar `/` | Activa el campo de comando (única tecla que lo hace). `Ctrl+F` busca en los logs. |
| CON-02 | Consola: `/say hola` + Enter | La barra inicial se recorta: se envía `say hola`. |
| CON-03 | Detalle: `c` (o `C` en Red) | Copia la dirección de conexión desde cualquier pestaña; un fallo no afirma "copiado". |
| WLD-01 | Mundos: `[G]` | Pide semilla (vacío = aleatoria), avisa de **pérdida de datos** y regenera al confirmar. |
| WLD-02 | Mundos: `[L]` | Abre un explorador de archivos; `↑/↓` navegan, Enter abre carpeta o elige un `.zip`, y la carga avisa de pérdida de datos. |
| WLD-03 | Mundos | Muestra la semilla actual y la biblioteca; estados de carga/vacío/error distinguibles. |
| ACL-11 | Modo offline (`ONLINE_MODE=FALSE`) con whitelist/ops | El compose envía el **UUID offline (v3)** de cada nombre en `WHITELIST`/`OPS`, nunca el nombre (que la imagen resolvería a un UUID premium). |

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

## Componentes base (Fase 6) y cableado UI (Fase 8) — ampliación

Casos nuevos añadidos a esta matriz (no se duplica ninguna suite). Complementan
los `UI-*`/`KEY-*` ya existentes y se ejecutan contra `App` + `Router` + `svc`.

### Selector unificado (P-01, P-02, P-03, T-04)

| ID | Escenario / pasos | Resultado esperado |
|---|---|---|
| SEL-01 | Selector en modo lista: `↑`/`↓` (y `j`/`k`) | Mueven la selección sin salir de la lista; los extremos no se pasan. |
| SEL-02 | Selector en modo grid: `←/→` y `↑/↓` | Las 4 flechas navegan en la rejilla; columnas adaptables al ancho; ninguna fila supera el ancho de la primera. |
| SEL-03 | `Enter` sobre la opción señalada | Confirma y aplica la opción elegida **por valor**; no por índice. |
| SEL-04 | `Esc` con el selector abierto | Cierra el selector sin aplicar ni preparar ningún cambio. |
| SEL-05 | Opción guardada distinta del cursor | Se marca «(actual)»; la enfocada lleva puntero y banda (visible sin depender del color). |
| SEL-06 | Opción deshabilitada | Explica el motivo; no se elige por accidente. |
| SEL-07 | Cambiar de motor en el asistente | Se resetea la versión y la lista se recarga para el nuevo motor (selección por valor). |
| SEL-08 | Selector de versiones del asistente en ancho | Se pinta como rejilla alineada; `Enter` selecciona y cierra; `Esc` cierra sin cambiar. |

### Barra inferior (SPEC 2 §1, UX-01, UX-15)

| ID | Escenario / pasos | Resultado esperado |
|---|---|---|
| BAR-01 | Cualquier pantalla | Formato único `[Tecla] Etiqueta` en **todas** las teclas; un solo componente renderiza la barra. |
| BAR-02 | Inicio y detalle | Solo atajos contextuales de la pantalla + esenciales; `[B] Backup`, `[F1] Ayuda`, `[TAB] Desplazarse`, `[D] Borrar` con sus etiquetas. |
| BAR-03 | Cualquier pantalla / Ayuda (F1) | `Ctrl+P`, `F2` y `Ctrl+Q` NO aparecen en la barra pero siguen funcionando y se documentan en Ayuda. |
| ESC-01 | Input de comandos con foco / sin foco | Con el foco, `[Esc] Atrás` va resaltado; `Esc` devuelve el foco sin ejecutar nada; sin foco no se resalta. |

### Notificaciones con TTL (SPEC 2 §12, P-05, UX-11)

| ID | Escenario / pasos | Resultado esperado |
|---|---|---|
| NOT-01 | Aviso info/éxito | TTL 6 s (info) / 8 s (éxito). |
| NOT-02 | Aviso/error que requiere atención | TTL 20 s (aviso) / 30 s (error); **ninguno supera 30 s**. |
| NOT-03 | Reloj avanza / `Esc` con avisos | Al vencer el TTL el aviso se descarta; `Esc` los descarta a mano. Ninguna notificación es permanente. |
| NOT-04 | MagicDNS desactivado | No es una notificación flotante: vive en Salud como aviso fijo con opción de descartar. |

### Estado único del servidor + semáforo (§8, UX-09)

| ID | Escenario / pasos | Resultado esperado |
|---|---|---|
| SEM-01 | Transiciones apagado → cargando → en línea → cargando → apagado | El estado tipado se refleja en el semáforo con luz + **texto** («En línea»/«Cargando»/«Apagado»), nunca solo color. |
| SEM-02 | Cualquier sub-pantalla de la instancia | El semáforo está en la franja fija (Chrome) y se actualiza con el sondeo, sin recarga manual. |
| SEM-03 | Respuesta tardía de otra instancia (detalle o sondeo) | No pisa el estado de la instancia actual. |
| SEM-04 | Operación de ciclo de vida en vuelo | Muestra «Cargando» y vuelve al estado sondeado al terminar. |

### Cableado UI de la Fase 8 (daemon, Docker, Tailscale, MagicDNS)

| ID | Escenario / pasos | Resultado esperado |
|---|---|---|
| INFRA-01 | Iniciar la TUI con el daemon apagado | Se comprueba y se fuerza el arranque (`internal/infra`), mostrando estado «Cargando» 🟡 sin bloquear el bucle. |
| INFRA-02 | Primer arranque sin servicio persistente | Se ofrece instalarlo con confirmación; al confirmar queda instalado; si se rechaza la elevación, se degrada a daemon de sesión con aviso. |
| INFRA-03 | Docker apagado | Se detecta y se intenta levantar **una sola vez** con el mismo mecanismo; fallo con causa y pasos manuales por SO. |
| INFRA-04 | Salud: `[D]` reparar Docker / `[T]` reparar Tailscale | Usan el mecanismo común (sin mecanismos paralelos) y avisan de la causa si fallan. |
| MAGIC-01 | MagicDNS activo en vivo | El aviso de la tarjeta de conexión desaparece automáticamente (se reevalúa con `tailscale.Probe`). |
| MAGIC-02 | MagicDNS desactivado (aviso legítimo) | Va a Salud con motivo tipado y opción de descartar, nunca como banner flotante permanente. |

### Prioridad de teclado determinista (P-06)

| ID | Escenario / pasos | Resultado esperado |
|---|---|---|
| FOCUS-01 | Input de comandos enfocado: escribir `n`, `a`, `q`, `?` | Se escriben literalmente; no se abre ninguna pantalla ni se ejecutan atajos globales. |
| FOCUS-02 | Cerrar un modal | Se restaura el foco anterior. |