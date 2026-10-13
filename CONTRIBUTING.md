# Contribuir a badomens

¡Gracias por querer ayudar! 🧱

## Antes de empezar

- Go 1.27 o superior.
- `make build`, `make test` deben pasar.
- Ejecuta `gofmt -w .` antes de enviar cambios.

## Flujo

1. Crea una rama con un nombre descriptivo (`feature/multi-instancia`).
2. Haz cambios pequeños y enfocados, con pruebas cuando aplique.
3. Verifica: `make fmt vet test`.
4. Abre un Pull Request explicando el qué y el por qué.

## Reglas del proyecto

- **El contrato JSON no se rompe** sin versionarlo: claves estables
  (`ok`, `exit_code`, `action`, `message`, `timestamp`), códigos de salida
  0/1/2 y códigos de error estables.
- **Nada de secretos en el repo**: usa `.env.example` y ejemplos genéricos. No
  incluyas IPs reales, seeds ni tokens.
- No uses shell: los procesos externos se ejecutan con argumentos separados
  (`internal/execx`).
- Los datos locales (instancias, mundos, backups, `.env`) nunca se suben.

## Estilo

- Comentarios y documentación en español, como el resto del proyecto.
- Errores tipados con `internal/errx` (código, mensaje y pista).
- Una funcionalidad se escribe una sola vez: el registro de herramientas
  (`internal/tools`) alimenta MCP y, con él, al agente de Emiria.
