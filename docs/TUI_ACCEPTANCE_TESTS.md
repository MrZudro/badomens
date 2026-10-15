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
| NAV-01 | `F2` desde Inicio; `Esc` en Salud; `F1` y `Esc` en la Ayuda (fila revisada en la Fase 10 por la §15: `Tab` ya no cambia de pantalla) | `F2` abre Salud (apilada) y `Esc` vuelve a Inicio; `F1` abre la Ayuda como overlay y `Esc` (o `F1`) la cierra dejando Inicio debajo. |
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

### Pulido de UI de la Fase 10 (SPEC 2 §6, §13, §14, §15; UX-14 … UX-17)

Casos nuevos: complementan los `UI-*`, `KEY-*`, `ESC-01` y `CON-*` existentes (no
duplican ninguna suite) y se ejecutan contra `App` + `Router` + `svc`.

**Interpretación de §15 (documentada).** La «pestaña superior» que se retira es la
**fila de navegación de primer nivel** que el marco pintaba bajo la cabecera
(`Inicio · Instancias · Salud · Ayuda`, recorrible con `Tab`/`Shift+Tab`): su pestaña
«Instancias» era la ruta redundante al detalle, porque desde Inicio ya se entra con
`Enter`. **No** se retira la fila de secciones del detalle (Resumen · Consola · … ·
Ajustes), que exige la especificación de rediseño (§3) y se navega con `←/→`. Tras
el cambio: Inicio es la raíz, `Enter` abre el detalle, `Esc` vuelve, `F1` abre la
Ayuda, `F2` abre Salud (y `Esc` vuelve de ella), `Ctrl+P` abre la paleta y
`Tab`/`Shift+Tab` quedan como **«Desplazarse»** entre las zonas/paneles de la
pantalla actual.

| ID | Escenario / pasos | Resultado esperado |
|---|---|---|
| UI-TABS-01 (§15/UX-17) | Inicio en 40×12, 80×24, 110×30 y 132×40; abrir el detalle con `Enter`; volver con `Esc` | No queda ninguna fila de pestañas de primer nivel (ni marcas `[Inicio]`/`[Instancias]`); el detalle se sigue abriendo desde Inicio y se vuelve con `Esc`; la navegación no depende de una fila de tabs, tampoco en compacto. |
| NAV-01 (revisado) | `F2` desde Inicio; `Esc` en Salud; `F1`/`Esc` en la Ayuda | Salud se apila sobre Inicio y `Esc` vuelve; la Ayuda es un overlay que se cierra con `Esc` o `F1` dejando Inicio debajo. |
| NAV-03 | `Tab` con el input de comandos enfocado | Sigue siendo del campo (autocompleta): no cambia de pestaña, de zona ni de pantalla. |
| NAV-04 | `Tab`/`Shift+Tab` con el asistente abierto (con y sin captura de entrada) | No sale del asistente: avanza de campo o de paso. |
| ZONE-01 | Inicio en ancho: `Tab` y `Shift+Tab` | El foco se desplaza lista → detalle → actividad sin cambiar de pantalla; la zona con foco se marca con `▸` en el título del panel, no solo con color. |
| ZONE-02 | Detalle, pestaña Resumen: `Tab` y `Shift+Tab` | El foco pasa de RESUMEN a CÓMO CONECTARSE y vuelve; no cambia de pestaña (eso es `←/→`) ni de pantalla. |
| ZONE-03 | Detalle, pestaña Backups (un solo panel): `Tab` | No hay zonas que desplazar: `Tab` no cambia de pestaña ni de pantalla. |
| HELP-01 (§13/UX-14) | Ayuda en 60×18, 80×24, 100×30 y 132×40 | Grid de columnas alineadas que se adapta al ancho: la vista ocupa exactamente el alto, ninguna línea desborda y están todos los atajos globales (`[F1]`, `[F2]`, `[TAB]`, `[Shift+Tab]`, `[Ctrl+P]`, `[Ctrl+Q]`, `[Esc]`, `[Enter]`, `[Ctrl+N]`), con las etiquetas de los retirados de la barra (`Ctrl+P` paleta, `F2` salud, `Ctrl+Q` salir). |
| HELP-02 | Grid de globales en anchos de 24, 50, 82 y 200 celdas | Todas las celdas arrancan en la misma columna en todas las filas (alineación estable) y el número de columnas se ajusta al ancho sin desbordar. |
| HELP-03 | Orden de los comandos globales | Cascada piramidal por longitud de tecla: `F1`, `F2`, `Tab` → `Shift+Tab` → `Ctrl+P`, `Ctrl+Q` → resto (`Esc`, `Enter`, `Ctrl+N`) de menor a mayor longitud. |
| HELP-04 | Texto de la hoja | Instrucciones coherentes: `Enter` abre o confirma, `Esc` vuelve o cancela sin guardar y la hoja se cierra con `F1` o `Esc`. |
| SOON-01 (§14/UX-16) | Pestaña Plugins/Mods en 120×40 y en 40×12 | `Plugins/Mods (SOON)` en la fila de pestañas y `PLUGINS/MODS (SOON)` con el aviso «Próximamente» en el panel: nunca sugiere que la función esté operativa. |
| ESC-02 (§6/UX-15) | Consola: `stop` + `Enter` (comando peligroso pendiente) y después `Esc` | `Esc` devuelve el foco **sin ejecutar nada**: no envía el comando pendiente, no toca el histórico y no abandona el detalle. |
| CON-04 (consola) | Consola con 2.000 líneas y una línea de 500 caracteres en 60×18, 80×24, 100×30 y 132×40 | El buffer queda acotado y el viewport ocupa exactamente su región: no desplaza el pie ni desborda el ancho, y la barra de atajos sigue en la última línea. |

