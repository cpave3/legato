#!/bin/sh
# Checks that a built AppImage carries the GStreamer plugins WebKitGTK plays
# media with. Without them, a page that made a <video> froze the app for
# good (LEG-162). Unpacks the AppImage into a scratch folder and looks for
# the plugins, the plugin scanner and the AppRun hook that points GStreamer
# at them.
#
#   sh scripts/appimage-media-check.sh path/to/Legato.AppImage
set -eu

image=${1:-}
[ -n "$image" ] && [ -f "$image" ] || { echo "appimage-media-check: no AppImage at '$image'" >&2; exit 2; }
image=$(cd "$(dirname "$image")" && pwd)/$(basename "$image")

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
(cd "$work" && APPIMAGE_EXTRACT_AND_RUN= "$image" --appimage-extract >/dev/null)
root=$work/squashfs-root

missing=""
# core, base (playback, app, videoconvertscale), gl, good (MP4, WebM, VP8/9),
# libav (H.264, AAC).
for plugin in coreelements playback app typefindfunctions videoconvertscale audioconvert autodetect opengl isomp4 matroska vpx libav; do
  [ -f "$root/usr/lib/gstreamer-1.0/libgst$plugin.so" ] || missing="$missing $plugin"
done
[ -z "$missing" ] || { echo "appimage-media-check: FAIL, plugins missing:$missing" >&2; exit 1; }
[ -x "$root/usr/lib/gstreamer1.0/gstreamer-1.0/gst-plugin-scanner" ] || { echo "appimage-media-check: FAIL, no gst-plugin-scanner" >&2; exit 1; }
grep -qs GST_PLUGIN_SYSTEM_PATH "$root"/apprun-hooks/*gstreamer* || { echo "appimage-media-check: FAIL, no GStreamer AppRun hook" >&2; exit 1; }
count=$(ls "$root/usr/lib/gstreamer-1.0" | grep -c '^libgst.*\.so$')
echo "appimage-media-check: ok, $count GStreamer plugins"
