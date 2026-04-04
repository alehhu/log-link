#!/usr/bin/env sh
set -eu

# Configuration
REPO_URL="https://github.com/alehhu/log-link.git"
BIN_NAME="loglink"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
TMP_DIR="/tmp/loglink-build"

# 1. Check for Go
if ! command -v go >/dev/null 2>&1; then
  echo "Error: Go is required but not found in PATH." >&2
  echo "Install Go first: https://go.dev/doc/install" >&2
  exit 1
fi

# 2. Setup Directories
mkdir -p "$INSTALL_DIR"
rm -rf "$TMP_DIR"
mkdir -p "$TMP_DIR"

# 3. Clone and Build
echo "Cloning $REPO_URL..."
git clone --depth 1 "$REPO_URL" "$TMP_DIR" > /dev/null 2>&1

echo "Building $BIN_NAME..."
cd "$TMP_DIR"
go build -o "$BIN_NAME" ./cmd/loglink

# 4. Install
cp "$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"
chmod 0755 "$INSTALL_DIR/$BIN_NAME"

# 5. Cleanup
rm -rf "$TMP_DIR"

echo "Installed to $INSTALL_DIR/$BIN_NAME"

# 6. Path Verification
case ":$PATH:" in
  *":$INSTALL_DIR:"*)
    echo "Ready: run '$BIN_NAME --help'"
    ;;
  *)
    echo "Add this to your shell profile:"
    echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
    echo "Then reload your shell and run: $BIN_NAME --help"
    ;;
esac
