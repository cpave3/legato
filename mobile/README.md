# Legato Mobile

Android-first Expo shell for managing multiple Legato servers.

## Fast path

Install Node, pnpm, Java 17+, and the Android SDK, then set `ANDROID_HOME`:

```sh
task mobile:apk
```

The command installs the frozen dependencies, generates the Android project,
builds a debug APK, and prints its path. Use `task mobile:device` to install and
launch on a connected phone. `task mobile:eas` creates an internal EAS APK.

The APK is a standalone release build with its JavaScript and assets embedded,
so it runs without Metro or an internet connection. Re-running the task and
installing with `adb install -r` updates it while preserving app data.
Use `task mobile:install` to perform that build, in-place install, and launch in
one command on an authorized USB or wireless-debugging device.

For the faster development loop, run `task mobile:dev-apk`, install the debug
APK once, then run `task mobile:dev`. That build connects to Metro over the LAN
and supports Fast Refresh; the standalone APK remains independent of Metro.

The app accepts the QR URI printed by `legato pair`. When machines share a
configuration hostname, advertise each machine's actual reachable address:

```sh
legato pair --host 192.168.1.20
```

The app's editable connection name distinguishes Desktop, Laptop, and other
instances independently of hostname or IP. Tokens are stored in Android secure
storage; connection names and URLs are stored locally outside the WebView.
Legato defaults to HTTP for local-network development. Treat the token and all
traffic as visible to other devices on that network.

For HTTPS, the generated certificate includes current local interface IP
addresses. Expo Go cannot use Android's user-installed CA store, so it cannot connect to
Legato's locally issued HTTPS certificates. Build the Legato APK with
`task mobile:eas` (no local Android SDK required) or `task mobile:apk`. The
Legato build opts into both Android's system and user CA stores; after installing
the CA offered by a connection, restart the app and connect using the exact IP
address advertised by `legato pair --host`.

## Returning from the background

Android reclaims the memory of a WebView's page while the app is in the
background, without closing the app. The page would then come back blank. The
app reopens it instead, at the page it was on (`src/ServerWebView.tsx`):

- When the system reports the page's process gone (`onRenderProcessGone`; on
  iOS, `onContentProcessDidTerminate`). A page lost in the background is
  reopened when the app returns to the foreground, not before.
- When the app returns and the page does not answer within four seconds, or
  answers that it has nothing on screen.

A page lost again within 30 seconds of being reopened, with the app in front the
whole time, is not reopened again: the app returns to the server list and says
so.

A reopened page is a fresh load, so what the web interface kept only in memory
is gone. The interface names the address that brings its view back by posting
`{"legatoShell":"restore","url":"…"}` through `window.ReactNativeWebView`
(`web/src/lib/mobileShell.ts`). The agents page does, so the agent that was open
is open again afterwards. A server built before that reopens at the same address
with no agent selected.

A page that is still there on return is told so: the app dispatches a
`legato:shell-return` event on its `window`. The web interface treats that as
picking the open agent again, which refreshes a native agent panel from its
snapshot (the same as choosing the agent in the picker while it is already
open). Terminal-backed agents are not refreshed this way.

`adb logcat -s RNCWebViewClient` shows "The WebView rendering process was killed
by the system." each time Android reclaims the page.

## Dictation

A page has a microphone only over HTTPS or on localhost, and Legato serves
plain HTTP by default, so the web UI has no microphone of its own in the app.
The app records in its place (`src/shellVoice.ts`, with `expo-audio`):

- Before the page loads, the app tells it so: `window.legatoShell = {voice: 1,
  key}`. The key is new each time the page is opened.
- The page asks to start, stop or cancel by posting `{legatoShell: "voice",
  key, action}`. The app acts only on a request carrying the key. The frames
  that agents' HTML runs in can post to the app too; they cannot read the key.
- The app asks Android for the microphone the first time, records AAC at 16 kHz
  mono, and answers with `legato:shell-voice` events in the page: `started`,
  `level` (0 to 1, several times a second, for the level bars), `audio` (the
  recording as base64), `cancelled` or `error`.
- The page decodes the recording and has Legato's server transcribe it, as it
  does with a browser's microphone. The server needs no change.

Inside the app the app always records, over HTTPS too. Outside it, a browser
with a microphone of its own (over HTTPS, or on the desktop) uses that.

