# ensure-docker.ps1 — Respaldo independiente para dejar Docker listo (Windows).
# Sin dependencias: mismo plan que 'badomens docker ensure' pero en PowerShell.
# El código Go es la fuente de verdad; este script es para quien no puede
# ejecutar la TUI.
$ErrorActionPreference = 'Stop'

function Test-Docker {
    docker info *> $null
    if (-not $?) { return $false }
    docker compose version *> $null
    return $?
}

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Write-Error 'Docker no está instalado. Instálalo desde https://docs.docker.com/get-docker/'
    exit 2
}

if (Test-Docker) {
    'Docker listo.'
    exit 0
}
'Docker no responde: intentando arrancar Docker Desktop…'

$exe = Join-Path $env:ProgramFiles 'Docker\Docker\Docker Desktop.exe'
if (Test-Path $exe) { Start-Process $exe }
else { Write-Warning 'No se encontró Docker Desktop.exe; ábrelo manualmente desde el menú Inicio.' }

$n = 0
while (-not (Test-Docker)) {
    $n++
    if ($n -ge 60) {
        Write-Error 'Docker no respondió a tiempo (120 s). Revisa el diagnóstico: badomens docker status'
        exit 2
    }
    Start-Sleep -Seconds 2
}
'Docker listo.'