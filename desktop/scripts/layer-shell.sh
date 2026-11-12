#!/bin/sh
# Fetches GTK 3's gtk-layer-shell for the AppImage (tauri.conf.json,
# bundle.linux.appimage.files), so the voice widget gets a layer surface on
# any Wayland desktop with the protocol, whatever the host has installed.
# gtk-layer-shell reads GTK's private structs, so it must know the GTK the
# AppImage carries, which is the build machine's: 0.10.1 knows GTK up to
# 3.24.52 (0.8.2 knew 3.24.39, and crashed on 3.24.52 opening a popup).
# Ubuntu 26.04's package. GTK 3's library wraps nothing of libwayland-client,
# so the app can dlopen it after GTK starts (src-tauri/src/voicewidget.rs,
# layer_shell). When the build machine's GTK passes the version this one
# knows, take a newer one.
#
# Run before bundling (build.beforeBundleCommand); does nothing off Linux,
# and nothing when the library is already in place.
set -eu

[ "$(uname -s)" = Linux ] || exit 0
case "$(uname -m)" in
  x86_64) ;;
  *) echo "layer-shell: no package for $(uname -m); the AppImage will use the system's" >&2; exit 0 ;;
esac

DEB=libgtk-layer-shell0_0.10.1-1_amd64.deb
URL=https://archive.ubuntu.com/ubuntu/pool/universe/g/gtk-layer-shell/$DEB
SHA256=653c0f0101bbfbefac94275a4570a0f91bf063d77770778124ae3e561f39b7b0
VERSION=0.10.1

here=$(cd "$(dirname "$0")/.." && pwd)
out=$here/src-tauri/target/layer-shell
[ "$(cat "$out/version" 2>/dev/null)" = "$VERSION" ] && [ -f "$out/libgtk-layer-shell.so.0" ] && [ -f "$out/copyright" ] && exit 0

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
curl -fsSL --retry 3 -o "$work/$DEB" "$URL"
echo "$SHA256  $work/$DEB" | sha256sum -c --status || { echo "layer-shell: $DEB does not match its checksum" >&2; exit 1; }
(cd "$work" && ar x "$DEB" data.tar.zst && tar --zstd -xf data.tar.zst ./usr/lib/x86_64-linux-gnu/libgtk-layer-shell.so.$VERSION ./usr/share/doc/libgtk-layer-shell0/copyright)
mkdir -p "$out"
cp "$work/usr/lib/x86_64-linux-gnu/libgtk-layer-shell.so.$VERSION" "$out/libgtk-layer-shell.so.0"
cp "$work/usr/share/doc/libgtk-layer-shell0/copyright" "$out/copyright"
echo "$VERSION" > "$out/version"
echo "layer-shell: $out/libgtk-layer-shell.so.0 (gtk-layer-shell $VERSION)"
