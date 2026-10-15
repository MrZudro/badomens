# Seguridad del runtime (Fase 12 · 12.4 · spec 6.3)

Documento de la parte de **seguridad del runtime** de la Fase 12: qué controles
existían, cuáles se han **añadido o corregido** en esta fase, cómo se verifican y
**qué no se puede garantizar**.

Ámbito revisado por este agente: `internal/execx`, `internal/dockerx`,
`internal/runtime`, `internal/config`, `internal/backup`, `internal/instance`,
`internal/worlds` (validación de nombres), `internal/plugins` (límites, ya de la
Fase 11.1), `internal/update` (verificación, revisada) y el lado de validación del
CLI y del MCP. `internal/tui/**` y `internal/cli/plugin*.go` los mantiene otro
subagente: **no se han auditado aquí**.

## 1. Resumen de controles

| Control (6.3) | Estado | Dónde |
| --- | --- | --- |
| Ejecución de Docker/Compose/procesos **sin shell** | Ya existía (verificado) | `internal/execx` (`exec.CommandContext`, argumentos separados); `runtime.compose` compone `docker compose -p … -f …`; `dockerx` |
| Validación de **nombres de instancia** (CLI y MCP) | **Añadido** | `instance.ValidateSlug` + `Manager.Load/Save/Remove` |
| Validación de **rutas y argumentos** de CLI/MCP | **Añadido/reforzado** | `backup.ValidateName`, `worlds.ValidateName`, `repos.Manifest.Validate`, `--base` de `doctor api` |
| Permisos de configuración, caché, backups y metadatos | **Reforzado** | Carpeta de backups 0700 (antes 0755); zip de backup 0600; el resto ya estaba en 0600/0700 |
| Restauración de backups con **checksum validado ANTES** de modificar datos | **Añadido (era un defecto real)** | `backup.Restore` (antes borraba `server/` y descomprimía sin verificar) |
| **Confirmación** de operaciones destructivas | Ya existía | `Env.confirm` (sin TTY exige `--yes`/`--replace`): `backup restore`, `instance remove`, `world use`, `uninstall --purge` |
| Límites de **tamaño de backups** | **Añadido** | `backup.MaxSourceBytes` (8 GiB) y `MaxArchiveBytes` |
| Límites de **memoria/CPU/paralelismo** | Ya existía | `spec.Memory`/`spec.CPU`, `max_parallel`, `host.ParseMemory` |
| Límites de **descargas** y anti-SSRF | Ya existía (Fase 11.1) | `internal/plugins/download`, `internal/plugins/archive` (ver `docs/plugins-seguridad.md`) |
| Secretos fuera de logs/JSON/archivos versionados | Ya existía (verificado y extendido) | `sanitizeURL`/`sanitizeCause` en el transporte; el diagnóstico nuevo publica solo la URL saneada; `.env.example` sin secretos |
| Verificación **criptográfica** de actualizaciones | Ya existía (verificado) | `update.Apply`: `checksums.txt` + sha256 del asset; se niega a actualizar sin verificar |
| Red restringida a la IP/puerto del tailnet | **Reforzado** | `runtime.HostIP` + `validatePublishIP` (nunca `0.0.0.0` ni IP vacía) y `runtime.ComposeTemplate`: `"{{.HostIP}}:{{.Port}}:25565"` |
| Mínimos privilegios del contenedor | Ya existía (verificado en la plantilla) | Solo se monta `./server:/data`; sin `privileged`, sin docker.sock; consola/RCON por `docker exec --user 1000` |
| Validación de nombres de **mundo** | **Añadido** | `worlds.ValidateName` + `SaveWorld`/`UseWorld`/`ImportFromZip` |
| Propagación de errores tipados por **MCP** | **Reforzado** | `mcp.server` publica `_meta.code/hint/details` (incluye el diagnóstico) |

## 2. Validación de nombres y rutas

