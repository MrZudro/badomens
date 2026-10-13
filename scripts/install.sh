#!/bin/sh
# Instalador de badomens desde la última release de GitHub.
# Uso:  curl -fsSL https://github.com/manuelserver/badomens/releases/latest/download/install.sh | sh
#       (o clona el repo y ejecuta ./scripts/install.sh)
set -eu

REPO="${BADOMENS_REPO:-manuelserver/badomens}"
DEST="${BADOMENS_DEST:-/usr/local/bin}"
VERSION="${BADOMENS_VERSION:-}" # vacío = última release

# --- Detecta sistema y arquitectura ---
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Arquitectura no soportada: $ARCH" >&2; exit 1 ;;
esac

case "$OS" in
  linux) ASSET="badomens_${OS}_${ARCH}.tar.gz"; EXE="badomens" ;;
  darwin) ASSET="badomens_${OS}_${ARCH}.tar.gz"; EXE="badomens" ;;
  *) echo "Sistema no soportado por install.sh; usa install.ps1 en Windows" >&2; exit 1 ;;
esac

# --- Resuelve la versión/URL de descarga ---
if [ -z "$VERSION" ]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')"
  VERSION="${VERSION#v}"
fi
BASE="https://github.com/$REPO/releases/download/v$VERSION"
echo "badomens $VERSION ($OS/$ARCH)"

# --- Descarga y verifica checksum ---
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
curl -fsSL "$BASE/$ASSET" -o "$TMP/badomens.tar.gz"
curl -fsSL "$BASE/checksums.txt" -o "$TMP/checksums.txt"

(cd "$TMP" && (command -v sha256sum >/dev/null && sha256sum -c 2>/dev/null || true))
EXPECTED="$(grep "  $ASSET$" "$TMP/checksums.txt" | awk '{print $1}')"
if [ -n "$EXPECTED" ]; then
  GOT="$(cd "$TMP" && (command -v sha256sum >/dev/null && sha256sum badomens.tar.gz || shasum -a 256 badomens.tar.gz) | awk '{print $1}')"
  if [ "$GOT" != "$EXPECTED" ]; then
    echo "Error de checksum: se esperaba $EXPECTED, llegó $GOT" >&2
    exit 1
  fi
fi

tar -xzf "$TMP/badomens.tar.gz" -C "$TMP"
echo "badomens $VERSION verificada y extraída."

mkdir -p "$DEST"
install -m 0755 "$TMP/$EXE" "$DEST/badomens"
echo "Instalado en $DEST/badomens"
"$DEST/badomens" version