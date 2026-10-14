# Encargo de implementación para el agente de código

Este brief se ejecuta dentro del checkout local de `badomens`. No es una invitación a generar solo recomendaciones: la tarea consiste en modificar código, añadir pruebas y entregar evidencias.

## Objetivo

Corregir los bugs de UX/funcionalidad de la TUI y aplicar `TUI_REDESIGN.md`, `TUI_ACCEPTANCE_TESTS.md` y `skills/tui-engineering/SKILL.md` como contrato de trabajo.

## Instrucciones de ejecución

1. Lee `AGENTS.md`, `CONTRIBUTING.md`, `go.mod`, `Makefile`, la skill de TUI, y los archivos de la ruta de producción.
2. Confirma el entrypoint de producción y traza `Run → NewApp → Router → Screen → Services`. Identifica código legacy y sus usos antes de modificarlo o borrarlo.
3. Reproduce primero cada fallo con tests. Añade un test de regresión para whitelist stale, la tecla `N` en un input, el contenido de consola fuera de su pestaña, las líneas largas que empujan el footer y respuestas asíncronas obsoletas.
4. Implementa en este orden: (a) captura de teclado/foco; (b) layout y viewport/footers; (c) servicio de whitelist con read-back/estado explícito; (d) reducción de I/O y respuestas obsoletas; (e) snapshots y tests.
5. No reescribas toda la TUI por gusto. Conserva el estilo/tema y los servicios existentes cuando sean correctos; cambia la menor superficie posible que permita tener una sola fuente de verdad.
6. Las letras imprimibles no deben despachar atajos globales mientras un input o modal captura teclas. Cualquier atajo global no imprimible debe respetar modal/foco.
7. Una operación de whitelist no se considera confirmada por tener éxito al enviar el comando. Después de mutar, consulta el estado y actualiza la vista; si no es verificable, informa el resultado como pendiente/no verificado.
8. Ninguna llamada a Docker, consola/RCON, filesystem o métricas puede bloquear `Update()`/`View()`. Cancela tareas que ya no aplican y descarta respuestas de otra instancia/pantalla.
9. Crea snapshots con datos representativos para `80×24`, `100×30`, `132×40`; incluye una línea de log de 500+ caracteres. Asegura que el footer conserva altura/posición y ninguna línea excede el ancho visual objetivo.
10. Ejecuta `gofmt`, `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...` y los targets documentados de `Makefile`. No mientas sobre resultados ni dependencias no disponibles.
11. Si el entorno permite generar capturas de terminal, guarda PNGs de Inicio, Consola, Jugadores/Acceso y formulario; revisa las imágenes con el subagente de visión. Si no es posible, registra la limitación y deja snapshots deterministas que se puedan inspeccionar.
12. Al terminar, edita los docs si el contrato cambia y entrega un resumen con causa confirmada, archivos modificados, pruebas ejecutadas/resultados, capturas revisadas y fallos pendientes.

## Criterio de finalización

La tarea no está lista si solo compila, si solo se ve mejor, si la whitelist se actualiza por optimismo sin verificación, si las pruebas solo ejercitan el modelo legacy o si falta la prueba que demuestra que `N` se escribe como texto con el input enfocado.