**Notas de implementación (Fase 10).**

- El ancho de celda del grid de Ayuda se acota a la mitad del ancho útil en anchos
  medios, de modo que el grid use al menos dos columnas; las etiquetas que no caben
  se abrevian con `…` (el detalle completo vive en la paleta, `Ctrl+P`). Si el alto
  no da para todo, primero se recorta el bloque «Pantalla» (sus atajos ya están en
  la barra) y sus teclas vuelven al bloque global; ninguna tecla queda sin
  documentar.
- `Tab`/`Shift+Tab` los atiende cada pantalla (`zoneFocus`), no el `App`: la
  precedencia de foco (modal → campo de texto → pantalla) sigue intacta, por eso el
  asistente y la consola conservan su uso de la tecla.
- Las teclas de la hoja de Ayuda se pintan con `components.DisplayKey`, la misma
  traducción que usa la barra inferior: un solo formato `[Tecla] Etiqueta` (P-04).

## Flujos de creación y funciones de instancia (Fases 7 y 9) — ampliación

Casos nuevos: complementan los `UI-*`, `KEY-*`, `SEL-*`, `WLD-*`, `CON-*` y
`ACL-*` anteriores (no duplican ninguna suite) y se ejecutan contra `App` +
`Router` + `svc` (fake determinista, sin Docker ni red).

### Creación rápida y avanzada (Fase 7: UX-02, UX-03, UX-04, UX-05)

| ID | Escenario / pasos | Resultado esperado |
|---|---|---|
| WIZ-01 | Paso 1 con el foco en la lista de campos: pulsar `↓` hasta el final y `Enter` sobre la celda «Continuar a Recursos →» | `↓` **nunca** cambia de paso (se detiene en la última celda); `Enter` sobre la celda de continuación (o `Tab`) sí avanza. `↑` vuelve sin cambiar de paso. |
| WIZ-02 | `Enter` sobre **Versión** en ancho: panel lateral de versiones; navegar con las 4 flechas; `Enter` selecciona y cierra; `Esc` cierra sin cambiar | El panel usa el selector único en rejilla (ancho de columna fijo, columnas adaptables, grupos por versión mayor y snapshots/betas aparte); `Enter` guarda el **valor** señalado y `Esc` no cambia la selección. |
| WIZ-03 | Fallo de carga de versiones con `stale` | Error claro en el panel con `[R] Reintentar` y la etiqueta de caché vencida con su antigüedad. Nunca una lista vacía silenciosa. |
| WIZ-04 | Cambiar de motor en el asistente | Se **resetea** la versión, se descarta la resolución anterior y se recarga la lista válida del nuevo motor; `Esc` conserva motor y versión elegidos. |
| WIZ-05 | Recorrer combinaciones motor/versión, cambiar de motor, retroceder con `Esc`, avanzar y crear | La config **persistida** coincide con la selección y con el resumen (motor, versión y pines obligatorios) en el 100 % de los casos. |
| WIZ-06 | La configuración persistida difiere del resumen (fallo simulado) | No hay éxito silencioso: se avisa de la discrepancia con el detalle (T-08/AD-02). |
| WIZ-07 | `[R]` en el panel de versiones | Fuerza el refresco del catálogo por la misma ruta del servicio (`catalog refresh`), deja el panel cargando y retira la etiqueta de caché vencida al terminar. |
| WIZ-08 | Resolución tardía de un motor/versión anterior | Se descarta sin tocar el estado vigente (patrón de secuencia). |
| WIZ-09 | Versiones del servicio | La UI lista **todas** las versiones que devuelve el servicio (no reintroduce filtros); solo marca como no elegible lo que el servicio declara `incompatible`, explicando el motivo, y anota lo que no está verificado. |

