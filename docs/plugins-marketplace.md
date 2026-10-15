# Marketplaces de plugins/mods (11.2 · AC-13) y actualizaciones (11.3 · AC-14)

Documento de las fases **11.2 "Marketplaces"** y **11.3 "Actualizaciones y
dependencias"**. Describe los adaptadores implementados, **los esquemas reales**
que usan, los límites aplicados, el contrato interno del resolvedor y **lo que no
se puede garantizar**.

Ámbito: `internal/plugins/marketplace/**` y el registro de procedencia de
`internal/plugins`. La descarga segura (streaming, límite, https, anti-SSRF,
temporal + rename atómico, inspección JAR/ZIP) es de la fase 11.1 y **no se
reimplementa aquí**: los marketplaces la reutilizan tal cual (ver
`docs/plugins-seguridad.md`).

## 1. Separación de responsabilidades

| Módulo | Responsabilidad | Red |
|--------|-----------------|-----|
| `marketplace` | Tipos de dominio (proyecto, versión, artefacto, dependencia), **regla de compatibilidad**, catálogo de fuentes y traducción de errores | no |
| `marketplace/modrinth` | Adaptador de la API v2 de Modrinth, con **su propio parser** | sí |
| `marketplace/hangar` | Adaptador de la API v1 de Hangar, con **su propio parser** | sí |
| `marketplace/resolve` | Resolvedor: proyecto + entorno ⇒ versión y archivo exactos, o error | no |
| `marketplace/install` | Descargador/orquestador: resolver → temporal → verificar → mover → registrar; y comprobación de actualizaciones | sí (solo descarga) |
| `plugins` (11.1) | Carpeta destino según motor, `Install`/`Add`, registro de procedencia | no |
| `plugins/download` (11.1) | Obtención segura del artefacto | sí |
| `plugins/archive` (11.1) | Validación estructural del JAR/ZIP | no |

**Un parser por proveedor (T-06)**: `modrinth` y `hangar` no comparten ningún
tipo de decodificación. Lo único común es el contrato `marketplace.Source` y los
tipos de dominio a los que cada adaptador traduce su esquema.

El transporte HTTP es el **común de catálogos** (`internal/engines.Client`) con
`RequestOpts{Provider, Operation}`: reintentos con backoff, respeto de
`Retry-After`, límite de tamaño aplicado durante la lectura, validación de
formato y errores tipados con códigos estables. Los marketplaces **no crean
clientes HTTP propios**.

### Contrato `marketplace.Source`

```go
type Source interface {
    ID() SourceID
    Label() string
    Search(ctx context.Context, q SearchQuery) (SearchResult, error)
    Project(ctx context.Context, ref string) (*Project, error)
    Versions(ctx context.Context, projectID string, f VersionFilter) ([]Version, error)
}
```

El llamador (CLI, TUI, MCP) construye **una vez** el catálogo:

```go
cat := marketplace.NewCatalog()
cat.Register(modrinth.New(client)) // client: *engines.Client con User-Agent de contacto
cat.Register(hangar.New(client))
```

## 2. Regla de compatibilidad: `compatible` ≠ `desconocido`

`marketplace.Evaluate(loaders, gameVersions, env)` es deliberadamente
conservador:

| Situación | Veredicto |
|-----------|-----------|
| El proveedor declara el loader **y** la versión de Minecraft, y ambos coinciden | `compatible` |
| El proveedor declara datos y **no** coinciden | `incompatible` |
| No hay entorno (motor/loader o versión de Minecraft) | `desconocido` |
| El proveedor no declara loaders (o no declara versiones) | `desconocido` |

Consecuencias que conviene tener presentes:

- **Un resultado de búsqueda no demuestra nada.** La búsqueda de Modrinth no
  publica loaders: solo `categories` (donde mezcla loaders con etiquetas) y
  `versions`. Por eso `modrinth.Search` deja `Loaders` vacío y **nunca** copia
  `categories` a loaders; el veredicto queda en `desconocido` aunque la búsqueda
  se haya filtrado por loader en el servidor.
- **El filtro del servidor no es una prueba.** `SearchQuery.FilterEnv` pide al
  proveedor que restrinja resultados (Modrinth: `facets`; Hangar: `platform`),
  pero la compatibilidad se calcula **siempre** con los metadatos declarados.
