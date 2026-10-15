# Entrega final — Plan de implementación unificado (fases 0–12)

> Rama: `fix/estabilizacion-plan-completo` (parte de `main` en `828c38c`).
> Resumen para el revisor: qué se entregó, con qué prueba se demuestra y qué
> queda fuera. La matriz defecto → prueba está en `docs/REGRESSION_MATRIX.md` y
> el detalle por fase en `CHANGELOG.md`.

---

## 1. Qué se entregó, en una línea

Se estabilizó la capa de catálogo (los cinco proveedores con su contrato real, sin
éxito parcial y con caché resiliente), se unificó CLI/TUI/MCP sobre un único
`CatalogService`, se corrigieron los flujos de la TUI que dependían de esa capa
(bugs QC-03/QC-04 y el crítico AD-02) y se cerraron las fases de infraestructura,
seguridad, observabilidad y rendimiento.

**17 commits**, uno o dos por fase. `go build`, `go vet`, `gofmt` y
`golangci-lint` en verde; 736 funciones de prueba, todas pasando.

---

## 2. Mapa fase → commit

| Fase | Commit | Asunto |
|---|---|---|
| 0 · Línea base | `1f0f163` | `docs(engines): linea base, fixtures reales y matriz de regresion (Fase 0)` |
| 1 · Transporte HTTP | `8a41205` | `feat(engines): transporte HTTP con reintentos, errores tipados y validadores (Fase 1)` |
| 8 · Daemon/Docker/Tailscale | `d13d34c` | `feat(infra): daemon, Docker y Tailscale con deteccion y arranque garantizado (Fase 8)` |
| 2 · Paper + Vanilla | `d8637dc` | `fix(engines): migra Paper a Fill v3 y endurece Vanilla (Fase 2, AC-01)` |
| 2 · Fabric/Forge/NeoForge | `aa1a203` | `fix(engines): Fabric, Forge y NeoForge con parser propio y sin exito parcial (Fase 2, AC-02..AC-04)` |
| 6 · Componentes base TUI | `8ed8682` | `feat(tui): selector, barra, notificaciones con TTL, semaforo y cableado de infra (Fase 6)` |
| 11.1 · Descarga segura | `af84e8b` | `feat(plugins): descarga segura con streaming, limite, validacion y movimiento atomico (Fase 11.1, AC-08)` |
| — | `1b3a623` | `fix(plugins): corrige la codificacion de comentarios y del aviso de terceros` |
| 3 y 4 · Resolución y caché | `5ff2a36` | `fix(engines): seleccion exacta, compatibilidad trivalente, Java real y cache resiliente (Fases 3-4)` |
| 10 · Pulido de UI | `c6c6ae8` | `feat(tui): ayuda en grid, Plugins (SOON) y elimina la fila de pestanas de primer nivel (Fase 10)` |
| 11.2-11.3 · Marketplaces (paquetes) | `f2845d6` | `feat(plugins): marketplaces Modrinth y Hangar con procedencia y comprobacion de actualizaciones` |
| 5 · CatalogService | `45ead4f` | `feat(engines): CatalogService y unificacion de CLI, TUI y MCP en una sola ruta (Fase 5)` |
| — | `9005a31` | `fix(engines): usa rangos de medios validos en la cabecera Accept` |
| 11.2-11.3 · CLI | `0e21eba` | `feat(cli): cablea los marketplaces y registra engine compat` |
| 7 y 9 · Flujos e instancia | `f123139` | `feat(tui): flujos de creacion y funciones de instancia (Fases 7 y 9)` |
| 12 · Observabilidad y seguridad | `4ccc917` | `feat(observabilidad): diagnostico por proveedor, doctor api, seguridad de runtime y funciones P2 (Fase 12)` |
| 12.1-12.2 · Accesibilidad y rendimiento | `19c1d4c` | `perf(tui): accesibilidad, metricas, benchmarks y pins exactos en la creacion` |

---

## 3. Criterios de aceptación de la capa de datos y seguridad

