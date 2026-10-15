# badomens — Matriz de regresión (Fase 0)

> **Autor:** Auditor de contratos (Agente 1, Fase 0).
> **Rama:** `fix/estabilizacion-plan-completo`. **Sin commit** (lo hace el orquestador).
> **Fecha:** 2026-10-14.
> **Alcance:** reproducción **estática** (lectura de código + consulta real a los proveedores).
> No se modifica lógica de negocio. No se crean suites paralelas: las pruebas de TUI se amplían
> en `docs/TUI_ACCEPTANCE_TESTS.md`.
> **Documento hermano:** `docs/UPSTREAM_VERIFICATION.md` (hechos externos y preguntas abiertas).

## 1. Cómo leer la matriz

Una fila por defecto. La columna **Estado** usa estos valores:

- **confirmado** — reproducido estáticamente (archivo:línea) o contra el proveedor real.
- **no reproducido** — no se pudo reproducir en Fase 0 (p. ej. requiere TUI interactiva o un
  servidor Minecraft/Docker en marcha); queda como **hipótesis** a confirmar en su fase.
- **bloqueado por upstream** — depende de un tercero (API de proveedor retirada, etc.).

La columna **Prueba propuesta** da el **nombre** del test que la Fase correspondiente debe añadir.
Las pruebas de UI se añaden a `docs/TUI_ACCEPTANCE_TESTS.md` (no a una matriz nueva).

## 2. Línea base (comandos ejecutados y resultado)

Toolchain: **Go 1.27.0 windows/amd64**, **golangci-lint 2.14.0** (built with go1.27.0).

| Comando | Resultado | Salida |
|---|---|---|
| `go build ./...` | **OK (exit 0)** | sin salida |
| `go test ./...` | **OK (exit 0)** | todos los paquetes `ok`/`[no test files]`; sin fallos |
| `go vet ./...` | **OK (exit 0)** | sin salida |
| `golangci-lint run` | **OK (exit 0)** | `0 issues.` |

Salida cruda guardada durante la auditoría en `%TEMP%\opencode\baseline.txt`.
Paquetes **con** pruebas (`ok`): `internal/backup`, `internal/cli`, `internal/config`,
`internal/connection`, `internal/dockerx`, `internal/engines`, `internal/events`,
`internal/host`, `internal/idle`, `internal/instance`, `internal/mcuuid`, `internal/ports`,
`internal/provision`, `internal/result`, `internal/runtime`, `internal/setup`, `internal/tools`,
`internal/tps`, `internal/tui` (+ subpaquetes `anim`, `components`, `help`, `i18n`, `layout`,
`svc`, `theme`), `internal/uninstall`, `internal/update`, `internal/worlds`.
Paquetes **sin** pruebas (`[no test files]`): `cmd/badomens`, `internal/doctor`,
`internal/engines/gen`, `internal/errx`, `internal/execx`, `internal/mcp`, `internal/output`,
`internal/plugins`, `internal/tailscale`, `internal/version`.

> Nota: `internal/plugins` **no tiene tests** — relevante para API-07 (Fase 11).

## 3. Matriz de regresión

### 3.1 `spec 1.md` (técnica): API-01 … API-08

