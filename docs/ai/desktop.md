# Desktop app

`desktop/` is Legato's desktop app: Tauri 2 (Rust) with a React and TypeScript shell. It shows every paired Legato server in one window, pairs by phrase with the server's certificate pinned, and signs approvals with a security key, or on a Mac with Touch ID. The person's page is `site/src/content/docs/running/desktop-app.md`; how to build and run it is in `desktop/README.md`.

## The window

`src-tauri/src/app.rs`. One frameless window (`decorations(false)`) holds:

- **The shell** (`src/`, React): the title bar, the dock, the Servers & keys panel and the approval sheet, across the whole window. It is the only webview with IPC (`capabilities/shell.json` names the `shell` webview; server pages are remote and get nothing).
- **A webview per server** (`server-<id>`), made the first time the server is shown and kept, over the content area: right of the dock (68px), under the title bar (40px), short of the right and bottom edges by 4px, where the shell's resize strips are. Only the active one is shown; `set_content_visible(false)` hides it while the shell draws a panel or the sheet over the content area.
- **Linux layout.** Tauri packs a window's webviews into a vertical `gtk::Box`, one under the other, and wry moves a webview only inside a `gtk::Fixed`. So `overlay::install` puts a `gtk::Overlay` in the window's box, and `overlay::adopt` moves the shell into it as its base and each server webview over it with margins; GTK keeps them laid out as the window resizes, and `relayout` does nothing on Linux. Elsewhere `relayout` sets positions and sizes on `WindowEvent::Resized`.
- **A server's page** gets an initialization script for its own origin: the token in `localStorage["legato:token:local"]`, as the web UI keeps it, and no service worker (`register` rejects; existing registrations are removed and the page reloaded once). The web UI's service worker took over requests from the second load, and its requests did not get the pinned certificate's allowance, so they hung.
- **Pinned TLS in the page** (Linux): `load-failed-with-tls-errors` on the WebKitGTK view checks the presented certificate with `Pin::trusts` and, when it holds, `allow_tls_certificate_for_host` and reloads. WebKit applies the allowance to the host's later loads, the WebSocket included. macOS's WKWebView has no hook in wry yet, so there a page loads only when the system trusts the server's certificate.
- **A page macOS would not load** (`systrust.rs`). Before the shell shows an https server on a Mac, `page_trusted` fetches the certificate the server presents (checked against the pin, `legato::presented_certificate`) and asks `security verify-cert -p ssl -s <host>` whether the system trusts it, with the pinned CA given to build the chain but not as an anchor. Positive answers are kept for the session. When it does not, the shell draws `TrustScreen` in place of the page: **Trust it on this Mac** (`trust_server_certificate`: `security add-trusted-cert -r trustRoot -p ssl -s <host>` into the login keychain, so the CA is a root for TLS to that host only; macOS asks for the password; then the check again and a reload), **Save the certificate** (`save_server_certificate`: the pinned CA to `~/Downloads/<server> Legato CA.cer`, shown in Finder; `open_certificate` opens it in Keychain Access), the steps to trust it by hand, and **Check again**. A check that fails (the server is down) shows the page as before. Elsewhere `page_trusted` is always true.

The shell's parts: `TitleBar` (drag region, server name, reload, the update pill, `N to sign`, window buttons), `Dock` (tiles in the server's tint with an online dot and a count of approvals waiting for a signature; `+`; the key button), `ServersPanel` (servers: rename, remove; keys: plugged in or not, each server's status, enrol, revoke, add), `PairForm`, `ApprovalSheet`. The server in front tints the chrome (`--server-tint`: a wash over the title bar, the dock and the edges, the page's border); panels stay neutral. `poll` (every 3 seconds) gives each server's state, tint (kept in the state file for when it is down) and open signed approvals.

## Pairing

