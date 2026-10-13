#!/bin/sh
# Desinstalador de badomens (macOS / Linux).
# Uso:  ./scripts/uninstall.sh            # conserva los datos
#       ./scripts/uninstall.sh --purge    # borra también TODOS los datos
set -eu

PURGE="${1:-}"
DEST="${BADOMENS_DEST:-/usr/local/bin}"
EXE="$DEST/badomens"

if [ -f "$EXE" ]; then
  rm -f "$EXE"
  echo "Binario eliminado: $EXE"
else
  echo "No hay binario en $EXE (¿ya desinstalado?)"
fi

# Verifica si existe una instalación *manual* en otra ruta:
if command -v badomens >/dev/null 2>&1 && [ "$(command -v badomens)" != "$EXE" ]; then
  echo "Aviso: badomens también está en $(command -v badomens); elimínalo manualmente."
fi

CFG=""
if [ -n "${XDG_CONFIG_HOME:-}" ]; then
  CFG="$XDG_CONFIG_HOME/badomens"
else
  CFG="$HOME/.config/badomens"
fi

if [ "$PURGE" = "--purge" ]; then
  rm -rf "$CFG"
  echo "Configuración y DATOS eliminados: $CFG"
else
  rm -f "$CFG/config.yaml"
  echo "Configuración eliminada: $CFG/config.yaml"
  echo "Datos conservados en: $CFG/data  (usa --purge para borrarlos)"
fi

echo "Desinstalación completada."