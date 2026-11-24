#!/bin/sh
# Checks the tray icon without a desktop: runs the built app in a headless
# KWin (kwin_wayland --virtual) on its own session bus, in scratch config
# and data folders, so nothing running is touched (as layer-shell-check.sh
# does), with kded6's StatusNotifierWatcher as the tray host. Clicks the
# icon over D-Bus (Activate, what Plasma sends on a left click), reads its
# menu (com.canonical.dbusmenu) and the saved setting after each click.
# Then starts it again with no tray host, where it must log that once and
# carry on.
#
#   sh scripts/tray-check.sh [path/to/legato-desktop]
#
# Needs kwin_wayland, kded6, dbus-run-session and gdbus; build the app
# (cargo build --release) first.
set -eu

here=$(cd "$(dirname "$0")/.." && pwd)
app=${1:-$here/src-tauri/target/release/legato-desktop}
for tool in kwin_wayland kded6 dbus-run-session gdbus; do
  command -v "$tool" >/dev/null || { echo "tray-check: no $tool" >&2; exit 2; }
done
[ -x "$app" ] || { echo "tray-check: no app at $app" >&2; exit 2; }

# A short folder: a Wayland socket's path must fit in 108 bytes.
work=$(mktemp -d /tmp/tray-check.XXXXXX)
[ -n "${KEEP:-}" ] && echo "tray-check: keeping $work" || trap 'rm -rf "$work"' EXIT

cat > "$work/inside.sh" <<'EOF'
set -u
mkdir -p "$WORK/run" "$WORK/config" "$WORK/data" "$WORK/cache"
chmod 700 "$WORK/run"
kwin_wayland --virtual --no-lockscreen --no-global-shortcuts --socket check --width 1920 --height 1080 >"$WORK/kwin.log" 2>&1 &
kwin=$!
for _ in $(seq 100); do [ -S "$XDG_RUNTIME_DIR/check" ] && break; sleep 0.1; done
if [ "$HOST" = 1 ]; then
  kded6 >"$WORK/kded.log" 2>&1 &
  kded=$!
  for _ in $(seq 100); do
    gdbus introspect --session --dest org.kde.StatusNotifierWatcher --object-path /StatusNotifierWatcher >/dev/null 2>&1 && break
    sleep 0.1
  done
fi
# No DISPLAY: the app would grab its key on the X display of the desktop
# this runs on.
env -u DISPLAY WAYLAND_DISPLAY=check GDK_BACKEND=wayland LEGATO_DESKTOP_STATE="$WORK/state.json" \
  XDG_CONFIG_HOME="$WORK/config" XDG_DATA_HOME="$WORK/data" XDG_CACHE_HOME="$WORK/cache" \
  "$APP" >"$WORK/app.log" 2>&1 &
app=$!

state() { cat "$WORK/state.json" 2>/dev/null || echo "{}"; }
hidden() { case "$(state)" in *'"widget_hidden": true'*) echo hidden ;; *) echo shown ;; esac; }
labels() { gdbus call --session --dest "$svc" --object-path "$menu" --method com.canonical.dbusmenu.GetLayout -- 0 -1 "['label']" 2>>"$WORK/gdbus.err" | tee -a "$WORK/layout.txt" | grep -o "'label': <'[^']*'>" | sed "s/'label': <'\\(.*\\)'>/\\1/" | tr '\n' '|'; }
# A menu item's dbusmenu ID, by its label, and a click on it.
id_of() { gdbus call --session --dest "$svc" --object-path "$menu" --method com.canonical.dbusmenu.GetLayout -- 0 -1 "['label']" | grep -o "([0-9]*, {'label': <'$1'>" | grep -o '^([0-9]*' | tr -d '('; }
choose() { gdbus call --session --dest "$svc" --object-path "$menu" --method com.canonical.dbusmenu.Event -- "$(id_of "$1")" clicked '<0>' 0 >/dev/null 2>>"$WORK/gdbus.err"; sleep 1; }

