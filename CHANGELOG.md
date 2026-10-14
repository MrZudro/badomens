# 📜 Changelog

Todas las novedades relevantes de **badomens** se documentan aquí ✨.
El formato sigue [Keep a Changelog](https://keepachangelog.com/es/1.1.0/) y el
versionado es [semántico](https://semver.org/lang/es/).

## [Unreleased]

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

## [Unreleased]

### ⏳ Próximos pasos

- Ideas, mejoras y correcciones tras el primer uso en producción. Abre un issue
  en [el repositorio](https://github.com/MrZudro/badomens/issues) 🐛.

---

[1.2]: https://github.com/MrZudro/badomens/releases/tag/v1.2
[1.0.1]: https://github.com/MrZudro/badomens/releases/tag/v1.0.1
[1.0.0]: https://github.com/MrZudro/badomens/releases/tag/v1.0.0
[v0.1.0-rc.2]: https://github.com/MrZudro/badomens/releases/tag/v0.1.0-rc.2
[v0.1.0-rc.1]: https://github.com/MrZudro/badomens/releases/tag/v0.1.0-rc.1
[v0.1.0-rc.0]: https://github.com/MrZudro/badomens/releases/tag/v0.1.0-rc.0