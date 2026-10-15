# Fase 8 — Daemon, Docker, Tailscale y MagicDNS (lado CLI/infra)

Este documento recoge el mecanismo común de infraestructura, los comandos
expuestos y la causa raíz del aviso persistente de MagicDNS. Corresponde a la
Fase 8 del plan de implementación, **sin** el cableado dentro de la TUI.

## 1. Mecanismo común: detectar → levantar → elevar → informar

Vive en `internal/infra` y es una máquina de estados pura sobre un entorno
inyectable (`infra.Env`: `Run`, `LookPath`, `Sleep`, `Elevated`, `Spawn`). Todos
los procesos se lanzan con **argumentos separados** (`internal/execx`); nunca se
concatena una cadena de shell.

1. **Detectar** si el binario/servicio está instalado y activo. Si falta, se
   devuelve la **instrucción de instalación**: nunca se instala en silencio.
2. **Levantar** con el plan por SO. Un paso que requiere privilegios
   (`RequiresElevation`) solo se ejecuta si el llamante autorizó la elevación
   (`Options.AllowElevation`) o el proceso ya es administrador; el motivo se
   explica. Si no, el paso queda **pendiente** y se degrada sin bloquear.
3. **Reintentar la verificación** con timeout y estado «Cargando» (🟡) mediante
   sondeo; la operación es cancelable con `context.Context`
   (`infra.Ensure` / `infra.EnsureAsync` + `Options.Progress`).
4. **Informar**: causa, motivo estable (`Reason`) y **pasos manuales por SO**
   (`ManualGuide`). Ningún fallo bloquea el resto de la app.

Estados tipados: `ready`, `starting`, `down`, `not_installed`, `needs_elevation`,
`needs_login`, `degraded`, `unavailable`.

## 2. Componentes

- **Docker** (`infra.Docker`): reutiliza `dockerx.Diagnose` y `dockerx.Plan`;
  no hay un segundo mecanismo. `docker ensure` sigue usando `dockerx.Ensure`.
- **Tailscale** (`infra.Tailscale`): reutiliza `tailscale.Probe`, que devuelve el
  estado tipado con motivo (incluye el de MagicDNS).
- **Daemon propio** (`infra.Daemon`): detecta el latido del vigilante
  (`idle.WatcherActive` sobre `data dir/idle.lock`); si hay servicio persistente
  lo arranca, y si no, levanta un **daemon de sesión** desacoplado
  (`Env.Spawn`, sin shell).

## 3. Servicio persistente del daemon

`internal/infra/service.go` generaliza lo que antes vivía en
`cli/idle_service.go` (que ahora delega aquí, sin mecanismos paralelos):

| SO | Usuario (por defecto) | Sistema (`ScopeSystem`) |
|---|---|---|
| Linux | unidad `systemd --user` `badomens-idle.service` | `/etc/systemd/system/...` (elevación) |
| macOS | `~/Library/LaunchAgents/com.badomens-idle.plist` | `/Library/LaunchDaemons/...` (elevación) |
| Windows | tarea programada `ONLOGON` (`schtasks`) | — |

El contenido de la unidad/plist y los argv de `schtasks` se generan y prueban sin
ejecutar nada. En Windows, `/TR` es una **línea de comandos** (formato que exige
`schtasks`, no una shell): cada elemento se cita con las reglas de
`CommandLineToArgvW`.

## 4. Comandos (aditivos)

- `badomens daemon status|ensure|install-service|uninstall-service`.
- `badomens tailscale status` incluye `status` (tipado), `magic_dns_enabled` y
  `magic_dns_reason`; `tailscale ensure` ejecuta el mecanismo común y admite
  `--allow-elevation` y `--login`.
- `badomens docker status|ensure` añade `infra_status` (mismo estado tipado).
- `badomens doctor` añade los chequeos **MagicDNS** y **Daemon**.
- MCP: nueva herramienta de solo lectura `badomens_infra_status` (Docker,
  Tailscale y daemon) y `badomens_tailscale_status` enriquecida.