- **La comparación de loader es exacta** (sin distinguir mayúsculas, porque
  Hangar usa `PAPER`). No hay equivalencias implícitas: un artefacto que declara
  `spigot` no se marca compatible con `paper`, porque esa equivalencia no la
  publica el proveedor.
- El **tipo de proyecto** se valida aparte (`Kind.Accepts`): un `plugin` en un
  servidor `paper` y un `mod` en `fabric`/`forge`/`neoforge`. Un modpack o un
  resourcepack no se instalan como plugin ni como mod.
- El **estado de publicación** también cuenta: un proyecto o una versión no
  publicados no se resuelven.

## 3. Esquemas reales usados

Fixtures capturados de los endpoints públicos (no se inventan campos); las
pruebas usan `httptest` y **no tocan la red**.

### Modrinth (`https://api.modrinth.com/v2`)

| Operación | Endpoint | Campos usados |
|-----------|----------|---------------|
| Buscar | `GET /search?query=&facets=&limit=&offset=` | `hits[].project_id`, `project_type`, `slug`, `title`, `description`, `author`, `categories`, `versions`, `downloads`, `icon_url`, `license`; `total_hits`, `offset`, `limit` |
| Proyecto | `GET /project/{id\|slug}` | `id`, `slug`, `project_type`, `title`, `description`, `status`, `loaders`, `game_versions`, `downloads`, `icon_url`, `license.{id,name}` |
| Versiones | `GET /project/{id\|slug}/version?loaders=["paper"]&game_versions=["1.21.1"]` | `id`, `project_id`, `name`, `version_number`, `version_type`, `status`, `loaders`, `game_versions`, `downloads`, `date_published`, `files[].{hashes,url,filename,primary,size}`, `dependencies[].{version_id,project_id,file_name,dependency_type}` |

Observaciones del esquema real:

- `facets` es un array de arrays JSON (OR interno, AND externo). Se envían en el
  orden recomendado: **tipo de proyecto, versiones, categorías**.
- `loaders` y `game_versions` en `/version` exigen **arrays JSON** codificados en
  la query, no listas separadas por comas.
- `/project/{id}/version` **no admite paginación**: devuelve todas las versiones
  que encajan. El tope `MaxVersionResults = 200` es **nuestro** y se documenta
  como tal (las versiones llegan de la más reciente a la más antigua, y el
  resolvedor reordena por `date_published` de todos modos).
- Los `hashes` publicados son **`sha512` y `sha1`**; Modrinth **no publica
  `sha256`**.
- `version_type` ∈ {`release`, `beta`, `alpha`}; `status` ∈ {`listed`,
  `archived`, …}. Solo `listed` cuenta como publicada.
- `dependency_type` ∈ {`required`, `optional`, `incompatible`, `embedded`}.
- `status` del proyecto: `approved` (publicado), `unlisted` (publicado pero
  oculto), `draft`/`archived`/`rejected`/… (no publicado).

### Hangar (`https://hangar.papermc.io/api/v1`)

| Operación | Endpoint | Campos usados |
|-----------|----------|---------------|
| Buscar | `GET /projects?q=&platform=PAPER&limit=&offset=` | `pagination.{count,limit,offset}`, `result[].{id,name,namespace,stats.downloads,category,description,visibility,supportedPlatforms,avatarUrl,settings.license}` |
| Proyecto | `GET /projects/{slug}` | igual, más `supportedPlatforms` |
| Versiones | `GET /projects/{slug}/versions?limit=&offset=` | `result[].{id,projectId,name,visibility,reviewState,createdAt,channel.name,stats.totalDownloads,downloads[plat].{fileInfo.{name,sizeBytes,sha256Hash},externalUrl,downloadUrl},pluginDependencies[plat][].{name,projectId,required},platformDependencies[plat][]}` |

Observaciones del esquema real:

- Hangar **solo publica plugins** (de Paper y plataformas relacionadas:
  `PAPER`, `WATERFALL`, `VELOCITY`, `BUNGEECORD`, `FOLIA`). El adaptador los
  traduce a `KindPlugin`.
- El **slug distingue mayúsculas** (`fancyglow`, `WirelessRedstoneTrigger`).
- La búsqueda filtra por `platform` pero **no por versión de Minecraft**; el
  listado de versiones **no acepta ningún filtro**: la plataforma y la versión de
  Minecraft se filtran **en el cliente**, sobre `platformDependencies` y
  `downloads`. Se recorren hasta `MaxPages = 4` páginas de `PageLimit = 25`.
