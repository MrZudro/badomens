# badomens — Reverificación de hechos externos (Fase 0)

> **Autor:** Auditor de contratos (Agente 1, Fase 0).
> **Rama:** `fix/estabilizacion-plan-completo`.
> **Fecha de consulta de fuentes:** 2026-10-14.
> **Fuentes leídas antes de empezar:** `AGENTS.md`, `PLAN_IMPLEMENTACION_badomens.md` (§1 y §8), `spec 1.md` (§2, §3, §7), `SPEC 2.md` (§2, §3, §5, §7, §9, §12).
> **Alcance:** no se modifica lógica de negocio. Los fixtures descargados viven en `internal/engines/testdata/`.

Este documento responde al mandato del plan y de `spec 1.md` §3.1: los hechos externos citados
por la spec técnica provienen de fuentes secundarias y **deben reverificarse** contra la fuente
oficial antes de basar código en ellos. Cada hecho se marca como **VERIFICADO**, **NO VERIFICADO**
o **CONTRADICHO**, con la evidencia (comando o URL).

## 1. Tabla de reverificación

| # | Hecho citado por la spec | Estado | Evidencia (fuente oficial / salida real) |
|---|---|---|---|
| E-01 | Paper API v2 dejó de recibir builds el 31-dic-2025 y su desactivación completa estaba prevista para el 1-jul-2026. | **VERIFICADO** (más estricto: ya está retirada) | `GET https://api.papermc.io/v2/projects/paper/versions` devuelve **404** con cuerpo `{"ok":false,"error":"unknown_method","message":"No endpoint GET /v2/projects/paper/versions."}` (ver `internal/engines/testdata/paper_v2/versions.json`). Foro oficial-adyacente (CubeCoders) documenta el estado `{"ok":false,"error":"sunset"}` y la migración a Fill v3. Documentación oficial del servicio: https://docs.papermc.io/misc/downloads-service/ |
| E-02 | El esquema de Fill v3 devuelve **objetos de versión**, no cadenas; el parser `apiVersions` (array de strings) no sirve. | **VERIFICADO** | `GET https://fill.papermc.io/v3/projects/paper/versions` devuelve `{"versions":[{"version":{"id":"26.3",...},"builds":[...]}, ...]}` (ver `paper_fill_v3/versions.json`). El struct `apiVersions` (`internal/engines/paper.go:12`) espera `["1.21.4", ...]`. |
| E-03 | Fill v3 publica el requisito de Java por versión y exige un `User-Agent` identificable con contacto. | **VERIFICADO** | `paper_fill_v3/versions.json` incluye `java.version.minimum` (p. ej. `25` para `26.3`). Docs oficiales exigen UA que identifique el software y **no** sea genérico (https://docs.papermc.io/misc/downloads-service/). |
| E-04 | Minecraft cambió a versionado basado en año/entrega (`26.x`). | **VERIFICADO** | `mojang/version_manifest_v2.json`: `latest.release = "26.3"`, `latest.snapshot = "26.4-snapshot-4"`. Fuente: https://piston-meta.mojang.com/mc/game/version_manifest_v2.json |
| E-05 | Minecraft Java Edition 26.1 requiere **Java 25**. | **VERIFICADO** | Minecraft Wiki (Java Edition 26.1 RC): "Minimum Java version: Java SE 25". Confirmado además por Paper Fill v3 (`java.version.minimum = 25` para `26.3`) y por el manifiesto (`26.3`). La página `minecraft.net/en-us/article/minecraft-java-edition-26-1` **no respondió** en la fecha de la auditoría (timeout); se cita la wiki y el metadato de Paper. |
| E-06 | La imagen `itzg/minecraft-server` publica una etiqueta `java25`. | **VERIFICADO** | Tabla oficial de etiquetas: https://github.com/itzg/docker-minecraft-server/blob/master/docs/versions/java.md → `latest`, `stable`, `java25`, `java25-alpine`, `java25-jdk`, `java25-graalvm` = **Java 25**. |
| E-07 | Regresión del catálogo Maven de NeoForge del 27-sep-2026 (solo tres versiones en los metadatos). | **VERIFICADO** (ocurrió; **ya resuelto upstream**) | Issue https://github.com/modrinth/code/issues/7725 (abierta 27-sep-2026, cerrada 29-sep-2026): `maven-metadata.xml` listaba solo 3 versiones. Estado actual: `neoforge/maven-metadata.xml` tiene **1816** entradas `<version>` con `latest=26.3.0.77`. |
| E-08 | Fabric Meta entrega las versiones **más nuevas primero**. | **VERIFICADO** | `fabric/versions_game.json` empieza por `26.4-snapshot-4`, `26.4-snapshot-3`… y desciende. `pickVersion` (`internal/engines/paper.go:174`) recorre desde el final. |
| E-09 | Forge expone el contrato de **promociones** (`recommended`/`latest` por versión del juego), distinto de una lista completa de builds. | **VERIFICADO** | `forge/promotions_slim.json`: `{"homepage":...,"promos":{"1.1-latest":"1.3.4.29", "<mc>-recommended":"...", ...}}` (117 promos, 40 con `recommended`). |
| E-10 | NeoForge exige **parsing XML** oficial y coexisten dos esquemas de versión (antiguo `21.x` y moderno `26.x`). | **VERIFICADO** | `neoforge/maven-metadata.xml`: 1003 versiones `21.*` (esquema antiguo) y 331 con esquema `26.*`, en el mismo documento. Docs: https://docs.neoforged.net/docs/gettingstarted/versioning/ |
| E-11 | Fabric dispone de `/v2/versions/loader/{game_version}` para loaders compatibles con una versión concreta del juego. | **VERIFICADO** | `GET https://meta.fabricmc.net/v2/versions/loader/26.3` → 200 (objetos con `loader.version`, `intermediary`, `launcherMeta`). Distinto del catálogo general `/v2/versions/loader`. |

### Hechos NO VERIFICADOS / CONTRADICHOS

| # | Hecho | Estado | Nota |
|---|---|---|---|
| E-12 | Fechas exactas de la hoja de ruta de Paper v2 (31-dic-2025 / 1-jul-2026) según la fuente secundaria citada por la spec (`TimInTech/minecraft-server-Proxmox`). | **NO VERIFICADO directamente** | No se encontró un anuncio oficial con esas fechas exactas. Lo que **sí** está verificado (E-01) es que a fecha de hoy el endpoint v2 devuelve 404. La conclusión para el código no cambia: hay que migrar a v3. |
| E-13 | "El código actual de `JavaMajorFor` devuelve Java 8 por defecto". | **VERIFICADO** en el código, no es un hecho externo | `internal/engines/engines.go:82-87` devuelve `8` cuando `javaMajorKnown` no reconoce la versión. Ver `REGRESSION_MATRIX.md` §API-Java. |

## 2. Conclusión y efecto sobre el plan

- **La migración de Paper a Fill v3 (Fase 2) es obligatoria hoy**, no una mejora: la API v2 está
  retirada y responde 404, por lo que el catálogo Paper actual está **completamente roto**.
- **Java 25 es un requisito real** para Minecraft 26.x; el mapeo de Java del repo debe cubrirlo y
  no debe inventar Java 8 para versiones desconocidas (ver AC-05).
- **La regresión de NeoForge de E-07 ya está resuelta por el upstream**; el código debe, aun así,
  ser resiliente a ese escenario (conservar el último catálogo válido, detectar reducción anómala,
  AC-07), porque volvió a ocurrir y puede repetirse.
- Todas las URLs anteriores respondieron con normalidad **salvo** `minecraft.net/.../26-1`
  (timeout) y los endpoints que hoy devuelven error por diseño (`api.papermc.io/v2/...` → 404,
  `maven.neoforged.net/.../maven-metadata.json` → 404). Ninguna respuesta se ha inventado.

## 3. Preguntas abiertas del plan (§8) — estado tras la Fase 0

| # | Pregunta (plan §8) | Resolución de la Fase 0 |
|---|---|---|
| 1 | **Política de regeneración (RG-03):** ¿requiere apagado, lo apaga, o se aplica al próximo arranque? | La implementación **ya apaga y reinicia** (`internal/tui/svc/worlds.go:89-112`): si el servidor corre, se detiene, se borra el mundo, se guarda la semilla y se vuelve a arrancar. **Pero el diálogo no comunica esa política** (`screen_instance.go:2939-2962` solo avisa de pérdida de datos). La política a fijar es "Apagar y regenerar (reinicia)" y debe explicarse antes de confirmar. Hipótesis del plan (apagar+regenerar) es la más cercana a lo implementado. |
| 2 | **Método de consulta de jugadores.** | **Server List Ping** (protocolo estándar), con `docker exec … rcon-cli list` como respaldo **solo en el daemon de auto-apagado**, no en la pestaña de la TUI. Ver `REGRESSION_MATRIX.md` §Jugadores. |
| 3 | **Nivel de instalación del daemon** (usuario vs sistema). | No abordado en Fase 0 (Fase 8). El repo no contiene aún la lógica de instalación de servicio; se documentará en la Fase 8. |
| 4 | **Hechos externos** (Paper v2, MC 26.x/Java 25, regresión NeoForge). | Resueltos en §1: E-01, E-04, E-05, E-07. |
| 5 | **Alcance de CurseForge.** | Sin cambios: fuera de P1 (requiere API key). Confirmado en `spec 1.md` §3.2. |
| 6 | **Agrupación de versiones en el grid.** | Depende de cada API: Fill v3 ofrece `channel`/`support`/`java`; Mojang ofrece `type`/`releaseTime`; Fabric ofrece `stable`; NeoForge no ofrece canal por versión. El grid cae a orden + marca básica donde no haya fecha/canal. |
| 7 | **Compatibilidad hacia atrás del JSON.** | A validar en las fases de implementación; los campos nuevos (`compatibility`, `stale`, `warning`) son aditivos. No se tocó el contrato en Fase 0. |

---

## 4. Cierre (tras las fases 0-12)

> Añadido por el orquestador al cerrar la entrega. Contrasta lo verificado en la
> Fase 0 con lo que acabó implementándose.

### 4.1 Efecto real de cada hecho externo

| Hecho | Efecto en el código entregado |
|---|---|
| E-01 (Paper v2 retirada) | `internal/engines/paper.go` migró a **Fill v3** con parser propio del esquema de objetos y resolución a un build **STABLE con artefacto de servidor descargable** |
| E-03 (Java por versión + User-Agent) | El Java **verificado** (`java.version.minimum`) gana a la regla versionada; el `User-Agent` dejó de ser `badomens/0` |
| E-05 / E-06 (Java 25) | `JavaMajorFor` mapea `26.1+` → 25 y `JavaTag` conoce `java25`; ya **no** se asume Java 8 |
| E-07 (regresión del Maven de NeoForge) | Detección de **reducción anómala** con conservación del último catálogo validado (AC-07), aunque el upstream ya lo arregló |
| E-08 (Fabric newest-first) | Se eliminó la suposición «el último elemento es el más reciente»: la selección ordena por esquema de versión |
| E-09 (Forge = promociones) | `promotions_slim.json` se trata como el mapa `recommended`/`latest` que es, contrastado contra artefactos descargables |
| E-10 (NeoForge XML, dos esquemas) | Parser XML con asociación **explícita** Minecraft ↔ NeoForge y sin ordenar alfabéticamente entre esquemas |
| E-11 (`/loader/{game_version}`) | Fabric consulta los loaders compatibles de la versión concreta, no el catálogo general |
| E-12 (fechas exactas de la hoja de ruta de Paper v2) | **Sigue sin verificarse**: la conclusión no depende de las fechas, sino de que el endpoint v2 devuelve 404 |

### 4.2 Bloqueos que NO se pueden resolver desde badomens

1. **Paper API v2 retirada** (`api.papermc.io/v2/...` → `404 unknown_method`): corregido en
   badomens migrando a Fill v3, pero cualquier consumidor externo de la v2 seguirá roto.
2. **CurseForge** (AC-13/AC-14 fuera de alcance por diseño): requiere *API key* y sus permisos.
   Punto de extensión documentado en `docs/plugins-marketplace.md` §3.
3. **`minecraft.net/.../26-1`** no respondió durante la auditoría; el requisito de Java 25 se
   confirmó por la wiki, por el metadato de Paper Fill v3 y por la tabla de etiquetas de itzg.
4. **`go test -race`**: no es un bloqueo de upstream, sino del entorno (requiere cgo/gcc). Queda
   pendiente de ejecutar en CI.

### 4.3 Preguntas abiertas del plan (§8): cierre

| # | Pregunta | Resolución final |
|---|---|---|
| 1 | Política de regeneración (RG-03) | **Implementada y comunicada**: «apagar y regenerar (y reiniciar)», declarada en el diálogo **antes** de confirmar, con backup previo ofrecido. Se descartó la variante «programada» a propósito |
| 2 | Método de consulta de jugadores | **Server List Ping** (con RCON como respaldo); la TUI no consulta en estado 🟡 y diferencia cuatro causas. RCON no es habilitable desde la TUI (`ENABLE_RCON: "FALSE"` en el compose) |
| 3 | Nivel de instalación del daemon | **Usuario por defecto** (systemd `--user`, LaunchAgent, tarea `ONLOGON`); no se pidió elevación sin explicar el motivo |
| 4 | Hechos externos | Reverificados (§1) e incorporados (§4.1) |
| 5 | Alcance de CurseForge | Confirmado: fuera de P1 |
| 6 | Agrupación de versiones en el grid | Se agrupa por versión mayor cuando la fuente lo permite; donde no hay fecha/canal (Paper Fill v3 por versión) el grid cae a orden + marca básica |
| 7 | Compatibilidad hacia atrás del JSON | **Cerrada**: el contrato (`ok/exit_code/action/message/data/phases/error/timestamp`) no se rompió; todo campo nuevo es aditivo. Hay **tres cambios de semántica** documentados en `CHANGELOG.md` (`compatible` derivado de la compatibilidad trivalente, `java_major` puede ser 0, y el mapeo incorpora Java 25) |
