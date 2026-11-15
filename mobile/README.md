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

**Signing.** A release build is signed with a private key when
`~/.config/legato/android/signing.properties` exists (or the file
`$LEGATO_ANDROID_SIGNING` names): `storeFile`, `storePassword`, `keyAlias`
and `keyPassword`. Keep the key out of the repository and keep a copy of it:
Android installs an update only when it is signed with the same key as the
installed app, so a lost key means uninstalling, and pairing and enrolling
again. Legato trusts approval keys only from the app signed with the keys
listed in `approval_devices.android_app_digests` (the certificate's SHA-256,
from `keytool -list -v` or `apksigner verify --print-certs`); see
`docs/ai/approvals.md` § Signed approvals.

Without that file, the build is signed with the React Native template's public
debug key (`app/debug.keystore`), and Gradle warns. Anyone can sign with it,
so a phone with such a build should not sign approvals: Legato's default
`android_app_digests` trusts it only until a private key is configured.

Changing the key of an installed app needs an uninstall (`adb uninstall
dev.legato.mobile`), which removes its saved servers and approval keys.
Never uninstall automatically to work around a signature mismatch.

## Official Android releases

The release code is implemented, but an official APK has not been published
yet. No production signing setup has been performed. Cameron must choose the
durable private signer before production publication or phone acceptance.

### Signing prerequisites (operator-owned)

First inspect the certificate of the app already on the phone:

```sh
adb shell pm path dev.legato.mobile
# Copy the returned base.apk path, without the package: prefix:
adb pull /data/app/.../base.apk installed-legato.apk
"$ANDROID_HOME/build-tools/36.0.0/apksigner" verify --print-certs installed-legato.apk
```

Compare its SHA-256 certificate digest with the chosen private signer's digest.
A same-key update can retain app data; a key switch requires a deliberate
uninstall, destroying saved servers, pairing tokens and approval keys. Pair and
enroll again afterwards. This is never an automatic migration. Explicitly
configure `approval_devices.android_app_digests` on each server to trust the
approved app certificate; publishing an APK does not change server trust.

The operator provisions the GitHub Actions environment `android-release` with
secrets `ANDROID_KEYSTORE_BASE64`, `ANDROID_STORE_PASSWORD`, `ANDROID_KEY_ALIAS`
and `ANDROID_KEY_PASSWORD`, plus the public environment variable
`ANDROID_CERT_SHA256`. Keep the durable private keystore backed up outside the
repository. The workflow decodes it into a temporary file and maps these values
to Gradle's `LEGATO_ANDROID_KEYSTORE`, `LEGATO_ANDROID_STORE_PASSWORD`,
`LEGATO_ANDROID_KEY_ALIAS` and `LEGATO_ANDROID_KEY_PASSWORD`.
`ANDROID_CERT_SHA256` becomes `LEGATO_ANDROID_CERT_SHA256`: the release helper
requires this private signer's certificate pin, verifies a single matching
signer, and rejects the bundled public debug certificate. Official tagged
builds never fall back to local signing properties or the debug key.

### Policy and assets

Run the release workflow on `main` with `android=auto|always|never` (default
`auto`), for example:

```sh
gh workflow run release -f bump=patch -f android=always
```

- `auto` builds the first Android publication, then compares against the most
  recently published stable, non-draft release carrying an uploaded, nonempty
  `legato-android.apk`. The helper paginates **all** releases, not just the newest
  100. Missing baseline tags are fetched from `origin`; API, fetch and diff
  failures are visible failures, never inferred skips.
- `always` forces Android publication; `never` skips it.

The exact `INPUTS` list in `scripts/android-release.py` is `mobile/`,
`themes/palettes.json`, `scripts/mobile-android.sh`, `scripts/android-release.py`
and `.github/workflows/release.yml`. These cover the app, packaged palette,
build launcher, release policy and workflow. Unrelated server or web UI changes
do not trigger an APK: the app loads the server's web UI at runtime.

Only stable `vMAJOR.MINOR.PATCH` tags without leading zeros are accepted.
`versionName` is the tag without `v`; `versionCode` is
`(major + 1) * 1,000,000 + minor * 1,000 + patch`, with major at most 2098 and
minor/patch at most 999. Preflight rejects publication below an already
published Android version code.

The canonical assets on the same GitHub release are `legato-android.apk` and
the Android-only `legato-android-SHA256SUMS`. This is one universal, directly
installable APK, not an AAB or a set of splits. The workflow verifies the packaged
`dev.legato.mobile` identity, tag-derived version and pinned certificate before
attesting build provenance and uploading. The CLI's `SHA256SUMS` is separate.
Verify a downloaded APK from that tag with:

```sh
sha256sum -c legato-android-SHA256SUMS
gh attestation verify legato-android.apk --repo cpave3/legato
```

The CLI release is published before the Android job runs. An Android failure
can therefore leave a valid CLI release without complete Android assets.
After fixing prerequisites, rerun **only the failed Android job** on that same
workflow run/tag, never all jobs: the CLI job would calculate another version.
The helper's `stage` reuses an immutable existing APK and checks its signature
and version instead of rebuilding it. Publication regenerates a missing
checksum from those exact bytes; existing APKs/checksums are never clobbered.
A conflicting APK or checksum fails and requires investigation.
An upload returning HTTP 502 can leave an empty asset in GitHub's `starter`
state. Recovery treats that placeholder as unpublished: it rebuilds a missing
APK or regenerates the checksum from the completed original APK. Only during
publication, after verifying the local APK, it rechecks the placeholder's exact
asset ID, canonical name, `starter` state and zero size before deleting that
failed placeholder and retrying the upload. A changed state, cleanup error,
empty `uploaded` asset or any other unexpected state fails for investigation;
completed assets and unrelated platform assets are never deleted or replaced.

### Obtainium (after first publication)

There is no in-app Android updater. In Obtainium, add
`https://github.com/cpave3/legato` as a **GitHub** source and set:

- **Filter links by regex:** `(^|/)legato-android\.apk$`; inversion **OFF**.
- **Fallback to older releases:** **ON**.
- **Include prereleases** and **Include drafts:** **OFF**.
- **Track-only:** **OFF**.
- **Sort method:** explicitly **Release date**.
- **Release title as version:** **OFF** (use the tag).

Allow Obtainium to install apps when Android asks. Whether an update can install
without confirmation depends on Android permissions and Obtainium's installation
method; silent installation is not guaranteed.

With fallback enabled, newer APK-less CLI releases are skipped automatically,
but only within the fetched window. Obtainium's GitHub source fetches at most
100 releases with **no pagination**; it does not search indefinitely. Publish
with `android=always` before the last APK falls outside that window. If it is
already outside, manually download from the last APK-bearing tag; the operator
must publish a fresh Android release to restore discovery.