- `downloads[plataforma]` tiene dos formas:
  - **alojado por Hangar**: `downloadUrl` + `fileInfo{name,sizeBytes,sha256Hash}`;
  - **externo**: `externalUrl` con `fileInfo: null`, sin tamaño ni checksum.
- `visibility` ∈ {`public`, `unlisted`, `draft`, `private`}; `reviewState` ∈
  {`reviewed`, `unreviewed`, `partial`}.
- `pluginDependencies` es un mapa **plataforma → dependencias** con
  `{name, projectId, required}`. `name` es el identificador habitual (no siempre
  hay `projectId`).
- Hangar **no publica loaders ni versiones de Minecraft por versión**: los
  declara por plataforma en `platformDependencies`. Cuando la consulta pide una
  plataforma, `GameVersions` se acota a lo que esa versión declara **para esa
  plataforma** (no a la unión), para no afirmar un soporte que solo tiene en otra.

### CurseForge — no implementado (P1 fuera de alcance)

CurseForge exige una **API key** y sus propios términos de uso, así que queda
fuera de esta fase. **Punto de extensión documentado**: implementar
`marketplace.Source` en un nuevo subpaquete `internal/plugins/marketplace/curseforge`
con su propio parser, registrar el cliente en el catálogo y aportar la clave por
configuración (nunca por flag en la línea de comandos, para no dejarla en el
historial) y sin registrarla jamás en los diagnósticos. El resto del flujo
(resolvedor, instalador, procedencia) no cambia.

### URL directa y archivo local — vía avanzada

No son marketplaces: no publican catálogo ni versiones, así que **no** pasan por
este contrato. Se siguen instalando con `internal/plugins` +
`internal/plugins/download` (`plugin add <url-o-archivo>`), con la misma política
de 11.1: https obligatorio, anti-SSRF, límite durante la transferencia, checksum
opcional e inspección estructural.

## 4. Contrato interno del resolvedor

```go
type Request struct {
    Source  marketplace.Source     // marketplace consultado
    Project string                 // slug o identificador
    Env     marketplace.Environment // Engine/Loader + GameVersion de la instancia
    Version string                 // versión exacta (vacío ⇒ la más reciente compatible)
    Channels []string              // vacío ⇒ solo "release"
    AllowPrerelease bool
    AllowUnverified bool           // aceptar un artefacto sin checksum publicado
    AllowMissingDeps bool          // degradar dependencias ausentes a aviso
    Installed []plugins.Record     // lo ya instalado, para evaluar dependencias
    VersionLimit int
}

type Resolution struct {
    Source, Project, Version, File, Hash, Compat, Publication string/struct
    Complete     bool
    Problems     []Problem            // motivo estable + mensaje + pista
    Warnings     []string
    Dependencies []DependencyStatus   // satisfecha | ausente | incompatible | opcional | incluida | no_verificable
}
```

`Resolve(ctx, req)` devuelve `(*Resolution, error)`. El error **no es nil si y
solo si** `Complete == false`, y en ese caso la `Resolution` se devuelve igual
para poder informar del porqué. Los motivos estables van en
`error.details.reason`:

| Motivo | Cuándo |
|--------|--------|
| `entorno_incompleto` | Faltan el motor/loader o la versión de Minecraft |
| `proyecto_no_publicado` | El proyecto no está publicado |
| `tipo_no_instalable` | El tipo de proyecto no encaja con el motor |
| `sin_version_compatible` | Ninguna versión declara ese loader y esa versión de MC (o el canal no está aceptado) |
| `version_inexistente` | La versión pedida no existe en el proyecto |
| `version_incompatible` | La versión existe pero no declara el entorno, o está archivada |
| `sin_archivo` | La versión no publica ningún artefacto descargable |
| `archivo_no_instalable` | El artefacto no es `.jar` ni `.zip` |
| `checksum_ausente` | No hay checksum `sha256`/`sha512` utilizable |
| `dependencia_no_satisfecha` | Falta una dependencia obligatoria, hay una incompatible instalada o una obligatoria sin identificar |

Códigos de badomens asociados: `error_usage` para lo que el usuario puede
corregir, `error_not_found` para una versión que no existe y `error_unavailable`
para fallos del proveedor (`reason`: `no_encontrado`, `red`,
`proveedor_no_disponible`, `respuesta_invalida`, `respuesta_excesiva`, con
`http_status` cuando lo hay).

