# Legato Mobile

An Android app for managing several Legato servers. It is a native Kotlin
shell: a server list, pairing and the app's own services (secure tokens,
dictation, getting a reclaimed page back), around the Legato web interface in
Android's WebView.

## Building

The app runs on Android 10 (API 29) and later.

Needs a JDK from 17 to 24 and the Android SDK (`ANDROID_HOME`, or
`~/Android/Sdk`, or `~/Library/Android/sdk`). No Node, pnpm or Metro.

```sh
task mobile:test      # unit tests on the JVM
task mobile:apk       # release APK; prints its path
task mobile:install   # build, install over the app on the connected device, start it
```

`scripts/mobile-android.sh` finds a JDK Gradle can run on by itself (a newer
default `java` is passed over). With several devices attached, pick one with
`ANDROID_SERIAL`.

The APK is signed with the React Native template's public debug key, which also
signed every local build of the earlier Expo app. That is what lets it install
over that app in place, and over itself, keeping the saved servers.

## The code

All of it is in `app/src/main/java/dev/legato/mobile/`.

| File | What it does |
| --- | --- |
| `ServerAddress.kt` | Server URLs and the `legato pair` QR code |
| `Pairing.kt` | SPAKE2, the pairing word list, reading a typed phrase |
| `PhrasePairing.kt` | The phone's side of pairing by phrase; trusting a pinned certificate |
| `Discovery.kt`, `AndroidDiscovery.kt` | Servers announced on the local network |
| `AndroidPairing.kt` | Pairing over HTTP(S), and pinned trust for the app's own requests |
| `PageContract.kt` | What the app and the web interface say to each other |
| `PageKeeper.kt` | Getting the page back after Android reclaims it |
| `ShellVoice.kt` | Recording for the page |
| `ConnectionStore.kt` | The saved servers |
| `ServerProbe.kt` | `/health` and `/api/meta` |
| `ServerPage.kt` | The WebView, wired to the four above |
| `AndroidStorage.kt`, `AndroidMicrophone.kt` | Keystore, SQLite and `MediaRecorder` |
| `MainActivity.kt`, `Screens.kt` | The activity and its Compose screens |

The logic is in plain Kotlin classes that tests drive with fakes
(`app/src/test/`); the Android files only connect them to the platform.

## Pairing and servers

**By phrase.** `legato pair`, or Settings › **Pair a device** in the web UI,
shows four words, such as `matchstick obnoxious eruption hybrid`. In the app,
**+** opens **Add server**:

- Servers announcing themselves on the network are listed under **On this
  network**; tapping one fills in its address. The address can also be typed.
- The phrase is typed in one field. Case and separators do not matter, and the
  first three letters of a word are enough: no two words of the list share
  them. The boxes under the field show the word each typed word stands for.
- **Pair** runs the exchange and opens the server.

A server reached over HTTPS has an **HTTPS** badge beside its name, in the list
and in the bar above its page.

A phrase is valid for five minutes, for one phone, and for five tries. The words
come from the EFF short word list (1,296 words, so about 2.8 trillion phrases).

The phrase is never sent. The phone and the server run SPAKE2 (RFC 9382, the
P-256 / SHA-256 suite) on it: each proves it knows the phrase, and both get a
key no one listening can work out. Someone listening learns nothing to test
guesses against; someone answering in the server's place gets one guess per
try. The server then sends the token, its name and the certificate to trust,
sealed with that key (AES-256-GCM). The phone sends its own name the same way,
which the server shows.

- `Pairing.kt` and the server's `internal/engine/pairing` are held to the same
  recorded exchange: `app/src/test/resources/pairing-exchange.json` is written
  by `go test ./internal/engine/pairing -update`, and the Go test fails when the
  app's copy differs. Both pass RFC 9382's own test vectors, and both copies of
  the word list hash to the same value.
- The app finds servers through Android's DNS-SD (`NsdManager`): Legato's web
  server announces `_legato._tcp` with its port and a `scheme` TXT record
  (`internal/engine/discovery`). Some networks, such as guest Wi-Fi, block
  this; then type the address.

**HTTPS without installing a certificate.** Over HTTPS the phone does not
trust the server's certificate when it first connects, so it accepts it for the
two pairing requests and keeps it. After the exchange, that certificate must
be the one the server sealed, or be issued by it. If it isn't, something
between the two answered for the server, and pairing fails. The sealed
certificate (the generated CA, or `web.tls.cert`) is pinned for that server:

- The app's own requests trust it beside the system's CAs (`PinnedTrust`).
- The WebView proceeds past an error whose only fault is an unknown issuer,
  when the pinned certificate is that issuer (`ServerPage.trustedByPin`).