Un **slug** y un **nombre** son identificadores, no rutas. `instance.ValidateSlug`
exige el patrón histórico `^[a-z0-9][a-z0-9-]{1,23}$` y **no** normaliza: rechaza
`""`, espacios, `.`, `..`, `/`, `\`, mayúsculas y caracteres de control.

`Manager.Dir/SpecPath` siguen componiendo la ruta con `filepath.Join`, pero
`Load`, `Save` y `Remove` **validan antes**: con `-i ../../etc/passwd` no se lee ni
se escribe nada fuera de `<data>/instances/<slug>/`. Lo mismo hacen
`backup.Create/List/Restore/Verify/Prune` con el slug y con el nombre de la copia
(sin separadores, sin `..`, sin punto inicial ni final, sin nombres reservados de
Windows), y `worlds.ValidateName` con los nombres de mundo.

`repro import` valida el manifiesto completo antes de escribir: motor conocido,
versión exacta (rechaza `latest`) y **todos** los pines obligatorios del motor
(`engines.RequiredPins`) presentes y distintos de `latest`.

## 3. Copias de seguridad

Flujo endurecido de `Create`:

1. valida `slug` y `nombre`;
2. mide el árbol `server/` y **rechaza** si supera `MaxSourceBytes`
   (`error_capacity_exceeded`, `details.reason=limite_excedido`);
3. escribe en `<nombre>.zip.part` (0600) y solo entonces lo mueve con `rename`;
4. calcula el sha256 del ZIP final y lo guarda en `<nombre>.zip.sha256` (0600).

`Restore` (el defecto corregido):

1. valida `slug` y `nombre`;
2. **lee el `.sha256` y verifica el archivo ANTES de tocar nada**; una copia
   alterada se rechaza con `details.reason=checksum_no_coincide` y los datos
   actuales quedan intactos (probado);
3. extrae el ZIP a `<instancia>/.restore-tmp` (mismo sistema de archivos);
4. sustituye `server/` por `rename` con **reversión**: si el segundo `rename`
   falla, se devuelve el directorio anterior a su sitio;
5. limpia `.restore-old` y `.restore-tmp`.

`Verify(slug, nombre)` expone la misma comprobación sin tocar los datos.

## 3 bis. Publicación de puertos (nunca en todas las interfaces)

La plantilla de Compose publica `"{{.HostIP}}:{{.Port}}:25565"`. Si `HostIP`
quedara vacío, Docker interpretaría `"":25565:25565` como **0.0.0.0** (todas las
interfaces). `Runtime.HostIP` y `Runtime.Render` lo impiden:

- con Tailscale desactivado se publica en `127.0.0.1` (solo local);
- con Tailscale activado se usa `tailscale.host_ip` si el operador la fijó
  (validada) y, si no, la IPv4 del tailnet;
- una IP vacía, no-IPv4 o comodín (`0.0.0.0`, `::`) **aborta el arranque** con
  error tipado y `details.reason` = `host_ip_desconocida`, `host_ip_invalida` o
  `host_ip_comodin`, en lugar de exponer el servidor en la red local o en
  Internet.

Nota: `tailscale.host_ip` existía en la configuración pero **no se usaba**; ahora
es una sobrescritura explícita y validada.

## 4. Permisos

| Ruta | Modo | Nota |
| --- | --- | --- |
| Directorio de configuración | 0700 | ya existía |
| `config.yaml` | 0600 | ya existía |
| `data/`, `instances/<slug>/` | 0700 | ya existía |
| `instance.yaml` | 0600 | ya existía |
| `versions/` (caché) y `<motor>.json` | 0700 / 0600, escritura atómica | ya existía |
| `instances/<slug>/backups/` | **0700** | antes 0755 |
| `backups/<nombre>.zip` | **0600** | antes el que diera el `umask` |
| `backups/<nombre>.zip.sha256` | 0600 | ya existía |
| Manifiesto reproducible (`repro export`) | 0600, escritura atómica | nuevo |
| `server/`, `plugins/`, `mods/` | 0755 | el contenedor (root) debe poder leer y escribir ahí |

Las verificaciones de permisos de las pruebas se **omiten en Windows**, donde
`os.FileMode` no representa las ACL reales.

## 5. Secretos

- El transporte sanea credenciales de la URL (`user:pass@`) y el valor de
  parámetros sensibles (`token`, `apikey`, `password`…) antes de guardarlos en el
  error, en el registro de diagnóstico o en el objeto de diagnóstico nuevo.
- El diagnóstico **nunca** incluye cuerpo de respuesta: se construye desde
  `TransportError` (código, estado, intentos, URL saneada).
- `badomens doctor api` publica la URL saneada aunque el operador pase un `--base`
  con credenciales (probado: el documento no contiene las credenciales ni el
  token y sí el marcador `***`).
- `.env.example` no contiene secretos versionados.

## 6. Pruebas

| Prueba | Qué cubre |
| --- | --- |
| `internal/backup/hardening_test.go` | Copia alterada rechazada **sin modificar datos**; copia sin checksum rechazada; ZIP corrupto rechazado antes de sustituir; restauración correcta y sin temporales; nombres inválidos (`../`, separadores, `..`, reservados, vacío); slug inválido no escapa del data dir; límite de tamaño (`error_capacity_exceeded`); permisos 0700/0600; `Create` no sobrescribe ni deja `.part`; `Verify` |
| `internal/instance/security_test.go` | `ValidateSlug` (trayectorias, espacios, mayúsculas, caracteres de control) en `Load`/`Save`/`Remove` sin escribir fuera del data dir |
| `internal/worlds/validate_test.go` | Nombres de mundo con ruta rechazados; `SaveWorld`/`UseWorld` no escriben fuera de la biblioteca |
| `internal/runtime/hostip_test.go` | La publicación nunca usa `0.0.0.0` ni una IP vacía/inválida; `127.0.0.1` cuando Tailscale está desactivado; `tailscale.host_ip` validada; motivo estable `host_ip_comodin` |
| `internal/cli/observabilidad_test.go` | Ausencia de secretos en el documento de `doctor api` |
| `internal/plugins/**` (Fase 11.1) | Límites, https, anti-SSRF, checksum, atomicidad |

```bash
go test ./internal/backup/ ./internal/instance/ ./internal/worlds/ ./internal/runtime/ -count=1 -v
# En Windows, añade -ldflags=-w -s o -ldflags=-w -buildid= si Smart App Control
# bloquea el binario de prueba recién compilado.
```

## 7. `doctor api --base`: política explícita

`--base motor=URL` es una **decisión explícita del operador** (usar un espejo o
probar): apunta una sonda GET a una URL arbitraria. A diferencia del descargador de
plugins (que rechaza loopback, rangos privados y link-local y valida cada
redirección), aquí **no se aplica** ese anti-SSRF, porque la URL la escribe quien
ejecuta el comando, con sus propios privilegios y en su propia máquina. Sí se
mantiene todo lo demás: solo GET, límite de tamaño, validación de estado/formato/
esquema y diagnóstico sin cuerpos. Si la URL lleva credenciales, la publicada va
saneada.

## 8. Lo que NO se puede garantizar

- **Windows**: `chmod` no aplica ACL reales; la sustitución por `rename` de
  directorios puede fallar si algo tiene abierto el directorio; y `os.Rename` no es
  atómico entre volúmenes distintos (aquí siempre es el mismo).
- **Espacio libre en disco**: se acota el tamaño de **origen** de una copia, pero
  no se consulta el espacio libre del sistema de archivos. Una copia puede fallar
  a mitad (se limpia el `.part`) si el disco se llena.
- **Integridad de un backup frente a un atacante con acceso al `.sha256`**: el
  checksum no está firmado; quien puede reescribir el ZIP puede reescribir también
  el `.sha256`. Detecta corrupción y manipulación accidental, no un ataque local.
- **Contenido de un mundo importado**: `worlds.Unzip` rechaza rutas inseguras
  (zip-slip), pero el contenido se copia tal cual; no se valida que sea un mundo
  de Minecraft.
- **Plugins**: la verificación es de *forma* (JAR/ZIP válido, tamaño, checksum
  publicado), no de contenido. Ver `docs/plugins-seguridad.md`.
- **Restricción de red del contenedor**: se garantiza que la publicación usa una
  IPv4 concreta (nunca el comodín); no se verifica el estado real de las reglas de
  Docker, la configuración del firewall del host ni que Tailscale tenga ACL
  restrictivas. 	ailscale.host_ip es una decisión del operador: si apunta a una
  IP de LAN, el puerto queda publicado en esa IP.
- **Revalidación de datos ya escritos**: el endurecimiento se aplica al escribir y
  al restaurar; un archivo creado antes no se revalida.
- **Permisos POSIX**: en este entorno (Windows) no se pueden comprobar los bits
  reales; las pruebas de permisos se omiten ahí y deben ejecutarse en Linux/macOS
  para verificarse.
- **`-race`**: no está disponible (requiere cgo).

## 9. Puntos a revisar por otros agentes (no tocados aquí)

- **Ubicación de plugins/mods**: `plugins.Installer.Dir` escribe en
  `<instancia>/plugins` o `<instancia>/mods`, mientras la plantilla de Compose solo
  monta `./server:/data`. Si el servidor espera los plugins en `/data/plugins`,
  esos archivos no llegarían al contenedor. **No se ha cambiado** (no es parte de
  12.4 y toca la lógica de plugins).
- **`internal/tui/**`**: no auditado por este agente; cualquier ruta que la TUI
  construya con contenido de la respuesta de un catálogo debe pasar por
  `instance`/`backup`/`worlds` (que ahora validan) y no componer rutas por su
  cuenta.
- **`internal/cli/plugin*.go`**: política de marketplaces fuera del ámbito de esta
  fase.