# Instalador de badomens desde la última release de GitHub (Windows).
# Uso (PowerShell):
#   irm https://github.com/MrZudro/badomens/releases/latest/download/install.ps1 | iex
#   (o clona el repo y ejecuta .\scripts\install.ps1 [-FromSource])
#
# -FromSource compila el binario con Go EN TU EQUIPO en vez de descargarlo. Es la
# vía recomendada cuando Smart App Control está activo (bloquea el .exe
# descargado, sin firma); los binarios que produce el toolchain de Go sí pasan.
param(
  [switch]$FromSource
)
$ErrorActionPreference = "Stop"

$Repo = if ($env:BADOMENS_REPO) { $env:BADOMENS_REPO } else { "MrZudro/badomens" }
$DestDir = if ($env:BADOMENS_DEST) { $env:BADOMENS_DEST } else { Join-Path $env:LOCALAPPDATA "Programs\badomens" }
$Version = if ($env:BADOMENS_VERSION) { $env:BADOMENS_VERSION } else { "" }

function Get-Arch {
  switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    default { throw "Arquitectura no soportada: $env:PROCESSOR_ARCHITECTURE" }
  }
}

# Smart App Control (Windows 11) bloquea los ejecutables SIN FIRMA y no ofrece un
# botón de «ejecutar de todas formas»: hay que saber si está activo para poder
# avisar ANTES de descargar y para explicar el fallo DESPUÉS.
#   HKLM\SYSTEM\CurrentControlSet\Control\CI\Policy\VerifiedAndReputablePolicyState
#   0 = desactivado · 1 = activado (aplica) · 2 = evaluación (aprendiendo)
function Get-SacState {
  try {
    $v = (Get-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Control\CI\Policy' `
        -Name VerifiedAndReputablePolicyState -ErrorAction Stop).VerifiedAndReputablePolicyState
  } catch {
    return "unknown"
  }
  switch ([int]$v) {
    0 { "off" }
    1 { "enforced" }
    2 { "evaluation" }
    default { "unknown" }
  }
}

function Show-SacHelp {
  param([string]$State, [string]$Bin)
  $estado = switch ($State) {
    "enforced" { "está ACTIVADO" }
    "evaluation" { "está en evaluación" }
    default { "está activado o en evaluación" }
  }
  Write-Host ""
  Write-Warning "Smart App Control de Windows bloqueó badomens.exe"
  Write-Host @"
Smart App Control ($estado) solo permite aplicaciones firmadas o con reputación
conocida y no ofrece un botón de «ejecutar de todas formas». Bloquea de forma
reproducible el .exe DESCARGADO: no está firmado y lleva la marca de descargado.
No es un fallo del programa ni del instalador.

El archivo quedó instalado en:
  $Bin

Opciones, de la menos a la más invasiva:

1) Instalarlo compilando con Go (lo más rápido de probar y no toca el sistema).
   El toolchain avala lo que compila y Smart App Control deja pasar esos
   binarios (verificado: 7 de 7 ejecuciones):
     go install github.com/MrZudro/badomens/cmd/badomens@latest
   deja el binario en %USERPROFILE%\go\bin\badomens.exe. Desde un clon:
     .\scripts\install.ps1 -FromSource
   Si el primer intento lo bloquea, reinténtalo pasado un rato (el veredicto de
   SAC depende de una comprobación de reputación) o usa la opción 2.

2) Usarlo en WSL2 o Docker (determinista). Smart App Control solo controla
   ejecutables de Windows, así que la versión de Linux corre sin más:
     wsl --install -d Ubuntu
   Dentro de WSL, con Docker Desktop y su integración activada:
     curl -fsSL https://raw.githubusercontent.com/$Repo/main/scripts/install.sh | sh
   badomens orquesta servidores en Docker Compose: es su entorno natural aquí.

3) Desactivar Smart App Control (determinista e IRREVERSIBLE):
   Seguridad de Windows -> Control de aplicaciones y navegador -> Smart App Control.
   Para volver a activarlo habría que restablecer o reinstalar Windows.

4) (Lado del proyecto) publicar los binarios FIRMADOS digitalmente: es la única
   forma de que el .exe descargado funcione con SAC activo. Requiere un
   certificado de firma de código y aún no está hecho.
"@
}

