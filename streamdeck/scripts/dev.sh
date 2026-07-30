#!/usr/bin/env bash
# dev.sh — fast dev loop: build, deploy, kill (OpenDeck auto-restarts the plugin).
#
# Usage: ./scripts/dev.sh
#
# This skips the zip/install/reload cycle entirely. It:
# 1. Builds the binary directly into OpenDeck's plugin directory
# 2. Kills the running plugin process (OpenDeck relaunches it immediately)
#
# After running this, switch to your Legato profile in OpenDeck and the
# new binary picks up automatically.
set -euo pipefail

cd "$(dirname "$0")/.."

PLUGIN_DIR="$HOME/.var/app/me.amankhanna.opendeck/config/opendeck/plugins/com.cpave3.legato.sdPlugin"

if [ ! -d "$PLUGIN_DIR" ]; then
  echo "error: plugin not installed. Run 'task streamdeck:package' and install the zip first." >&2
  exit 1
fi

echo "→ building..."
go build -o /tmp/legato-streamdeck ./cmd/legato-streamdeck

echo "→ deploying to $PLUGIN_DIR/bin/"
install /tmp/legato-streamdeck "$PLUGIN_DIR/bin/legato-streamdeck"

echo "→ killing old process (OpenDeck will restart it)..."
pkill -f "legato-streamdeck" 2>/dev/null || true

echo "→ done. Switch to your Legato profile in OpenDeck."
echo "  log: tail -f ~/.var/app/me.amankhanna.opendeck/data/opendeck/logs/plugins/com.cpave3.legato.sdPlugin.log"