### Jugadores (Fase 9/§7: UX-08)

| ID | Escenario / pasos | Resultado esperado |
|---|---|---|
| PLAY-01 | Servidor en 🟡 Cargando y `[R]`/apertura de la pestaña | **No se consulta** (0 peticiones) y el mensaje dice que está iniciando y que se reintentará. |
| PLAY-02 | Apagado, iniciando, consulta no disponible (RCON/SLP) y error de conexión | Cuatro mensajes **distintos**, con la causa y la acción recomendada (arrancar, esperar, habilitar el método, reintentar). |
| PLAY-03 | Respuesta tardía de otra instancia (o de una consulta anterior) | No pisa el estado de la instancia actual. |
| PLAY-04 | Servidor en línea | Se muestran jugadores y cupo con el método (Server List Ping / RCON) y se consulta una sola vez. |

### Regeneración (Fase 9/§9: UX-10, RG-01..03)

| ID | Escenario / pasos | Resultado esperado |
|---|---|---|
| REG-01 | `[G]` → semilla → diálogo | El diálogo **declara la política** («apagar y regenerar»: se detiene, se borra el mundo y se vuelve a arrancar) antes de confirmar, detalla qué se pierde, ofrece backup previo y arranca con el foco en el botón seguro; `↑/↓` y `←/→` mueven el foco; cancelar no llama al servicio. |
| REG-02 | Confirmar la regeneración | Progreso visible: semáforo 🟡 Cargando + indicador **persistente** «Regeneración en curso» en la pestaña Mundos y operación en curso en la barra de estado; al terminar, el indicador desaparece y el resultado se notifica (semilla y backup). |
| REG-03 | `[B]` en el diálogo | Se crea **un** backup previo y después se regenera. |
| REG-04 | Fallo del servicio y resultado no verificable | Un fallo deja estado recuperable y lo explica («revisa el estado: si quedó detenido, arráncalo con [S]»); un resultado que no se puede verificar no se presenta como éxito (T-08). |

### Borrado (Fase 9/§11: UX-13)

| ID | Escenario / pasos | Resultado esperado |
|---|---|---|
| DEL-01 | `[D]` desde la lista; `Esc`; nombre que no coincide | Nunca se borra sin confirmación: abrir el diálogo y cancelar no llaman al servicio; un nombre distinto no borra y el diálogo sigue abierto. |
| DEL-02 | `[D]` desde la lista con el nombre exacto (servidor en línea) | El diálogo detalla qué se elimina (contenedor, datos, mundo, backups) y avisa del apagado; se **detiene antes** de borrar (`stop` → `delete`); se verifica que la instancia ya no existe, la fila desaparece y el resultado se notifica. |
| DEL-03 | `[D]` desde el detalle | La misma confirmación reforzada y el mismo resultado (la acción está disponible en la barra con la etiqueta «Borrar»). |
| DEL-04 | `Ctrl+B` en el diálogo | Con el nombre escrito hace un backup previo y después borra; **sin** el nombre no borra (la confirmación reforzada es obligatoria). |
| DEL-05 | Fallo al borrar | No se informa de éxito: se explica el estado recuperable (puede haber quedado detenida) y la instancia sigue en el registro. |

### Copiar dirección (Fase 9/§10: UX-12)

| ID | Escenario / pasos | Resultado esperado |
|---|---|---|
| COPY-01 | `[c]` en la lista y en el detalle | Copia la dirección preferida (tailnet) y lo confirma citando la dirección. |
| COPY-02 | Sin ninguna dirección posible | El mensaje explica **por qué** no hay dirección y **qué hacer** (arrancar con `[S]`, ejecutar `tailscale up`). |
| COPY-03 | Varias direcciones válidas (tailnet + MagicDNS + red local) en la pestaña Red con `[C]` | Se abre el selector único para **elegir**; `↑/↓` navegan, `Enter` copia la elegida y `Esc` cierra. |
| COPY-04 | Antes del primer arranque, sin tailnet pero con IP local | La dirección se deriva de la IP local + puerto y se copia indicando por qué red funciona. |

### Backup (Fase 9/§13)

