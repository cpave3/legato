#!/usr/bin/env bash
# package.sh — assemble the .sdPlugin distribution folder.
#
# Usage: ./scripts/package.sh [output-dir]
#
# Produces dist/com.cpave3.legato.sdPlugin/ with the manifest, icons, and
# compiled binary ready to drop into ~/.local/share/opendeck/plugins/.
set -euo pipefail

cd "$(dirname "$0")/.."

OUTPUT_DIR="${1:-dist}"
PLUGIN_DIR="${OUTPUT_DIR}/com.cpave3.legato.sdPlugin"

rm -rf "$PLUGIN_DIR"
mkdir -p "$PLUGIN_DIR/bin" "$PLUGIN_DIR/icons"

# Copy manifest.
cp plugin/manifest.json "$PLUGIN_DIR/manifest.json"

# Copy icons.
cp plugin/icons/*.png "$PLUGIN_DIR/icons/" 2>/dev/null || true

# Copy binary.
if [ -f plugin/bin/legato-streamdeck ]; then
  cp plugin/bin/legato-streamdeck "$PLUGIN_DIR/bin/legato-streamdeck"
else
  echo "error: binary not found. Run 'task streamdeck:build' first." >&2
  exit 1
fi

echo "Packaged to $PLUGIN_DIR"
echo "Install: cp -r $PLUGIN_DIR ~/.local/share/opendeck/plugins/"