function Add-ToUserPath {
  param([string]$Dir)
  $destFull = [System.IO.Path]::GetFullPath($Dir)
  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  $parts = @($userPath -split ';' | Where-Object { $_ })
  $normalized = @($parts | ForEach-Object { $_.TrimEnd('\') })
  if ($normalized -contains $destFull.TrimEnd('\')) {
    Write-Host "Ya estaba en tu PATH de usuario: $Dir"
    return
  }
  try {
    [Environment]::SetEnvironmentVariable("Path", (($parts + $destFull) -join ';'), "User")
    Write-Host "Añadido a tu PATH de usuario (reabre la terminal para usarlo): $Dir"
  } catch {
    Write-Warning "No se pudo añadir el PATH automáticamente: $($_.Exception.Message)"
    Write-Warning "Añádelo a mano: [Environment]::SetEnvironmentVariable('Path', ((($env:Path -split ';') + '$Dir') -join ';'), 'User')"
  }
}

# Ejecuta el binario recién instalado; si Smart App Control lo bloquea, lo explica
# en vez de dejar un error críptico de PowerShell.
function Invoke-Installed {
  param([string]$Bin)
  try {
    & $Bin version
    if ($LASTEXITCODE -is [int] -and $LASTEXITCODE -ne 0) {
      throw "badomens version salió con código $LASTEXITCODE"
    }
  } catch {
    $sac = Get-SacState
    if ($sac -ne "off") {
      Show-SacHelp -State $sac -Bin $Bin
      exit 2
    }
    throw "badomens se instaló en $Bin pero no pudo ejecutarse: $($_.Exception.Message)"
  }
}

$Arch = Get-Arch
if (-not $Version) {
  # Listado de releases (no /releases/latest): GitHub ignora las pre-releases
  # en /latest, y badomens publica pre-releases.
  $rels = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases?per_page=1" -Headers @{ "User-Agent" = "badomens-installer" }
  $rels = @($rels)
  if ($rels.Count -gt 0) { $Version = $rels[0].tag_name.TrimStart("v") }
}
if (-not $Version) { throw "No se pudo resolver la última versión de badomens (¿hay releases publicadas?)" }

New-Item -ItemType Directory -Force -Path $DestDir | Out-Null
$bin = Join-Path $DestDir "badomens.exe"

if ($FromSource) {
  # --- Compilar desde el código fuente de la versión (evita la descarga de un
  #     binario sin firma, que es lo que bloquea Smart App Control) ---
  if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "Se necesita Go para -FromSource: instálalo desde https://go.dev/dl/ y reintenta"
  }
  Write-Host "Compilando badomens $Version desde el código (Go)…"
  $src = Join-Path ([System.IO.Path]::GetTempPath()) "badomens-src-$([guid]::NewGuid().ToString('N'))"
  New-Item -ItemType Directory -Force -Path $src | Out-Null
  try {
    $zip = Join-Path $src "src.zip"
    Invoke-WebRequest -Uri "https://github.com/$Repo/archive/refs/tags/v$Version.zip" -OutFile $zip -UseBasicParsing
    Expand-Archive -Path $zip -DestinationPath $src -Force
    $root = Get-ChildItem -Path $src -Directory | Select-Object -First 1
    if (-not $root) { throw "no se encontró el código fuente en el paquete" }
    Push-Location $root.FullName
    try {
      & go build -trimpath -ldflags "-s -w -X github.com/MrZudro/badomens/internal/version.Version=$Version" -o $bin ./cmd/badomens
      if ($LASTEXITCODE -ne 0) { throw "«go build» falló con código $LASTEXITCODE" }
    } finally {
      Pop-Location
    }
  } finally {
    Remove-Item -Recurse -Force $src -ErrorAction SilentlyContinue
  }
  Write-Host "Compilado en $bin"
  Add-ToUserPath -Dir $DestDir
  Invoke-Installed -Bin $bin
  exit $LASTEXITCODE
}

# --- Descarga del binario publicado (firma ausente: puede chocar con SAC) ---
$sac = Get-SacState
if ($sac -ne "off") {
  Write-Warning "Smart App Control no está desactivado ($sac): bloqueará el .exe descargado (sin firma). Si ocurre, este script explicará las alternativas (compilar con Go, WSL2/Docker o desactivar SAC)."
}

# El asset de GoReleaser incluye la versión: badomens_<version>_<os>_<arch>.zip
$Asset = "badomens_${Version}_windows_$Arch.zip"
$Base = "https://github.com/$Repo/releases/download/v$Version"

Write-Host "badomens $Version (Windows/$Arch)"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) "badomens-install-$([guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Force -Path $tmp | Out-Null

try {
  $zip = Join-Path $tmp $Asset
  Invoke-WebRequest -Uri "$Base/$Asset" -OutFile $zip -UseBasicParsing
  Invoke-WebRequest -Uri "$Base/checksums.txt" -OutFile (Join-Path $tmp "checksums.txt") -UseBasicParsing

  $expected = (Get-Content (Join-Path $tmp "checksums.txt") | Where-Object { $_ -match "  $Asset$" }) -split '\s+' | Select-Object -First 1
  $got = (Get-FileHash -Algorithm SHA256 -Path $zip).Hash.ToLower()
  if ($expected -and $got -ne $expected) {
    throw "Error de checksum: se esperaba $expected, llegó $got"
  }

  Expand-Archive -Path $zip -DestinationPath $tmp -Force
  $exe = Get-ChildItem -Path $tmp -Recurse -Filter "badomens.exe" | Select-Object -First 1
  if (-not $exe) { throw "No se encontró badomens.exe en el paquete" }
  Copy-Item $exe.FullName $bin -Force
}
finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

# La descarga deja la marca de «archivo descargado» (Mark of the Web). No influye
# en Smart App Control (que decide por firma y reputación), pero sí en SmartScreen
# y en antivirus que la usan: quitarla es lo correcto para un binario que acabamos
# de verificar por checksum.
try {
  Unblock-File -Path $bin -ErrorAction Stop
} catch {
  Write-Verbose "No se pudo quitar la marca de descargado: $($_.Exception.Message)"
}

Write-Host "Instalado en $bin"
Add-ToUserPath -Dir $DestDir
Invoke-Installed -Bin $bin