### AC-06: resolución incompleta = NO exitosa

Si falta la versión exacta, el loader, la versión de Minecraft **o el checksum**,
la resolución falla con error tipado y explicación. Nunca se descarga "lo que
haya".

**Integridad**: se prefiere `sha256` (lo verifica `download.StageURL` durante la
copia) y, si el proveedor solo publica `sha512` (el caso de Modrinth), se
verifica releyendo el temporal ya acotado por el límite de tamaño. Un `sha1`
publicado **se conserva para informarlo pero no se usa como prueba de
integridad**. Un artefacto sin checksum fuerte solo se instala con
`AllowUnverified` explícito, y entonces queda un aviso en el resultado: no se
finge ninguna verificación.

### Dependencias (11.3)

- Se cruzan con lo instalado (`Register.Installed`, ver §6) por identificador de
  proyecto, slug o nombre de archivo/proyecto. Nunca se inventa una coincidencia.
- Una dependencia **obligatoria ausente** bloquea la resolución con
  `dependencia_no_satisfecha`; con `AllowMissingDeps` queda como aviso.
- Una dependencia declarada **incompatible** que esté instalada bloquea siempre.
- Una dependencia **obligatoria sin identificador** queda `no_verificable` y
  bloquea: no se puede comprobar, así que no se da por buena.
- Las **opcionales** y las **incluidas** se informan y no condicionan.
- **Nunca se instala una dependencia automáticamente.** El resultado lista lo que
  falta y el usuario decide.

## 5. Flujo de instalación

`install.Client.Install`:

1. `resolve.Resolve` (proyecto → versión exacta → archivo exacto).
2. Descarga a un temporal **dentro** de la carpeta destino con la política de
   11.1 (límite durante la transferencia, https, anti-SSRF por salto e IP
   resuelta, redirecciones acotadas).
3. Verifica tamaño, checksum publicado e integridad estructural JAR/ZIP
   (`archive.Validate`).
4. `Commit`: rename atómico al destino (política de conflicto de 11.1).
5. Escribe **una sola** entrada en el registro de procedencia.

Si algo falla, **no queda archivo instalado, ni entrada de registro, ni
temporales**; si falla el guardado del registro, se retira el archivo recién
instalado (mismo criterio que 11.1) para no dejar registro y disco
desincronizados.

## 6. Registro de procedencia y migración aditiva (11.3)

Vive en `<data>/instances/<slug>/plugins.json` — **fuera** de `plugins/` y
`mods/`, para que el servidor nunca se encuentre un archivo que no sea un
artefacto:

```json
{
  "schema_version": 1,
  "records": [
    {
      "name": "ViaVersion-5.5.0.jar",
      "version": "5.5.0",
      "target_version": "",
      "sha256": "…",
      "sha512": "…",
      "size_bytes": 6506259,
      "loader": "paper",
      "game_version": "1.21.1",
      "installed_at": "2026-10-14T18:00:00Z",
      "origin": {
        "source": "modrinth",
        "project_id": "P1OZGk5p",
        "project_slug": "viaversion",
        "project_name": "ViaVersion",
        "version_id": "VvREL5x1",
        "url": "https://cdn.modrinth.com/data/…/ViaVersion-5.5.0.jar",
        "channel": "release",
        "kind": "plugin"
      }
    }
  ]
}
```

- **`Upsert` deja exactamente una entrada por nombre**: reinstalar no duplica.
- La URL de origen se guarda **saneada** (sin credenciales ni parámetros): nunca
  se persisten tokens ni URLs firmadas.
- Los nombres con separadores de ruta, `..` o vacíos se **descartan** al leer y al
  escribir: el registro no puede apuntar fuera de la carpeta de plugins.
- **Migración aditiva**: `SchemaVersion` se marca al guardar y un registro de un
  esquema anterior se lee igual. `Register.Merge(names)` cruza el registro con la
  lista de la instancia (`instance.Spec.Plugins`): un nombre instalado **sin**
  entrada de procedencia —una instalación antigua, anterior a este registro— se
  devuelve con `origin.source = "desconocido"`, **sin inventar** versión,
  checksum ni proyecto.
- `Register.Installed(names)` devuelve la **unión** de las dos fuentes; es la
  vista que se usa para evaluar dependencias y actualizaciones, para que una
  instalación no se pierda por estar registrada en una sola de ellas.