| ID | Síntoma | Causa raíz (confirmada / hipótesis) | Evidencia (archivo:línea o comando) | Estado | Prueba propuesta |
|---|---|---|---|---|---|
| **API-01** | Fallos transitorios de red se convierten en errores de operación. | **Confirmada.** El cliente no implementa reintentos pese a que el comentario del tipo los anuncia. | `internal/engines/client.go:12` ("con **reintento mínimo**"), `:21-23` `DefaultClient` (solo timeout 15 s), `:27-47` `Get` sin bucle de reintento. | confirmado | `TestClientReintentaTransitorios` (429/500/502/503/timeout con backoff), `TestClientRespetaRetryAfter`, `TestClientNoReintenta4xx` (400/401/403/404) |
| **API-02** | Identificación deficiente del cliente; posible bloqueo del proveedor. | **Confirmada.** UA fijo `badomens/0`, sin versión ni contacto. | `internal/engines/client.go:22` `UserAgent: "badomens/0"`; `internal/plugins/plugins.go:136` `req.Header.Set("User-Agent", "badomens/0")`. Fill exige UA identificable: `docs/UPSTREAM_VERIFICATION.md` E-03. | confirmado | `TestClientUserAgentIdentificable` (incluye versión y, si aplica, contacto) |
| **API-03** | Paper se resuelve sin un pin de build verificable; catálogo Paper hoy roto. | **Confirmada.** Usa API v2 (**retirada: 404**) y silencia errores de builds; el parser espera el esquema v2 (`{"builds":[{build,channel}]}`) en vez del v3 (array de objetos con `id`/`channel`/`downloads`). | `internal/engines/paper.go:29` base `https://api.papermc.io/v2/projects/paper`; `:34` `ListVersions` con `apiVersions`; `:44-49` `paperBuilds` esquema v2; `:63` `if err := ...GetJSON(...); err == nil && len(builds.Builds) > 0 {` (error silenciado). Comando: `GET .../v2/projects/paper/versions` → **404** (`paper_v2/versions.json`). | confirmado (upstream: v2 retirada) | `TestPaperFillV3ListVersions`, `TestPaperResolveSeleccionaBuildEstable`, `TestPaperResolveSinBuildFalla` (no aceptar resolución sin pin) |
| **API-04** | Resolución Fabric sin el pin del loader. | **Confirmada.** Usa el catálogo general `/v2/versions/loader` (no `/loader/{game_version}`) y silencia el error. | `internal/engines/fabric.go:57` `GetJSON(ctx, p.Base()+"/versions/loader", &loaders); err == nil && len(loaders) > 0`; `:58` `loaders[0].Version`. Endpoint específico existe y devuelve otra forma: `fabric/loader_26.3.json`. | confirmado | `TestFabricLoaderPorGameVersion`, `TestFabricResolveSinLoaderFalla` |
| **API-05** | `latest` puede devolver la versión más antigua. | **Confirmada.** `pickVersion` asume que el último elemento es el más reciente; Fabric entrega **más nuevo primero**. | `internal/engines/paper.go:174-193` `pickVersion` (recorre desde `len-1`); `internal/engines/fabric.go:34-44` conserva el orden del proveedor sin invertir. Fabric newest-first: `fabric/versions_game.json` (E-08). | confirmado | `TestPickVersionIndependienteDelOrden`, `TestFabricLatestEsElMasReciente` |
| **API-06** | Clasificación/parseo Maven incorrecto; catálogo Forge vacío y NeoForge roto. | **Confirmada.** Parser Maven compartido Forge/NeoForge que asume `versioning.versions`; ambos usan `maven-metadata.json`. Forge publica `{"<mc>":[...]}` (JSON válido sin `versioning` → catálogo vacío); NeoForge `maven-metadata.json` **no existe** (404) y el oficial es XML. Estabilidad por heurística de substrings. | `internal/engines/fabric.go:68-72` `mavenRoot{versioning.versions}`; `:89-91` y `:116-118` comparten `mavenVersions`; `:128-141` heurística (`strings.ContainsAny(lower,"beta")`…). Comandos: `forge/maven-metadata.json` (forma `{"1.1":["1.1-..."]}`) y `maven.neoforged.net/.../maven-metadata.json` → **404**; `neoforge/maven-metadata.xml` (oficial). | confirmado | `TestForgePromotionsSchema`, `TestForgeMavenJsonNoVacíoOMatchSchema` (rechazar `schema_mismatch`), `TestNeoForgeXmlEsquemaAntiguoYModerno`, `TestMavenEstabilidadNoPorSubstring` |
| **API-07** | Descarga de plugins sin límites ni verificación; consumo de memoria. | **Confirmada.** Lee hasta **256 MiB en memoria** y escribe directo al destino; sin HTTPS forzado, sin anti-SSRF, sin atómico, sin validar ZIP/JAR ni checksum. | `internal/plugins/plugins.go:156` `io.ReadAll(io.LimitReader(resp.Body, 256*1024*1024))`; `:160` `os.WriteFile(targetDir/name, raw, 0o644)`; `:127-164` `download`. `internal/plugins` **sin tests**. | confirmado (estático; explotabilidad no probada) | `TestPluginDownloadStreamingYLimite`, `TestPluginDownloadRechazaRedPrivada`, `TestPluginDownloadAtomicoYChecksum` |
| **API-08** | Comportamientos distintos entre motores; fallos difíciles de diagnosticar. | **Confirmada (gap de diseño).** `Version` no expone `source`/`fetched_at`/`stale`/`compatibility` trivalente ni `warning`; la "compatibilidad" se rellena a `true` sin verificación. | `internal/engines/engines.go:54-75` (`Version`); `internal/engines/versions.go:182-184` `if !out[i].Compatible { out[i].Compatible = true }` (convierte desconocido en compatible). | confirmado | `TestModeloNormalizadoTieneProcedencia` (source/fetched_at/stale), `TestCompatibilidadTrivalente` |