Ninguna clave existente del contrato JSON cambia: todo lo nuevo es aditivo.

## 5. MagicDNS — causa raíz

**Síntoma:** el aviso «MagicDNS desactivado…» permanece en pantalla
indefinidamente.

**Causa raíz (código):** el aviso no se calcula con el estado en vivo de
Tailscale. Las pantallas de inicio/detalle construyen la información de conexión
con `connectionFromSpecWithHost` (`internal/tui/screen_home.go`), que alimenta
`connection.FromData` con `spec.Connection.MagicDNS`: el valor **persistido en el
registro de la instancia**, escrito solo al crear o arrancar
(`rememberConnection`). `connection.Derive` emite
`"MagicDNS desactivado: falta el nombre de conexión; usa la dirección IP"`
siempre que ese campo esté vacío, y la TUI lo pinta como **banner fijo**
(`components.ConnectLines`), no como notificación con TTL. Por tanto la
condición **nunca se reevalúa**: aunque MagicDNS se active después, el valor
guardado sigue vacío y el banner no desaparece. No es una notificación, es
contenido derivado de un dato obsoleto.

A esto se suma que `tailscale.MagicDNS()` devolvía `""` **sin motivo** en cuatro
situaciones distintas (no instalado / servicio parado / sin autenticar / MagicDNS
desactivado), así que la UI no podía distinguir la causa ni reevaluar.

**Corrección del lado CLI/infra:**
- `tailscale.Probe` devuelve un estado tipado con motivo estable y, en
  particular, `magic_dns_enabled` y `magic_dns_reason`
  (`magicdns_ok`, `magicdns_disabled`, `magicdns_no_self`, `magicdns_no_name`),
  leyendo el campo correcto (`Self.DNSName`, con el sufijo `.invalid.` como
  señal de desactivado) y distinguiendo «parado»/«sin autenticar» mediante
  `BackendState`.
- `infra.Tailscale.Detect` expone ese estado (con `ip`, `magic_dns`,
  `magic_dns_enabled`, `magic_dns_reason`) para que la TUI pueda **reevaluar** la
  condición en cada refresco y dejar de mostrar el aviso cuando se resuelva.
- `doctor` registra MagicDNS como chequeo (lugar natural de Salud) con su motivo
  tipado, en vez de un aviso permanente sin causa.

**Pendiente de cablear en la TUI (Fase 8, lado UI):** la TUI aún no consume
`infra.Tailscale`/`tailscale.Probe`: debe (a) alimentar la conexión con el nombre
de MagicDNS **en vivo** (y no solo con el valor persistido), (b) mover el aviso
persistente a Salud/indicador fijo con opción de descartar, y (c) contemplar los
`options` de elevación del daemon y de Docker/Tailscale ofreciéndolos con
confirmación.

## 6. Defecto corregido de paso: `tailscale.InRange`

`InRange` comparaba el segundo octeto del rango CGNAT **como texto**
(`parts[1] >= "64" && parts[1] <= "127"`), lo que es falso para los valores de
dos dígitos: `"64" <= "127"` no se cumple lexicográficamente. En consecuencia se
descartaban direcciones válidas `100.64.x.x`–`100.99.x.x` (p. ej.
`100.66.32.62`, el caso real observado en la máquina de pruebas). Ahora se
compara numéricamente y se validan los cuatro octetos.

## 7. Pruebas

- `internal/infra`: máquina de estados (listo, no instalado, arranque y espera,
  elevación denegada y autorizada, timeout con causa y pasos manuales, paso
  desacoplado, cancelación, aviso «Cargando») con `Env` falso; construcción de
  comandos (argumentos separados, sin metacaracteres de shell); contenido de
  systemd/launchd/tarea programada y citado de argumentos.
- `internal/tailscale`: `ProbeWith` (todos los estados), motivo de MagicDNS,
  límites de `InRange`.
- Verificación: `go build ./...`, `go vet` de los paquetes tocados y
  `go test -count=1` de `cli`, `dockerx`, `tailscale` e `infra`.