- `Register.Orphans(names)` detecta entradas cuyo archivo ya no está (borrado a
  mano).

## 7. Actualizaciones (11.3): comprobar, nunca instalar

`install.Client.CheckUpdates(ctx, UpdateRequest{Spec, Channels, AllowPrerelease,
PersistTargets, VersionLimit})` devuelve, por cada plugin/mod instalado:

| Estado | Significado |
|--------|-------------|
| `al_dia` | La versión instalada sigue siendo la más reciente compatible |
| `actualizacion_disponible` | Hay una versión nueva compatible; **no se instala** |
| `desconocida` | No se puede saber (sin procedencia, o sin versión registrada) |
| `bloqueada` | La consulta falló (proveedor caído, sin versión compatible, marketplace no registrado) |

Garantías:

- **No descarga ni instala nada** y **no modifica el archivo instalado** ni su
  checksum (las pruebas comparan el hash y la fecha de modificación antes y
  después).
- Con `PersistTargets` se anota la **versión objetivo** (`target_version`) en el
  registro: es un dato, no una instalación. La versión **instalada** no cambia.
- Un fallo puntual se reporta por elemento (`bloqueada` + `reason` + `detail`);
  solo un fallo del propio registro es un error de la llamada.
- La resolución de la comprobación es deliberadamente permisiva
  (`AllowUnverified`, `AllowMissingDeps`): comprobar no debe fallar porque un
  artefacto no publique checksum; eso se informa, no se rechaza.

## 8. Límites y valores por defecto

| Control | Valor | Dónde |
|---------|-------|-------|
| Página de búsqueda en Modrinth | 20 por defecto, máximo 100 | `modrinth.DefaultSearchLimit`, `MaxSearchLimit` |
| Versiones devueltas por Modrinth | tope **propio** de 200 | `modrinth.MaxVersionResults` |
| Página de búsqueda en Hangar | 20 por defecto, máximo 25 | `hangar.DefaultSearchLimit`, `MaxSearchLimit` |
| Versiones por página en Hangar | 25, hasta 4 páginas | `hangar.PageLimit`, `hangar.MaxPages` |
| Timeout del catálogo | 15 s por petición, 3 intentos | `engines.DefaultClient` |
| Límite de lectura de la respuesta | 8 MiB (JSON) | `engines.DefaultMaxJSONBytes` |
| Descarga del artefacto | los de 11.1 (64 MiB, https, anti-SSRF, 5 redirecciones, 2 min) | `download.DefaultPolicy` |
| Inspección del JAR/ZIP | los de 11.1 | `archive.DefaultLimits` |

Hangar exige el slug con su capitalización exacta; Modrinth acepta slug o
identificador. Modrinth exige un `User-Agent` identificable **con vía de
contacto**: se compone con `engines.DefaultUserAgent()`
(`badomens/<versión> (+https://github.com/MrZudro/badomens)`). Los adaptadores
crean `engines.DefaultClient()` si se les pasa `nil`; si se inyecta un cliente
propio, **debe** llevar esa cabecera.

## 9. Lo que NO se puede garantizar

- **Que el artefacto sea seguro.** La compatibilidad se calcula sobre metadatos
  *declarados por el proveedor*; no se inspecciona el bytecode ni el
  comportamiento. Un JAR válido, íntegro y "compatible" puede ser malicioso.
- **Que la compatibilidad declarada sea cierta.** Un proveedor puede declarar
  `paper` y `1.21.1` y el plugin seguir sin arrancar. Nosotros comprobamos lo que
  el proveedor dice, no lo que hace.
- **Que la versión de Minecraft coincida exactamente con la del servidor real.**
  Se compara la cadena de `instance.Spec.Version` con la declarada; no se resuelve
  un rango semántico ni se normaliza `1.21` vs `1.21.0`.
- **Que el proyecto y la versión sigan existiendo mañana.** Un
  `actualizacion_disponible` es una fotografía del momento.
- **Autenticidad del origen.** No hay firma ni cadena de confianza. El checksum
  solo sirve si el propio proveedor lo publica por un canal fiable y el canal no
  está comprometido.
- **Cobertura del catálogo.** Cada marketplace solo conoce **su** catálogo: una
  dependencia publicada en otro marketplace no se encuentra ni se ofrece.
