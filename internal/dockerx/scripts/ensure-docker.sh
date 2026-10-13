#!/usr/bin/env sh
# ensure-docker.sh — Respaldo independiente para dejar Docker listo (Linux/macOS).
# Sin dependencias: mismo plan que 'badomens docker ensure' pero en la shell.
# El código Go es la fuente de verdad; este script es para quien no puede
# ejecutar la TUI.
set -e

if ! command -v docker >/dev/null 2>&1; then
  echo "Docker no está instalado. Instálalo desde https://docs.docker.com/get-docker/" >&2
  exit 2
fi

docker info >/dev/null 2>&1 && docker compose version >/dev/null 2>&1 && { echo "Docker listo."; exit 0; }
echo "Docker no responde: intentando arrancarlo…"

if [ "$(uname)" = "Darwin" ]; then
  # macOS: Docker Desktop, Colima y OrbStack, en ese orden.
  open -a Docker 2>/dev/null || colima start 2>/dev/null || orb start 2>/dev/null || true
else
  # Linux: systemd con y sin sudo, luego servicio clásico.
  systemctl start docker 2>/dev/null \
    || sudo -n systemctl start docker 2>/dev/null \
    || systemctl start docker.socket 2>/dev/null \
    || service docker start 2>/dev/null || true
fi

n=0
until docker info >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; do
  n=$((n + 1))
  if [ "$n" -ge 60 ]; then
    echo "Docker no respondió a tiempo (120 s). Revisa el diagnóstico: badomens docker status" >&2
    exit 2
  fi
  sleep 2
done
echo "Docker listo."