#!/bin/sh
# Minimal installer: downloads the latest release binary for this platform.
set -e
REPO="ravistakumar/fan"
BIN="fan"
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
esac
TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | cut -d '"' -f4)
URL="https://github.com/$REPO/releases/download/$TAG/${BIN}_${OS}_${ARCH}.tar.gz"
echo "Downloading $URL"
TMP=$(mktemp -d)
curl -fsSL "$URL" | tar -xz -C "$TMP"
DEST="${PREFIX:-/usr/local/bin}"
mv "$TMP/$BIN" "$DEST/$BIN"
chmod +x "$DEST/$BIN"
echo "Installed $BIN to $DEST"
