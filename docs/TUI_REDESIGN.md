# badomens — Especificación de rediseño de la TUI

**Estado:** propuesta lista para implementación  
**Stack actual:** Go, Bubble Tea, Bubbles, Lip Gloss  
**Objetivo:** que la TUI sea predecible, legible, rápida, accesible y verificable con pruebas automáticas.

## 1. Criterios de éxito

1. Una tecla solo puede activar la acción de la pantalla o control que tiene el foco.
2. Ningún formulario modal pierde datos, cambia de pantalla o ejecuta una acción global porque el usuario escribió una letra normal.
3. La vista de consola solo muestra la salida cruda del servidor en la pestaña **Consola**. Otras pantallas pueden mostrar eventos breves claramente etiquetados, pero no una segunda consola encubierta.
4. Cabecera, pestañas, estado y barra de atajos conservan su lugar; solo el cuerpo central puede desplazarse.
5. Las líneas se miden con el ancho visual del terminal, se ajustan o truncan deliberadamente y no provocan desbordamiento horizontal accidental.
6. La whitelist distingue entre estado deseado guardado y estado real del servidor. La UI nunca afirma que una modificación funcionó solo porque pudo enviar un comando.
7. El renderizado y la navegación no esperan a Docker, RCON, disco, red ni consultas de métricas.
8. Cada flujo crítico tiene pruebas unitarias, pruebas del enrutamiento real, pruebas de dimensiones y, cuando corresponda, verificación visual.

## 2. Diagnóstico dirigido al código actual

Estos hallazgos se basan en la lectura del código público de `main`; no son una afirmación de que se hayan ejecutado pruebas en un clon local.

- `internal/tui/screen_instance.go`: `playersView()` pinta `Spec.WhitelistEnabled` y `Spec.Whitelist`, mientras que `playersKey()` abre un prompt y `runPrompt()` envía `whitelist add <jugador>` a la consola. Después de enviar el comando no se fuerza una recarga confirmada del estado de acceso. La tecla `R` recarga el conteo de jugadores, no la ficha/whitelist. Por tanto, esta vista puede quedarse obsoleta o presentar configuración guardada como si fuera estado vivo.
- `internal/tui/screen_instance.go`: `consoleView()` recorta el número de líneas, pero no ajusta cada línea al ancho visible. Líneas extensas pueden envolverse en el terminal y desplazar el contenido inferior.
- `internal/tui/screen_home.go`: al mover la selección, `move()` lanza cargas de detalle y logs; la carga de cada fila también inicia ambas. Hay que medir y reducir consultas repetidas y descartar respuestas obsoletas.
- `internal/tui/router.go`: la aplicación ya posee abstracciones `Screen`, `Router` y `overlay`. La política de foco global debe integrarse con esta arquitectura, no duplicarse en handlers de cada pantalla.
- `internal/tui/app.go`: el `Run()` de producción construye `NewApp(...)`. El repositorio también contiene `internal/tui/model.go` y pruebas para un modelo anterior. Antes de borrar nada, separar qué código se usa en producción y qué código solo se conserva para compatibilidad; las pruebas del modelo legado no prueban por sí solas el flujo de `App`/`Router`.

## 3. Arquitectura de información

### Inicio — Instancias

- Cabecera: nombre del producto, versión, estado Docker/Tailscale y límites de instancias.
- Zona principal en terminal ancha: lista de instancias, resumen de la selección y estado operativo compacto.
- Solo eventos/avisos resumidos en Inicio. No renderizar ahí el stream crudo de la consola.
- Acciones contextuales: abrir, iniciar, detener, reiniciar, backup y copiar conexión.

### Detalle de una instancia

Mantener nueve secciones funcionales, pero darles una jerarquía visual y atajos contextuales claros:

1. Resumen
2. Consola
3. Jugadores y acceso
4. Backups
5. Mundos
6. Plugins/Mods
7. Red
8. Auto-apagado
9. Ajustes

En terminales anchas, usar una fila de pestañas. En anchos reducidos, mostrar el nombre completo de la pestaña activa y un indicador `3/9`, con navegación izquierda/derecha; nunca truncar silenciosamente todas las pestañas hasta hacerlas irreconocibles. No hacer que las opciones se mezclen con el contenido desplazable.

### Jugadores y acceso

La vista debe contener:

- Estado de whitelist: `ACTIVADA`, `DESACTIVADA`, `NO VERIFICADA` o `ERROR DE CONSULTA`.
- Fuente del dato y fecha de la última verificación.
- Lista de jugadores permitidos y operadores, con estados vacíos explícitos.
- Acciones Añadir, Quitar, Activar/desactivar whitelist, Hacer operador y Quitar operador.
- Un mensaje que explique el resultado real de la operación.

Si el servidor está apagado, no presentar un valor de configuración como si fuera una lectura en vivo. Usar, por ejemplo, `Configuración guardada: activada · Estado en vivo: no verificable (servidor apagado)`.

## 4. Boceto visual de referencia

Ejemplo conceptual en un terminal ancho (no representa una captura real):

```text
┌─ BADOMENS ─ INSTANCIAS ───────────────────────── Docker ●  Tailscale ●  2/3 activos ─┐
│ Inicio   Instancias   Salud   Ayuda                                     vX.Y.Z       │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ INSTANCIA: survival             Estado: ● EN EJECUCIÓN       1.21.x · Paper          │
│                                                                                      │
│ Resumen   Consola   Jugadores y acceso   Backups   Mundos   Mods   Red   Ajustes      │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ ACCESO                                      │ OPERADORES                              │
│ Whitelist: ● ACTIVADA                       │  Alex                                   │
│ Estado: verificado hace 2 s                 │  Sam                                    │
│                                            │                                         │
│ Jugadores permitidos                       │                                         │
│  ● Alex                                    │                                         │
│  ● Sam                                     │                                         │
│  ● Maria                                   │                                         │
│                                            │                                         │
│                                            │                                         │
│ [A] Añadir  [D] Quitar  [W] Activar/desactivar  [O] Operador  [R] Verificar de nuevo  │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ ✓ Whitelist verificada: 3 jugadores                                              22:10 │
│ ↑/↓ navegar   Enter seleccionar   Esc volver   Ctrl+P acciones   ? ayuda   Ctrl+C salir│
└──────────────────────────────────────────────────────────────────────────────────────┘
```

Consola:

```text
┌─ survival ─ Consola ────────────────────────────── ● EN EJECUCIÓN · seguimiento ON ─┐
│ Resumen   [Consola]   Jugadores y acceso   Backups   Mundos   Mods   Red   Ajustes     │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ [22:10:03 INFO]: Alex joined the game                                               │
│ [22:10:10 INFO]: There are 3 of a max of 20 players online                          │
│ [22:10:19 INFO]: Done (1.234s)! For help, type "help"                               │
│                                                                                      │
│                                                                                      │
│                                                                                      │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ > whitelist list                                                                     │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ ✓ Comando enviado · resultado pendiente de verificar                               │
│ Enter enviar · ↑/↓ historial · Tab completar · Esc salir del campo · F1 ayuda       │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

### Reglas de distribución

- Diseñar por ancho útil, no por la suposición fija de 80 columnas:
  - **Compacto** (`< 80` columnas): una sola zona; etiquetas abreviadas solo si hay equivalente accesible; nada de columnas que desborden.
  - **Medio** (`80–119`): lista y detalle, o una pantalla de detalle a ancho completo.
  - **Ancho** (`>= 120`): lista + resumen + estado operativo; no agregar una zona de logs crudos en Inicio.
- Calcular el espacio en columnas visuales (`lipgloss.Width`, `ansi.StringWidth` o helper equivalente), no con `len()` ni número de runas.
- El cuerpo recibe una altura calculada: `altoTerminal - cabecera - pestañas - estado - barraAtajos - márgenes`. La consola/logs tienen viewport limitado al cuerpo.
- La barra inferior debe renderizarse después de reservar su altura fija. Nunca se añade después de contenido sin límites.
- Reaccionar a `tea.WindowSizeMsg`; no mantener `input.Width = 50` o anchos fijos desconectados del terminal.
- Lip Gloss no puede cambiar el tamaño físico de la fuente del emulador de terminal. Mejorar la legibilidad mediante jerarquía, contraste, negrita, espacio vertical y menos densidad. Para aumentar el tamaño real de los caracteres debe configurarse el terminal.

## 5. Estado y foco: contrato obligatorio

Orden de prioridad de entrada, sin excepciones silenciosas:

1. **Modal/confirmación activo**: solo él recibe las teclas. `Esc` cancela y devuelve el foco al control anterior.
2. **Campo de texto con foco**: todas las teclas imprimibles se entregan al editor; no se procesan como atajos de navegación. `Enter` confirma el formulario o envía el comando según el contexto del editor.
3. **Control interactivo de la pestaña**: recibe navegación propia, selección, scroll o edición.
4. **Pantalla activa**: atajos contextuales de esa pantalla y nada más.
5. **Atajos globales**: solo combinaciones explícitas como `Ctrl+P`, `F1` o `Ctrl+Q`; las letras imprimibles simples nunca deben crear instancias ni cambiar de pantalla desde un formulario.

### Invariantes de foco

- `prompt == true` implica que `promptAct` y `promptLabel` son válidos y `input` tiene foco.
- El foco en consola es independiente del hecho de estar en la pestaña Consola; solo esa pestaña puede enviar comandos.
- La pantalla activa/overlay activo es el único destino de `tea.KeyMsg`.
- Al abrir un modal se registra el foco anterior; al cerrarlo se restaura.
- El enrutador global pregunta primero si una pantalla/modal captura el teclado. No despacha atajos globales por delante de un texto input.
- Si el usuario pulsa `N`, `n`, `A`, `a`, `Esc` o `Enter` al escribir un nombre, probar el resultado exacto: no debe dispararse ningún comando distinto del propio formulario.

### Máquina de estados recomendada

```text
HOME ──Enter──> INSTANCE/Resumen
  │                  │
  └─Ctrl+N──────────> CREAR (modal/pantalla modal)
                     │