- **Detección completa de dependencias.** Modrinth publica dependencias por
  proyecto/versión; Hangar, por nombre y plataforma. Las dependencias que un
  plugin resuelve *en tiempo de ejecución* (descargas en el primer arranque,
  APIs de otros plugins, librerías propias) son invisibles para el catálogo.
- **Integridad frente a proveedor comprometido**: un `sha512` malicioso
  publicado por el propio proveedor verifica contra un artefacto malicioso.
- **Cobertura de los esquemas.** Si un proveedor cambia su API, la respuesta no
  encaja con el esquema conocido y el adaptador **falla con
  `respuesta_invalida`** en lugar de devolver datos a medias; pero eso implica que
  el marketplace deja de funcionar hasta actualizar el adaptador.
- **`-race`**: el entorno actual no tiene cgo, así que las pruebas no se pueden
  ejecutar con el detector de carreras.

## 10. Endurecimiento pendiente (fuera de 11.2/11.3)

- Pin de certificado o firma del artefacto (no solo checksum contra el mismo
  canal que lo publica).
- Lista blanca de orígenes permitidos más allá del anti-SSRF de 11.1.
- Caché con validadores (`ETag`/`Last-Modified`) para búsquedas y versiones:
  `engines.Meta` ya los expone; falta la capa de caché (fase 12).
- Revalidación periódica de lo ya instalado (11.1 solo comprueba al instalar).
- CurseForge con API key (ver §3).

## 11. Comandos del CLI (11.2/11.3)

El cableado vive en `internal/cli/plugin_market.go` y se cuelga de `plugin`.
Ninguno de estos comandos altera el contrato JSON: las claves nuevas van
**dentro de `data`** y son aditivas.

```bash
badomens plugin sources
badomens plugin search <texto> [-i <instancia>] [--source modrinth|hangar]
                                            [--kind plugin|mod|...] [--limit N] [--offset N]
badomens plugin info <proyecto> [--source <id>]
badomens plugin versions <proyecto> [--source <id>] [--compatible]
badomens plugin install <proyecto>[@<versión>] [-i <instancia>] [--source <id>]
                                            [--channel release|beta|alpha]
                                            [--allow-prerelease] [--allow-unverified]
                                            [--allow-missing-deps]
                                            [--on-conflict fail|rename|replace] [--max-size N]
                                            [--allow-http] [--allow-private] [--allow-loopback]
badomens plugin updates [-i <instancia>] [--persist-targets]
                                            [--channel release|beta|alpha] [--allow-prerelease]
```

`-i`/`--instance` es el flag global: selecciona la instancia de la que se toman
**el motor (loader)** y **la versión de Minecraft**. Ejemplos:

```bash
badomens plugin sources
badomens plugin search viaversion -i demo
badomens plugin search essentials --source hangar --kind plugin --limit 10
badomens plugin info viaversion
badomens plugin versions viaversion -i demo --compatible
badomens plugin install viaversion -i demo                       # la más reciente release compatible
badomens plugin install viaversion@5.5.0 -i demo --allow-prerelease
badomens plugin install viaversion -i demo --allow-unverified --allow-missing-deps
badomens plugin updates -i demo --persist-targets
```

### Claves añadidas en `data`

| Comando | Claves |
|---------|--------|
| `sources` | `count`, `sources[]{id,label,status,live_checked,verified,endpoint,kinds,strongest_checksum}`, `note` |
| `search` | `query`, `instance`?, `environment`?, `kind`?, `results[]{source,id,slug,title,kind,url,downloads,license,publication,compatibility,compatibility_reason,compatibility_verified,loaders?,game_versions?,categories?,total_matches}`, `count`, `total`, `filter_env`, `sources_queried`, `failures`?, `note` |
| `info` | `source`, `project{id,slug,title,kind,url}`, `publication`, `published`, `loaders`, `game_versions`, `categories`, `compatibility`, `compatibility_reason`, `compatibility_verified`, `environment`?, `instance`?, `note` |
| `versions` | `source`, `project{...}`, `versions[]{id,number,name,channel,prerelease,publication,published,compatibility,compatibility_reason,compatibility_verified,loaders?,game_versions?,file_count,file{name,size_bytes,primary,external,verifiable,hash?},dependencies?}`, `count`, `total`, `environment`?, `compatible_only`, `instance`?, `note` |
| `install` (éxito) | `instance`, `source`, `project{id,slug,title,kind}`, `version`, `version_id`, `channel`, `compatibility`, `publication`, `resolution_complete`, `hash{algorithm,value}`?, `file{name,size_bytes,url,external}`, `installed{name,size_bytes,sha256,checksum_verified,archive,renamed_from?,replaced?}`, `dependencies[]{project_id,version_id,name,kind,status,detail}`, `provenance` (la entrada completa del registro), `warnings`?, `security_warning` |
| `install` (fallo) | `instance`, `source`, `project`, `version_requested`, `stage`, `resolution_complete`, `problems[]{reason,message,hint,fatal}`?, `reason`? |
| `updates` | `instance`, `updates[]` (el `install.UpdateCheck`: `name,version,target_version,target_version_id,target_channel,status,compatibility,publication,detail,reason,warnings`), `checked`, `available`, `blocked`, `unknown`, `persisted_targets`, `installed_modified:false`, `note` |