**By QR code.** **Pair a device** and `legato pair` show a QR code of the same
request: `legato://pair?url=…&phrase=…`. Scanned with Google's code scanner (no
camera permission) or opened as a link, it fills in the address and the words
and pairs at once, exactly as typed words would. Without a web server running,
`legato pair` falls back to a code that carries the token itself
(`legato://pair?url=…&token=…[&ca=…]`); that fills in **Add by address and
token**, and a `ca` in it is pinned the same way. When
machines share a configuration hostname, advertise each machine's actual
reachable address:

```sh
legato pair --host 192.168.1.20
```

The editable connection name distinguishes Desktop, Laptop and other instances
independently of hostname or IP. Legato defaults to HTTP for local-network
development; treat the token and all traffic as visible to other devices on
that network.

Saved servers are kept as the Expo app kept them, so the first run of this app
takes them over:

- The list is JSON in the shared preferences `legato.connections`. On first run
  it is copied from the Expo app's AsyncStorage (`databases/RKStorage`).
- Tokens are in the shared preferences `SecureStore`, each encrypted with an AES
  key in the Android Keystore (`AES/GCM/NoPadding:key_v1:keystoreUnauthenticated`),
  exactly as `expo-secure-store` kept them, so they are read as they are.

For HTTPS, the generated certificate includes current local interface IP
addresses. The app trusts Android's system and user CA stores
(`res/xml/network_security_config.xml`) as well as the pinned certificate.
The app needs no CA installed. The phone's browser does, to open Legato over
HTTPS outside the app: save it from the server's ⋯ menu (**Save CA
certificate**) and install it through **Certificate settings**.

## Opening a server

The app asks `/api/meta` whether it can open the server, then shows the page.
Before the page's own scripts run, it signs the page in (the token in
`localStorage`) and tells it the app can record (see Dictation). The page posts
to the app through `window.ReactNativeWebView.postMessage`, the name the Expo
app used, so servers built for that app work unchanged. Only frames from the
server's own origin get that object; an agent's HTML runs in sandboxed frames
with an origin of their own.

The page lives on while the person goes back to the server list: opening the
same server again shows it at once, where it was. Opening another server, or
editing or removing this one, closes it.

The WebView's engine is loaded while the server list is on screen, so the first
server opens without that wait.

## Returning from the background

Android reclaims the memory of a WebView's page while the app is in the
background, without closing the app. The page would then come back blank. The
app keeps the page's renderer as important as itself, so it is reclaimed less
often, and reopens it when it is, at the page it was on (`PageKeeper.kt`):

- When the system reports the page's renderer gone (`onRenderProcessGone`). A
  page lost in the background is reopened when the app returns to the
  foreground, not before.
- When the app returns and the page does not answer within four seconds, or
  answers that it has nothing on screen.

A page lost again within 30 seconds of being reopened, with the app in front the
whole time, is not reopened again: the app returns to the server list and says
so.

A reopened page is a fresh load, so what the web interface kept only in memory
is gone. The interface names the address that brings its view back by posting
`{"legatoShell":"restore","url":"…"}` (`web/src/lib/mobileShell.ts`). The agents
page does, so the agent that was open is open again afterwards.

A page that is still there on return is told so: the app dispatches a
`legato:shell-return` event on its `window`. The web interface treats that as
picking the open agent again, which refreshes a native agent panel from its
snapshot. A page on screen while the person is in the server list counts as in
the background, so coming back to it does the same.

`adb logcat -s LegatoPage` shows each time Android reclaims the page.

## Dictation

A page has a microphone only over HTTPS or on localhost, and Legato serves
plain HTTP by default, so the web UI has no microphone of its own in the app.
The app records in its place (`ShellVoice.kt`, `AndroidMicrophone.kt`):

- Before the page loads, the app tells it so: `window.legatoShell = {voice: 1,
  key}`. The key is new each time the server is opened.
- The page asks to start, stop or cancel by posting `{legatoShell: "voice",
  key, action}`. The app acts only on a request carrying the key from the top
  frame.
- The app asks Android for the microphone the first time, records AAC at 16 kHz
  mono, and answers with `legato:shell-voice` events in the page: `started`,
  `level` (0 to 1, several times a second, for the level bars), `audio` (the
  recording as base64), `cancelled` or `error`.
- The page decodes the recording and has Legato's server transcribe it, as it
  does with a browser's microphone.

Inside the app the app always records, over HTTPS too. Outside it, a browser
with a microphone of its own (over HTTPS, or on the desktop) uses that.

## File uploads and links

A file input in the page opens Android's file picker. A link to another site
opens in the browser; a link that opens a new window opens in the page, where
the same rule applies.