INSTANCE/Consola ──e─> INPUT_FOCUSED
                     │ Enter: envía; Esc: devuelve foco a Consola
INSTANCE/Jugadores ─A─> ACCESS_PROMPT
                     │ Enter: valida → operación asíncrona → verificación → refresh
                     │ Esc: cancela sin mutar estado
GLOBAL PALETTE ─Ctrl+P> OVERLAY (captura total de teclado)
```

No modelar el foco como un booleano sin invariantes si ya existe un tipo `FocusTarget` o `InputMode` que pueda representar estos estados. Evitar un refactor por abstracción: la meta es una fuente de verdad y transiciones comprobables.

## 6. Contrato de acceso / whitelist

Separar UI, operación de dominio y mecanismo RCON/consola.

Interfaz de servicio sugerida (nombres orientativos; ajustar al estilo del repositorio):

```go
type AccessState struct {
    WhitelistEnabled bool
    Whitelist        []string
    Operators        []string
    VerifiedAt       time.Time
    Source           AccessSource // live, saved-config, unavailable
}

type AccessService interface {
    Read(ctx context.Context, slug string) (AccessState, error)
    AddWhitelist(ctx context.Context, slug, player string) (AccessState, error)
    RemoveWhitelist(ctx context.Context, slug, player string) (AccessState, error)
    SetWhitelistEnabled(ctx context.Context, slug string, enabled bool) (AccessState, error)
    AddOperator(ctx context.Context, slug, player string) (AccessState, error)
    RemoveOperator(ctx context.Context, slug, player string) (AccessState, error)
}
```

- Elegir y documentar la fuente de verdad. Preferencia: el estado en vivo del servidor para mostrar el estado actual; configuración guardada como intención/configuración, no como prueba del estado runtime.
- Si el runtime actual solo puede enviar comandos y no leer la respuesta, ampliar la frontera de servicio para soportar una lectura verificable o etiquetar el resultado como `enviado, pendiente de verificar`. No inventar éxito.
- Validar el nombre del jugador, normalizar espacios y rechazar entrada vacía/control characters antes de hacer I/O.
- Ejecutar operaciones fuera de `Update()`/`View()` con timeout y `context.Context`.
- Tras añadir, quitar o cambiar la whitelist: esperar resultado, consultar de nuevo el estado y actualizar la pantalla con la respuesta. Si la actualización del servidor funciona pero la lectura posterior falla, mostrar el éxito parcial con advertencia de sincronización.
- Enviar dos veces el mismo jugador debe ser idempotente o comunicar claramente que ya estaba agregado.
- Si hay servidor apagado o RCON sin responder, conservar los datos visibles pero etiquetarlos como no verificados; ofrecer reintentar.
- Evitar mutar `s.detail.Spec` en el lugar antes de persistir: mantener `pendingAction` y aplicar un nuevo estado inmutable tras el resultado.

## 7. Rendimiento y ciclo de actualización

- `Update()` y `View()` deben ser funciones rápidas y no realizar I/O.
- Toda llamada a Docker, filesystem, RCON, socket, métricas o API se convierte en `tea.Cmd` con timeout y contexto cancelable.
- Tener un único planificador de polling por ámbito visible; no crear cadenas duplicadas de `tea.Tick` desde handlers de mensajes distintos.
- Home consulta una lista en el intervalo acordado; cambios de selección usan debounce (p. ej. 100–200 ms) para detalle/logs cuando el usuario recorre varias instancias rápidamente.
- Cada request de detalle/logs/acceso lleva el `slug` y un identificador/generación. Ignorar respuestas que no correspondan a la selección/pantalla/operación vigente.
- No consultar logs mientras la pestaña Consola no esté visible, salvo una carga puntual documentada. Inicio no necesita logs crudos.
- No recalcular tamaño completo del mundo, métricas de contenedor o conectividad en cada pulsación. Cachear, limitar frecuencia o mover esos cálculos a una tarea bajo demanda.
- Si una operación está en curso, impedir duplicados del mismo tipo/instancia; la UI muestra progreso, timeout y resultado.
- Al salir/cambiar de pantalla se cancelan las tareas que ya no tengan consumidor.
- Evitar renderizar todas las líneas de log: mantener un buffer acotado, por ejemplo 2.000 líneas, y usar viewport; truncar líneas excesivas con un indicador `…` o permitir scroll horizontal explícito.
- Añadir benchmarks para `View()` y navegación. Medir primero, optimizar después.

## 8. Estados visuales y mensajes

Todos los componentes deben diferenciar al menos:

- **Vacío:** qué falta y la acción para resolverlo.
- **Cargando:** indicador discreto y control todavía navegable si corresponde.
- **Éxito confirmado:** acción completada por el servicio y estado confirmado.
- **Enviado/no confirmado:** el comando salió, pero aún no hay lectura de verificación.
- **Advertencia:** la operación tiene un resultado parcial o requiere reiniciar.
- **Error recuperable:** mensaje breve, contexto útil y acción de reintento.
- **Error fatal:** indicar qué pantalla/operación no puede continuar, sin congelar toda la TUI.

No depender solo del color: usar texto e icono/etiqueta. No ocultar errores en un toast fugaz si implican que whitelist/configuración no coincide.

## 9. Criterios de aceptación de diseño

- Ninguna línea supera el ancho útil del terminal en los tamaños de referencia.
- Cabecera, indicador de pestaña, estado y barra de atajos permanecen en su ubicación con logs vacíos, 1 línea, 200 líneas y líneas muy largas.
- En terminal compacto, ningún control esencial desaparece sin una alternativa de navegación visible.
- En pantallas de formulario, cada campo muestra etiqueta, valor, validación y tecla de confirmación/cancelación.
- El texto activo/foco se distingue sin depender exclusivamente del color.
- No se mezclan los controles de consola con los atajos de la pestaña Jugadores/Acceso.
- No se muestran logs crudos fuera de Consola.

## 10. Plan de implementación

1. **Caracterizar:** añadir tests de regresión para whitelist, foco, barra inferior, dimensiones, respuestas tardías y carga al navegar.
2. **Unificar teclado:** localizar atajos globales del `App`, introducir captura de entrada por modal/editor y mover creación de instancia a `Ctrl+N` (o mantener `N` solo en Home sin input activo).
3. **Fijar layout:** header/tabs/body/status/action bar con alturas reservadas; viewport y ajuste de líneas por ancho visible.
4. **Arreglar whitelist:** servicio de lectura/operaciones, estado live vs saved, verificación después de cada mutación y actualización de UI.
5. **Reducir I/O:** debounce, límites de concurrencia, cancelación y protección contra respuestas obsoletas.
6. **Actualizar snapshots y pruebas visuales:** compacto, medio, ancho, consola, acceso, formulario y error.
7. **Retirar duplicidad con cuidado:** confirmar usos de `internal/tui/model.go`/vistas antiguas antes de eliminarlas. Mantener una única ruta probada para la TUI de producción.
8. **Merge gate:** formato, vet, build, tests normales y race detector obligatorios; la revisión visual ayuda, pero no sustituye la suite automática.