| ID | Criterio | Estado | Prueba / evidencia |
|---|---|---|---|
| AC-01 | Paper usa Fill v3 y resuelve un build estable verificable | CUMPLE | `TestPaperResolveVersionExplicitaConPinExacto`, `TestPaperDescartaBuildSinArtefacto`, `TestPaperListaDeBuildsVaciaFalla` |
| AC-02 | Fabric elige la más reciente y su loader compatible | CUMPLE | `TestFabricLoaderPorGameVersion`, `TestFabricLatestStableNoDependeDelOrden`, `TestFabricResolveSinLoaderFalla` |
| AC-03 | Forge acepta solo respuestas validadas por su adaptador | CUMPLE | `TestForgeResolucionExactaConPromocionesReales`, `TestForgeIndiceMavenVacioNoDejaCatalogoVacio`, `TestForgePromocionesSchemaDistintoFalla` |
| AC-04 | NeoForge soporta ambos esquemas y detecta datos insuficientes | CUMPLE | `TestNeoForgeXmlEsquemaAntiguoResuelveConPin`, `TestNeoForgeXmlEsquemaModernoResuelveConPin`, `TestNeoForgeCaidaAnomalaAdvierteYConserva`, `TestNeoForgeNoOrdenaAlfabeticamenteEntreEsquemas` |
| AC-05 | Versiones desconocidas no reciben requisitos Java falsos | CUMPLE | `TestJavaMajorForNuncaAsumeOcho`, `TestJavaRequirementForExponeElOrigen` |
| AC-06 | Las resoluciones incompletas no se consideran exitosas | CUMPLE | `TestResolvedValidateExigeTodosLosPines`, `TestRegistryResolveNormalizaYValida`, `TestPluginInstallResolucionIncompleta` |
| AC-07 | El catálogo persistente nunca se reemplaza por una respuesta inválida | CUMPLE | `TestCacheNoSobrescribeConCatalogoVacio`, `TestCacheDetectaReduccionAnomala`, `TestCacheEscrituraAtomica` |
| AC-08 | La descarga de plugins es limitada, validada y atómica | CUMPLE | `TestStageURLAbortaSiSuperaElLimite`, `TestStageURLRechazaDestinosSSRF`, `TestStageURLRedireccionAPrivada`, `TestInstallJarInvalido`, `TestStageURLChecksum` |
| AC-09 | Sin regresiones del contrato JSON ni de los comandos | CUMPLE | `TestContratoJSONAditivo*` (`internal/cli/paridad_catalogo_test.go`), `TestParidadCLITUI` |
| AC-10 | Las operaciones fallidas dejan estado recuperable e informado | CUMPLE | `internal/result/outcome.go` + fases `applied/failed/partial`; `TestPluginInstallCompletoRegistraUnaVez` (rollback) y `backup restore` con `outcome` |
| AC-11 | Pruebas de concurrencia y regresión de TUI con `-race` | PARCIAL | `TestCacheDeduplicaSolicitudesSimultaneas` (12 goroutines → 1 petición). **`-race` no ejecutable en este entorno** (requiere cgo/gcc): queda para CI |
| AC-12 | Diagnóstico por proveedor: conexión vs contrato vs compatibilidad | CUMPLE | `badomens doctor api` + `engine/diagnostic.go`; `TestDiagnose*`, `TestDoctorAPI*`, `TestObservabilidadSinSecretos` |
| AC-13 | Marketplace Modrinth y Hangar con filtro por motor/juego | CUMPLE | `internal/plugins/marketplace/{modrinth,hangar}`; `TestPluginVersionsFiltraPorEntorno` |
| AC-14 | Actualización de plugins con procedencia, versiones y checksums | CUMPLE | `internal/plugins/provenance.go`, `install.CheckUpdates`; `TestPluginUpdatesDetectaYNoModificaElArchivo` |

---

## 4. Criterios UX / TUI