`src-tauri/src/pairing.rs` is SPAKE2 (RFC 9382, P-256/SHA-256/HKDF/HMAC) as `internal/engine/pairing` has it, the app as party A with the phone's identities, and `legato.rs::pair` the two requests (`POST /api/pair/exchange`, `/api/pair/confirm`). `tests/fixtures/pairing-exchange.json` is written by `go test ./internal/engine/pairing -update`, which fails when the desktop's copy differs; `resources/pairing-words.txt` is checked by a Rust test against the digest `internal/engine/pairing/phrase_test.go` holds. A phrase is read word by word by its first three letters.

Over HTTPS the certificate presented during pairing is recorded (`PinVerifier` with no pin) and must afterwards be the one the server sealed (`ca_certificate`) or be issued by it for the host, else pairing fails. The sealed certificate is the server's pin from then on.

## Talking to a server

`legato.rs::Client`: reqwest with a rustls configuration whose only verifier is `PinVerifier` (`pin.rs`: the presented leaf is the pinned certificate, or webpki finds it issued by the pinned one for the host). An `https` server without a pin is refused. Every request carries `Origin: https://legato-desktop.invalid`, which `/api/devices/passkey/options` takes as the page's origin.

## Keys

The app is its own WebAuthn client (WebKitGTK has none), over CTAP2 with `ctap-hid-fido2` (`fido.rs`), and `webauthn.rs` builds what a browser would send.

