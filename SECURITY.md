# Seguridad

## Reportar una vulnerabilidad

No abras un issue público. Envía un correo o un aviso privado al mantenedor del
repositorio con los detalles y, si puedes, una reproducción. Responderemos lo
antes posible.

## Principios

- **Nada de secretos en el repositorio.** `.env` está en `.gitignore`; los
  ejemplos van vacíos.
- **Superficie de red mínima.** Los puertos de cada servidor se publican
  únicamente en la IP del tailnet (o en localhost), nunca en `0.0.0.0`. Si no se
  detecta la IP, el arranque se aborta en lugar de exponer el puerto.
- **Sin shell.** Los procesos externos (Docker, Tailscale) se ejecutan con
  argumentos separados, no interpolados en una cadena.
- **Datos locales a salvo.** Instancias, mundos, backups y configuración viven
  fuera del repositorio.
- **Operaciones destructivas con red de seguridad.** Antes de tocar un mundo o
  cambiar de motor, la instancia debe estar detenida y se recomienda un backup.

## Alcance

badomens gestiona contenedores de Docker Compose creados por él. Rechaza operar
sobre contenedores que no gestiona (por etiquetas de Compose) para no dañar
recursos ajenos.
