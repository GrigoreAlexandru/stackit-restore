#!/usr/bin/env bash
set -e

REPO="GrigoreAlexandru/stackit-restore"
BINARY_NAME="stackit-restore"
INSTALL_DIR="/usr/local/bin"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" && exit 1 ;;
esac

case "$OS" in
  linux) ;;
  darwin) ;;
  *) echo "Unsupported OS: $OS" && exit 1 ;;
esac

LATEST_TAG="$(curl -sSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')"

if [ -z "$LATEST_TAG" ]; then
  echo "Error: Could not fetch latest release tag for ${REPO}."
  exit 1
fi

VERSION="${LATEST_TAG#v}"
TARBALL="${BINARY_NAME}_${VERSION}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST_TAG}/${TARBALL}"

TEMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TEMP_DIR"' EXIT

echo "Downloading ${BINARY_NAME} ${LATEST_TAG} for ${OS}/${ARCH}..."
curl -sSL "$DOWNLOAD_URL" -o "$TEMP_DIR/$TARBALL"

tar -xzf "$TEMP_DIR/$TARBALL" -C "$TEMP_DIR"

echo "Installing ${BINARY_NAME} to ${INSTALL_DIR}..."
if [ -w "$INSTALL_DIR" ]; then
  mv "$TEMP_DIR/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
else
  sudo mv "$TEMP_DIR/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
fi

chmod +x "$INSTALL_DIR/$BINARY_NAME"
echo "Successfully installed ${BINARY_NAME} ${LATEST_TAG} to ${INSTALL_DIR}/${BINARY_NAME}"