- **One credential per key.** `add_key` makes a non-resident credential on the one plugged-in key for relying party `legato-desktop.invalid` (a touch, and the key's PIN only when it insists: CTAP error `0x36`). The app keeps the credential ID and the authenticator data.
- **Enrolled on every server** by `enroll`: the server's options, client data with its challenge and the app's origin, and a `none` attestation over the kept authenticator data. No touch is needed, since the credential exists. A server keeps the origin and relying party and checks them; a server with enrolled devices makes the new one `pending` behind a `devices` approval. Pairing a server enrolls every key.
- **Signing**: `prepare` finds a plugged-in key the server knows (each plugged-in key is asked, without a touch, `up=false`, whether it holds each credential), fetches `/api/approvals/{id}/challenge` for that device and keeps the request on the Rust side. `sign` checks the option is one of the request's allow options, builds the message a phone signs (`webauthn::signed_message`), asks the key for an assertion over the SHA-256 of it with a touch and no PIN, and posts the decision `by: "desktop"`. Legato records `verification: presence` (see [Approvals](approvals.md)). The sheet draws the request from what `prepare` returned, so the page cannot change what is signed.
- **Revoking** asks every server to remove the key's device (each a signed approval) and forgets the key. `keys` drops an enrolment the server no longer lists.

### Touch ID

`src-tauri/src/touchid.rs`. On a Mac the app is the authenticator: a P-256 key in the Secure Enclave with the access control `privateKeyUsage` and `biometryCurrentSet`, so it signs only after a fingerprint. `touchid::make` asks the chip for a key and builds the authenticator data a platform authenticator gives (rpIdHash of `legato-desktop.invalid`, flags UP, UV and AT, counter 0, AAGUID zero, the credential ID, which is the SHA-256 of the public key, and the key as a COSE_Key); no fingerprint is asked for. It is enrolled as a security key's credential is, a `none` attestation over that data, with `authenticatorAttachment: platform` and transport `internal`, so Legato lists it as a `passkey`. `touchid::sign` signs the authenticator data (UP, UV, counter 0) and the client data's hash; the dialog reads "Legato is trying to sign “<title>” on <server>". Legato records `verification: verified`. The counter stays 0, which WebAuthn takes as "no counter".

- **The chip** is behind the `Enclave` trait. On a Mac it is `touchid/`, a Swift package of three C functions (`legato_touchid_available`, `_create`, `_sign`; buffers are the caller's; 0 worked, 1 failed with words, 2 cancelled) that `build.rs` builds and links with `swift-rs` when the build machine is a Mac. The key is CryptoKit's `dataRepresentation`, a blob only this Mac's chip can use, not a Keychain item: so no entitlement or developer account is needed, and every build of the app can use it (checked on a Mac with two ad hoc builds with the hardened runtime, as the release signs it). Any program running as the person can ask the chip to sign with the blob, as any can ask a security key, but not without a fingerprint.
- **One per Mac** (`add_touch_id`). `touch_id` says whether this computer can (`this_mac`: a Secure Enclave and Touch ID set up), and the panel offers **Add Touch ID** only then.
- **Signing** prefers Touch ID when the server knows it and this Mac has it, else a plugged-in security key it knows. `prepare` says which (`with`), and the sheet asks for a fingerprint or a touch.
- Adding or removing a fingerprint in System Settings invalidates the key for good (`biometryCurrentSet`): remove it and add Touch ID again.

## What the app keeps

`store.rs`: `~/.config/legato-desktop/state.json` (mode `0600`, written beside and renamed over; `LEGATO_DESKTOP_STATE` names another file): each server's name, address, sealed certificate, tint and the device each key is on it; each key's name, credential ID and authenticator data, and a Touch ID key's Secure Enclave blob (`enclave`). Tokens are in the desktop's keyring (`keyring`: Secret Service on Linux, Keychain on macOS) under service `dev.legato.desktop`, account the server's ID. Removing a server deletes its token. WebKit's data (cookies, storage) is in `~/.local/share/dev.legato.desktop/`.

## Network discovery

`discovery.rs` browses `_legato._tcp` with `mdns-sd` from start, asking again every 5 seconds for the first minute and every minute after (an early query can go unanswered, and the library's own backoff is long). A server's address is its IPv4 address (else IPv6), its port, and `https` when its `scheme` TXT record says so, as `mobile/.../Discovery.kt`. `discovered` leaves out paired servers.

## Releases and updates

The release workflow builds the app when `scripts/desktop-changed.sh` says so (the `desktop` input: `always`, `never`, or `auto`: `desktop/` changed since the newest stable release that carries the AppImage), on `ubuntu-22.04` (`legato-desktop-x86_64.AppImage`, `legato-desktop-SHA256SUMS`) and `macos-14` (`legato-desktop-macos-arm64.dmg`, ad hoc signed, not notarized), with build provenance, attached to the release Legato's job published. The version is the release's: `LEGATO_DESKTOP_VERSION` at build time (`updates::version`, else `dev`) and the bundle's version through `--config`.

`updates.rs`: `check_update` (at start and every 6 hours; nothing for `dev`) lists the repository's releases and takes the newest stable one with the AppImage and the checksums (`newest_with`), since not every release has a desktop build. `apply_update` (only when started as an AppImage, `$APPIMAGE`) downloads both, checks the AppImage's SHA-256, and `install` writes it beside the running one, keeps the old one as `.prev` and renames the new one over it; `restart_app` starts it and exits. Like Legato's own updater, the checksum comes from the same release.

## Tests

`task desktop:test`: `cargo test --lib` (pairing against the shared fixture, the pin check against certificates `rcgen` makes, the WebAuthn messages, Touch ID's authenticator data and signatures with a software key in place of the chip, the state file, discovery's addresses, the updater's choice, checksum and swap) and `tsc -b`. `tests/fixtures/touch-id-key.json` records what the app builds for a known key (`LEGATO_UPDATE_FIXTURES=1 cargo test --lib` rewrites it); `internal/service`'s `TestADesktopTouchIDKeyEnrollsAndSignsAsVerified` enrolls and signs with those bytes through Legato's own checks. The Swift in `touchid/` builds only on a Mac. The window, the keys and pairing with a server are checked by running the app against a scratch Legato (`task desktop:dev`, `LEGATO_DESKTOP_STATE` on a scratch file).

## Known limits

- macOS: no pinned TLS in server pages (the trust screen has the Mac trust the CA instead), no self-update; the app is untested on a Mac (Touch ID's Secure Enclave key was tried there alone). No Windows build.
- One key at a time is added; signing finds the right key among several plugged in.
- The server webview is hidden while a panel or the sheet is open, rather than drawn under it.