| ID | Escenario / pasos | Resultado esperado |
|---|---|---|
| BK-01 | `[B] Backup` en la lista y en el detalle | El atajo está visible con la etiqueta «Backup» y crea la copia desde ambas pantallas. |
| BK-02 | `[X]` restaurar | Pide confirmación explicando que se valida el checksum y se crea un backup previo; `Esc` no restaura; `[y]` restaura el backup señalado. |
| BK-03 (svc) | Restaurar un backup dañado o sin `sha256` | La restauración se rechaza **antes** de tocar `server/`, no se crea el backup previo y el error explica el problema de integridad. |


## Accesibilidad y rendimiento (Fase 12) — ampliación

Casos nuevos: complementan los `KEY-*`, `UI-*`, `PERF-*`, `FOCUS-*`, `SEM-*` y
`WIZ-*` anteriores (no duplican ninguna suite) y se ejecutan contra `App` +
`Router` + `svc` (fake determinista). Código:
`internal/tui/accessibility_test.go`, `internal/tui/perf12_test.go`,
`internal/tui/components/serverstate_test.go`, `internal/tui/svc/memo_test.go`,
`internal/tui/svc/limit_test.go`, `internal/provision/provision_test.go`.

### Accesibilidad (§12.1 del plan; §6.1 de la spec)

| ID | Escenario / pasos | Resultado esperado | Prueba |
|---|---|---|---|
| A11Y-01 | Semáforo en `running`, `restarting`, `exited` y un estado crudo desconocido (`paused`) | Cada estado lleva luz + TEXTO («En línea»/«Cargando…»/«Apagado»/«Desconocido»); ninguno depende solo del color y el desconocido no se confunde con el apagado. | `TestA11Y01SemaforoConTextoSegunEstado`, `TestServerStateTextoSiemprePresente`, `TestServerStateOfDesconocido` |
| A11Y-02 | Input enfocado (consola con `/` y prompt de whitelist con `A`): escribir `n`, `a`, `q`, `d`, `b` | Se escriben LITERALMENTE; no navegan, no abren overlays, no borran, no hacen backup ni salen. | `TestA11Y02LetrasLiteralesConInputEnfocado` |
| A11Y-03 | Compatibilidad del catálogo: compatible / incompatible / desconocida (`unknown`) / sin dato | Cada veredicto se comunica con texto («compatible», «incompatible», «sin verificar (unknown)»), nunca solo color. | `TestA11Y03CompatibilidadTrivalenteConTexto` |
| A11Y-04 | Inicio, Detalle (con `←/→`) y Ayuda (F1/Esc) en 60×18, 80×24, 100×30 y 132×40 | Ninguna línea desborda; el pie vive en la última línea con «[F1] Ayuda»; abrir/cerrar la Ayuda funciona; nada esencial depende de una fila de pestañas. | `TestA11Y04TerminalesCompactos` |
| A11Y-05 | Operación en curso (`busy`) | El indicador de actividad lleva TEXTO y no bloquea la navegación ni el pie (no es el único indicador). | `TestA11Y05CargaNoBloqueaNiEsElUnicoIndicador` |
| A11Y-06 | Modo `--plain` y salida estructurada | `badomens tui --plain` imprime un resumen lineal sin cajas; el contrato JSON sigue siendo un único documento con las claves estables. | `TestTUIPlainResumenLineal`, `TestSalidaEstructuradaYHumana` |

### Estabilidad y rendimiento (§12.2 del plan; §6.2 de la spec)

