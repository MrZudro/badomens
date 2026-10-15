# 📜 Changelog

Todas las novedades relevantes de **badomens** se documentan aquí ✨.
El formato sigue [Keep a Changelog](https://keepachangelog.com/es/1.1.0/) y el
versionado es [semántico](https://semver.org/lang/es/).

## [Unreleased]

## [1.3.1] - 2026-10-14

### 🐛 Corregido

- **La biblioteca de mundos quedaba accesible por grupo y otros**: `copyDir`
  creaba la raíz de `<data>/worlds` con `0755` (permiso pensado para que *root*
  lea los mundos dentro del contenedor), así que la biblioteca no cumplía el
  criterio de mínimos privilegios. Ahora `SaveWorld` e `ImportFromZip` crean la
  raíz con **`0700`** antes de copiar; los directorios internos siguen en `0755`
  porque el contenedor sí necesita leerlos al desplegar el mundo activo.
  Lo detectó la CI de Linux; la misma corrida confirmó que **no hay condiciones
  de carrera** en ninguna prueba (`go test -race ./...`).

## [1.3.0] - 2026-10-14

> 🧭 **Cierre del plan de implementación unificado (fases 0–12)**. Fusiona las dos
> especificaciones (técnica y de
> bugs/UX): transporte HTTP, los cinco proveedores, selección y compatibilidad
> trivalente, caché resiliente, servicio de catálogo único para CLI/TUI/MCP, la
> TUI (componentes base, flujos de creación, funciones de instancia y pulido),
> daemon/Docker/Tailscale, descarga segura de plugins, marketplaces y el cierre
> de observabilidad y seguridad del runtime. **Antes de leer lo demás, mira los
> cambios de SEMÁNTICA**: hay tres que afectan a quien consume el contrato JSON.
> La matriz defecto → prueba está en `docs/REGRESSION_MATRIX.md`.

### 🔄 Cambios de SEMÁNTICA (léelos antes de consumir el contrato JSON)

- **`compatible` pasa a derivarse de la compatibilidad trivalente (Fase 3,
  T-03/API-08).** Antes, `Enrich` rellenaba `compatible: true` a ciegas cuando el
  dato era desconocido (`if !out[i].Compatible { out[i].Compatible = true }`).
  Ahora `compatible` es cierto **solo cuando la compatibilidad está verificada**
  (`compatibility == "compatible"`). Consecuencia: un `compatible: false` puede
  significar `incompatible` **o** `unknown`. Consulta el campo nuevo
  `compatibility` (`compatible` / `incompatible` / `unknown`) antes de tratar una
  versión como no seleccionable. La caché y el catálogo embebido anteriores a
  esta versión (que traían `compatible: true` sin el campo trivalente) se migran
  a `compatible` verificado.
- **`java_major` puede valer `0` (desconocido) y ya NO se asume Java 8 (Fase 3,
  AC-05).** `JavaMajorFor` devolvía `8` para cualquier versión no reconocida;
  ahora devuelve `0` y el campo nuevo `java_status` dice el origen del dato:
  `verified` (lo declara el proveedor, p. ej. `java.version.minimum` de Fill v3),
  `inferred` (regla versionada de badomens) o `unknown`. Un `java_major: 0` es un
  dato **desconocido**, no un Java 8. `JavaImageFor` conserva su `fallback`
  anterior, así que la imagen Docker de una versión desconocida **no** se
  degrada.
- **El mapeo de Java incorpora Java 25 y Minecraft 26.x (Fases 2–3).** Se
  reconoce el versionado por año/entrega: `26.1+` → **Java 25** (nueva etiqueta
  `java25` en `JavaTag`). Reglas vigentes: `26.1+` → 25; `1.20.5–1.21.x` → 21;
  `1.18–1.20.4` → 17; `1.17` → 16; `≤1.16.x` → 8; series futuras o no
  reconocidas → **desconocido**.
- **El contrato JSON NO se ha roto (AC-09).** Se conservan `ok`, `exit_code`,
  `action`, `message`, `data`, `phases`, `error` y `timestamp`, y **todos los
  comandos existentes** mantienen sus claves, su `exit_code` y sus códigos de
  error. Todas las claves nuevas son **aditivas**: van dentro de `data` (o de
  `error.details`) y ningún consumidor existente (Emiria/Jamcord, scripts)
  necesita cambios. Pruebas: `internal/result/contract_test.go`
  (`TestContratoClavesEstables`) y `internal/cli/paridad_catalogo_test.go`
  (`TestContratoJSONCatalogoAditivo`).

### 🐛 Corregido

#### Fase 1 — Transporte HTTP común (`internal/engines/client.go`)
- **Reintentos de verdad (API-01).** El tipo `Client` anunciaba "reintento
  mínimo" en un comentario pero no tenía bucle alguno. Ahora reintenta errores de
  conexión, timeouts, 429 y 5xx (500/502/503) con **backoff exponencial con
  jitter**, respeta `Retry-After` (segundos y fecha HTTP) y **no reintenta**
  400/401/403/404. La política **no** depende del texto del error (nuevo
  `TestClientNoReintentaAunqueElTextoParezcaTransitorio`).
- **`User-Agent` identificable (API-02).** Ya no es el fijo `badomens/0`: la
  cabecera se compone en un único sitio (`DefaultUserAgent`) con el nombre, la
  **versión real** del binario y la URL de contacto, como exige Paper Fill.
- **Timeouts diferenciados** (conexión, TLS, cabeceras y cuerpo) más límite total
  por `context.Context`; **límite de tamaño** de respuesta para JSON y XML;
  validación de estado HTTP, `content-type` y formato; **errores tipados**
  (`timeout`, `rate_limited`, `upstream_unavailable`, `invalid_response`,
  `schema_mismatch`, `not_found`, `response_too_large`); soporte de
  `ETag`/`Last-Modified`; diagnóstico con proveedor, operación y URL **saneada**
  (sin credenciales ni cuerpos de respuesta).
- **Cabecera `Accept` con rangos de medios válidos** (corrección posterior del
  propio transporte).

#### Fase 2 — Reparación de cada proveedor
- **Paper migra a Fill v3 (API-03, AC-01)** y el catálogo deja de estar roto. Se
  usan `GET /v3/projects/paper/versions` y `.../versions/{version}/builds`; el
  esquema v3 (array de **objetos**) se transforma explícitamente; se elige un
  build **estable con artefacto de servidor descargable** y **una versión no está
  resuelta hasta tener el pin de build exacto** (se acabó el "éxito parcial": un
  404/500 o una lista vacía de builds ahora propagan error tipado).
- **Fabric usa el endpoint correcto y no acepta resolución sin loader (API-04,
  AC-02).** Se consulta `/v2/versions/loader/{game_version}` (no el catálogo
  general) y se **rechaza** la resolución si el loader no se confirmó.
- **Forge deja de compartir parser con NeoForge (API-06, AC-03).** Se parsea
  `promotions_slim.json` como lo que es (claves `recommended`/`latest` por versión
  del juego) y se contrasta contra artefactos descargables; un JSON válido con
  otra estructura produce `schema_mismatch` en vez de un catálogo vacío.
- **NeoForge con parser XML propio (API-06, AC-04).** Se parsea el
  `maven-metadata.xml` oficial (el `.json` no existe: 404), se soportan los dos
  esquemas de versión (antiguo `21.x` y moderno `26.x`) con asociación explícita
  Minecraft ↔ NeoForge, se **detecta la caída anómala** del número de versiones
  (conservando el último catálogo validado) y **nunca se ordena alfabéticamente
  entre esquemas**.
- **Vanilla endurecido.** Manifiesto oficial `version_manifest_v2`, releases y
  snapshots diferenciados y mapeo de Java actualizado (Java 25 / 26.x).
- **Se elimina el patrón que silenciaba errores**
  (`if err := client.GetJSON(...); err == nil { /* continuar */ }`): si el dato es
  obligatorio, el error se propaga.

#### Fases 3-4 — Selección exacta, compatibilidad y caché
- **`latest` correcto con independencia del orden de respuesta (API-05).** `pickVersion`
  ya no asume que el último elemento es el más reciente: compara versiones
  **estructuradas** con la estrategia del esquema del motor. Fabric Meta y Paper
  Fill v3 (que publican newest-first) dan el mismo resultado que una lista
  ascendente.
- **`Recommended` se calcula DESPUÉS de normalizar y ordenar (QC-05, AD-02b).** Se
  recalcula desde cero en cada pasada, así que la recomendada es la última estable
  real y ninguna normalización posterior deja marcas obsoletas.
- **Orden por esquema, nunca alfabético entre esquemas.** `Enrich` ordena con la
  estrategia de cada motor: Vanilla por la fecha del manifiesto (dato oficial) y
  Paper/Fabric/Forge/NeoForge por componentes numéricos (`26.10 > 26.2`,
  `26.1 > 1.21.1`).
- **`NormalizeMaven` con reglas propias por proveedor** (Forge `<mc>-<build>`;
  NeoForge con su asociación explícita antiguo/moderno) y elección del build por
  comparación estructurada. Es idempotente.
- **Compatibilidad trivalente (T-03, API-08) y modelo normalizado con
  procedencia**: `loader_version`, `build`, `channel`, `released_at`,
  `java_major`, `java_status`, `compatibility`, `source`, `fetched_at`, `stale` y
  `warning`.
- **Se rechazan las versiones explícitas inexistentes o incompatibles**, con
  error tipado `not_found` y sin resultado parcial (T-02/AC-06).
- **`Resolved.Validate` + `Registry.Resolve`**: ruta única que valida **todos** los
  pins obligatorios antes de devolver. No existe el objeto de resolución
  parcialmente válido.
- **Caché: no se sobrescribe con datos inválidos (AC-07).** Un catálogo vacío o
  una **reducción anómala** (menos del 50 % del último validado, con un baseline
  de al menos 20 versiones) conservan el catálogo previo y generan un aviso
  legible. NeoForge añade su propio baseline de versiones publicadas. La escritura
  es **atómica** y hay **deduplicación** de solicitudes simultáneas.

#### Fase 5 — Cliente y proveedores en una sola ruta
- **`CatalogService` es el único punto de entrada** para CLI, TUI y MCP; ningún
  consumidor vuelve a hablar con `provider.Resolve`. La TUI deja de reconstruir
  reglas de resolución.
- **Operaciones que modifican runtime informan su desenlace (AC-10):** `applied`,
  `partial` o `failed`, con sus `phases` y un `follow_up` con el reintento exacto.
  Ninguna operación informa de éxito antes de verificar (T-08).

#### Fase 6 — Componentes base de la TUI
- **Selector unificado** (lista y grid, con las cuatro flechas y selección por
  **valor**) reutilizado por creación, regeneración y ajustes; se eliminan las
  implementaciones propias.
- **Barra inferior única** con formato `[Tecla] Etiqueta` y contextual; `Ctrl+P`,
  `F2` y `Ctrl+Q` salen de la barra pero siguen funcionando y se documentan en
  Ayuda.
- **Sistema central de notificaciones con TTL** (5-8 s info/éxito, hasta 30 s
  error/aviso; **máximo absoluto 30 s** y descarte manual). Lo persistente
  (MagicDNS) vive en Salud, no como aviso flotante.
- **Estado único del servidor + semáforo** 🟢/🟡/🔴 **con texto** (nunca solo
  color), visible en todas las sub-pantallas; las consultas asíncronas se
  cancelan al cambiar de instancia y una respuesta tardía no pisa otro estado.
- **Prioridad de teclado determinista** (modal → input → control → pantalla →
  globales): las letras imprimibles se escriben literalmente mientras un
  formulario captura el foco.
- **MagicDNS se reevalúa en vivo.** El aviso nació de leer el `MagicDNS`
  **persistido** en el registro de la instancia (`connection.Derive` lo emitía
  siempre que estuviera vacío), así que nunca desaparecía. Ahora la TUI consulta
  el estado real de Tailscale (`tailscale.Probe`) y el aviso vive en Salud.

#### Fase 7 — Creación rápida y avanzada
- **`↓` nunca avanza de paso.** El paso cambia con `Enter` sobre la celda
  «Continuar a Recursos →» (o `Tab`); `↑/↓` mueven la selección dentro del paso.
- **Panel lateral de versiones en grid**, agrupado por versión mayor, con la
  última estable marcada, snapshots/betas separadas y atajo `[R]` de refresco
  (`catalog refresh`). Si la carga falla: error claro con reintento y etiqueta de
  caché vencida, nunca una lista vacía silenciosa.
- **AD-02 (se elige un motor/versión y arranca otro) resuelto:** al cambiar de
  motor se **resetea** la versión y la resolución (se descartan las respuestas
  tardías por número de secuencia), la selección va por **valor**, el payload se
  construye desde el objeto `Resolved` del servicio y la config **persistida se
  verifica contra el resumen** (si no coinciden, se avisa; no hay éxito
  silencioso).

#### Fase 8 — Daemon, Docker, Tailscale y MagicDNS
- **Mecanismo común detectar → levantar → elevar → informar** en
  `internal/infra`, con estados tipados (`ready`, `starting`, `down`,
  `not_installed`, `needs_elevation`, `needs_login`, `degraded`, `unavailable`),
  sondeo con estado «Cargando» 🟡, cancelable, y **pasos manuales por SO**.
  Ninguna elevación se pide sin explicar el motivo y ningún fallo bloquea el resto
  de la app.
- **Servicio persistente del daemon** para Linux (systemd usuario/sistema), macOS
  (launchd agent/daemon) y Windows (tarea programada `ONLOGON`), sin instalar
  nada en silencio.
- **Todos los procesos se lanzan con argumentos separados** (`internal/execx`):
  nunca se concatena una cadena de shell.
- **Defecto corregido de paso: `tailscale.InRange`** comparaba el segundo octeto
  del rango CGNAT como **texto**, lo que descartaba direcciones válidas
  `100.64.x.x`–`100.99.x.x` (como `100.66.32.62`). Ahora compara numéricamente y
  valida los cuatro octetos.

#### Fase 9 — Funciones de instancia
- **Jugadores:** Server List Ping con respaldo RCON, sin consultar mientras el
  servidor está 🟡 Cargando, con reintentos y **cuatro mensajes diferenciados**
  (apagado / iniciando / consulta no disponible / error de conexión) y sin
  respuestas tardías que pisen otra instancia.
- **Regeneración con política ÚNICA y explícita:** «apagar y regenerar» (detiene,
  borra el mundo, guarda la semilla y vuelve a arrancar), **explicada en el
  diálogo antes de confirmar**, con backup previo ofrecido, progreso visible,
  indicador persistente mientras dura y aviso cuando no se pudo verificar (T-08).
- **`[D] Borrar`** visible en la lista y en el detalle, con confirmación
  reforzada (nombre exacto), detalle de lo que se elimina, backup previo y apagado
  previo con aviso; estado recuperable ante fallo parcial.
- **Copiar dirección** se deriva antes del primer arranque (tailnet, MagicDNS, red
  local), permite elegir cuando hay varias y explica **por qué** y **qué hacer**
  cuando no hay ninguna.
- **Backup visible y funcional**; la restauración **valida el checksum antes** de
  modificar datos (era un defecto real: antes borraba `server/` y descomprimía sin
  verificar).

#### Fase 10 — Pulido de UI
- **Ayuda en grid alineado** que se adapta al ancho, con orden en cascada
  piramidal por longitud de tecla e incluyendo los comandos retirados de la barra
  (`Ctrl+P`, `F2`, `Ctrl+Q`).
- **`[Esc] Atrás` resaltado** mientras el input de comandos tiene el foco; `Esc`
  devuelve el foco sin ejecutar nada.
- **Plugins muestra `(SOON)`** en la pestaña y en el panel: ya no sugiere que la
  gestión desde la TUI esté operativa.
- **Eliminada la pestaña superior de primer nivel** de detalle (sección 15):
  desaparece la fila `Inicio · Instancias · Salud · Ayuda` y el código muerto
  asociado. El detalle se sigue abriendo desde Inicio con `Enter` y se vuelve con
  `Esc`; `Tab` queda como «Desplazarse» entre zonas.
- Consola: buffer y viewport dentro de su región, sin desplazar la barra inferior
  ni mostrar logs crudos en otra pestaña.

#### Fase 11.1 — Descarga segura de plugins/mods (AC-08, API-07)
- **Se acabó el `io.ReadAll` de hasta 256 MiB en memoria y la escritura directa al
  destino.** Ahora hay **streaming con límite aplicado durante la copia**
  (64 MiB por defecto, techo absoluto 1 GiB), **https obligatorio** (excepto
  `--allow-http` explícito), **anti-SSRF** (loopback, privados, link-local, CGNAT,
  multicast y reservados; validado en la URL y sobre la **IP resuelta** en el
  dialer, cerrando el DNS rebinding, y en **cada** redirección, máximo 5),
  **nombre de archivo validado**, temporal dentro del destino y **movimiento
  atómico**, inspección estructural del JAR/ZIP **sin descomprimir** (límites de
  entradas, bytes declarados, ratio de compresión, rutas inseguras y cifrado) y
  **SHA-256** cuando el proveedor lo publica. El registro persistente se actualiza
  **solo tras la instalación completa**; un fallo no deja archivo, ni registro, ni
  temporales.

#### Fase 11.2-11.3 — Marketplaces y actualizaciones (AC-13, AC-14)
- **Adaptadores Modrinth (API v2) y Hangar (API v1) con un parser propio cada uno**
  (T-06) sobre el **transporte HTTP común** y sin clientes paralelos.
- **`compatible` ≠ `desconocido`:** un resultado de búsqueda **no** demuestra nada;
  la compatibilidad se calcula sobre metadatos declarados y un loader que solo
  declara `spigot` **no** se marca compatible con `paper`.
- **Resolución exacta o error (AC-06):** sin versión, loader, versión de Minecraft
  o checksum utilizable, la resolución falla con motivo estable y **no se descarga
  "lo que haya"**. Un `sha1` se informa pero **no** se usa como prueba de
  integridad.
- **Dependencias obligatorias ausentes o incompatibles bloquean** la resolución con
  explicación; **nunca se instala una dependencia automáticamente**.
- **Registro de procedencia** (`<data>/instances/<slug>/plugins.json`) con proyecto
  de origen, versión instalada, versión objetivo y checksum, **migración aditiva**
  de los registros antiguos (que se leen como `origen: desconocido`, sin inventar
  datos) y URLs de origen **saneadas**.
- **`plugin updates` comprueba sin instalar** (`installed_modified: false`): no
  descarga, no modifica el archivo ni su checksum.

#### Fase 12 — Observabilidad, seguridad del runtime y funciones P2
- **Diagnóstico por proveedor (AC-12):** cada fallo de catálogo publica
  `provider`, `operation`, `status`, `http_status`, `cache_state`,
  `resolution_complete`, `retryable`, `attempts`, `retry_after_seconds`, la
  antigüedad legible de la caché y un mensaje humano, además de códigos estables
  para agentes; distingue red / respuesta inválida / versión inexistente /
  incompatibilidad real. **Nunca** guarda cuerpos de error, tokens ni URLs con
  credenciales.
- **Seguridad del runtime (6.3):** validación de **slugs, nombres de backup y de
  mundo** y de las rutas de CLI/MCP (rechaza `..`, separadores, mayúsculas,
  caracteres de control y nombres reservados de Windows); **restauración de
  backups con checksum verificado ANTES de tocar los datos** (antes se borraba y
  descomprimía a ciegas) y sustitución por `rename` con reversión; **límites** de
  tamaño de backup (8 GiB de origen) y de artefactos; permisos 0700/0600 donde
  aplica; **la publicación de puertos ya nunca usa `0.0.0.0`** (aborta con motivo
  tipado si `HostIP` falta, no es IPv4 o es comodín); secretos fuera de logs y
  JSON; verificación criptográfica de las actualizaciones del binario.

#### Correcciones menores de codificación
- Comentarios y avisos de terceros de `internal/plugins` con la codificación
  corregida.

### ➕ Añadido

#### Fases 1-4
- Errores tipados del transporte y `Meta` con `ETag`/`Last-Modified`.
- `engines.Filter` para filtrar canales desde CLI, TUI y MCP.
- `ConditionalProvider` (Paper y Vanilla) para aprovechar los validadores HTTP.
- `java.go` con reglas versionadas, `JavaRequirementFor` (con origen) y la etiqueta
  `java25`.

#### Fase 4
- Comandos **`catalog status [motor]`** y **`catalog refresh [motor] | --all`**.
- `Cache.Status` con los estados `fresh`, `stale`, `invalid` y `unavailable`, y
  `Cache.Refresh`.
- El documento de caché guarda `provider`, `updated_at`, `schema`, `validated`,
  `etag`, `last_modified` y `published_count` (los documentos antiguos se siguen
  leyendo).

#### Fase 5
- `engines.CatalogService` con `ListVersions`, `ResolveVersion`,
  `GetCatalogStatus`, `RefreshCatalog` y `CheckCompatibility`.
- **`engine compat <motor>`**, con códigos de salida estables: `compatible` → 0,
  `unknown` → 1 (aviso con `reason`), `incompatible` → 2.
- **`engine list <motor>` unificado** sobre la MISMA ruta de caché y normalización
  que `engine versions` (antes consultaba el proveedor sin caché); `engine
  versions` incorpora `--refresh` y expone el estado del catálogo y el modelo
  normalizado. `versionData` se renombró a `engineVersionData`.
- `result.Outcome` (`applied`/`partial`/`failed`) y `result.PhaseFor`.
- Herramientas MCP nuevas: `badomens_engine_resolve`, `badomens_catalog_status` y
  `badomens_engine_compat`; la respuesta de error de MCP publica
  `_meta.code`, `_meta.hint` y `_meta.details` (con el diagnóstico cuando aplica).

#### Fase 6
- `components.Selector`, `components.Footer`, `components.Notification` y el
  estado de servidor (`components/serverstate.go`).
- `svc.Infra` (fachada de infraestructura para la TUI) con su fake.

#### Fase 8
- Comandos **`daemon status | ensure | install-service | uninstall-service`**.
- `tailscale status` publica `status` (tipado), `magic_dns_enabled` y
  `magic_dns_reason`; `tailscale ensure` admite `--allow-elevation` y `--login`.
- `docker status|ensure` añade `infra_status`; `doctor` añade los chequeos
  **MagicDNS** y **Daemon**.
- Herramienta MCP de solo lectura `badomens_infra_status` y
  `badomens_tailscale_status` enriquecida.

#### Fase 11.1
- Paquetes `internal/plugins/download` (descargador con política) y
  `internal/plugins/archive` (inspección JAR/ZIP sin descomprimir).
- En `plugin add`: `--max-size`, `--allow-http`, `--allow-loopback`,
  `--allow-private`, `--on-conflict`, `--sha256`. Claves nuevas en `data`:
  `source`, `size_bytes`, `sha256`, `checksum_verified`, `archive`,
  `security_warning` y, cuando aplican, `renamed_from`, `replaced` y `warning`; los
  fallos HTTP añaden `details.http_status`.

#### Fase 11.2-11.3
- Paquete `internal/plugins/marketplace` con `modrinth`, `hangar`, `resolve` e
  `install`, y el registro de procedencia (`internal/plugins/provenance.go`).
- Comandos **`plugin sources`, `plugin search`, `plugin info`, `plugin versions`,
  `plugin install` y `plugin updates`**, con sus claves aditivas en `data`
  (`sources`, `results`, `versions`, `provenance`, `updates`…) y su propia entrada
  en el manifiesto (`badomens commands`) y en la tabla de paridad CLI↔TUI.

#### Fase 12
- **`doctor api [motor]`**: conectividad, código HTTP, tiempos, formato, contrato,
  esquema, edad y procedencia de la caché por proveedor, con veredicto `ok` /
  aviso (hay caché utilizable) / error y resumen con códigos 0/1/2. Admite
  `--timeout` y `--base motor=URL` (espejo o pruebas).
- **`doctor preflight [instancia]`**: veredicto trivalente previo al arranque.
- **`repro export [instancia]` / `repro import <archivo>`**: exportación
  declarativa reproducible de una instancia (manifiesto 0600 y escritura atómica)
  que valida motor, versión exacta (rechaza `latest`) y todos los pines
  obligatorios antes de escribir.
- `internal/engines/probe.go` (sonda por proveedor), `diagnostic.go` (objeto de
  diagnóstico) y `doctor/api.go`.
- `doctor` añade un chequeo `Catálogo <motor>` por motor con estado, número de
  versiones, aviso y antigüedad legible (sin tocar la red).

### 🔧 Cambiado

- **Limpieza de documentos de sesión**: se retiran del repositorio
  `docs/tui-auditoria.md`, `docs/TUI_IMPLEMENTATION_BRIEF.md`, el informe de
  entrega y la verificación de upstream (documentos de trabajo ya cerrados), y
  las capturas PNG de `docs/capturas/` (se regeneran con la skill
  `terminal-capture`; ahora están en `.gitignore`). Se conserva
  `docs/REGRESSION_MATRIX.md`, con el mapeo defecto → prueba.
- `internal/engines/{paper,fabric,forge,neoforge}.go` reciben cambios
  **estrictamente aditivos**: fijan `Compatibility` en las entradas que su propio
  catálogo oficial publica. `pickVersion` se traslada a `select.go` con la firma
  `pickVersion(engine, list, version)`.
- `internal/engines/embedded.json` incorpora `compatibility: "compatible"` en sus
  27 entradas (proceden de catálogos oficiales).
- `provision.Resolver` pasa de `Provider(name)` a
  `ResolveVersion(ctx, motor, versión)`: `*engines.CatalogService` la cumple
  directamente, así que `provision` deja de hablar con proveedores.
- `internal/cli/idle_service.go` delega en `internal/infra/service.go` (sin
  mecanismos paralelos).
- `runtime.HostIP` y la plantilla de Compose publican `"{{.HostIP}}:{{.Port}}:25565"`;
  `tailscale.host_ip` (que existía sin usarse) pasa a ser una sobrescritura
  explícita y validada.
- El paquete `internal/plugins` se reparte en `plugins` (catálogo y orquestación),
  `plugins/download` y `plugins/archive`; `Add` se conserva para los llamadores
  existentes.

### 🔒 Seguridad

- **Descarga de plugins/mods (API-07, AC-08):** streaming con límite, https por
  defecto, anti-SSRF por URL, nombre **y** IP resuelta, redirecciones acotadas y
  revalidadas, nombre normalizado, inspección estructural JAR/ZIP sin descomprimir,
  checksum SHA-256 y movimiento atómico. Un JAR válido **no** es un plugin seguro:
  el aviso de código de terceros acompaña siempre al resultado y no promete
  garantías que no se pueden verificar.
- **Marketplaces:** un resultado de búsqueda no demuestra compatibilidad ni
  seguridad; los checksums `sha512` se verifican, los `sha1` solo se informan, y un
  artefacto sin checksum fuerte exige `--allow-unverified` (y queda avisado).
- **Runtime (12.4):** slugs y nombres validados en CLI y MCP (sin escrituras fuera
  de `<data>/instances/<slug>/`), restauración de backups con checksum previo,
  límites de disco/tamaño de backup, permisos 0700/0600, publicación de puertos
  jamás en `0.0.0.0`, secretos fuera de logs/JSON y verificación criptográfica de
  las actualizaciones del binario.
- **Procedencia:** la URL de origen se guarda saneada (sin credenciales ni
  parámetros) y el diagnóstico nunca incluye cuerpos de respuesta ni tokens.

### 🗑️ Retirado

- La **fila de pestañas de primer nivel** de la TUI (`Inicio · Instancias · Salud ·
  Ayuda` con `Tab`) y su código muerto asociado (claves, mensajes y estilos):
  duplicaba el acceso al detalle y obligaba a depender de una fila completa de
  pestañas para navegar.

### ⏳ Próximos pasos

- Ideas, mejoras y correcciones tras el primer uso en producción. Abre un issue
  en [el repositorio](https://github.com/MrZudro/badomens/issues) 🐛.
- **Lo que sigue bloqueado o pendiente**: Paper API v2 está retirada (ya migrado
  a Fill v3), la regresión del Maven de NeoForge del 27-sep-2026 quedó **cerrada
  por el upstream**, la API de CurseForge exige una **API key** (fuera de P1) y
  `-race` no puede ejecutarse en el entorno de desarrollo actual (requiere cgo,
  sin compilador C).

## [1.2.1] - 2026-10-14

> 🔧 Correcciones y mejoras de la TUI tras el rediseño 1.2, con revisión visual
> pantalla a pantalla.

### 🐛 Corregido

- **Whitelist/ops en modo crackeado (offline)**: la imagen itzg resolvía los
  nombres de `WHITELIST`/`OPS` contra PlayerDB y escribía un **UUID premium**, que
  no coincide con el UUID offline del servidor y expulsaba al jugador
  (`You are not white-listed on this server!`). Ahora, con `ONLINE_MODE=FALSE`,
  badomens calcula el **UUID offline (versión 3)** y lo manda en `WHITELIST`/`OPS`;
  con modo premium manda el nombre tal cual.
- **Ajustes**: las flechas ←/→ ya **no** actúan como Enter. La dificultad, el modo
  de juego, la memoria y las cuentas se eligen con **selectores**; Enter abre el
  selector y confirma.
- **Consola**: el texto de Ajustes (p. ej. `4G`) ya no se filtra al campo de la
  consola (modelos de entrada separados) y el campo se activa **solo con `/`**.
- **Botón/tecla `C`**: ahora copia la dirección de conexión también desde
  cualquier pestaña del detalle de la instancia (antes solo desde Red).
- **Ayuda**: se separan con claridad los atajos de la pantalla activa de los del
  menú principal (globales), sin duplicados, para que no se confundan.
- **Ajustes en terminales estrechas**: el foco y las opciones del selector abierto
  ya no quedan fuera de la vista; el aviso «Esc: cancelar» es visible también a 40
  columnas.
- **Barra inferior compacta**: conserva el atajo `Tab` (navegación entre pantallas)
  sin perder «Salir».
- **Paleta de comandos**: la fila enfocada resalta toda la línea y alinea su atajo,
  como el resto.
- **Ayuda de «Cuentas»**: el texto ahora habla de **Premium/Crackeado**, coherente
  con el selector (antes decía «Activo/Inactivo»).

### ➕ Añadido

- **Cuentas premium/crackeadas en Ajustes**: se puede alternar entre ambos modos
  desde la instancia (requiere reiniciar), sin recrearla.
- **Mundos**: muestra la semilla actual, permite **regenerar** el mundo (con
  semilla propia o aleatoria) y **cargar un mundo desde un explorador de archivos**
  (`.zip`), siempre con aviso de **pérdida de datos**.
- **Navegación con Tab**: Tab/Shift+Tab cambian de pantalla de primer nivel
  (Inicio → Instancias → Salud → Ayuda); las flechas ←/→ cambian las pestañas
  dentro de la instancia.

## [1.2] - 2026-10-14

> 🧭 Rediseño completo de la TUI guiado por `docs/TUI_REDESIGN.md` y
> `docs/TUI_ACCEPTANCE_TESTS.md`, con la skill `skills/tui-engineering`.

### ➕ Añadido

- **Marco de la app (§2)**: borde de caja (Unicode o ASCII) alrededor de todo el
  panel, con cabecera (producto + versión + estado Docker/Tailscale + contadores
  `n/max inst · r/max en marcha`, que se degradan si no caben), navegación de
  primer nivel (`Inicio · Instancias · Salud · Ayuda`, sección activa entre
  corchetes sin depender del color), barra de estado (último mensaje de dominio +
  indicador de operación) y barra de acciones contextual en el borde inferior.
  Nuevas interfaces opcionales `Statuser` y `NavSectioner`.
- **Servicio de acceso verificable (`svc.Access`)**: la pestaña Jugadores lee
  whitelist y operadores con fuente explícita (`live`, `saved`, `unavailable`) y
  hace **read-back** tras cada mutación; nunca presenta configuración guardada
  como estado en vivo ni afirma éxito por haber enviado un comando.
- **Captura de teclado por foco (§5)**: modal → campo de texto → control →
  pantalla → global. Las letras imprimibles ya no disparan atajos globales
  mientras un formulario tiene el foco (p. ej. escribir `n` en la consola).
- **Franja de pestañas reservada (§4)**: la pantalla expone `Chrome(w)` y el App
  reserva su altura; en terminales compactas se muestra el nombre completo de la
  pestaña activa con un indicador `n/9` y navegación `←/→`.
- **Debounce de navegación en Inicio (§7)**: recorrer instancias no lanza una
  consulta de detalle por pulsación; los ticks obsoletos se descartan.
- **Benchmarks y pruebas de rendimiento/layout**: `PERF-01..05`
  (`BenchmarkViewHome`, `BenchmarkViewConsolaGrande`) y `UI-07` (resize con la
  consola activa).
- **Capturas doradas de Consola y Jugadores/Acceso** en 40×12, 70×20, 110×30 y
  200×50, incluida una línea de log de 240 caracteres.
- **Tests de aceptación `KEY-*`, `ACL-*`, `UI-*`** sobre la ruta de producción
  (`acceptance_test.go`).
- **Inicio rediseñado en paneles (§5.2)**: la lista y el detalle viven en cajas
  con borde (`components.Panel`); la cabecera de la lista lleva los contadores
  `n/max · r/max en marcha`, la fila seleccionada usa banda + puntero y en
  terminales anchas aparece una tercera zona `ACTIVIDAD` con estado derivado
  (nunca logs crudos). Estados explícitos de **vacío** (CTA + requisitos),
  **carga** y **error de listado**, con la acción primaria siempre visible —también
  en 40×12 con banner de Docker. El detalle se **cachea por slug** con TTL corto
  (3 s, invalidado al mutar) para no repetir consultas al recorrer instancias.
- **Consola rediseñada en panel (§5.4)**: el log vive en un `Panel` «CONSOLA» con
  contador `seguir/pausa/filtro`; la línea de entrada queda fuera del marco y el
  buffer se acota a 2.000 líneas. Cada lectura lleva una **generación** (se
  descarta al salir de la pestaña) y solo se consultan logs con la pestaña
  visible, salvo una carga puntual tras una caída para deducir la causa. Estados
  de vacío (`(vacío)`), muchas líneas y línea larga; ante un fallo de lectura se
  conserva el buffer y se muestra un aviso breve y accionable. La pestaña activa
  se marca con corchetes (`[Consola]`) además del color.
- **Jugadores y acceso en paneles (§5.5)**: la pestaña se compone en dos paneles
  `ACCESO` | `OPERADORES` (una sola columna en compacto) con el estado de la
  whitelist (`ACTIVADA`/`DESACTIVADA`/`NO VERIFICADA`/`ERROR DE CONSULTA`) con
  icono + texto, la fuente del dato en el borde (`en vivo`/`guardado`/`error`) y
  las listas con estado vacío accionable. Una lectura solo se marca como
  verificada si hubo **estado en vivo**; con configuración guardada ya no se
  afirma verificación. Pruebas `ACL-02/03/05/09` y `UI-06`.
- **Resto de pestañas en paneles (§5.3/§5.6–§5.11)**: Resumen en dos columnas
  (datos + conexión) con estado de caída; Backups con lista (nombre/tamaño/checksum),
  selección y estados vacío/cargando/creando-restaurando/error; Mundos y
  Plugins/Mods informativos con estado vacío accionable; Red con paneles CONEXIÓN |
  PUERTO y estados comprobando/libre/ocupado (+sugerido)/error; Auto-apagado con
  presets, backup y **cuenta atrás desde el bus de eventos**; Ajustes con estados
  editando/guardando/guardado/error y sin mutar el `Spec` antes de persistir. La
  franja de pestañas muestra siempre completa la pestaña activa (ventana `…`).
- **Asistente en paneles (§5.12)**: elección de modo, cada paso del formulario
  (con contador `n/N`), el selector de versiones y el resumen «Revisar» en paneles
  con borde, con panel AYUDA lateral en anchos; `F1` (además de `?`) abre la ayuda
  del campo; versiones **cacheadas por motor** (no se repiden al cambiar de motor);
  validación de puerto en vivo (debounce 300 ms), versión incompatible, riesgo
  offline (también repetido en «Revisar») y error de creación visibles. Todo cabe
  de 40×12 a 200×50 (alto exacto).
- **Salud, Ayuda, Paleta y Modales (§5.13–§5.15)**: Salud agrupa las comprobaciones
  en paneles `ENTORNO` | `RECURSOS` con tag de estado (`ok`/`aviso`/`crítico`) e
  icono+texto; la RAM asignada se mide en `tea.Cmd` (nada de I/O en `View`). La
  Ayuda muestra atajos de **Pantalla** + **Globales** (priorizando los esenciales y
  cerrando el marco en alturas pequeñas). La Paleta añade pista de filtrado y
  búsqueda difusa (subsecuencia). El borrado y el cambio de versión pasan a
  `components.Modal` (el borrado exige el nombre exacto, con el foco en «No, volver»);
  la salida con operación en curso sigue siendo modal.
- **Cierre de la matriz de aceptación (§7)**: `UI-08` explícito (ANSI, emoji, ZWJ,
  acentos y CJK se miden por celdas y no desbordan; `stripANSI` no deja restos),
  `ACL-04` (alta fallida conserva el estado sin éxito optimista) y **benchmarks por
  pantalla** para todas las pestañas (Resumen, Backups, Mundos, Plugins, Red,
  Auto-apagado, Ajustes, Paleta) además de los ya existentes. Toda la matriz
  UI-01..10, KEY-01..10, ACL-01..10 y PERF-01..05 queda cubierta.

### 🔧 Cambiado

- **Barras de acciones dentro del cuerpo**: se retiran de Inicio (incluida la
  barra compacta); los atajos contextuales viven solo en la barra inferior de la
  carcasa. El banner de Docker caído usa una variante corta (`✖ Docker caído ·
  [Ctrl+R] Reparar`) cuando el texto completo no cabe, y se corrigen referencias
  obsoletas al atajo `H` (ahora `Ctrl+R`).
- **Ancho interior y clasificación**: el marco reserva 4 columnas, así que las
  pantallas reciben el ancho interior y `layout.ClassifyWidth` usa 36 como umbral
  crítico. Los estilos del marco se cachean por tema (nada de construir
  `lipgloss.Style` en cada frame).
- **Atajos globales no imprimibles (§5)**: crear instancia `Ctrl+N`, comandos
  `Ctrl+P`, reparar Docker `Ctrl+R`, tema `Ctrl+T`, ayuda `F1`, salud `F2` y
  salir `Ctrl+Q`/`Ctrl+C`. Desaparecen `q`, `?`, `H`, `:` como atajos globales;
  `n` solo crea desde Inicio y la pantalla de Salud atiende sus propias teclas.
- **Layout con alturas reservadas**: cabecera, pestañas y barra de atajos nunca
  se desplazan por banners, toasts o logs; el cuerpo recibe `alto − reservas`.
- **Consola**: envuelve las líneas al ancho visual con un recorrido O(n) por
  runa (de ~46 ms a ~2 ms en 2.000 líneas) y el input sigue el ancho del terminal.
- **Inicio**: la zona de actividad muestra estado derivado, no logs crudos.
- **Respuestas tardías**: el detalle del Home lleva `slug` y generación y se
  descartan las respuestas de una selección ya cambiada.

### 🗑️ Retirado

- **Modelo legacy de la TUI**: se eliminan `model.go`, `views.go`, `backend.go`,
  `create.go`, `styles.go`, `types.go` y sus tests (`model_test.go`,
  `views_test.go`). Queda una **única ruta probada** (`Run → NewApp → Router →
  Screen → Services`); los helpers compartidos (`stripANSI`, `humanBytes`,
  `shortSha`, `intValue`) pasan a `text.go`.

## [1.0.1] — 2026-10-14

> 🔧 Ajuste fino de la TUI v2: el auto-apagado corre **dentro** de la TUI, los
> puertos se validan en vivo, la app se recupera de caídas y se amplía la
> cobertura dorada a todas las pantallas, temas y modo ASCII.

### ➕ Añadido

- **Auto-apagado dentro de la TUI (RF-8 §6)**: la TUI arranca su propio vigilante
  (bloqueo compartido con `badomens idle daemon`), se suscribe al bus de eventos
  del dominio y convierte `IdleCountdownStarted/Cancelled` e `IdleStopped` en
  avisos; sigue avisando cuando nadie vigila.
- **Validación de puerto en vivo con debounce (RF-3)**: el asistente (modo
  avanzado) incluye un campo de puerto con estados `⟳ Comprobando…`, `✓ Libre`,
  `✖ Ocupado … sugerimos N` y `⚠ Responde en la tailnet`, con botón
  `[P] usar el puerto sugerido`. `setup --port` y `provision.Request.Port`
  respetan el puerto elegido (reserva el preferido).
- **Recuperación ante caídas (P-08)**: detección de apagado inesperado
  (`running → exited` sin acción del usuario), causa probable a partir de los
  logs (OutOfMemoryError, puerto ocupado, Java incorrecto, EULA) y política de
  *crash-loop* (3 caídas en 10 min deshabilitan «Reiniciar» con motivo).
- **Confirmación destructiva en la TUI (P-03)**: eliminar una instancia exige
  **escribir su nombre**; cambiar de versión crea un **backup previo** y pide
  confirmación explícita (avisa de que bajar de versión puede corromper el mundo).
- **Clic de ratón con `bubblezone` (§4.1)**: los botones (`components.Button`) y
  las filas del Home son clicables, además de la rueda de scroll.
- **Java real por versión (RF-5)**: la imagen de `itzg/minecraft-server` se elige
  según el Java que requiere la versión de Minecraft (`java8/16/17/21…`) con
  `engines.JavaImageFor`; si la versión no se reconoce se conserva la imagen de
  `config.Image`.
- **i18n**: catálogo ampliado y **carcasa de la app** (cabecera, banners, toasts,
  salida y modales) migrada al catálogo; el lint de i18n ahora recorre las
  subcarpetas y se añade un test de cableado de la carcasa.
- **Capturas doradas (§10.2)**: Detalle, Asistente (cada paso), Salud, Ayuda y
  Paleta en 40×12, 70×20, 110×30 y 200×50, más variantes ASCII.

## [1.0.0] — 2026-10-13

> 🎉 **Primera versión estable de badomens.** Cierra el plan F0–F5 y completa la
> TUI v2 (`SPEC-tui-v2.md`): las pantallas consumen el dominio (conexión,
> puertos, versiones, auto-apagado), el modo ASCII es completo y el layout
> respeta el tamaño (con guarda global). Sustituye a las pre-releases
> `v0.1.0-rc.*`.

### ➕ Añadido

- **Home rediseñado (§5.1)**: columnas con jugadores/máx, RAM usada/asignada,
  `ip:puerto`, cuenta atrás de auto-apagado y límites (`n/5 · r/3`); en `Wide`
  tres zonas (lista · detalle · **actividad** en vivo); estado vacío con
  requisitos Docker/Tailscale y su botón de reparación.
- **Detalle de instancia (§5.2)**: 9 pestañas (Resumen, Consola, Jugadores,
  Backups, Mundos, Plugins/Mods, Red, Auto-apagado, Ajustes). Consola con
  filtro (`/`), seguir (`f`), pausa (`p`), autocompletado (`Tab`), historial
  (↑↓) y confirmación de comandos peligrosos. Backups con crear/restaurar
  (backup previo) y podar. Red con comprobación del puerto. Auto-apagado con
  presets. Ajustes editables con ayuda y marca "Requiere reiniciar".
- **Asistente (§5.3, RF-4/RF-5)**: rápido/avanzado con 7 pasos y stepper,
  descripción corta por campo y larga bajo demanda, **selector de versión** por
  motor (recomendada ★, incompatibles atenuadas con motivo, `s` snapshots),
  aviso **P-05** (offline sin whitelist) y wiring con el registro `help`.
- **Salud del sistema (P-01)**: filas de Docker, Compose, Tailscale, puertos,
  disco (<10/2 GB), RAM asignada, actualización y plan, con reparación por fila.
- **Paleta de comandos (P-02)**: ahora **ejecuta** la acción elegida (antes no
  hacía nada); cubre acciones de pantalla y globales.
- **Modo ASCII completo (§4.2)**: bordes `+-|`, cursor `>`, iconos `[OK]/[!!]/
  [..]/[XX]` en cabecera, tarjetas, modales, listas y pantalla de salud.
- **Layout con guarda global (§4.1)**: App recorta cada línea al ancho real
  (`x/ansi`) y el total al alto; `layout.ClassifyWidth` evita que las pantallas
  cambien de número de paneles por el alto del cuerpo.
- **Toasts con vida útil** (4 s / 8 s en error, descarte con `Esc`), pista de la
  acción enfocada en la barra inferior, ratón (rueda = navegación) y confirmación
  al salir con operación en curso.
- **Guarda de prerrequisitos (RF-6)**: banner fijo si Docker cae, acciones
  dependientes deshabilitadas con motivo y reparación con espera activa.
- **CLI**: `docker ensure` corrige el timeout desde `docker.ensure_timeout`;
  `instance list` avisa (exit 1) si Docker no está disponible; `instance start`
  hace *preflight* y devuelve `error_dependency_missing` con `details.reason`;
  `connect`/`instance inspect`/`instance start` aceptan `-i <slug>` además del
  posicional; `engine versions` añade `warnings` cuando usa caché/embebido.
- **Dominio**: catálogo **embebido** de versiones (`go:embed` + `make
  gen-versions`), respaldo de peers de la tailnet cacheado 30 s, `docker compose
  ls`, traducción de *"port already allocated"*, `idle install-service`/
  `uninstall-service` (systemd/launchd/schtasks), RCON de respaldo en el
  vigilante, `WatcherActive`, claves `idle.default_timeout`/`versions.cache_ttl`/
  `docker.ensure_timeout` con override por entorno, versión de esquema del
  registro + migración, y tool MCP `badomens_idle`.
- **Tests**: `TestSnapshotRespetaElTamano` (ancho/alto), lint de botones sobre
  las acciones **reales**, cobertura de campos del asistente, claves i18n usadas
  y contrato JSON (`result/contract_test.go`); goldens de Home regenerados.

### 🧱 Base: dominio, paridad CLI/MCP y cimientos

> 🧱 Primera entrega de la `SPEC-tui-v2.md` (Fases 0–4 parciales): paquetes de
> dominio nuevos (puertos, conexión, Docker, auto-apagado, eventos), paridad
> CLI/MCP para todo lo nuevo y el catálogo de ayudas del asistente.

#### ➕ Añadido

- **`internal/ports` (RF-3)**: detección de puertos ocupados en 4 fuentes
  (registro, Docker, bind local, peers de la tailnet), rango configurable
  (`ports.range`), `tailnet_scan: off|warn|block`, reserva atómica con bloqueo
  de archivo, revalidación y sugerencia del siguiente libre. Comandos
  `badomens ports check|list` y tool MCP `badomens_ports_check`.
- **`internal/connection` (RF-2)**: cálculo de la dirección de conexión,
  MagicDNS, loader y pasos para el jugador; se **persiste** en el registro y se
  **recalcula en cada arranque**. Comando `badomens -i <n> connect [--copy]`,
  campo `data.connection` en `instance inspect|start|create` y tool MCP
  `badomens_connection`.
- **`internal/dockerx` (RF-6)**: diagnóstico tipado (8 estados) y plan de
  reparación por plataforma con espera activa; `badomens docker status|ensure
  [--wait] [--print-script]`, scripts de respaldo `scripts/ensure-docker.{sh,ps1}`
  y tool MCP `badomens_docker_status`.
- **`internal/idle` (RF-8)**: máquina de estados con reloj inyectable, detección
  por Server List Ping (y RCON como respaldo), parada limpia y vigilante con
  pidfile (`badomens -i <n> idle set|status|disable`, `badomens idle daemon` y
  tool MCP `badomens_idle_status`).
- **`internal/events`**: bus de eventos de dominio ligero (estado, jugadores,
  auto-apagado, Docker/Tailscale, backups).
- **`internal/engines` (RF-5)**: `Version` enriquecida (canal, fecha, Java,
  recomendada, build), caché en disco con `stale-while-revalidate` y respaldo
  embebido; snapshots de Vanilla vía `engine versions --all`.
- **`internal/tui/help` (RF-4)**: registro único de descripciones de opciones
  (corta ≤ 90 y larga) que alimenta el asistente, los ajustes y
  `badomens setup --explain <campo>`.
- **Cimientos de la TUI v2 (Fase 0.2)**: `internal/tui/theme` (4 temas + test de
  contraste WCAG AA), `internal/tui/layout` (breakpoints y truncado por ancho
  real), `internal/tui/i18n` (catálogo es), `internal/tui/components`
  (`Action` + lint de botones, RF-7), `internal/tui/anim` (tick global sin
  ticks en reposo, §4.3), `internal/tui/svc` (fachada de servicios + fakes) y
  `internal/tui/router.go` (interfaz `Screen` + pila/overlay).
- **Contrato JSON**: `error.details` (aditivo) con `details.reason` estable; el
  resto de cambios nuevos son campos aditivos en `data` (ver
  `docs/contrato-json.md`).
- **Documentación**: `docs/tui-auditoria.md`, `docs/tui-paridad.md` y el test de
  regla que exige paridad CLI↔TUI para cada subcomando.
- **Accesibilidad y flags de la TUI**: `badomens tui --plain` (resumen lineal
  sin cajas ni color, P-16), `--theme`, `--no-animations` y `--ascii`.
- **RF-6/RF-3**: `doctor` reutiliza `internal/dockerx` (motivo estable en el
  diagnóstico) y `instance start` **revalida el puerto** antes de arrancar (si
  está ocupado devuelve `error_capacity_exceeded` con `details.reason=port_in_use`
  y el puerto sugerido).

## [v0.1.0-rc.2] — 2026-10-13

> 🔧 Segunda pre-release con **correcciones de distribución**: el instalador de
> un comando y `update --apply` vuelven a funcionar (ya no dependen de
> `/releases/latest` ni de nombres de asset sin versión), y los instaladores se
> publican como assets de la release.

### 🔧 Correcciones

- **Instalador de un comando**: usa `raw.githubusercontent.com` (las URLs
  `releases/latest` web y API ignoran las pre-releases) y resuelve la versión
  vía `releases?per_page=1`. En Windows añade el directorio al **PATH de
  usuario** automáticamente (y `uninstall.ps1` lo quita al desinstalar).
- **Assets de release**: GoReleaser nombra los assets con la versión
  (`badomens_<v>_<os>_<arch>.<ext>`); los instaladores y `update --apply` ahora
  lo respetan (antes descargaban 404).
- **`update` / `update --apply` / check de la TUI**: consultan el listado de
  releases en vez de `/releases/latest` (que devolvía 404 al haber solo
  pre-releases) y descargan con el nombre de asset correcto.
- **GoReleaser**: `install.sh` e `install.ps1` se publican como assets
  (`extra_files`) para que la URL `…/releases/latest/download/…` funcione
  cuando exista una release **estable**.

## [v0.1.0-rc.1] — 2026-10-13

> 🚀 Segunda **pre-release** de badomens: la TUI crea instancias y avisa de sus
> requisitos, y el binario puede **actualizarse y desinstalarse** desde el CLI.
> Mira [la release](https://github.com/MrZudro/badomens/releases) para descargar
> binarios verificados.

### ➕ Añadido

- **🖥️ TUI**: asistente de creación de instancias con **paridad total** con
  `badomens setup` (tecla `N`): nombre, motor, versión, RAM, CPU, dificultad,
  modo, jugadores, premium, whitelist+usuarios, ops, semilla, MOTD y plugins.
- **⚠️ Requisitos en la TUI**: preflight al abrir (directorios, Docker/Compose,
  Tailscale), banner con lo que falla y pantalla detallada en la ayuda (tecla
  `R`; `--no-preflight` para saltarlo).
- **🔄 `update --apply`**: descarga la última release, **verifica el sha256** y
  reemplaza el binario (con confirmación). Renombre a `.old` en Windows.
- **🔔 Check de actualización al abrir la TUI**: pregunta si actualizar, con
  throttle por caché (12 h) y recordatorio de versión descartada
  (`update_check`, `BADOMENS_UPDATE_CHECK`, `--no-preflight`).
- **🧹 `badomens uninstall`**: binario + configuración (conserva los datos);
  `--purge` borra también instancias/mundos/backups; `--dry-run` previsualiza.
  Scripts `scripts/uninstall.sh` y `scripts/uninstall.ps1`.
- **🧩 `internal/provision`**: núcleo compartido de creación entre `setup` y la
  TUI (una sola validación, sin duplicar lógica).

### 🔧 Otros

- `doctor` expone `Preflight` (subconjunto de comprobaciones para la TUI).
- Documentación actualizada: README, `docs/setup-install.md`, `docs/tui-plan.md`
  y `AGENTS.md`.

## [v0.1.0-rc.0] — 2026-10-13

> 🧱 Primera **pre-release** de badomens: el binario ya cubre todo el plan
> (F0–F5). Mira [la release](https://github.com/MrZudro/badomens/releases) para
> descargar binarios verificados.

### ➕ Añadido

- **🏗️ F0 — Fundación**
  - Binario único en Go con **contrato JSON estable** (`ok`, `exit_code`,
    `action`, `message`, `data`, `error`, `timestamp`).
  - Comandos `version`, `doctor`, `capacity`, `config`, `update`, `commands`.
  - Servidor MCP por stdio y registro único de herramientas, con `--read-only`.
  - CI (Linux/macOS/Windows), GoReleaser y releases con checksums.

- **🧩 F1 — Multi-instancia**
  - Hasta **5 instancias** (`setup`, `instance create/list/inspect/remove`) y
    **3 en paralelo** por defecto.
  - Asistente `setup` interactivo y desatendido (motor, versión, RAM, mundo,
    seed, whitelist, ops, MOTD, icono, plugins…).
  - Motores Paper/Fabric/Forge/NeoForge/Vanilla, biblioteca de mundos, backups
    con sha256, plugins según motor y apagado *graceful* (`close`).
  - Comandos `logs`, `console`, `access` (whitelist/ops) y `resolve-ip`.

- **🩺 F2 — Doctor TPS y métricas**
  - `doctor tps [--instance]`: TPS/MSPT por consola, CPU/RAM por `docker stats`
    y recuento de eventos de retraso. Clasifica good/degraded/critical.
  - `metrics`: CPU/RAM del contenedor, tamaño del mundo y estado de backups.

- **🤖 F3 — Integración con Emiria (Jamcord)**
  - Cliente Jamcord que ejecuta `badomens --json` y registra herramientas
    `badomens_*`; comando `jamcord mc`, bot `/mc` y MCP con `--read-only`.

- **🖥️ F4 — TUI (panel multi-instancia)**
  - `badomens tui`: panel interactivo con lista, detalle, logs, consola,
    backups y ayuda (bubbletea + lipgloss + bubbles).
  - Operaciones asíncronas que no bloquean la UI y salud TPS bajo demanda.
  - `--snapshot --view list|detail|logs|backups|help` y `--force` para pruebas
    sin terminal. La TUI no toca el contrato JSON.

- **📦 F5 — Distribución**
  - Instalador de un comando (`install.sh` / `install.ps1`) con verificación de
    checksum y releases precompiladas para Windows/macOS/Linux (amd64 y arm64).

### 🔧 Otros

- **🚚 Repositorio**: el proyecto se publica ahora en
  [`MrZudro/badomens`](https://github.com/MrZudro/badomens) (módulo Go y URLs
  de instalación actualizados).
- **📖 Documentación**: README y docs reescritos con estilo amigable, emojis y
  Markdown de GitHub (badges, admonitions, diagramas y tablas).
- **🚀 Workflows**: el pipeline de release crea la *prerelease* para etiquetas
  semánticas de pre-lanzamiento (ver `.goreleaser.yaml` y `release.yml`).

---

[1.3.1]: https://github.com/MrZudro/badomens/releases/tag/v1.3.1
[1.3.0]: https://github.com/MrZudro/badomens/releases/tag/v1.3.0
[1.2]: https://github.com/MrZudro/badomens/releases/tag/v1.2
[1.0.1]: https://github.com/MrZudro/badomens/releases/tag/v1.0.1
[1.0.0]: https://github.com/MrZudro/badomens/releases/tag/v1.0.0
[v0.1.0-rc.2]: https://github.com/MrZudro/badomens/releases/tag/v0.1.0-rc.2
[v0.1.0-rc.1]: https://github.com/MrZudro/badomens/releases/tag/v0.1.0-rc.1
[v0.1.0-rc.0]: https://github.com/MrZudro/badomens/releases/tag/v0.1.0-rc.0