# Desinstalador de badomens (Windows / PowerShell).
# Uso:  .\scripts\uninstall.ps1            # conserva los datos
#       .\scripts\uninstall.ps1 -Purge     # borra también TODOS los datos
param(
  [switch]$Purge
)
$ErrorActionPreference = "Continue"

$DestDir   = if ($env:BADOMENS_DEST)      { $env:BADOMENS_DEST }      else { Join-Path $env:LOCALAPPDATA "Programs\badomens" }
$ConfigDir = if ($env:BADOMENS_CONFIG_DIR) { $env:BADOMENS_CONFIG_DIR } else { Join-Path $env:APPDATA "badomens" }
$bin = Join-Path $DestDir "badomens.exe"

if (Test-Path $bin) {
  try {
    Remove-Item $bin -Force -ErrorAction Stop
    Write-Host "Binario eliminado: $bin"
  } catch {
    try {
      Rename-Item $bin "$bin.old" -ErrorAction Stop
      Write-Host "Binario en uso: renombrado a $bin.old (elimínalo después de cerrar badomens)"
    } catch {
      Write-Warning "No se pudo borrar $bin. Elimínalo manualmente y quita la carpeta del PATH."
    }
  }
} else {
  Write-Host "No hay binario en $bin (¿ya desinstalado?)"
}

if ($Purge) {
  Remove-Item -Recurse -Force $ConfigDir -ErrorAction SilentlyContinue
  Write-Host "Configuración y DATOS eliminados: $ConfigDir"
} else {
  Remove-Item -Force (Join-Path $ConfigDir "config.yaml") -ErrorAction SilentlyContinue
  Write-Host "Configuración eliminada: $(Join-Path $ConfigDir 'config.yaml')"
  Write-Host "Datos conservados en: $(Join-Path $ConfigDir 'data')  (usa -Purge para borrarlos)"
}

# --- Quita el directorio del PATH de usuario (HKCU\Environment) ---
$destFull = [System.IO.Path]::GetFullPath($DestDir)
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
$parts = @($userPath -split ';' | Where-Object { $_ -and $_.TrimEnd('\') -ne $destFull.TrimEnd('\') })
try {
  [Environment]::SetEnvironmentVariable("Path", ($parts -join ';'), "User")
  Write-Host "Quitado del PATH de usuario: $DestDir"
} catch {
  Write-Warning "No se pudo quitar del PATH automáticamente: $($_.Exception.Message)"
}

Write-Host "Desinstalación completada."