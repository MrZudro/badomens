# Distribución e instalación

badomens se distribuye precompilado en cada `vX.Y.Z` (GoReleaser): binarios
para Windows/macOS/Linux en amd64 y arm64, con `checksums.txt`.

## Instalación de un comando

```bash
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/MrZudro/badomens/main/scripts/install.sh | sh

# Windows (PowerShell)
irm https://raw.githubusercontent.com/MrZudro/badomens/main/scripts/install.ps1 | iex
```

> Se usa la ruta `raw.githubusercontent.com` porque el repositorio publica
> **pre-releases** y la URL corta
> `…/releases/latest/download/install.{sh,ps1}` solo estará disponible cuando
> exista una release **estable** (los instaladores se publican también como
> assets de cada release).

Los scripts (`scripts/install.sh`, `scripts/install.ps1`):

1. Detectan el sistema y la arquitectura.
2. Resuelven la última release (o usan `BADOMENS_VERSION`).
3. Descargan el paquete y su `checksums.txt`.
4. **Verifican el sha256** antes de instalar (abortan si no coincide).
5. Instalan en `/usr/local/bin` (o `%LOCALAPPDATA%\Programs\badomens`) y
   muestran `badomens version`. En Windows añaden el directorio al **PATH de
   usuario** automáticamente (reabre la terminal); `scripts/uninstall.ps1` lo
   quita al desinstalar.

Variables opcionales: `BADOMENS_REPO`, `BADOMENS_DEST`, `BADOMENS_VERSION`.

`install.ps1` acepta además `-FromSource`, que **compila** el binario con Go desde
el código de la versión (en vez de descargar un `.exe`).

## Windows: Smart App Control y binarios sin firma

Windows 11 trae **Smart App Control** (SAC), que activado **solo permite
ejecutables firmados digitalmente o con reputación conocida** y **no** ofrece un
botón de «ejecutar de todas formas». Los binarios de badomens **aún no están
firmados**, así que con SAC activo Windows los bloquea con:

```
Una directiva de Control de aplicaciones bloqueó este archivo
```

Medido en una máquina con SAC **activo** (Windows 11, Go 1.27) — el resultado
**no** depende de si el binario se descargó o se compiló:

| Caso | Resultado |
|---|---|
| `.exe` **descargado** del release | **Bloqueado** (4 de 4 intentos) |
| **Compilado** localmente (`go build`, `go install`, `go run`) | **Mixto**: 21 de 24 ejecutaron; el binario exacto de `install.ps1 -FromSource` se bloqueó 3 de 3 |
| El mismo fichero bloqueado, **copiado** a otra ruta | Sigue **bloqueado** |
| El mismo fichero bloqueado **con un byte añadido** | **Ejecuta** (y sigue sin firma) |
| Un binario permitido **con marca de descargado** (MOTW) | Sigue **ejecutando** |

Es decir: el veredicto de SAC va ligado a la **reputación del fichero exacto**
(hash), no a la procedencia ni a la firma (un binario sin firma puede pasar), así
que **no es determinista y no se puede forzar reintentando**: un build
bit-reproducible recibe siempre el mismo veredicto.

Para saber si SAC está activo:

```powershell
Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\CI\Policy' VerifiedAndReputablePolicyState
# 0 = desactivado · 1 = activado (aplica) · 2 = evaluación (aprendiendo)
```

Salidas, de la menos a la más invasiva:

1. **Probar a compilar con Go** (gratis y sin tocar el sistema; algunos binarios
   compilados sí arrancan):

   ```powershell
   go install github.com/MrZudro/badomens/cmd/badomens@latest
   # deja el binario en %USERPROFILE%\go\bin\badomens.exe
   ```

   o, desde un clon, `.\scripts\install.ps1 -FromSource`. Si SAC también bloquea
   ese fichero, pasa a la opción 2.

2. **WSL2 o Docker** (determinista). SAC solo controla ejecutables de Windows,
   así que el binario de Linux corre sin problema:

   ```bash
   wsl --install -d Ubuntu
   # dentro de WSL, con Docker Desktop y su integración con WSL activada:
   curl -fsSL https://raw.githubusercontent.com/MrZudro/badomens/main/scripts/install.sh | sh
   ```

   Es además el entorno natural de badomens, que orquesta servidores en Docker
   Compose.

3. **Desactivar Smart App Control** en *Seguridad de Windows → Control de
   aplicaciones y navegador → Smart App Control*. Es **irreversible**: volver a
   activarlo exige restablecer o reinstalar Windows.

4. **Firmar los binarios** (tarea pendiente del proyecto): es la única forma de
   que el `.exe` publicado funcione con SAC activo. Requiere un certificado de
   firma de código.
`install.ps1` detecta SAC antes de descargar y, si el binario no arranca, explica
estas opciones en lugar de dejar el error críptico de PowerShell.

## go install

```bash
go install github.com/MrZudro/badomens/cmd/badomens@latest
```

## Verificar la instalación

```bash
badomens version
badomens doctor      # Docker, Tailscale y configuración
badomens update      # confirma que estás en la última versión
```

## Actualizar

`badomens update` solo comprueba. Para **aplicar** la actualización (descargar,
verificar el sha256 y reemplazar el binario):

```bash
badomens update                # comprueba
badomens update --apply        # descarga, verifica y reemplaza (pide confirmación)
```

Al abrir la **TUI**, badomens comprueba si hay una versión nueva (una vez cada
12 h) y pregunta si quieres actualizar. Se desactiva con `update_check: false`
en `config.yaml`, `BADOMENS_UPDATE_CHECK=0` o `--no-preflight`.

> En Windows el binario en uso no se puede pisotear: se renombra a `.old` y el
> nuevo queda instalado; cierra y vuelve a abrir `badomens` para usarlo (y borra
> el `.old`).

## Desinstalar

```bash
badomens uninstall --dry-run      # qué haría (no borra nada)
badomens uninstall                # binario + configuración, conserva los datos
badomens uninstall --purge        # borra también TODOS los datos
```

Sin `--purge` se **conservan** las instancias, mundos y backups (viven en
`<config>/data`). También hay scripts: `scripts/uninstall.sh` (macOS/Linux) y
`scripts/uninstall.ps1` (Windows), que respetan las mismas variables
(`BADOMENS_DEST`…).