### 3.2 `SPEC 2.md` (bugs/UX): QC-01 … QC-05

| ID | Síntoma | Causa raíz (confirmada / hipótesis) | Evidencia | Estado | Prueba propuesta |
|---|---|---|---|---|---|
| **QC-01** | No se navega con `↑/↓` en los selectores. | **Hipótesis (UI).** Requiere ejecución de la TUI; el flujo de creación es `wizardScreen` (campo `cycle` + picker de versiones con `j/k/↑/↓`). | `internal/tui/screen_wizard.go:350-386` (picker responde a `j/down`, `k/up`); campo `cycle` en `:654-655`. | no reproducido (estático); hipótesis | Ampliar `docs/TUI_ACCEPTANCE_TESTS.md`: `KEY-SEL-01` navegación `↑/↓` en todos los selectores del asistente |
| **QC-02** | Abrir/avanzar de paso con teclas no intuitivas. | **Hipótesis (UI).** En el código actual el paso avanza con `Enter`/`Tab` (`handleStepKey`), no con `↓`. Reproducir en TUI para confirmar si el síntoma persiste. | `internal/tui/screen_wizard.go:388-411` (`enter`/`tab` avanzan), `:640` (`create`). | no reproducido (estático); hipótesis | `KEY-SEL-02` (Enter abre/confirma; `↓` nunca avanza de paso) |
| **QC-03** | La lista no incluye las versiones más recientes. | **Confirmada (causa técnica en API-03/API-05/API-06).** Paper (v2 404), Fabric (orden invertido), NeoForge (`maven-metadata.json` 404), Forge (catálogo vacío) ⇒ falta lo más reciente. | Ver API-03, API-05, API-06. Comandos: Paper v2 → 404; NeoForge `maven-metadata.json` → 404; `fabric/versions_game.json` newest-first. | confirmado | `TestCatalogoMuestraUltimaEstablePorMotor` (integración, fixtures) |
| **QC-04** | Algunas versiones dan 404 al descargar. | **Confirmada parcial.** El código resuelve versiones sin garantizar artefacto: Paper acepta resolución sin build (API-03), Forge queda vacío, y no se valida la existencia del artefacto antes de ofrecer la versión. | `internal/engines/paper.go:63-68` (pin solo si hay builds y sin arte), `internal/engines/versions.go:115-118` (no valida artefacto). Fixtures: `paper_fill_v3/builds_26.3.json` (artefacto `downloads."server:default".url`). | confirmado (causa) / hipótesis (síntoma 404 exacto) | `TestResolucionConArtefactoDescargable`, `TestNoSeOfrecenVersionesSinBuild` |
| **QC-05** | Versiones desordenadas y pegadas. | **Hipótesis (UI) + causa técnica.** `Enrich` marca `Recommended` **antes** de ordenar (`sortByDate`), por lo que la marca puede caer en la versión equivocada. | `internal/engines/versions.go:115` `Enrich(...)` seguido de `:116` `sortByDate(...)`; `:187-193` asigna `Recommended` al último `release` del orden **previo** a ordenar. | confirmado (causa técnica) | `TestRecommendedTrasOrdenar`, más caso UI `KEY-SEL-03` (grid alineado y última estable marcada) |

### 3.3 Creación avanzada: AD-01, AD-02