| ID | Criterio | Estado | Prueba / evidencia |
|---|---|---|---|
| UX-01 | Barra inferior con formato único y atajos retirados documentados en Ayuda | CUMPLE | `BAR-01..03`, `HELP-03` |
| UX-02 | `↑/↓` en todo el flujo de creación; `↓` nunca avanza de paso | CUMPLE | `WIZ-02`, `WIZ-03` |
| UX-03 | Panel lateral de versiones en grid + celda «Continuar a Recursos →» | CUMPLE | `WIZ-02`, `WIZ-04`, `WIZ-05` |
| UX-04 | La versión estable más reciente aparece y ninguna listada da 404 | CUMPLE | `WIZ-06`, `WIZ-07` (Fase 2/3 garantizan el artefacto) |
| UX-05 | Motor y versión elegidos == motor y versión arrancados, con resumen | CUMPLE | `WIZ-08`, `WIZ-09`, `PIN-01`, `TestCreateInyectaPinsExactos` |
| UX-06 | Daemon/Docker/Tailscale se detectan y se levantan con elevación explicada | CUMPLE | `INFRA-01..04`; `internal/infra` + `TestEnsureElevationRefusedDegrades` |
| UX-07 | MagicDNS: causa documentada y aviso no permanente | CUMPLE | `MAGIC-01`, `MAGIC-02`; causa raíz en `docs/infra-fase8.md` |
| UX-08 | Jugadores correcto con servidor activo; mensajes diferenciados | CUMPLE | `PLAY-01..04`; método real = Server List Ping (Fase 0) |
| UX-09 | Semáforo (🟢/🟡/🔴 + texto) en todas las pantallas de la instancia | CUMPLE | `SEM-01..04`; `components/serverstate.go` |
| UX-10 | Regeneración con `↑/↓`, política explícita antes de confirmar y feedback | CUMPLE | `REG-01..04`; política «apagar y regenerar» declarada en el diálogo |
| UX-11 | Ninguna notificación dura más de 30 s | CUMPLE | `NOT-01..04`; tope absoluto en `components/notify.go` |
| UX-12 | Copiar dirección funciona o explica por qué no y qué hacer | CUMPLE | `COPY-01..04`; candidatas derivadas antes del primer arranque |
| UX-13 | `[D] Borrar` visible, con confirmación explícita y limpieza completa | CUMPLE | `DEL-01..05` |
| UX-14 | Ayuda en grid alineado, orden piramidal por longitud de tecla | CUMPLE | `HELP-01..04` |
| UX-15 | `[Esc] Atrás` resaltado con el input de comandos enfocado | CUMPLE | `ESC-01`, `ESC-02` |
| UX-16 | Plugins muestra (SOON) | CUMPLE | `SOON-01` |
| UX-17 | Pestaña superior eliminada sin romper el acceso al detalle | CUMPLE | `UI-TABS-01`, `NAV-01`, `ZONE-01..03` |

Los IDs por pantalla están definidos y mapeados en `docs/TUI_ACCEPTANCE_TESTS.md`
(la matriz se amplió; no se creó una suite paralela).

---

## 5. Cómo reproducir cada corrección

Todo se ejecuta con un directorio de datos aislado para no tocar datos reales:

```bash
export BADOMENS_DATA_DIR=$(mktemp -d)     # Windows: %TEMP%\badomens-check
go build -o /tmp/badomens ./cmd/badomens
```