if [ "$HOST" = 1 ]; then
  item=""
  for _ in $(seq 200); do
    item=$(gdbus call --session --dest org.kde.StatusNotifierWatcher --object-path /StatusNotifierWatcher \
      --method org.freedesktop.DBus.Properties.Get org.kde.StatusNotifierWatcher RegisteredStatusNotifierItems 2>/dev/null \
      | grep -o "'[^']*'" | head -1 | tr -d "'")
    [ -n "$item" ] && break
    sleep 0.1
  done
  echo "item=$item" >"$WORK/result"
  if [ -n "$item" ]; then
    svc=${item%%/*}
    path=/${item#*/}
    menu=$(gdbus call --session --dest "$svc" --object-path "$path" --method org.freedesktop.DBus.Properties.Get org.kde.StatusNotifierItem Menu 2>>"$WORK/gdbus.err" | tee "$WORK/menu.txt" | grep -o "'/[^']*'" | tr -d "'")
    sleep 1
    echo "start: $(hidden) menu=$(labels)" >>"$WORK/result"
    gdbus call --session --dest "$svc" --object-path "$path" --method org.kde.StatusNotifierItem.Activate 0 0 >/dev/null
    sleep 1
    echo "click: $(hidden) menu=$(labels)" >>"$WORK/result"
    gdbus call --session --dest "$svc" --object-path "$path" --method org.kde.StatusNotifierItem.Activate 0 0 >/dev/null
    sleep 1
    echo "click: $(hidden) menu=$(labels)" >>"$WORK/result"
    choose "Hide widget"
    echo "menu: $(hidden) menu=$(labels)" >>"$WORK/result"
    choose "Show widget"
    echo "menu: $(hidden) menu=$(labels)" >>"$WORK/result"
    choose "Talk to the Reeve"
    choose "Open Legato"
    choose "Servers & keys…"
    choose "Quit"
    kill -0 "$app" 2>/dev/null || echo "quit" >>"$WORK/result"
  fi
  kill "$kded" 2>/dev/null
else
  sleep 5
  kill -0 "$app" 2>/dev/null && echo running >"$WORK/result"
fi
kill "$app" 2>/dev/null; wait "$app" 2>/dev/null
kill "$kwin"; wait "$kwin" 2>/dev/null || true
EOF

run() {
  rm -rf "$work/run" "$work/config" "$work/data" "$work/cache" "$work/result"
  HOST=$1 WORK=$work APP=$app XDG_RUNTIME_DIR=$work/run QT_QPA_PLATFORM=offscreen dbus-run-session -- sh "$work/inside.sh" >/dev/null 2>&1 || true
}

fail() { echo "tray-check: FAIL, $1" >&2; cat "$work/app.log" >&2; exit 1; }

run 1
cat "$work/result"
grep '^\[tray\]' "$work/app.log" || true
grep -q '^item=.' "$work/result" || fail "no StatusNotifierItem was registered"
sed -n 2p "$work/result" | grep -q '^start: shown menu=.*Hide widget|Talk to the Reeve|.*Open Legato|Servers & keys…|.*Quit' || fail "the menu at the start is not as expected"
sed -n 3p "$work/result" | grep -q '^click: hidden menu=.*Show widget|' || fail "a click did not hide the widget"
sed -n 4p "$work/result" | grep -q '^click: shown menu=.*Hide widget|' || fail "a second click did not show the widget"
sed -n 5p "$work/result" | grep -q '^menu: hidden menu=.*Show widget|' || fail "the menu's Hide widget did not hide it"
sed -n 6p "$work/result" | grep -q '^menu: shown menu=.*Hide widget|' || fail "the menu's Show widget did not show it"
for action in Talk OpenLegato Settings Quit; do
  grep -q "^\[tray\] $action\$" "$work/app.log" || fail "the menu did not ask $action"
done
grep -q '^\[voice key\] Talk from the tray' "$work/app.log" || fail "Talk to the Reeve did not press the key"
grep -q '^quit$' "$work/result" || fail "Quit did not end the app"

run 0
grep '^\[tray\]' "$work/app.log" || true
[ "$(grep -c '^\[tray\] no icon by the clock' "$work/app.log")" = 1 ] || fail "with no tray host, the app did not say so once"
grep -q '^running' "$work/result" 2>/dev/null || fail "with no tray host, the app did not keep running"
echo "tray-check: ok"
