# Fixtures de proveedores (Fase 0 — Auditor de contratos)

Respuestas **reales** de los endpoints oficiales de cada proveedor, descargadas el
**2026-10-14** con `curl.exe -sS -L -A "badomens-audit/0.1 (contract-audit)"`.
Sirven como base de las pruebas unitarias deterministas (no dependen de red).

Algunos archivos están **recortados** para no engordar el repositorio: se conserva la
**forma del esquema** y los **primeros/últimos** elementos. El recorte se indica por archivo.
Todos siguen siendo JSON/XML **válidos** y parseables.

## paper_fill_v3/ — Paper, Fill API v3 (endpoint oficial actual)

| Archivo | URL | HTTP | Recorte aplicado |
|---|---|---|---|
| `versions.json` | `https://fill.papermc.io/v3/projects/paper/versions` | 200 (65 682 B) | Se conservan 40 primeras + 10 últimas versiones (de 50 en total tras recorte). Se eliminó `version.java.flags.recommended` (JVM flags repetidos) y se recortaron los arrays `builds` a 4 primeros + 4 últimos. |
| `builds_26.3.json` | `https://fill.papermc.io/v3/projects/paper/versions/26.3/builds` | 200 (60 404 B) | 5 primeros builds. |
| `builds_1.21.4.json` | `https://fill.papermc.io/v3/projects/paper/versions/1.21.4/builds` | 200 (142 423 B) | 5 primeros builds. |

**Esquema (clave para API-03):**
- `GET /versions` → `{"versions":[{"version":{"id":"26.3","support":{"status":"SUPPORTED"},"java":{"version":{"minimum":25}}},"builds":[3,4,…]}, …]}`.
  Es un **array de objetos**, no un array de strings. El orden es **descendente** (más nuevo primero: `26.3` … `1.7.10`).
- `GET /versions/{v}/builds` → **array** de `{"id":190,"time":…,"channel":"STABLE","commits":[…],"downloads":{"server:default":{"name":"paper-26.3-190.jar","checksums":{"sha256":…},"size":…,"url":…}}}`.
  El canal estable es `"STABLE"` (mayúsculas) y el número de build es `id` (no `build`).

> Nota: `26.3` declara `java.version.minimum = 25`. Corrobora E-03/E-05 de `docs/UPSTREAM_VERIFICATION.md`.

## paper_v2/ — Paper, API v2 (**retirada**)

| Archivo | URL | HTTP | Contenido |
|---|---|---|---|
| `versions.json` | `https://api.papermc.io/v2/projects/paper/versions` | **404** | `{"ok":false,"error":"unknown_method","message":"No endpoint GET /v2/projects/paper/versions."}` |

Conserva la **respuesta de error real**: demuestra que la migración a v3 es hoy obligatoria
(E-01). No se ha fabricado ninguna respuesta.

## mojang/ — Manifiesto de versiones de Minecraft

| Archivo | URL | HTTP | Recorte aplicado |
|---|---|---|---|
| `version_manifest_v2.json` | `https://piston-meta.mojang.com/mc/game/version_manifest_v2.json` | 200 (277 821 B) | Se conserva `latest` y 60 primeras + 20 últimas entradas de `versions` (de ~1000). |

**Esquema:** `{"latest":{"release":"26.3","snapshot":"26.4-snapshot-4"},"versions":[{"id":…,"type":"release|snapshot|old_beta|old_alpha","url":…,"time":…,"releaseTime":…,"sha1":…,"complianceLevel":…}, …]}`.
El orden es **descendente** (más nuevo primero). Confirma el versionado `26.x` (E-04).

## fabric/ — Fabric Meta v2

| Archivo | URL | HTTP | Recorte aplicado |
|---|---|---|---|
| `versions_game.json` | `https://meta.fabricmc.net/v2/versions/game` | 200 (30 358 B) | Ninguno (completo). |
| `loader_general.json` | `https://meta.fabricmc.net/v2/versions/loader` | 200 (38 900 B) | Ninguno (completo). |
| `loader_26.3.json` | `https://meta.fabricmc.net/v2/versions/loader/26.3` | 200 (589 772 B) | 3 primeros loaders; en cada uno se vació `launcherMeta.libraries` conservando `version` y `min_java_version`. |

**Esquema:**
- `/versions/game` → `[{"version":"26.4-snapshot-4","stable":false}, …]`, **más nuevo primero** (E-08).
- `/versions/loader` (general) → `[{"separator":".","build":5,"maven":…,"version":"0.19.5","stable":true}, …]` (versión en la **raíz**).
- `/versions/loader/{game}` → `[{"loader":{"version":"0.19.5","maven":…,"stable":true},"intermediary":{…},"launcherMeta":{…}}]` (versión **anidada** en `loader.version`).

## forge/ — Forge

| Archivo | URL | HTTP | Recorte aplicado |
|---|---|---|---|
| `promotions_slim.json` | `https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json` | 200 (4 113 B) | Ninguno (completo). |
| `maven-metadata.json` | `https://files.minecraftforge.net/net/minecraftforge/forge/maven-metadata.json` | 200 (122 348 B) | Se conservan las 8 últimas claves de Minecraft; de cada una, 3 primeros + 3 últimos builds. |

**Esquema:**
- `promotions_slim.json` → `{"homepage":…,"promos":{"<mc>-latest":"<build>","<mc>-recommended":"<build>", …}}` (117 promos, 40 `recommended`). Es el contrato de **promociones** (E-09).
- `maven-metadata.json` → `{"<mc>":["<mc>-<build>", …], …}`. **NO** tiene `versioning.versions`: es exactamente el caso que deja vacío el parser compartido `mavenRoot` (API-06).

## neoforge/ — NeoForge (Maven XML oficial)

| Archivo | URL | HTTP | Recorte aplicado |
|---|---|---|---|
| `maven-metadata.xml` | `https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml` | 200 (67 439 B) | 40 primeras + 10 últimas `<version>` (de 1816); se conservan `groupId`, `artifactId`, `latest` (26.3.0.77) y `release`. |

**Esquema:** XML `<?xml?><metadata><groupId>net.neoforged</groupId><artifactId>neoforge</artifactId><versioning><latest>26.3.0.77</latest><release>26.3.0.77</release><versions><version>…</version>…</versions></versioning></metadata>`.
Coexisten el **esquema antiguo** (`21.x`) y el **moderno** (`26.x`) en el mismo documento (1003 y 331 entradas respectivamente en el original). Confirma E-10.

> El endpoint `maven-metadata.json` que usa hoy el código (`https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.json`) devuelve **404**; no hay fixture porque no existe documento.

## Reproducir la descarga

```powershell
$ua = "badomens-audit/0.1 (contract-audit)"
curl.exe -sS -L -A $ua "https://fill.papermc.io/v3/projects/paper/versions" -o paper_fill_v3/versions.json
curl.exe -sS -L -A $ua "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json" -o mojang/version_manifest_v2.json
curl.exe -sS -L -A $ua "https://meta.fabricmc.net/v2/versions/game" -o fabric/versions_game.json
curl.exe -sS -L -A $ua "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json" -o forge/promotions_slim.json
curl.exe -sS -L -A $ua "https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml" -o neoforge/maven-metadata.xml
# y los que devuelven error, tal cual:
curl.exe -sS -L -A $ua "https://api.papermc.io/v2/projects/paper/versions" -o paper_v2/versions.json
```

Estos fixtures **no** sustituyen a las pruebas de contrato contra upstream (job programado,
`spec 1.md` §7.1); son su base offline y determinista.
