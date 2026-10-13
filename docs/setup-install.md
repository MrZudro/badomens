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