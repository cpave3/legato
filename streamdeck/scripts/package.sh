#!/usr/bin/env bash
# package.sh — assemble the .sdPlugin folder and zip it for installation.
#
# Usage: ./scripts/package.sh [output-dir]
#
# Produces:
#   dist/com.cpave3.legato.sdPlugin/       — unpacked plugin folder
#   dist/com.cpave3.legato.streamDeckPlugin — zipped plugin (install via OpenDeck UI)
#
# OpenDeck's install_plugin command expects a zip whose top-level entry is a
# *.sdPlugin directory. The .streamDeckPlugin extension is the Elgato/OpenAction
# convention for these installable archives.
set -euo pipefail

cd "$(dirname "$0")/.."

OUTPUT_DIR="${1:-dist}"
PLUGIN_NAME="com.cpave3.legato.sdPlugin"
PLUGIN_DIR="${OUTPUT_DIR}/${PLUGIN_NAME}"
ZIP_FILE="${OUTPUT_DIR}/com.cpave3.legato.streamDeckPlugin"

rm -rf "$PLUGIN_DIR" "$ZIP_FILE"
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

# Copy config file if it exists (contains legato_url + token).
# This is needed because OpenDeck runs plugins inside a Flatpak sandbox
# where the host's ~/.local/share/legato/auth-token is not readable.
if [ -f plugin/streamdeck.yaml ]; then
  cp plugin/streamdeck.yaml "$PLUGIN_DIR/streamdeck.yaml"
  echo "  Config: plugin/streamdeck.yaml (copied into package)"
fi

# Zip the plugin folder. The archive must contain the top-level .sdPlugin dir
# so OpenDeck can extract it into ~/.local/share/opendeck/plugins/.
(cd "$OUTPUT_DIR" && zip -r "$PLUGIN_NAME.zip" "$PLUGIN_NAME" && mv "$PLUGIN_NAME.zip" "com.cpave3.legato.streamDeckPlugin")

echo "Packaged:"
echo "  Folder: $PLUGIN_DIR"
echo "  Zip:    $ZIP_FILE"
echo ""
echo "Install via OpenDeck UI: Settings → Plugins → Install from file → $ZIP_FILE"
echo "Or manual: unzip $ZIP_FILE -d ~/.local/share/opendeck/plugins/"
