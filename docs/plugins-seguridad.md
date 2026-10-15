# Seguridad de la descarga de plugins/mods (11.1 · AC-08 · API-07)

Documento de la fase **11.1 "Descarga segura"**. Describe qué controles se
aplican al instalar un plugin/mods desde una URL o desde un archivo local, con
qué límites por defecto y **qué no se puede garantizar**.

Ámbito: `internal/plugins/**` (catálogo + instalación), `internal/plugins/download`
(descargador), `internal/plugins/archive` (inspección del JAR/ZIP) y
`internal/cli/plugin.go` (comando `plugin add`). Los marketplaces (11.2) y las
actualizaciones con procedencia (11.3) **no** están implementados todavía.

## Separación de responsabilidades

| Módulo | Responsabilidad |
|--------|-----------------|
| `internal/plugins` | Catálogo de la instancia: carpeta destino según motor, alta/baja/listado (`Dir`, `List`, `Remove`) y orquestación de la instalación (`Install`, `Add`). No realiza red ni inspecciona archivos. |
| `internal/plugins/download` | Obtención del artefacto: streaming con límite, archivo temporal en el directorio destino, verificación de tamaño y SHA-256, política de conflicto de nombres y **movimiento atómico**. |
| `internal/plugins/archive` | Inspección estructural del JAR/ZIP con límites de recursos. Solo lee la firma y el directorio central: **no descomprime nada**. |

Flujo (`Installer.Install`):

1. Validar motor, origen y política.
2. Descargar (o copiar, si es ruta local) a un temporal `.badomens-download-*.tmp`
   **dentro de la carpeta destino** (mismo sistema de archivos ⇒ el paso 5 es un
   `rename` atómico).
3. Aplicar el límite DURANTE la copia (`io.LimitReader` + copia por bloques,
   nunca `io.ReadAll`) y calcular el SHA-256 al vuelo.
4. Verificar checksum esperado (si se indicó) e inspeccionar la estructura
   JAR/ZIP con límites.
5. Mover el temporal a su destino final de forma atómica.
6. El comando actualiza el **registro persistente** (`spec.Plugins`) solo aquí.
   Si el guardado falla, retira el archivo recién instalado para no dejar el
   registro y el disco desincronizados.

`Add(ctx, spec, source)` se conserva para los llamadores existentes
(`internal/provision`) y aplica la política por defecto.

## Límites y políticas por defecto

| Control | Valor por defecto | Configurable |
|---------|-------------------|--------------|
| Tamaño máximo del artefacto | **64 MiB** aplicados durante la transferencia | `--max-size` (hasta un techo absoluto de 1 GiB) |
| Protocolo | **solo `https://`** | `--allow-http` (riesgo asumido) |
| Destinos de red | **bloqueados**: loopback, privados, link-local, CGNAT, multicast, reservados y documentación | `--allow-loopback`, `--allow-private` (riesgo de SSRF asumido) |
| Redirecciones | máximo **5**, validando esquema y destino en **cada salto** | `download.Policy.MaxRedirects` |
| Tiempo total de la operación | **2 minutos** | `download.Policy.Timeout` |
| Extensión del archivo final | **`.jar`, `.zip`** | `download.Policy.AllowedExtensions` |
| Nombre del archivo | rechazado si está vacío, empieza por punto, tiene separadores, `..`, caracteres de control, punto/espacio final, >120 caracteres o es un nombre reservado de Windows (`CON`, `LPT1`…) | — |
| Archivo ya existente | **error explícito** (`fail`) | `--on-conflict=rename|replace` |
| Temporales huérfanos (proceso de descarga interrumpido) | se limpian al instalar si superan 1 hora; nunca aparecen en `plugin list` | `download.CleanStaleTemps` |
| Entradas del JAR/ZIP | **4096** | `archive.Limits.MaxEntries` |
| Bytes descomprimidos declarados | **512 MiB** en total, **128 MiB** por entrada | `archive.Limits` |
| Compresión sospechosa | ratio **> 200:1** en entradas de ≥1 MiB | `archive.Limits.MaxRatio` |
| Entradas cifradas | rechazadas | — |
| Entradas con ruta insegura (`/…`, `..`) | rechazadas | — |