| ID | Síntoma | Causa raíz (confirmada / hipótesis) | Evidencia | Estado | Prueba propuesta |
|---|---|---|---|---|---|
| **AD-01** | Mismos problemas de selector/`Enter`/flechas que la creación rápida. | **Hipótesis (UI).** Comparte `wizardScreen` con el modo rápido. | `internal/tui/screen_wizard.go:123-165` (mismo `start`/`buildRequest` para modos 0 y 1). | no reproducido (estático); hipótesis | `KEY-SEL-04` (avanzado: mismos selectores) |
| **AD-02** | Se elige un motor/versión y arranca otro. | **Parcialmente confirmada (causa técnica) / hipótesis (síntoma de arranque).** (a) Rutas de resolución divergentes: `engine list` usa `provider.ListVersions` crudo, `engine versions` usa `cache.Versions` (enriquecido/ordenado) y `engine use` usa `provider.Resolve` directo. (b) `Recommended` se calcula antes de ordenar. (c) El asistente guarda por **valor** (`req.Engine=w.engine.Name`, `req.Version=f.value`) y resetea la versión al cambiar de motor, lo que **mitiga** la carrera, pero la versión mostrada puede venir de una lista mal ordenada/marcada. | CLI: `internal/cli/engine.go:137` (`list` → `ListVersions`), `:29` (`versions` → `cache.Versions`), `:189` (`use` → `Resolve`). Enriquecido/orden: `internal/engines/versions.go:115-118,187-193`. Asistente: `internal/tui/screen_wizard.go:752-760` (`buildRequest`), `:673-684` (reset al cambiar motor), `:236-240` (descarta respuestas de otro motor). | confirmado (divergencia y `Recommended`); hipótesis (arranque con otro motor) | `TestResolucionUnicaCliTuiMcp` (misma entrada ⇒ mismo resultado), test de flujo del asistente (recorrer combinaciones, cambiar motor, `Esc`, avanzar ⇒ config final == selección) |
| **AD-02b** | — (subtarea de AD-02) | **Confirmada.** `Enrich`'s `Recommended` no se recalcula tras `sortByDate`, y en Fabric (newest-first) marca el más **antiguo**. | `internal/engines/versions.go:187-193` + fabric order (E-08). | confirmado | `TestEnrichRecommendedEsElMasReciente` |

### 3.4 Regeneración: RG-01 … RG-03 (ver también SEC-09)

| ID | Síntoma | Causa raíz (confirmada / hipótesis) | Evidencia | Estado | Prueba propuesta |
|---|---|---|---|---|---|
| **RG-01** | La selección usa `←/→` en una lista vertical. | **Hipótesis (UI).** | `internal/tui/screen_instance.go:2906-2934` (atajos de Mundos); picker/selector reutilizable. | no reproducido (estático); hipótesis | `KEY-REG-01` (regeneración con `↑/↓`) |
| **RG-02** | Tras confirmar, no se ve feedback. | **Parcialmente confirmada.** Existe toast de resultado y estado de operación, pero no un indicador persistente "pendiente" ni progreso diferenciado. | `internal/tui/screen_instance.go:151-171` (`worldsOpMsg`, texto de éxito), `:2965-2973` (`worldsRegenCmd`). | confirmado (parcial) | `TestRegeneracionMuestraProgresoYResultado` |
| **RG-03** | No está claro si se puede regenerar con el servidor encendido. | **Confirmada.** La política real **apaga y reinicia** el servidor, pero el diálogo **no la comunica**. | `internal/tui/svc/worlds.go:88-112` (`Regenerate`: `stopIfRunning` → borra mundos → guarda semilla → `Start` si estaba corriendo); `screen_instance.go:2939-2962` (modal solo avisa de pérdida de datos). | confirmado | `TestRegeneracionPoliticaExplicita` (el diálogo explica apagar+reiniciar antes de confirmar) |

### 3.5 `SPEC 2.md` secciones 5 … 15 (SEC) + §1 y §4

