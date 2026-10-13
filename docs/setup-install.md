# Distribución e instalación

badomens se distribuye precompilado en cada `vX.Y.Z` (GoReleaser): binarios
para Windows/macOS/Linux en amd64 y arm64, con `checksums.txt`.

## Instalación de un comando

```bash
# macOS / Linux
curl -fsSL https://github.com/manuelserver/badomens/releases/latest/download/install.sh | sh

# Windows (PowerShell)
irm https://github.com/manuelserver/badomens/releases/latest/download/install.ps1 | iex
```

Los scripts (`scripts/install.sh`, `scripts/install.ps1`):

1. Detectan el sistema y la arquitectura.
2. Resuelven la última release (o usan `BADOMENS_VERSION`).
3. Descargan el paquete y su `checksums.txt`.
4. **Verifican el sha256** antes de instalar (abortan si no coincide).
5. Instalan en `/usr/local/bin` (o `%LOCALAPPDATA%\Programs\badomens`) y
   muestran `badomens version`.

Variables opcionales: `BADOMENS_REPO`, `BADOMENS_DEST`, `BADOMENS_VERSION`.

## go install

```bash
go install github.com/manuelserver/badomens/cmd/badomens@latest
```

## Verificar la instalación

```bash
badomens version
badomens doctor      # Docker, Tailscale y configuración
badomens update      # confirma que estás en la última versión
```