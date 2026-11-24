# Legato desktop app

Every Legato server in one window: a dock of servers, each one's own web UI beside it, pairing by four words with the server's certificate pinned, and approvals signed with a touch of a security key, or on a Mac with Touch ID. Tauri 2 (Rust, `src-tauri/`) with a React and TypeScript shell (`src/`).

What it does for a person is on the docs site: [The desktop app](https://cpave3.github.io/legato/running/desktop-app/). How it works is in [docs/ai/desktop.md](../docs/ai/desktop.md).

## Building

You need Rust, Node 24 with pnpm 11, and on Linux the WebKitGTK 4.1, GTK 3, librsvg, libudev and Ayatana AppIndicator (`libayatana-appindicator3-dev`; Tauri's bundler asks for it because of the tray icon, though the app uses KSNI) development packages. On a Mac, Xcode or its command line tools (`xcode-select --install`), which build the Swift in `src-tauri/touchid/`.

```bash
task desktop:test    # Rust unit tests and the TypeScript check
task desktop:dev     # run it, the shell reloading as you edit
task desktop:build   # an AppImage in src-tauri/target/release/bundle/appimage/, with GStreamer's plugins
```

To try it without touching your own servers or saved state, run a scratch Legato (see `docs/ai/claude-code-native.md` § Trying it without touching real state) and point the app at a scratch state file:

```bash
LEGATO_DESKTOP_STATE=/tmp/desktop-state.json task desktop:dev
```

Tokens still go to your keyring under `dev.legato.desktop`; removing a server in the app deletes its token.

To try updating on a Mac, build an older version, put it in Applications, and let it update to the newest release that carries a Mac build:

```bash
LEGATO_DESKTOP_VERSION=v0.0.1 pnpm tauri build --bundles app
cp -R src-tauri/target/release/bundle/macos/Legato.app /Applications/
```

## Layout

| File | What it is |
| --- | --- |
| `src-tauri/src/app.rs` | The window, the webviews, and the commands the shell calls |
| `src-tauri/src/pairing.rs` | SPAKE2 pairing by phrase, held to `tests/fixtures/pairing-exchange.json` |
| `src-tauri/src/legato.rs` | A server's API over its pinned certificate, and pairing |
| `src-tauri/src/pin.rs` | Whether a certificate is the pinned one or issued by it |
| `src-tauri/src/webauthn.rs` | The WebAuthn messages Legato checks |
| `src-tauri/src/fido.rs` | Security keys over CTAP2 |
| `src-tauri/src/bridge.rs` | What a server's page and the shell say to each other: the page hands a signed approval to the sheet |
| `src-tauri/src/systrust.rs` | On a Mac, whether the system trusts a server's page, and trusting its CA |
| `src-tauri/src/touchid.rs` | Touch ID: a Secure Enclave key, as a WebAuthn authenticator |
| `src-tauri/touchid/` | The Swift that reaches the Secure Enclave, linked on a Mac only |
| `src-tauri/src/store.rs` | The state file and the keyring |
| `src-tauri/src/discovery.rs` | Servers on the network over mDNS |
| `src-tauri/src/updates.rs` | The AppImage, and Legato.app on a Mac, updating themselves from GitHub releases |
| `src/` | The shell: title bar, dock, panels, the approval sheet |

`tests/fixtures/pairing-exchange.json` is a copy of the server's recorded exchange: `go test ./internal/engine/pairing` fails when it differs, and `-update` rewrites it. `resources/pairing-words.txt` is the server's word list; a test here checks its digest against the one the Go test holds. `tests/fixtures/touch-id-key.json` is what the app builds for a known Touch ID key: `LEGATO_UPDATE_FIXTURES=1 cargo test --lib` rewrites it, and `go test ./internal/service` enrolls and signs with it.

## Follow server appearance

Servers & keys → Appearance → **App theme** offers **Follow server** as well as
named themes. Follow server preserves the saved local choice as fallback. The
picker shows the resolved theme; selecting a named theme exits Follow server.

Native code reads `theme_preference: {id, revision}` from the existing pinned
`GET /api/meta` poll. Each saved server keeps its own nonsecret snapshot in
`state.json`. An offline server keeps only its own cache. Missing metadata
(older servers), no preference (`id: ""`), unknown IDs and no active server use
the local choice. Removing a server removes its cache. Mode is persisted in
`follow_server_theme`, separately from local `app_theme`.

The shell and widget resolve from the active server. A retained or newly loaded
server page resolves from its own server, not the page in front. Native delivery
writes `legato:theme-mode` (`server` or `local`), `legato:app-theme` (the preserved
local ID), and `legato:effective-app-theme`, then sends `legato:theme-changed`.
The keyed, top-frame, origin-guarded bridge advertises `legatoShell.theme = 2`;
`id: "server"` selects Follow server, and named IDs keep working for v1 callers.
`legato:shell-theme` replies with the requested `{id, error?}`, including `server`.
Remote pages have no additional Tauri capability and cannot set server preferences.

Headless checks exercise native polling against a local HTTP fixture, saved-state
reopen/failures, selection/switch/late-response behavior, and the actual guard and
delivery scripts in Node. They do not prove native WebKit/WKWebView load timing,
visible repaint, platform chrome or native picker rendering. No GUI or live
Legato instance is required by these checks.