| ID | Síntoma | Causa raíz (confirmada / hipótesis) | Evidencia | Estado | Prueba propuesta |
|---|---|---|---|---|---|
| **SEC-01** (§1 barra inferior) | Teclas mostradas de forma inconsistente; demasiados atajos globales. | **Hipótesis (UI).** Barra única en `App`; requiere revisión visual. | `internal/tui/app_v2.go:753-792` (banner/barra), `docs/TUI_ACCEPTANCE_TESTS.md`. | no reproducido (estático); hipótesis | `UI-BAR-01` (formato `[Tecla] Etiqueta`; `Ctrl+P`/`F2`/`Ctrl+Q` fuera de la barra) |
| **SEC-04** (§4 daemon/Docker/Tailscale) | Si el daemon/Docker/Tailscale están apagados, la app queda inútil. | **Hipótesis (infra).** Fase 8; no abordado en Fase 0. Existe banner de prerrequisitos. | `internal/tui/app_v2.go:847-879` (`bannerView`/`bannerText` Docker/Tailscale). | no reproducido (estático); hipótesis | `TestDaemonSeLevanta`, `TestDockerTailscaleSeDetectanYLevantan` |
| **SEC-05** (§5 MagicDNS) | El aviso de MagicDNS no desaparece. | **Confirmada (parcial).** El aviso proviene de `connection.Derive`, que se calcula a partir del **dato persistido** `spec.Connection.MagicDNS`, escrito la primera vez; no se reevalúa en vivo ni pasa por el sistema de notificaciones con TTL. Si MagicDNS se habilita después, el valor guardado queda obsoleto y el aviso persiste. | `internal/connection/connection.go:96-101` (genera el warning si `MagicDNS == ""`); `internal/tui/screen_home.go:802-810` y `:805` (usa `spec.Connection.MagicDNS` persistido); `internal/tui/screen_health.go:126` (sí consulta `tailscale.MagicDNS` en salud, aparte). | confirmado (origen) / hipótesis (persistencia exacta en pantalla) | `TestAvisoMagicDNSDesapareceAlResolver` + `SEC-05` en TUI_ACCEPTANCE_TESTS |
| **SEC-06** (§6 salida del campo de comandos) | No es claro cómo salir del input de comandos. | **Hipótesis (UI).** | `internal/tui/screen_instance.go` (consola); `docs/TUI_ACCEPTANCE_TESTS.md` CON-*. | no reproducido (estático); hipótesis | `KEY-CON-ESC` (`[Esc]` resaltado con el input enfocado) |
| **SEC-07** (§7 Jugadores) | "En línea: no se pudo verificar (¿servidor apagado?)". | **Confirmada.** La TUI mide por **Server List Ping** con context de 2 s y timeout de 1,5 s; si el host está vacío o falla, devuelve `known=false` y se pinta un mensaje **genérico** sin distinguir causa. | `internal/tui/screen_instance.go:376-394` (`playersCmd`), `:505-506` (adopción del mensaje), `:2444-2451` (`onlineLine` genérico), `:1969-1975` (`playersText`). Ver §Jugadores. | confirmado | `TestJugadoresMensajesDiferenciados`, `TestJugadoresReintentaAlArrancar` |
| **SEC-08** (§8 semáforo) | Estado del servidor poco visible/fiable. | **Hipótesis (UI).** Existe estado por instancia alimentando filas y detalle; requiere validar unicidad de fuente y ubicación fija. | `internal/tui/screen_home.go:744-746` (estado de la fila seleccionada), `screen_instance.go` (`applyState`). | no reproducido (estático); hipótesis | `UI-STATE-01` (transiciones apagado→cargando→en línea→cargando→apagado con texto) |
| **SEC-09** (§9 regeneración) | Política de regeneración no definida en la UI. | **Confirmada (ver RG-03).** Política real: apaga y reinicia; no comunicada. | `internal/tui/svc/worlds.go:88-112`; `screen_instance.go:2939-2962`. | confirmado | `KEY-REG-02` (el diálogo explica la política; feedback de inicio y fin) |
| **SEC-10** (§10 copiar dirección) | "No hay dirección de conexión todavía". | **Confirmada (parcial).** La dirección se deriva del `spec.Connection` persistido (se escribe en el primer arranque), por lo que antes del primer arranque no hay dirección; no se deriva de IP local + puerto. | `internal/cli/connect.go:54-71` (escribe `spec.Connection` tras calcular); `internal/tui/screen_home.go:802-810` (solo lee lo persistido); `internal/connection/connection.go:69-104` (`Derive`). | confirmado (causa) / hipótesis (mensaje) | `TestCopiarDireccionDerivable`, `TestCopiarDireccionExplicaPorQueNo` |
| **SEC-11** (§11 borrar instancias) | Acceso poco claro para eliminar. | **Hipótesis (UI).** Existe `instanceDeletedMsg` y confirmación; validar `[D]` visible en lista y detalle. | `internal/tui/screen_instance.go:446-452` (borrado notificado). | no reproducido (estático); hipótesis | `KEY-DEL-01` (`[D]` visible y confirmación explícita) |
| **SEC-12** (§12 notificaciones) | Notificaciones que no expiran. | **Hipótesis (UI).** Existe `toastMsg` central; validar TTL ≤ 30 s y que nada sea permanente. | `internal/tui/app_v2.go:164,197` (toast/eventos). | no reproducido (estático); hipótesis | `TestNotificacionesTTLMaximo` (≤ 30 s, todas por el sistema central) |
| **SEC-13** (§13 ayuda) | Opciones globales descuadradas. | **Confirmada (UI).** La hoja pintaba los globales como una lista vertical sin columnas, desordenada por orden de declaración y con los retirados al final. | `internal/tui/screen_help.go` (`helpScreen.View`). | **resuelto (Fase 10)** | `HELP-01`…`HELP-04` (grid alineado y adaptable, cascada piramidal, comandos retirados incluidos) |
| **SEC-14** (§14 Plugins SOON) | Plugins parece operativo. | **Confirmada (UI).** La pestaña «Plugins/Mods» no avisaba de que la gestión desde la TUI aún no existe. | `internal/tui/screen_instance.go` (`instanceTabs`, `pluginsView`). | **resuelto (Fase 10)** | `SOON-01` (`(SOON)` en la pestaña y en el panel) |
| **SEC-15** (§15 pestaña superior) | Pestaña superior de detalle redundante. | **Confirmada (UI).** La fila de navegación de primer nivel (`Inicio · Instancias · Salud · Ayuda`, con `Tab`) duplicaba el acceso al detalle, que ya se abre desde Inicio con `Enter`; además obligaba a depender de una fila completa de pestañas para navegar. | `internal/tui/app_v2.go` (`navView`, `cycleSection`, `gotoSection`, `sectionOrder`). | **resuelto (Fase 10)** | `UI-TABS-01`, `NAV-01`, `ZONE-01`…`ZONE-03` (sin fila de pestañas; acceso al detalle intacto; `Tab` = «Desplazarse») |