### Rangos de red rechazados por defecto

`0.0.0.0/8`, `10.0.0.0/8`, `100.64.0.0/10`, `127.0.0.0/8`, `169.254.0.0/16`,
`172.16.0.0/12`, `192.0.0.0/24`, `192.0.2.0/24`, `192.88.99.0/24`,
`192.168.0.0/16`, `198.18.0.0/15`, `198.51.100.0/24`, `203.0.113.0/24`,
`224.0.0.0/4` (multicast), `240.0.0.0/4`, `255.255.255.255/32`,
`::/128`, `::1/128`, `fc00::/7`, `fe80::/10`, `ff00::/8`, `64:ff9b::/96`,
`100::/64`, `2001::/32`, `2001:db8::/32`, `2002::/16`. También se rechazan los
nombres `localhost`, `*.localhost`, `*.local`, `*.internal`, `*.home.arpa` y
`*.lan`, así como las IPv4 embebidas en IPv6 (`::ffff:10.0.0.1`).

La comprobación se hace en **dos puntos**: al validar la URL (mensaje temprano,
útil para el usuario) y en el `ControlContext` del dialer, es decir sobre la **IP
realmente resuelta** justo antes de abrir el socket. Ese segundo punto es el que
cierra el **DNS rebinding** (un hostname de confianza puede resolver a una IP
privada después de validar la URL). Cada salto de redirección se vuelve a validar
con las mismas reglas.

### Rutas locales y política explícita

Una ruta de archivo local (sin `esquema://`) se considera la **vía avanzada
explícita**: el usuario nombra un artefacto que ya tiene en disco. Aun así pasa
por las mismas garantías: nombre validado, extensión permitida, límite de tamaño,
checksum opcional, inspección estructural, temporal y movimiento atómico. Se
rechazan esquemas distintos de `http`/`https` (`ftp://`, `file://`…).

Un `http://` sin cifrado solo se acepta con `--allow-http`; un destino
loopback/privado solo con `--allow-loopback`/`--allow-private`. Estos flags son
política explícita y **desactivan una protección**: úsalos únicamente con
artefactos y orígenes en los que confíes.

## Contrato JSON (aditivo)

`plugin add` conserva sus claves estables (`ok`, `exit_code`, `action`, `message`,
`data`, `phases`, `error`, `timestamp`) y su mensaje humano. Campos **nuevos** en
`data`: `source`, `size_bytes`, `sha256`, `checksum_verified`, `archive`
(`entries`, `uncompressed_bytes`, `compressed_bytes`, `max_ratio`,
`has_manifest`, `descriptor`), `security_warning` y, cuando aplican,
`renamed_from`, `replaced` y `warning` (rendimiento). El código de error de un
fallo HTTP sigue siendo `error_unavailable` (sin regresión) y añade
`details.http_status`.

### Códigos y motivos

| Situación | `error.code` | `details.reason` |
|-----------|--------------|------------------|
| HTTP 404/403/5xx | `error_unavailable` | `http_error` (+ `http_status`) |
| Fallo de red/DNS | `error_unavailable` | — |
| Destino loopback/privado/link-local | `error_usage` | `destino_no_permitido` |
| `http://` sin política | `error_usage` | `tls_requerido` |
| Esquema no soportado | `error_usage` | `esquema_no_soportado` |
| Nombre final inválido | `error_usage` | `nombre_invalido` |
| El archivo ya existe | `error_usage` | `destino_existente` |
| Exceso de redirecciones | `error_usage` | `redirecciones_excesivas` |
| Límite de tamaño superado | `error_capacity_exceeded` | `limite_excedido` |
| Límites de recursos del JAR/ZIP | `error_capacity_exceeded` | `limites_recursos` |
| JAR/ZIP corrupto, truncado o con entradas inseguras | `error_usage` | `artefacto_invalido` |
| SHA-256 no coincide | `error_general` | `checksum_no_coincide` |
| Archivo local inexistente | `error_not_found` | — |

