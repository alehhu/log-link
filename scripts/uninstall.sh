#!/usr/bin/env sh
set -eu

BIN_NAME="${BIN_NAME:-loglink}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
TARGET="$INSTALL_DIR/$BIN_NAME"

if [ -f "$TARGET" ]; then
  rm -f "$TARGET"
  echo "Removed $TARGET"
else
  echo "No binary found at $TARGET"
fi

case ":$PATH:" in
  *":$INSTALL_DIR:"*)
    echo "Uninstall complete."
    ;;
  *)
    echo "Note: $INSTALL_DIR is not currently in PATH."
    ;;
esac
