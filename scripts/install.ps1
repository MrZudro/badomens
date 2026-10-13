# Instalador de badomens desde la última release de GitHub (Windows).
# Uso (PowerShell):
#   irm https://github.com/MrZudro/badomens/releases/latest/download/install.ps1 | iex
#   (o clona el repo y ejecuta .\scripts\install.ps1)
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
function Get-Asset {
  if ($env:PROCESSOR_ARCHITECTURE -eq "AMD64") { "badomens_windows_amd64.zip" } else { "badomens_windows_arm64.zip" }
}

$Arch = Get-Arch
if (-not $Version) {
  $rel = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers @{ "User-Agent" = "badomens-installer" }
  $Version = $rel.tag_name.TrimStart("v")
}
$Asset = "badomens_windows_$Arch.zip"
$Base = "https://github.com/$Repo/releases/download/v$Version"

Write-Host "badomens $Version (Windows/$Arch)"
New-Item -ItemType Directory -Force -Path $DestDir | Out-Null
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
  Copy-Item $exe.FullName (Join-Path $DestDir "badomens.exe") -Force
}
finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

$bin = Join-Path $DestDir "badomens.exe"
Write-Host "Instalado en $bin"
& $bin version
Write-Host "Añade a tu PATH: $DestDir"