### Decisiones de cableado

- **`plugin sources` no consulta la red**: informa de lo REGISTRADO
  (`status: "registrada"`, `live_checked: false`). Afirmar disponibilidad sin
  preguntar sería inventar.
- **`search`/`info` nunca afirman compatibilidad verificada**:
  `compatibility_verified` es siempre `false` y `compatibility` es la DECLARADA
  por el proveedor (normalmente `desconocido` en una búsqueda, porque la
  búsqueda de Modrinth no publica loaders). `compatibility_reason` explica el
  veredicto y `note` recuerda que un resultado no demuestra nada.
- **Fuente por defecto**: la primera registrada del catálogo (Modrinth). El
  resultado siempre lleva `source`, así que la elección es explícita.
- **`publication` en una búsqueda suele ser `desconocido`**: el esquema de
  búsqueda de Modrinth no publica el estado del proyecto. No se rellena con
  datos de otra consulta.
- **`--source` inexistente** ⇒ `error_usage` con `reason:
  marketplace_no_registrado`.
- **`search` con todas las fuentes caídas** ⇒ error (no una búsqueda vacía); con
  solo algunas ⇒ aviso (`exit_code 1`) y `failures[]`.
- **`--compatible` sin instancia** ⇒ `error_usage` con `reason:
  entorno_incompleto`: sin entorno no hay con qué comparar.
- **`install` (fallo) clasifica la etapa** en `stage`:
  - `resolucion` ⇒ AC-06: `resolution_complete:false`, `problems[]` con los
    motivos estables de `resolve.Problems` (duplicados en
    `error.details.reason`) y **nada escrito** en disco;
  - `consulta` ⇒ no se pudo hablar con el proveedor (tampoco hay resolución);
  - `instalacion` ⇒ la resolución era completa y falló la descarga, el checksum,
    el conflicto de nombre o el registro (`resolution_complete:true`).
- **Un fallo nunca deja nada**: ni archivo, ni entrada de procedencia, ni
  temporales (lo garantiza `install.Client`).
- **Tras la instalación completa** se añade el nombre del archivo a
  `instance.Spec.Plugins` (convención de 11.1). Si el guardado de la instancia
  falla, se retira el archivo recién instalado **y** su entrada de procedencia
  (`rollbackMarketInstall`), para no dejar disco, registro y procedencia
  desincronizados.
- **Avisos = `exit_code 1`** (`result.Warn`), igual que `plugin add` con un
  plugin exigente: `--allow-unverified`, `--allow-missing-deps` y los plugins
  conocidos por su coste. La instalación sigue siendo completa
  (`resolution_complete:true`).
- **Las dependencias nunca se instalan solas**: o bloquean (`error_usage`,
  `dependencia_no_satisfecha`) o quedan como aviso con `--allow-missing-deps`,
  con su estado (`satisfecha|ausente|incompatible|opcional|incluida|no_verificable`)
  en `dependencies`.
- **`plugin updates` no instala ni modifica nada**: `installed_modified:false`,
  `exit_code 1` (aviso) cuando hay actualizaciones o comprobaciones bloqueadas,
  `0` cuando no hay nada que revisar. `--persist-targets` solo escribe
  `target_version` en el registro.
- **`plugin add` (URL directa) no cambia**: sigue con la política de 11.1.

### Paridad CLI ↔ TUI