| ID | Escenario / pasos | Resultado esperado | Prueba |
|---|---|---|---|
| PIN-01 | Resolver (el catálogo devuelve el build 1) y crear con un catálogo que publica un build 2 durante la creación | Se persiste EXACTAMENTE el build revisado (1); sin pins inyectados rige el catálogo. Pins inválidos (`""`/`latest`) se rechazan sin dejar instancia escrita. | `TestCreateInyectaPinsExactos`, `TestCreateSinPinsUsaLosDelCatalogo`, `TestCreateRechazaPinsInvalidos`, `TestPIN01InyeccionDePinsDelResumen` |
| PIN-02 | `badomens setup --pins PAPER_BUILD=123` | Flag aditivo: los pins se inyectan en la creación; sin `--pins` se resuelve por el servicio. | `parsePins` (CLI) + `PIN-01` |
| PERF-06 | `trimLogs` con 0, 1, 200 y 2.000 líneas (y por encima del límite) | Buffer circular acotado a `maxLogLines`; 0 líneas → vacío. | `TestPERF06TrimLogsBufferCircular` |
| PERF-08 | Consola con 2.000 líneas + una línea de 500 caracteres en 60×18 | El viewport ocupa exactamente su región; el pie no se desplaza; el render no pinta el buffer completo. | `TestPERF08ViewportAcotadoAZonaVisible` |
| PERF-09 | 90 latidos de sondeo (intervalo de estado 3 s) con navegación de por medio | Exactamente 30 consultas de estado (una por intervalo); sin tareas duplicadas. | `TestPERF09PollingSinTareasDuplicadas` |
| PERF-09b | 400 ciclos de navegación con la app abierta | No se acumulan goroutines. | `TestPERF09bNavegacionSinAcumularGoroutines` |
| PERF-10 | Operación lenta con `busy` | El teclado responde de inmediato y la vista sigue pintándose. | `TestPERF10OperacionLentaNoBloqueaTeclado` |
| PERF-11 | Métricas mínimas | Latencia de interacción, tiempo de render y heap medidos; peticiones externas acotadas por el semáforo del servicio. | `TestPERF11MetricasMinimas` |
| PERF-12 | 50 avisos seguidos | La cola de avisos/eventos queda acotada (máx. 3). | `TestPERF12EventosAcotados` |
| PERF-07 (svc) | 30 operaciones externas con límite 3, y cancelación de contexto en espera | Nunca hay más de N en vuelo; `acquire` respeta la cancelación. | `TestSemAcotaConcurrencia`, `TestSemCancelaEnEspera` |
| PERF-06b (svc) | Lecturas repetidas de `Versions`/`Resolve`/`Status` dentro del TTL; `Refresh`; TTL vencido | La caché en MEMORIA sirve lo repetido (1 consulta); `Refresh` invalida y el TTL caduca. | `TestMemoCatalogLecturasRepetidas` |

**Caché en memoria, deduplicación y cancelación.** La caché persistente y la
deduplicación de peticiones simultáneas viven en `engines.Cache` (no se
duplican). Encima, la TUI añade una caché en MEMORIA de TTL corto
(`svc.NewMemoCatalog`, 2 s) para lecturas repetidas. La cancelación por
**generación/secuencia** ya cubre el abandono de pantalla o el cambio de
instancia: consola (`instanceLogsMsg` con `gen`), detalle por slug/`gen`,
asistente (`wizardResolvedMsg`/`wizardVersionsMsg` con `seq`) y jugadores. Ver
`KEY-10`, `TestConsolaDescartaRespuestasObsoletas`,
`TestConsolaSalirInvalidaGeneracion`, `WIZ-08` y `TestHomeCacheDetallePorSlug`.

### Estabilidad de la TUI (plan §6.3) — mapa de cobertura

| Requisito del plan (§6.3) | Dónde se prueba |
|---|---|
| Letras literales en inputs | `KEY-02`, `KEY-06`, `FOCUS-01`, `A11Y-02` |
| Cambios rápidos de instancia con peticiones pendientes | `KEY-10`, `PERF-01`, `WIZ-08`, `SEM-03`, `PLAY-03`, `TestConsolaDescartaRespuestasObsoletas` |
| Whitelist persistida distinta de la lista real | `ACL-02`, `ACL-06` |
| Consola con 0, 1, 200 y 2.000 líneas | `UI-03`, `CON-04`, `PERF-06` |
| Logs largos con secuencias ANSI, emojis y acentos | `UI-04`, `UI-08`, `PERF-08` |
| Terminales 60×18, 80×24, 100×30 y 132×40 | `A11Y-04`, `UI-01`…`UI-10`, `HELP-01` |
| Operaciones lentas sin bloquear teclado | `PERF-03`, `PERF-04`, `PERF-10` |
| Goroutines y polling sin acumulación | `PERF-02`, `PERF-09`, `PERF-09b` |
| Caché en memoria + persistente; sin deduplicar lo ya deduplicado | `PERF-06b` (memoria), caché del catálogo (persistente/dedup) |
| Límite de concurrencia en consultas externas | `PERF-07` |

### Comandos de verificación (Fase 12)

```bash
# Interacción, foco, accesibilidad, estabilidad y creación (sin red ni Docker)
go test ./internal/tui/... ./internal/provision/ ./internal/cli/ -count=1

# Benchmarks reproducibles de render e interacción (baseline en perf_test.go)
go test -bench BenchmarkView ./internal/tui/
go test -bench 'BenchmarkInteraction|BenchmarkViewConsola' ./internal/tui/

# Detector de carreras: NO está disponible en este entorno (requiere cgo).
# Debe ejecutarse en CI con cgo habilitado:
#   CGO_ENABLED=1 go test -race ./...
```