## 4. Reproducción estática detallada (API + Java)

### API-01 — el cliente no reintenta (`internal/engines/client.go`)
- `:12` — comentario: "Cliente es un cliente HTTP con **reintento mínimo**…".
- `:21-23` — `DefaultClient()` solo fija `http.Client{Timeout: 15s}` y `UserAgent: "badomens/0"`.
- `:27-47` — `Get` hace **una** llamada `c.HTTP.Do(req)`; ante error devuelve `fmt.Errorf("no se pudo consultar %s: %w", url, err)` sin reintentar; ante no-2xx devuelve `"%s respondió %d"`. **No hay bucle, ni backoff, ni respeto a `Retry-After`, ni clasificación tipada.**
- Límite de lectura: `io.LimitReader(resp.Body, 8*1024*1024)` (8 MiB) — existe, pero no valida content-type ni esquema.

### API-02 — `User-Agent "badomens/0"`
- `internal/engines/client.go:22` y `internal/plugins/plugins.go:136`. Fill exige UA identificable con contacto (E-03); hoy es `badomens/0` sin versión real ni contacto.

### API-03 — Paper v2 + error de builds silenciado (`internal/engines/paper.go`)
- `:29` base `https://api.papermc.io/v2/projects/paper` (**retirada**, E-01).
- `:32-42` `ListVersions` deserializa `apiVersions{ Versions []string }` (`:12-14`); el esquema real v3 es array de objetos ⇒ lista vacía/incorrecta.
- `:62-68` `Resolve`:
  ```go
  if err := p.client.GetJSON(ctx, p.Base()+"/versions/"+v.Version+"/builds", &builds); err == nil && len(builds.Builds) > 0 {
      b := builds.Builds[len(builds.Builds)-1]
      res.Pins[Paper.DepVar] = fmt.Sprintf("%d", b.Build)
      ...
  }
  // si falla, se devuelve res igualmente (sin pin) y sin error
  ```
  El error se descarta y se devuelve una resolución **sin el pin obligatorio** (viola T-02/AC-06). El struct `paperBuilds` (`:44-49`) espera `{"builds":[{build,channel}]}`; el v3 real es `[{id, time, channel, commits, downloads}]`.

### API-04 — Fabric: loader general + error silenciado (`internal/engines/fabric.go`)
- `:56-61` `Resolve`: `GetJSON(ctx, p.Base()+"/versions/loader", &loaders)` (catálogo **general**, no `/loader/{game_version}`) con `err == nil && len(loaders) > 0`; toma `loaders[0].Version`. Si falla, devuelve `res` sin `Fabric.DepVar`. Además, `/loader/{game_version}` devuelve la versión **anidada** en `loader.version` (`fabric/loader_26.3.json`), forma distinta de la del catálogo general.

### API-05 — `pickVersion` asume "último = más reciente" (`internal/engines/paper.go`)
- `:174-193`: con `version==""` recorre `for i := len(list)-1; i >= 0; i--` y devuelve el primer `Stable` encontrado desde el final. Fabric entrega newest-first (E-08) y `fabricProvider.ListVersions` (`fabric.go:34-44`) **no invierte** ⇒ devuelve la estable **más antigua**. (Vanilla sí invierte a ascendente en `paper.go:106-120`, por eso no lo sufre.)