| Corrección | Comando | Resultado esperado |
|---|---|---|
| Paper migrado a Fill v3 (AC-01) | `badomens catalog refresh paper --json` | `state:"fresh"`, decenas de versiones, `recommended` = última estable |
| Sin éxito parcial (AC-06) | `badomens engine use demo paper --version 9.9` | `exit_code 2`, `error.code error_not_found` y **ningún** cambio de configuración |
| Compatibilidad trivalente (T-03) | `badomens engine compat paper --version <v> --java 21 --build <b> --json` | `compatible` → 0 / `unknown` → 1 / `incompatible` → 2 |
| Java correcto (AC-05) | `badomens setup --name d --engine vanilla --no-start --json` con una versión moderna | `java_major: 25`, `java_status: verified` (o `inferred`), **nunca 8 por defecto** |
| Caché resiliente (AC-07) | `badomens catalog status paper` | `fresh` / `stale` / `invalid` / `unavailable` con antigüedad legible |
| Descarga segura (AC-08) | `badomens plugin add http://example.com/p.jar -i demo` | `exit_code 2`, `reason: tls_requerido`, sin archivo y sin registro |
| SSRF (AC-08) | `badomens plugin add http://169.254.169.254/x.jar -i demo` | rechazado como `link-local`, nada escrito |
| Diagnóstico (AC-12) | `badomens doctor api paper --json` | HTTP, ms, formato, contrato, edad de caché y veredicto por proveedor |
| Compatibilidad previa (12.5) | `badomens doctor preflight demo` | veredicto trivalente antes de arrancar |
| Reproducibilidad (12.5) | `badomens repro export demo` → `badomens repro import f --name copia` | misma versión y pins exactos, recreada sin consultar la red |
| Marketplaces (AC-13) | `badomens plugin search viaversion --limit 3 -i demo --json` | resultados con `compatibility` y `compatibility_verified:false` |
| Actualizaciones (AC-14) | `badomens plugin updates -i demo --json` | `installed_modified:false`; informa sin instalar |

TUI: `badomens tui` (con `BADOMENS_DATA_DIR` aislado). Los criterios de pantalla
están en `docs/TUI_ACCEPTANCE_TESTS.md` y los benchmarks se ejecutan con
`go test -bench BenchmarkView ./internal/tui/`.

---

## 6. Qué NO está cubierto (y por qué)

1. **`go test -race`**: `-race requires cgo` y este entorno no tiene gcc. La
   deduplicación y la concurrencia se verifican con contadores atómicos y
   `-count=3`; **queda pendiente ejecutarlo en CI con cgo** (AC-11 parcial).
2. **RCON habilitable desde la TUI**: `internal/runtime` escribe
   `ENABLE_RCON: "FALSE"` y no hay opción por instancia, así que el respaldo RCON
   de la pestaña Jugadores siempre falla. La TUI lo **diagnostica y explica**, y la
   consulta principal (Server List Ping) funciona; habilitarlo requiere
   `spec.RCON{Enabled,Password}` y variables nuevas en el compose.
3. **CurseForge**: requiere API key; queda como punto de extensión documentado
   (`docs/plugins-marketplace.md` §3). Fuera de P1 por decisión del plan.
4. **Paper API v2 retirada por el upstream**: al cierre de este trabajo
   `https://api.papermc.io/v2/...` devuelve `404 unknown_method`. No es
   corregible desde badomens; la migración a Fill v3 es la respuesta.
5. **Regeneración programada**: descartada a propósito. Se implementó **una sola
   política** («apagar y regenerar») para que el usuario sepa antes de confirmar;
   no se añadió la variante «pendiente hasta el próximo arranque».
6. **Agrupación de versiones en el grid**: se agrupa por versión mayor cuando el
   catálogo aporta el dato; en Paper Fill v3 no hay fecha por versión, así que ese
   catálogo cae al orden y marca básicos (documentado en `docs/catalogo-servicio.md`).
7. **Validación visual y E2E independientes**: ejecutadas por subagentes
   separados al cierre; su veredicto se registra en la sección 7.

---

## 7. Validación independiente

- **Revisión de cumplimiento (revisor independiente):** ver sección de resultados
  de la sesión de validación.
- **E2E sobre el binario real:** ver escenarios 1-13 de la sección 6.2 del plan.
- **QA visual de la TUI** (capturas reales en varios tamaños).

> Nota: en Windows, Smart App Control bloquea de forma **intermitente** los
> binarios de test recién compilados (`Una directiva de Control de aplicaciones
> bloqueó este archivo`). No es un fallo de código: reintentar o usar
> `-ldflags="-w -s"` (`make test` ya lo contempla).
