#!/bin/sh
# Checks that the voice widget is drawn as a layer surface, without a
# desktop: runs the built app in a headless KWin (kwin_wayland --virtual)
# with the carried gtk-layer-shell, on its own session bus and in scratch
# config and data folders, so nothing running is touched, and reads its
# Wayland traffic. Passes when the widget's layer surface gets a buffer.
#
#   sh scripts/layer-shell-check.sh [path/to/legato-desktop]
#
# Needs kwin_wayland and dbus-run-session; build the app (cargo build
# --release) and fetch the library (scripts/layer-shell.sh) first.
set -eu

here=$(cd "$(dirname "$0")/.." && pwd)
app=${1:-$here/src-tauri/target/release/legato-desktop}
lib=$here/src-tauri/target/layer-shell/libgtk-layer-shell.so.0
command -v kwin_wayland >/dev/null || { echo "layer-shell-check: no kwin_wayland" >&2; exit 2; }
[ -x "$app" ] || { echo "layer-shell-check: no app at $app" >&2; exit 2; }
[ -f "$lib" ] || { echo "layer-shell-check: no $lib; run scripts/layer-shell.sh" >&2; exit 2; }

# A short folder: a Wayland socket's path must fit in 108 bytes.
work=$(mktemp -d /tmp/layer-shell-check.XXXXXX)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/run" "$work/appdir/usr/lib" "$work/config" "$work/data" "$work/cache"
chmod 700 "$work/run"
cp "$lib" "$work/appdir/usr/lib/"

cat > "$work/inside.sh" <<'EOF'
kwin_wayland --virtual --no-lockscreen --no-global-shortcuts --socket check --width 1920 --height 1080 >"$WORK/kwin.log" 2>&1 &
kwin=$!
for _ in $(seq 100); do [ -S "$XDG_RUNTIME_DIR/check" ] && break; sleep 0.1; done
WAYLAND_DISPLAY=check GDK_BACKEND=wayland APPDIR="$WORK/appdir" \
  XDG_CONFIG_HOME="$WORK/config" XDG_DATA_HOME="$WORK/data" XDG_CACHE_HOME="$WORK/cache" \
  WAYLAND_DEBUG=1 timeout 15 "$APP" >"$WORK/app.log" 2>&1 || true
kill "$kwin"; wait "$kwin" 2>/dev/null || true
EOF
WORK=$work APP=$app XDG_RUNTIME_DIR=$work/run QT_QPA_PLATFORM=offscreen dbus-run-session -- sh "$work/inside.sh" >/dev/null 2>&1

log=$work/app.log
grep '^\[voice widget\]' "$log" || true
surface=$(grep -o 'get_layer_surface(new id zwlr_layer_surface_v1#[0-9]*, wl_surface#[0-9]*' "$log" | grep -o 'wl_surface#[0-9]*$' | head -1)
[ -n "$surface" ] || { echo "layer-shell-check: FAIL, the widget made no layer surface" >&2; exit 1; }
grep -E 'zwlr_layer_surface_v1#[0-9]+\.(set_anchor|set_margin|set_size|configure)\(' "$log" | sed 's/^[^}]*} *//'
buffers=$(grep -c "$surface\.attach(wl_buffer" "$log" || true)
[ "$buffers" -gt 0 ] || { echo "layer-shell-check: FAIL, the layer surface was never drawn" >&2; exit 1; }
echo "layer-shell-check: ok, the layer surface was drawn ($buffers buffers)"
