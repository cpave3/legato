#!/bin/sh
# Fetches GTK 3's gtk-layer-shell for the AppImage (tauri.conf.json,
# bundle.linux.appimage.files), so the voice widget gets a layer surface on
# any Wayland desktop with the protocol, whatever the host has installed.
# Ubuntu 24.04's package, the release's build machine: 0.8.2, before 0.9
# wanted loading ahead of libwayland-client, so the app can still dlopen it
# after GTK starts (src-tauri/src/voicewidget.rs, layer_shell).
#
# Run before bundling (build.beforeBundleCommand); does nothing off Linux,
# and nothing when the library is already in place.
set -eu

[ "$(uname -s)" = Linux ] || exit 0
case "$(uname -m)" in
  x86_64) ;;
  *) echo "layer-shell: no package for $(uname -m); the AppImage will use the system's" >&2; exit 0 ;;
esac

DEB=libgtk-layer-shell0_0.8.2-1build2_amd64.deb
URL=https://archive.ubuntu.com/ubuntu/pool/universe/g/gtk-layer-shell/$DEB
SHA256=7e3010154387c4e84a3197210495e4a19ec5f166872e25f57b3354d2e7ab48da

here=$(cd "$(dirname "$0")/.." && pwd)
out=$here/src-tauri/target/layer-shell
[ -f "$out/libgtk-layer-shell.so.0" ] && [ -f "$out/copyright" ] && exit 0

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
curl -fsSL --retry 3 -o "$work/$DEB" "$URL"
echo "$SHA256  $work/$DEB" | sha256sum -c --status || { echo "layer-shell: $DEB does not match its checksum" >&2; exit 1; }
(cd "$work" && ar x "$DEB" data.tar.zst && tar --zstd -xf data.tar.zst ./usr/lib/x86_64-linux-gnu/libgtk-layer-shell.so.0.8.2 ./usr/share/doc/libgtk-layer-shell0/copyright)
mkdir -p "$out"
cp "$work/usr/lib/x86_64-linux-gnu/libgtk-layer-shell.so.0.8.2" "$out/libgtk-layer-shell.so.0"
cp "$work/usr/share/doc/libgtk-layer-shell0/copyright" "$out/copyright"
echo "layer-shell: $out/libgtk-layer-shell.so.0 (gtk-layer-shell 0.8.2)"