Los subcomandos nuevos tienen **su propia entrada** en el manifiesto
(`badomens commands`) y en la tabla de paridad, separada de `plugin
add|list|remove`: `plugin sources|search|info|versions|install|updates` →
`instance/plugins` (install/updates) + `solo-cli` (sources/search/info/versions).
Se eligió una entrada nueva, y no ampliar la existente, para no perder la
correspondencia histórica de `add|list|remove` con la pantalla de plugins de la
TUI ni romper el formato `familia.sub1|sub2`.

## 12. Cómo verificar

```bash
gofmt -l internal/cli internal/plugins
go build ./...
go vet ./internal/cli/ ./internal/plugins/...
go test ./internal/plugins/... -count=1
go test ./internal/plugins/marketplace/... -count=1 -v
go test ./internal/cli/ -count=1
```

Cobertura de AC-13/AC-14 (todas con `httptest` y fixtures, sin red real).

En `internal/cli/plugin_market_test.go`, sobre un marketplace de Modrinth
simulado con el MISMO esquema y el mismo filtrado en el servidor:

- `plugin sources`: lista modrinth y hangar sin consultar la red
  (`live_checked:false`).
- `plugin search`: traduce los resultados sin afirmarlos compatibles
  (`compatibility_verified:false`); sin resultados, mensaje claro y
  `exit_code 0`; `--source` inexistente ⇒ `marketplace_no_registrado`.
- `plugin versions`: con `-i` el proveedor filtra por loader y versión de
  Minecraft y `--compatible` descarta el resto; sin instancia se listan todas con
  compatibilidad `desconocido`; `--compatible` sin instancia ⇒
  `entorno_incompleto`.
- `plugin info`: compatibilidad declarada (nunca verificada); proyecto
  inexistente ⇒ `error_not_found`/`no_encontrado`.
- `plugin install` (AC-06): resolución incompleta ⇒ `exit_code 2`,
  `resolution_complete:false`, `problems[0].reason` = `sin_version_compatible` /
  `version_inexistente` / `checksum_ausente` / `dependencia_no_satisfecha` /
  `proyecto_no_publicado`, y **ni archivo ni procedencia ni lista de la
  instancia**.
- `plugin install` correcto: archivo en destino, checksum `sha512` verificado,
  procedencia y lista de la instancia actualizadas **una sola vez**; reinstalar
  ⇒ `destino_existente` sin duplicar nada.
- `plugin install` con dependencia obligatoria: sin `--allow-missing-deps`
  bloquea; con el flag avisa (`exit_code 1`) y **no instala la dependencia**.
- `plugin updates`: detecta la versión nueva sin tocar el archivo (bytes y fecha
  idénticos), `--persist-targets` solo anota `target_version`; un archivo sin
  procedencia queda `desconocida`.

- **Modrinth**: búsqueda traducida sin afirmar compatibilidad; filtro por loader y
  versión de Minecraft como arrays JSON; proyecto inexistente ⇒ `not_found`;
  versión que solo declara `neoforge` ⇒ `incompatible`; respuesta con esquema
  incompleto ⇒ `respuesta_invalida`; 500 ⇒ `proveedor_no_disponible` con
  `http_status`; redirección a 404.
- **Hangar**: traducción de `supportedPlatforms`/`platformDependencies`;
  archivo alojado (`downloadUrl` + `sha256`) frente a archivo externo (sin
  checksum, se informa de que **no hay verificación disponible**); versión que
  solo publica `VELOCITY` y no se ofrece para `paper`; filtro por versión de
  Minecraft en el cliente; dependencia obligatoria por nombre.
- **Compatibilidad desconocida**: una versión sin loaders/versiones no se marca
  compatible ni se selecciona.
- **Registro**: instalación correcta actualiza procedencia+versión+checksum una
  sola vez; fallo de checksum, 404 y 5xx no dejan registro ni archivos ni
  temporales; registros antiguos (solo el nombre) se leen como `desconocido`;
  nombres inseguros se descartan.
- **Actualizaciones**: detecta la versión nueva sin instalar, con y sin
  `PersistTargets`, y **no modifica** el archivo instalado; sin procedencia el
  estado es `desconocida`; proveedor caído ⇒ `bloqueada` con motivo.
- **Política de 11.1 reutilizada**: una URL de artefacto a IP privada se rechaza
  con `destino_no_permitido`, y `http://` sin política con `tls_requerido`, sin
  dejar rastro.