### API-06 — Parser Maven compartido (`internal/engines/fabric.go`)
- `:65-72` `mavenRoot{ Versioning struct{ Versions []string } }`.
- `:89-91` `forgeProvider.ListVersions` → `mavenVersions(ctx, client, p.Base()+"/maven-metadata.json")`.
- `:116-118` `neoforgeProvider.ListVersions` → **el mismo** `maven-metadata.json`.
- `:128-141` `mavenVersions`: parsea `versioning.versions` y marca estabilidad por substrings (`beta`/`rc`/`alpha`/`α`).
- Realidad: Forge `maven-metadata.json` es `{"<mc>":["<mc>-<build>", …]}` (sin `versioning`) ⇒ `Versions` vacío; Forge debería usar `promotions_slim.json` (E-09). NeoForge `maven-metadata.json` → 404; el oficial es XML (E-10).

### API-07 — Plugins hasta 256 MiB en memoria (`internal/plugins/plugins.go`)
- `:127-164` `download`: crea cliente de 2 min, `Get`, comprueba `StatusOK`.
- `:156` `io.ReadAll(io.LimitReader(resp.Body, 256*1024*1024))` — **256 MiB en RAM**, sin límite durante la descarga más allá de ese techo.
- `:160` `os.WriteFile(filepath.Join(targetDir, name), raw, 0o644)` — **escritura directa** al destino (no temporal + rename), sin validación ZIP/JAR, sin checksum, sin anti-SSRF. `internal/plugins` no tiene tests.

### JavaMajorFor devuelve 8 (`internal/engines/engines.go`)
- `:82-87`:
  ```go
  func JavaMajorFor(mc string) int {
      if major, ok := javaMajorKnown(mc); ok { return major }
      return 8   // valor por defecto peligroso
  }
  ```
- `javaMajorKnown` (`:91-105`) solo reconoce `1.12`–`1.22`; **no reconoce `26.x`** ⇒ `JavaMajorFor("26.3") == 8`, y `Enrich` (`versions.go:185`) lo escribe en `Version.JavaMajor`. Nota: `JavaImageFor` (`:120-137`) sí usa `javaMajorKnown` (no `JavaMajorFor`) y devuelve el `fallback` cuando es desconocido, por lo que la **imagen Docker no se degrada**, pero la etiqueta `java_major` del catálogo sí miente (AC-05).

## 5. Cómo se consultan los jugadores hoy

Método real: **Server List Ping** (protocolo estándar de Minecraft, handshake + status request), implementado en `internal/idle/slp.go:30-92`.

- **Pestaña Jugadores de la TUI** (`internal/tui/screen_instance.go:376-394`): `playersCmd()` hace `idle.Ping(ctx, host, port, 1500ms)` con `context.WithTimeout(..., 2s)`. Si `host == ""` (p. ej. instancia nunca arrancada / sin IP de tailnet) devuelve directamente `instancePlayersMsg{known:false}` **sin consultar nada**. Si `Ping` falla, devuelve `known:false`. El mensaje se adopta en `:505-506` y se pinta en `:2444-2451` como texto **genérico** ("En línea: no se pudo medir (¿servidor apagado?)"), sin distinguir apagado / iniciando / RCON-Query deshabilitado / error de conexión. Se lanza manualmente con la tecla `t` (`:1328-1329`).
- **Auto-apagado (daemon)** (`internal/cli/idle.go:238-272`): usa un `idle.ChainCounter` = **SLP primero** (`SLPCounter`, timeout 2 s) y **`docker exec … rcon-cli list` como respaldo** (`DockerCounter`, `internal/idle/counter.go:52-83`, regex `There are (\d+) of a max`). Este respaldo **no** se usa en la pestaña de la TUI.
- **No** se usa RCON como método principal ni log parsing. RCON está deshabilitado por defecto en el compose (`internal/runtime/runtime.go:56` `ENABLE_RCON: "FALSE"`), lo que hace que el respaldo `DockerCounter` devuelva `known=false` salvo que el usuario lo habilite.

## 6. Dónde se origina el aviso de MagicDNS

- El texto se genera en `internal/connection/connection.go:99-101`: si `in.MagicDNS == ""` se añade el warning "MagicDNS desactivado: falta el nombre de conexión; usa la dirección IP".
- El valor de entrada en la TUI **no se consulta en vivo**: `internal/tui/screen_home.go:802-810` (`connectionFromSpecWithHost`) toma `magic = spec.Connection.MagicDNS`, es decir, el valor **persistido** en la instancia (escrito en el primer arranque por `internal/cli/connect.go:54-71`).
- El warning se **renderiza** dentro de la tarjeta "Cómo conectarse" (`internal/tui/components/dialogs.go:126-145`, `for _, warn := range info.Warnings`), no como notificación con TTL.
- Consecuencia: como el dato es persistido y no se reevalúa, si MagicDNS se habilita después el aviso puede permanecer; y por diseño **no** pasa por el sistema de notificaciones (sección 12). La pantalla de Salud **sí** consulta `tailscale.MagicDNS` en vivo (`screen_health.go:126`), pero ese valor se muestra como dato informativo, no como aviso flotante.

