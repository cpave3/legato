# Legato desktop app

Every Legato server in one window: a dock of servers, each one's own web UI beside it, pairing by four words with the server's certificate pinned, and approvals signed with a touch of a security key, or on a Mac with Touch ID. Tauri 2 (Rust, `src-tauri/`) with a React and TypeScript shell (`src/`).

What it does for a person is on the docs site: [The desktop app](https://cpave3.github.io/legato/running/desktop-app/). How it works is in [docs/ai/desktop.md](../docs/ai/desktop.md).

## Building

You need Rust, Node 24 with pnpm 11, and on Linux the WebKitGTK 4.1, GTK 3, librsvg and libudev development packages. On a Mac, Xcode or its command line tools (`xcode-select --install`), which build the Swift in `src-tauri/touchid/`.

```bash
task desktop:test    # Rust unit tests and the TypeScript check
task desktop:dev     # run it, the shell reloading as you edit
task desktop:build   # an AppImage in src-tauri/target/release/bundle/appimage/
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
| `src-tauri/src/systrust.rs` | On a Mac, whether the system trusts a server's page, and trusting its CA |
| `src-tauri/src/touchid.rs` | Touch ID: a Secure Enclave key, as a WebAuthn authenticator |
| `src-tauri/touchid/` | The Swift that reaches the Secure Enclave, linked on a Mac only |
| `src-tauri/src/store.rs` | The state file and the keyring |
| `src-tauri/src/discovery.rs` | Servers on the network over mDNS |
| `src-tauri/src/updates.rs` | The AppImage, and Legato.app on a Mac, updating themselves from GitHub releases |
| `src/` | The shell: title bar, dock, panels, the approval sheet |

`tests/fixtures/pairing-exchange.json` is a copy of the server's recorded exchange: `go test ./internal/engine/pairing` fails when it differs, and `-update` rewrites it. `resources/pairing-words.txt` is the server's word list; a test here checks its digest against the one the Go test holds. `tests/fixtures/touch-id-key.json` is what the app builds for a known Touch ID key: `LEGATO_UPDATE_FIXTURES=1 cargo test --lib` rewrites it, and `go test ./internal/service` enrolls and signs with it.