## Aviso sobre código de terceros

Instalar un plugin o un mod es **instalar código ejecutable**, no copiar un
archivo. `plugins.ThirdPartyWarning()` acompaña siempre al resultado y el mensaje
evita promesas verificables: se comprueba la *forma* del archivo (JAR/ZIP
estructuralmente válido, tamaño, integridad frente a un checksum publicado), no
su contenido.

## Lo que NO se puede garantizar

- **Que el artefacto sea seguro**: no se analiza el bytecode ni el
  comportamiento. Un JAR válido e íntegro puede ser malicioso.
- **Que el contenido descomprimido cumpla los límites**: la inspección lee las
  cabeceras del ZIP; los tamaños declarados pueden mentir. Nunca se descomprime
  en el directorio de ejecución (ni en ningún otro), así que un *zip bomb* no se
  materializa, pero tampoco se valida byte a byte.
- **Compatibilidad** con el motor, el cargador o la versión de Minecraft: no se
  comprueba (llega con 11.2/11.3).
- **Autenticidad del origen**: no hay firma ni cadena de confianza; el checksum
  SHA-256 solo es útil si lo publica el propio proveedor por un canal fiable.
- **Integridad frente a HTTP**: con `--allow-http` el artefacto puede ser
  alterado en tránsito.
- **Que el servidor arranque**: un plugin incompatible puede impedir el arranque.
- **Protección frente a un proxy de entorno**: si `HTTP_PROXY`/`HTTPS_PROXY` está
  configurado, la conexión se hace contra el proxy (cuya IP sí se valida) y es el
  proxy quien elige la IP final; la garantía anti-SSRF plena se da sin proxy.
- **Reemplazo atómico en Windows**: al usar `--on-conflict=replace` en Windows
  hay una ventana mínima entre el borrado y el renombrado (en POSIX el `rename`
  es atómico). Sin esa política nunca se borra nada existente.
- **Vigilancia continua**: la comprobación ocurre en la instalación; un archivo
  instalado antes del endurecimiento no se revalida.

## Cómo verificar

```bash
gofmt -l internal/plugins internal/cli
go build ./...
go vet ./internal/plugins/ ./internal/cli/
go test ./internal/plugins/ -count=1 -v      # descargador, inspección y CLI
go test ./internal/cli/ -count=1
```

Pruebas que cubren AC-08: error HTTP 404/500 sin archivo final ni registro;
límite superado con temporales limpios; nombres inválidos (`../evil.jar`,
`a/b.jar`, vacío, `.txt`); JAR corrupto/truncado; checksum incorrecto vs correcto;
URL a `localhost`/`127.0.0.1`/`10.0.0.1`/`169.254.169.254`/`192.168.x`; redirección
a IP privada; `http://` rechazado por defecto; instalación correcta con registro
actualizado una sola vez; ausencia de archivos `.tmp` tras cualquier fallo.

Para evidenciar que los criterios fallan contra la implementación anterior, se
replicó el `download` previo (`plugins.go:127-164`) en un arnés aparte: aceptaba
1 MiB pese a un límite de 4 KiB, aceptaba `http://127.0.0.1`, sobrescribía en
silencio y instalaba basura que no era un JAR.

## Pendientes para 11.2 y 11.3

- 11.2 Marketplaces: adaptadores Modrinth y Hangar (CurseForge exigirá API key),
  catálogo/resolvedor con caché y compatibilidad por motor y versión de Minecraft;
  la URL directa y la ruta local quedan como vía avanzada sobre `download`.
- 11.3 Actualizaciones y dependencias: registrar proyecto de origen, versión
  instalada/objetivo y checksum; comprobar actualizaciones sin instalarlas y
  advertir de dependencias ausentes sin resolverlas de forma arbitraria.
- Reutilizar `download.Request.SHA256`, `archive.Validate` y el registro de
  `Installed` para la procedencia; no crear clientes HTTP ni parsers paralelos.
- Endurecimiento adicional pendiente (fuera del alcance de 11.1): pin de
  certificado o firma del artefacto, lista de orígenes permitidos y revalidación
  de lo ya instalado.