These settings and limits were checked against Obtainium's
[official source wiki](https://wiki.obtainium.imranr.dev/sources/) and
[GitHub source implementation](https://github.com/ImranR98/Obtainium/blob/main/lib/app_sources/github.dart).

### Validation and remaining acceptance

```sh
# Set JAVA_HOME to JDK 21 and ANDROID_HOME to an SDK with platform/build-tools 36.
bash scripts/test-android-apk.sh
```

This script uses a disposable key and real Gradle builds for two versions,
verifies their certificate/version and packaged palette, and never installs or
uploads. It is not a phone upgrade test.

Production acceptance remains blocked on Cameron's signer choice and setup.
Operator checks still required: a fresh phone install, a same-signer upgrade
preserving saved servers, pairing and approval keys, and real Obtainium updates
across APK-less releases. None is established by the packaging test or by the
implemented release workflow.

## The code

All of it is in `app/src/main/java/dev/legato/mobile/`.

| File | What it does |
| --- | --- |
| `ServerAddress.kt` | Server URLs and the `legato pair` QR code |
| `QrScanner.kt`, `QrDecoder.kt` | Scanning a pairing QR code with the app's own camera (CameraX, ZXing) |
| `Pairing.kt` | SPAKE2, the pairing word list, reading a typed phrase |
| `PhrasePairing.kt` | The phone's side of pairing by phrase; trusting a pinned certificate |
| `Discovery.kt`, `AndroidDiscovery.kt` | Servers announced on the local network |
| `AndroidPairing.kt` | Pairing over HTTP(S), and pinned trust for the app's own requests |
| `PageContract.kt` | What the app and the web interface say to each other |
| `PageKeeper.kt` | Getting the page back after Android reclaims it |
| `ShellVoice.kt` | Recording for the page |
| `ShellVoiceStream.kt` | Streaming the microphone to the page for a conversation with the Reeve |
| `ReeveVoice.kt` | Which saved server is the main, from `/api/reeve/voice` |
| `ConnectionStore.kt` | The saved servers |
| `ServerProbe.kt` | `/health` and `/api/meta` |
| `ShellWorkers.kt` | Pairing another saved server as a worker of the page's server |
| `ServerPage.kt` | The WebView, wired to the files above |
| `ApprovalSigning.kt` | Signed approvals: the server's endpoints, the key's parameters, enrollment, the signed message, what the sheet shows |
| `AndroidApprovalKeys.kt` | The approval keys in the Keystore, the fingerprint prompt, calls to a server's API |
| `ApprovalSheet.kt`, `ApprovalsScreen.kt` | The approval sheet with its seal, and the open approvals of every server |
| `AndroidStorage.kt`, `AndroidMicrophone.kt` | Keystore, SQLite, `MediaRecorder`, `AudioRecord` and audio focus |
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
request: `legato://pair?url=…&phrase=…`. Scanned with the app's own camera
(`QrScanner.kt`: CameraX, and ZXing's decoder in `QrDecoder.kt`, which also
reads a code drawn light on dark, as a terminal shows it; the camera permission
is asked for when **Scan** is tapped; nothing depends on Google Play services)
or opened as a link, it fills in the address and the words
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

## Appearance

On the server list, **Appearance › Theme** selects **Legato**, **Dracula**,
**Ocular Light**, **Ocular Dark**, **Ocular Light Vivid**, or **Ocular Dark Vivid**. The choice applies to the native screens,
system bars and the open server page. It is kept across app restarts, for all
saved servers. Legato remains the existing dark default; this does not add or
change an Android system-theme mode.

Gradle packages the repository's `themes/palettes.json` directly as an Android
asset and JVM test resource. Alternate palettes are not copied into Kotlin;
native roles use the same readability derivation as `themes/render.ts`: muted
and comment fall back to text below 4.5:1 against canvas; border falls back to
focus below 3:1 against selection. Original Ocular tokens stay unchanged. The additive Vivid variants keep their neutral bases and use stronger shared accents. The installed app needs no
repository files or palette download. The launch splash keeps its existing
Legato background; the saved theme takes effect when the activity starts.

Depth is separate from those readable tokens: `AppTheme.materials` resolves
the canonical light/dark `materials.colors` recipes against the readable
tokens or literal hex colors, blending encoded sRGB bytes and rounding each
channel with `Math.round`. Native chrome uses rail, grouped content uses
panel, menus and sheets use raised, and decorative lines use divider. Error
containers use errorSurface; the HTTPS badge uses successSurface. Reading
ground and page backgrounds stay canvas. Required input/control boundaries
keep the readable border (`Lw.lineStrong`), not the decorative divider.
Material3 containers map lowest to ground, low to rail, regular to panel,
and high/highest to raised. Android keeps native Material3 elevation rather
than copying web shadow recipes. Legato's existing default stays unchanged.
`AppThemeTest` selects each alternate theme through the real catalog/settings
and checks distinct layers and shared-recipe parity.

Native text uses a separate `AppTheme.inks` copy of the readable tokens.
For each `material_ink_contract.roles` entry, it starts from the original
canonical ink and takes the first bytewise blend
toward text, in `blend_step` increments, that meets `minimum_contrast` (4.5:1)
on canvas, selection and every material color except divider. This matches
`themeMaterial().inks`; material colors themselves do not change. Focus,
boundaries and backgrounds keep their readable values. Canonical/readable
`tokens`, terminal colors, body foreground and reading/page canvas stay unchanged.
The catalog/settings tests check semantic error/link/success/warning/keyword
contrast on these backgrounds and preserve the source tokens and page canvas.

Each theme's `ui` role references and surface recipes come from the same JSON. Identity/notice surfaces join the material backgrounds before ink contrast derivation; native primary/focus uses the derived UI accent instead of the independent source focus token. Ocular action/signing identity is purple, success remains green, and real links remain blue. Dracula keeps its previous mappings; Legato's fallbacks are unchanged. The web voice widget consumes the same shared roles when embedded.

The app sets `localStorage['legato:app-theme']` before each page loads, with
the stable ID `legato`, `dracula`, `ocular-light`, or `ocular-dark`. A native
change updates that key and dispatches `legato:theme-changed` immediately;
future loads and renderer recovery use the latest choice too. Servers with
the app-theme web support apply it to the page.

Changing Theme in web Settings uses an explicit request, not an event listener:
`window.legatoShell.theme === 1` advertises support, and the page posts
`{legatoShell: "theme", type: "theme", key, id: "ocular-light"}` through
`ReactNativeWebView.postMessage`. The origin-restricted, main-frame bridge
and the page's session key protect the request. Only the four stable IDs are
accepted; an invalid keyed ID receives an error, never success.

Both pickers use the same serialized, off-main-thread persistence path. The
app checks SharedPreferences `commit()` before changing its native appearance
or delivering the storage key and `legato:theme-changed` event. The requester
then receives `legato:shell-theme` with detail `{id}` on success, or
`{id, error: string}` on failure. Failed saves leave the applied theme unchanged
and surface an error in the native server list as well. Repeated selections
also check persistence. All theme scripts check the server origin and top frame;
there is no event-to-native listener to echo native updates back to the app.
On WebViews without the message-listener feature, the native picker still
controls the page but web-to-native synchronization is unavailable.

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
snapshot, and reconnects its WebSocket at once if the connection dropped while
the app was away, showing "Reconnecting…" instead of Connection Lost. A page on screen while the person is in the server list counts as in
the background, so coming back to it does the same.

When the app goes to the background (another app, the server list, or the
screen turned off), it dispatches `legato:shell-leave` in the page and pauses
the WebView (`WebView.onPause`); five seconds later it pauses the page's timers
too (`pauseTimers`). On `legato:shell-leave` the web interface stops following
the agents it shows: the terminal stream and the native agent panel unsubscribe
(`usePageInView` in `web/src/hooks/usePageInView.ts`). On return the app resumes
the WebView before asking the page anything, and the interface subscribes again:
a native panel from the last event it had, or from its snapshot when the server
no longer holds what it missed. Before this, a phone with its screen off kept
receiving a busy agent's whole conversation, about 100 MB an hour.

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

## Talking with the Reeve

The web UI's voice sheet holds a spoken conversation with the main's Reeve
(see `docs/analysis/reeve-voice-conversation.md`). The page runs the session
and plays the Reeve's lines with `<audio>`; the app only listens for it
(`ShellVoiceStream.kt`, `AndroidAudioStream` and `AndroidAudioFocus` in
`AndroidMicrophone.kt`). Dictation keeps its own `MediaRecorder`.

- The app tells the page it can: `window.legatoShell.voiceStream === 1`.
- `{legatoShell: "voice-stream", key, action: "start"}` asks for the
  microphone as dictation does, takes the audio focus
  (`AUDIOFOCUS_GAIN_TRANSIENT`, so music pauses), and records with
  `AudioRecord` (`VOICE_RECOGNITION`, 16 kHz mono 16-bit). The app answers on
  `legato:shell-voice`: `stream-started`, then a `frame` every 100 ms
  (`data`: 3,200 bytes of little-endian samples, base64).
- `action: "stop"` stops recording and gives the focus back, answered with
  `stream-stopped`. The app stops by itself, with `stream-stopped` too, when
  it leaves the foreground, when a call takes the focus
  (`AUDIOFOCUS_LOSS_TRANSIENT`), and when the page loads another document,
  or when the person answers the microphone dialog after the page stopped.
  A loss of focus for good does not stop it, since the page's own `<audio>`
  (a line of the Reeve's) takes the focus so in the WebView; nor does a loss
  it may duck under. Anything that fails is an `error` with a `message`.
- **Which server is the main.** The app asks each saved server
  `GET /api/reeve/voice` with its token and keeps the instance's ID and role
  with the connection. The main is the one server whose role is `main`; else,
  with one server saved, that server; else none (`voiceMain` in
  `ReeveVoice.kt`). It asks when the server list refreshes and when a page
  asks for the main.
- **From a worker's page.** `{legatoShell: "open-voice", key}` opens the
  main's page at `/#reeve-voice`, which opens the sheet there. When the app
  knows no main, it answers with an `error` and stays where it is.
- Only requests with the page's key from its top frame are obeyed.

## Approval links

A notification of a new approval opens `legato://approval?url=<server>&id=<approval>` (see `docs/ai/approvals.md`). When this phone signs approvals for that server, the app opens the approval sheet (see Signed approvals). Otherwise it opens the saved server at that address, or the only server saved, at `/?approval=<id>`, where the web UI's tray shows the request; an open page of that server goes there without reloading. A link the app cannot read, or for a server it does not know, says so on the server list (`parseApprovalUri`, `serverForApproval` in `ServerAddress.kt`).

## Signed approvals

Some approvals can be decided only by a person at an enrolled phone: the
server refuses an allow without a signature from one (see
`docs/ai/approvals.md` § Signed approvals). The phone signs with a key it holds
for that server, which the person's fingerprint or PIN unlocks for one
signature.

- **The key.** One for each saved server, alias `approval-<connection id>`:
  EC P-256, for signing only, in StrongBox when the phone has one, unlocked for
  each use (fingerprint or PIN from Android 11, fingerprint on Android 10). It is made with the server's
  enrollment challenge, so its attestation chain proves it is new and in secure
  hardware. Removing the server deletes it.
- **Enrolling.** The app asks `POST /api/devices/challenge`, makes the key,
  and sends its chain to `POST /api/devices`. The first phone of a server is
  enrolled at once; a later one waits for an enrolled phone to allow it. The
  device's ID is kept with the connection (`approvalDevice`). Settings ›
  Devices in the page starts it (`{legatoShell: "enroll", key}`, answered with
  `legato:shell-enroll`), and so does the sheet.
- **The sheet** (`ApprovalSheet.kt`). The app asks the server itself for the
  approval (`POST /api/approvals/{id}/challenge`) and draws only what that
  returns: the canonical request and a one-time challenge. The page passes
  nothing but the ID, so a page cannot change what is signed. It shows the
  scope in Legato's words, the agent's reason quoted and marked as the
  agent's, and a button for each way to allow. A choice opens the system's
  fingerprint prompt, titled with the option and the scope, never the reason;
  the unlocked key signs `{"challenge","device","option","request","v":1}` and
  the decision goes to `POST /api/approvals/{id}/decision`. Deny needs no
  signature.
- **The seal.** A signed decision ends on a seal that draws itself: a dial of
  ticks lights up as an arc sweeps from violet to green, a hexagon fills and a
  check lands with a haptic tick, then a readout types out the option, the key
  (and whether it is in StrongBox), the start of the hash of what was signed,
  of the signature, and the server.
- **From the page.** A tray card of a signed approval inside the app posts
  `{legatoShell: "approval", key, id}` (the ID must look like `ap-` and 8 hex
  characters); the app opens the sheet and answers with `legato:shell-approval`
  (`status` of `decided`, `cancelled` or `error`, with `option` or `error`).
- **From a notification.** The approval link opens the sheet when this phone
  signs for that server, and the page otherwise (see Approval links).
- **The list.** Approvals on the server list shows the open approvals of every
  saved server, reread every 10 seconds; a signed one opens the sheet, another
  opens its server's page on it. A server that does not answer says why.
- **A new fingerprint.** From Android 11 the key keeps working: Android
  retires a key on a new fingerprint only when a fingerprint alone unlocks
  it, and this one takes the PIN too. That costs no protection, since adding
  a fingerprint needs the PIN, which unlocks the key anyway (checked on a
  Pixel 9 Pro). On Android 10 the key is fingerprint-only and is retired: the
  app deletes it, forgets the device, and offers to enroll again, which an
  enrolled phone or passkey, or `legato device reset` at the host, allows.

## Adding a worker from another saved server

A main's Settings › Handoff › Workers › **Add a worker** can pair one of the
app's other servers as a worker, without opening "Pair a main instance" on the
worker and typing its address and phrase (`ShellWorkers.kt`). The page never
sees another server's token: the app holds it, opens the pairing request and
asks the person itself. It is the contract the desktop app shares (see
`docs/ai/desktop.md`); the web side is `shellWorkers` in
`web/src/lib/mobileShell.ts`.

- The page is told `window.legatoShell.workers === 1`.
- `{legatoShell: "workerCandidates", key, id}` (the contract names the kind
  `type`; the app reads either) is answered with a `legato:shell-worker` event,
  detail `{id, servers: [{key, name, url}]}`: every saved server except the
  page's own, `key` being the connection's ID.
- `{legatoShell: "openWorkerPairing", key, id, server, main}` opens the app's
  own dialog, over the page and out of its reach: "Make <server> a worker of
  <main>? <main> will be able to hand tasks to <server>, which runs their
  agents there." (`main` is the page's name for its server, else the saved
  name.) On **Make it a worker** the app calls `POST <server>/api/pairing` with
  `{"purpose":"main"}`, that server's token and pinned certificate, and answers
  `{id, address, phrase, expires_at}`: `address` is the app's URL for the
  server, `phrase` and `expires_at` as the server sent them. On **Cancel**:
  `{id, cancelled: true}`. On a failure: `{id, error}`, in the server's words
  where it gave any.
- A server from before handoff ignores the purpose and opens a phone's request;
  the app withdraws it (`DELETE /api/pairing`) and says the server is too old
  to be a worker.
- The page then gives its own server the address and phrase, as the manual
  form does, and the two servers pair between themselves.
- Only a request with the page's key from its top frame is obeyed. A new
  request replaces a confirm still open, and closing the page cancels it.

## File uploads and links

A file input in the page opens Android's file picker. A link to another site
opens in the browser; a link that opens a new window opens in the page, where
the same rule applies.
