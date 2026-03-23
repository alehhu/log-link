#!/usr/bin/env sh
set -eu

REPO="${REPO:-github.com/alehhu/log-link/cmd/loglink}"
BIN_NAME="${BIN_NAME:-loglink}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

if ! command -v go >/dev/null 2>&1; then
  echo "Error: Go is required but not found in PATH." >&2
  echo "Install Go first: https://go.dev/doc/install" >&2
  exit 1
fi

GO_BIN="$(go env GOPATH)/bin"
mkdir -p "$GO_BIN"
mkdir -p "$INSTALL_DIR"

# Go binary name based on the last part of REPO path
REPO_BIN_NAME=$(basename "$REPO")

echo "Installing $REPO@$([ -n "${VERSION:-}" ] && echo "$VERSION" || echo "latest")..."
if [ -n "${VERSION:-}" ]; then
  GO111MODULE=on go install "$REPO@$VERSION"
else
  GO111MODULE=on go install "$REPO@latest"
fi

# Locate the installed binary
if [ -x "$GO_BIN/$REPO_BIN_NAME" ]; then
  ACTUAL_BIN="$GO_BIN/$REPO_BIN_NAME"
elif [ -x "$GO_BIN/$BIN_NAME" ]; then
  ACTUAL_BIN="$GO_BIN/$BIN_NAME"
else
  echo "Error: expected binary not found at $GO_BIN/$REPO_BIN_NAME" >&2
  exit 1
fi

cp "$ACTUAL_BIN" "$INSTALL_DIR/$BIN_NAME"
chmod 0755 "$INSTALL_DIR/$BIN_NAME"

echo "Installed to $INSTALL_DIR/$BIN_NAME"
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
