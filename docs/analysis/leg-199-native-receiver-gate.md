# LEG-199 native receiver feasibility gate

Status: implementation may proceed under approved Legato plan revision 3. This is a repository inspection, not native platform acceptance. No app was installed, no provider registration/login was performed, and no live instance was changed.

## Desktop: existing seams, receiver still missing

- `desktop/src-tauri/Cargo.toml:57` enables Tokio networking. Existing loopback test listeners in `src/legato.rs` and `src/voiceclient.rs` show the available socket API; they are not an OAuth receiver.
- `desktop/src-tauri/src/legato.rs:105` constructs the server client, requiring a pinned certificate for HTTPS. Its `post` operation can deliver a bounded response to the server recorded at begin time.
- `desktop/src-tauri/src/links.rs:43` opens the system browser without a shell. `src/bridge.rs:55` supplies the existing trusted-page bridge pattern.
- The registered `legato-desktop` deep link currently handles pairing/import/voice, not OAuth. It is not evidence that an MCP provider accepts that URI scheme.

Next desktop slice: implement a native, ephemeral IP-loopback receiver and in-memory transaction-to-paired-server mapping. Enforce HTTPS for non-loopback delivery even if another app feature permits plain HTTP. Keep codes out of the webview. Test a fake provider redirect through the real receiver and pinned HTTP client into an isolated Go handler, including wrong server, denial, replay and cancellation. Do not infer phone support from these tests.

## Android: transport exists; callback profile is unverified

- `mobile/app/src/main/AndroidManifest.xml:30` registers `legato://pair` and `legato://approval`, not a verified HTTPS App Link or OAuth receiver.
- `mobile/app/src/main/java/dev/legato/mobile/MainActivity.kt:91` receives intents; `handlePairingLink` handles those existing link types. `ACTION_VIEW` provides a browser launch seam. Foreground/background callbacks currently update page visibility, not receiver ownership.
- `mobile/app/src/main/java/dev/legato/mobile/AndroidApprovalKeys.kt:130` provides authenticated server HTTP with `PinnedTrust.apply`. `ConnectionStore.kt` records each server's address, certificate and token. Use the transaction's saved server rather than the currently displayed one.
- There is no production mobile loopback receiver, OAuth return bridge, or app-association document in this checkout. The existing custom-scheme links are not interchangeable with a provider-approved redirect.

An app-owned loopback listener is a candidate, not a supported phone mode yet. Its lifetime while the system browser is foreground must be established on a device/emulator, including app freezing, process death and resume. If that requires a foreground service or a different OS callback profile, its permissions, notification and provider registration constraints need a separate explicit design decision. A hosted App Links domain must not be invented as an implicit dependency.

## Verification boundary

The plan still excludes installs, upstream registrations/logins, deployment, DNS/firewall/tunnel changes and running-instance restarts. Therefore installed Android browser/background acceptance cannot run within the present authorization. Request a bounded isolated device/emulator verification scope before doing that work; do not use an operator's live app or settings. Mock-provider tests can prove protocol handling, but not mobile OS lifecycle or universal provider redirect acceptance.

Until that gate passes, report Android OAuth setup as unverified/unsupported. Remote browser/PWA without an app remains unsupported for OAuth setup under revision 3. Existing credentials can still be used and refreshed server-side once the remaining broker/token lifecycle is implemented. No public callback or pasted-code fallback is added to bypass this gate.

Provider restrictions and source citations remain in `leg-199-mcp-broker-oauth.md` and the superseding revision-3 plan at `.legato/plans/leg-199-mcp-broker/plan.md`.
