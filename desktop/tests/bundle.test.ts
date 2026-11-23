import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import { test } from "node:test"

const config = JSON.parse(readFileSync(new URL("../src-tauri/tauri.conf.json", import.meta.url), "utf8"))

// WebKitGTK plays media through GStreamer. Without its plugins in the
// AppImage, a page that made a <video> froze the app for good (LEG-162), and
// dictation's decodeAudioData never settled. The AppImage cannot use the
// host's plugins instead: its AppRun points GStreamer inside it, and they are
// built for another GStreamer.
test("the AppImage carries GStreamer's plugins", () => {
  assert.equal(config.bundle.linux.appimage.bundleMediaFramework, true)
})
