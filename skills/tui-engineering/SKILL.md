---
name: tui-engineering
description: Diseña, implementa, depura y verifica interfaces TUI de terminal con foco en UX, enrutamiento de teclado, layout responsivo, concurrencia, rendimiento y pruebas de flujos completos. Úsala ante cambios de pantallas, formularios, listas, consola/logs, atajos, rendering, bugs de foco o regresiones visuales.
---

# Skill: ingeniería de TUI robustas

## Propósito

Construir interfaces de terminal que sean seguras ante errores de entrada, legibles en varios tamaños, fluidas bajo carga y verificables sin depender del juicio subjetivo del agente. La TUI forma parte del contrato del producto: una pantalla bonita que pierde teclas, confirma operaciones falsas o se congela es un fallo.

## Activación

Usa esta skill cuando la tarea involucre una TUI existente o nueva, incluida la modificación de pantallas, formularios, pestañas, comandos de consola, logs, menús, navegación, accesibilidad, rendimiento o tests de interfaz.

## Reglas no negociables

1. Inspecciona el código real y encuentra el entrypoint que ejecuta producción antes de cambiar nada.
2. No supongas que tests de un modelo viejo cubren una arquitectura nueva. Traza la ruta `Run → App → Router → Screen → Service` (o los equivalentes del repo).
3. Nunca ejecutes I/O bloqueante en el loop de eventos ni dentro de `View()`.
4. Un input/modal con foco captura las teclas antes de los atajos globales. Las teclas imprimibles no son atajos globales mientras un campo captura entrada.
5. Los estados y resultados de dominio deben proceder de servicios, no de strings del renderizador ni de notificaciones optimistas sin verificación.
6. Layout significa controlar ancho y alto de cada región. Cabecera, pestañas, mensajes/estado y atajos no deben desplazarse con la lista o los logs.
7. Usa ancho visual de terminal, no `len(text)` o conteo de runas para gestionar ANSI, emoji, CJK o caracteres combinados.
8. Los datos asíncronos deben incorporar contexto de cancelación, timeout, identidad del recurso y protección contra respuestas tardías.
9. No retires una implementación duplicada hasta comprobar imports, entrypoints, snapshots y pruebas. Si existe una ruta legacy, declara cuál está en producción.
10. Nunca informes una prueba como pasada si no se ejecutó realmente. Distingue `pasó`, `falló`, `no ejecutada` y `requiere entorno real`.

## Flujo obligatorio

### Fase 1 — Entender antes de codificar

- Leer instrucciones locales (`AGENTS.md`, contribución, skill del proyecto y README relevante).
- Identificar lenguaje/framework, entrypoint de producción, router, pantallas, overlays, servicio/backend, tema/layout y comandos de tests.
- Seguir el flujo completo del bug: tecla/evento → router → handler → servicio → mensaje de resultado → modelo → render.
- Identificar fuente de verdad y si un estado mostrado es live, persistido, cacheado o desconocido.
- Registrar hipótesis verificables; no confundir una hipótesis con causa confirmada.
- Crear una lista de criterios de aceptación antes de aplicar refactor.

### Fase 2 — Modelar la interacción

Para cada pantalla o modal, documenta:

- propósito y datos mostrados;
- acciones disponibles y sus efectos;
- tecla/gesto y ámbito del atajo;
- elemento que tiene el foco;
- transiciones de entrar, confirmar, cancelar, error, recarga y salir;
- estados `loading`, `empty`, `success`, `warning`, `error` y `stale`, cuando apliquen;
- comportamiento con terminal compacto, medio y ancho.

Dibuja un wireframe textual en el PR/spec para cambios que afecten layout. No intentes resolver un fallo de estado solo con estilos.

### Fase 3 — Implementar con límites claros

En Bubble Tea:

- Mantener el patrón Elm: `Update` muta estado y emite `tea.Cmd`; `View` solo representa estado.
- Usar una pantalla/router central o arquitectura equivalente, no varios routers de atajos superpuestos.
- Centralizar la precedencia de teclado: modal/confirmación → editor con foco → control enfocado → pantalla activa → atajos globales.
- Preferir combinaciones explícitas para acciones globales (`Ctrl+P`, `F1`, etc.). Una letra normal como `n` siempre debe poder escribirse en un campo.
- Encapsular llamadas remotas/de sistema en servicios inyectables. Para tests, ofrecer fake/double determinista.
- No declarar éxito confirmado por el mero envío de una orden; verificar el estado cuando la semántica del producto lo permita.
- Limitar poll/ticks concurrentes, cancelar tareas no usadas, aplicar debounce de navegación y descartar mensajes con IDs/slug obsoletos.
- Reservar alturas de cabecera, cuerpo, estado y barra de atajos. Renderizar logs/listas en un viewport con límites conocidos.
- Usar `lipgloss.Width` u otro medidor visual; limpiar o medir ANSI con una biblioteca apropiada. Nunca truncar primero el texto coloreado con operaciones de string ciegas.

En otros frameworks, conserva los principios equivalentes: actualización pura, comandos asíncronos, foco explícito, layouts acotados y estado tipado.

### Fase 4 — Pruebas funcionales y de estado

Añade pruebas deterministas para:

- la tabla de rutas de teclas por pantalla;
- texto imprimible dentro de cada input;
- `Esc`, `Enter`, tabulación, navegación y devolución/restauración del foco;
- validaciones de formulario, vacío, éxito, error y reintento;
- que una acción no se ejecute cuando no está disponible o ya está ocupada;
- que una acción de una instancia nunca se envíe a otra;
- comandos enviados vs operaciones confirmadas;
- refresco tras mutaciones;
- respuestas tardías después de cambiar pantalla/instancia;
- errores de timeout, cancelación y servicio no disponible.

Los tests de key routing deben probar la aplicación/router de producción, no solo un handler auxiliar. Si existen atajos globales, prueba que no se ejecutan cuando un modal/editor captura entrada.

### Fase 5 — Pruebas de layout y regresión visual

Probar como mínimo las dimensiones `80×24`, `100×30` y `132×40`, además de una terminal excepcionalmente estrecha y una muy baja.

Capturar snapshots de:

- inicio con cero instancias;
- inicio con varias instancias;
- resumen de instancia;
- consola vacía, normal, con muchas líneas y una línea de log muy larga;
- formulario/modal con foco;
- lista de jugadores/whitelist vacía y poblada;
- estado loading, error y verificación pendiente;
- cambio de tamaño mientras la pantalla de consola está activa.

Criterios automáticos:

- ninguna línea visible excede el ancho objetivo medido en celdas;
- el número de líneas del footer y su posición no dependen del contenido de logs;
- todos los nombres de pestaña/acción son accesibles por texto, atajo o navegación explícita;
- snapshots normalizados con ANSI removido (además de capturas ANSI si es útil);
- goldens intencionales revisados, no regenerados ciegamente para silenciar un diff.

### Fase 6 — Prueba visual con modelo de visión

Cuando haya capturas, enviar imágenes reales de los tamaños de referencia a un modelo de visión/subagente y pedir revisión con esta rúbrica:

1. ¿Se corta, solapa o envuelve de manera imprevista algún contenido?
2. ¿El título, la pestaña activa, el estado y los atajos se distinguen inmediatamente?
3. ¿Puede identificarse dónde escribir y cuál control tiene el foco?
4. ¿Se ve alguna consola/log fuera de la pantalla que promete mostrarla?
5. ¿La barra inferior está fija y visible?
6. ¿Hay demasiada densidad visual, texto pegado al borde o espacio útil desperdiciado?
7. ¿Los errores, estados desconocidos y acciones peligrosas están descritos con claridad?
8. Compara imágenes de todos los anchos y señala problemas concretos por coordenadas/área, no una opinión genérica.

El modelo de visión complementa, pero no reemplaza, los tests de estado, la medición de líneas ni el test de terminal/PTY. Registra el modelo, las capturas y los hallazgos accionables. No afirmes que se validó visualmente si no se examinó una imagen.

### Fase 7 — Rendimiento

- Crear benchmark para la función de render y/o navegación relevante; medir antes y después.
- Verificar que una tecla solo actualiza estado local, sin ejecutar servicios bloqueantes.
- Simular servicios lentos y respuestas fuera de orden.
- Comprobar que no se acumulan ticks, goroutines o consultas al mantener la TUI abierta.
- Mantener logs en buffer acotado y evitar recalcular métricas caras para eventos de teclado.
- Ejecutar detector de carreras en Go si aplica.

### Fase 8 — Merge gate

Para Go, ejecutar según soporte del repositorio:

```bash
gofmt -w <archivos-modificados.go>
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

Si el repo define `make test`, `make lint`, `make ci` o un comando snapshot propio, respetar esos scripts y reportarlos. No iniciar una TUI interactiva desde CI sin PTY controlado. Diferenciar tests herméticos de los que requieren Docker, Minecraft, RCON o Tailscale.

## Uso recomendado de los modelos

La separación es orientativa; la CI conserva la autoridad sobre resultados verificables.

- **Haiku (planificación de flujo completo):** mapear pantallas/transiciones, escribir criterios de aceptación, revisar consistencia entre pantallas y preparar el plan de pruebas antes de ejecutar refactors amplios.
- **DeepSeek V4 Flash (implementación/análisis):** desarrollar cambios acotados por capa, añadir tests de estado, ejecutar comandos de verificación y entregar un resumen de diff/resultados.
- **DeepSeek V4 Flash Vision EXP (revisión visual):** evaluar PNG/capturas de snapshots en todos los tamaños, detectar clipping, densidad, jerarquía, foco aparente y footer móvil. Debe devolver hallazgo, captura/viewport afectado, severidad y sugerencia concreta.
- **Agente coordinador:** no aceptar `PASS` visual o funcional sin evidencia. Resuelve discrepancias, verifica que los requisitos se cubrieron y publica el informe de cierre.

No repartir el cambio sin contrato compartido. Antes de delegar, escribir un solo artefacto con pantallas, estados, key map, límites de layout y pruebas obligatorias. Los agentes trabajan sobre ese contrato y no inventan atajos incompatibles.

## Formato de entrega requerido

Al terminar, informar:

1. causa confirmada de cada bug (o decir explícitamente si quedó como hipótesis);
2. archivos y capas modificadas;
3. cambios de interacción/layout con antes/después resumido;
4. comandos de tests ejecutados y resultado real;
5. snapshots/capturas y revisión visual realizada;
6. pruebas no ejecutadas y dependencias de entorno;
7. riesgos abiertos y siguiente paso más pequeño.

No cerrar una tarea de TUI solo porque compila o el agente de visión dice que se ve bien.