## 7. Política actual de regeneración

- Implementación real en `internal/tui/svc/worlds.go:88-112` (`realWorlds.Regenerate`):
  1. Carga el `Spec`.
  2. `stopIfRunning` (`:80-86`): si el contenedor corre, lo **detiene**.
  3. `os.RemoveAll` de los directorios de mundos activos.
  4. Guarda la nueva `Seed` en el `Spec`.
  5. Si estaba corriendo, lo **vuelve a arrancar** (`rt.Start`).
- Por tanto la política vigente es **"apagar y reiniciar (regenerar en el acto)"**. El diálogo de confirmación (`internal/tui/screen_instance.go:2939-2962`) **solo** advierte de la pérdida de Overworld/Nether/End; **no** explica que el servidor se apagará y reiniciará, ni ofrece backup previo, ni hay un estado "pendiente para el próximo arranque". Si el arranque posterior falla, el servidor queda detenido (estado parcialmente recuperable → T-08/AC-10).

## 8. Fixtures de proveedores

Descargados con `curl.exe` (UA `badomens-audit/0.1 (contract-audit)`) el 2026-10-14 y guardados en
`internal/engines/testdata/` (subcarpetas por proveedor). Revisa `internal/engines/testdata/README.md`
para el origen, el recorte aplicado a cada archivo y las notas de esquema.

| Proveedor | Archivo | Endpoint |
|---|---|---|
| Paper (Fill v3) | `paper_fill_v3/versions.json`, `paper_fill_v3/builds_26.3.json`, `paper_fill_v3/builds_1.21.4.json` | `https://fill.papermc.io/v3/projects/paper/versions` y `.../versions/{v}/builds` |
| Paper (v2, retirada) | `paper_v2/versions.json` | `https://api.papermc.io/v2/projects/paper/versions` (**404**) |
| Mojang | `mojang/version_manifest_v2.json` | `https://piston-meta.mojang.com/mc/game/version_manifest_v2.json` |
| Fabric | `fabric/versions_game.json`, `fabric/loader_26.3.json`, `fabric/loader_general.json` | `https://meta.fabricmc.net/v2/versions/game`, `/versions/loader/{game}`, `/versions/loader` |
| Forge | `forge/promotions_slim.json`, `forge/maven-metadata.json` | `https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json` y `.../forge/maven-metadata.json` |
| NeoForge | `neoforge/maven-metadata.xml` | `https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml` |

## 9. Reglas respetadas y efectos del trabajo

- **No** se ha modificado lógica de negocio ni ningún `.go` de producción. Únicos artefactos nuevos:
  `docs/UPSTREAM_VERIFICATION.md`, `docs/REGRESSION_MATRIX.md` e `internal/engines/testdata/**`.
- **No** se han creado suites paralelas: las pruebas de TUI se amplían en `docs/TUI_ACCEPTANCE_TESTS.md`.
- **No** se ha ejecutado `commit`.


## 10. Aviso de concurrencia (trabajo de otros agentes)

Durante esta auditoría, **otros agentes trabajaban en el mismo árbol** de la rama
`fix/estabilizacion-plan-completo`. En particular, al cerrar la Fase 0 el árbol mostraba
modificaciones **ajenas a esta auditoría**:

- `internal/engines/client.go` (reescrito; probablemente Fase 1 — transporte HTTP)
- `internal/engines/client_test.go` (nuevo)
- `internal/tailscale/tailscale.go` (modificado) y `internal/tailscale/state.go` (nuevo)
- `internal/infra/` (nuevo)

**Efectos sobre este documento:**

- La evidencia **API-01** (`client.go:12,21-23,27-47`) y **API-02** (`client.go:22`) corresponde
  al estado **original** leído al inicio de la sesión (línea base de Fase 0). Si la Fase 1 ya
  reescribió ese archivo, los números de línea habrán cambiado; el defecto documentado sigue
  siendo el punto de partida válido.
- La **Línea base** (§2) se ejecutó al inicio de la sesión (todo en verde). El árbol volvió a
  compilar (`go build ./...` = exit 0) tras las ediciones concurrentes, pero **no** se volvió a
  ejecutar la suite completa sobre el árbol ya modificado por otros agentes.
- El Auditor de contratos **no** ha modificado ningún archivo `.go` de producción.
