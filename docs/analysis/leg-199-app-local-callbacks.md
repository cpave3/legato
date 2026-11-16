# LEG-199: app or local-web callback decision

Date: 2026-11-15. Current proposal: `.legato/plans/leg-199-mcp-broker/plan.md`, revision 3. No feature implementation.

## Settled scope

Cameron stipulates two supported OAuth setup paths:

1. **Native Legato app:** the app receives the provider's authorization response and forwards it over its authenticated connection to the initiating Legato. That Legato can be remote.
2. **Web UI without app:** the frontend asks the Go server to start a temporary localhost receiver. The frontend opens consent in the current browser. This works only when that browser and the Go server share a machine.

No smart topology detection is required. The web path can be attempted remotely but is unsupported; localhost will address the browser's machine and the attempt will fail/expire. Explain this assumption before consent. Do not add public server callbacks, callback-origin onboarding, automatic proxies, hosted relays or pasted-callback recovery.

This narrows connection setup, not token custody: **Legato remains the MCP/OAuth client.** It creates PKCE/state, exchanges the code, stores tokens and refreshes after the app is closed. The app briefly handles the authorization response, never provider tokens or the verifier. Agent grants and audit remain server-owned and default-deny.

## Native app → remote server

The app prepares a permitted native receiver before consent. It and the server bind the transaction to the selected paired instance, connection generation, receiver, exact redirect, issuer/resource/registration and operator authority. The app opens the authorization URL received directly from that server.

The browser returns to the app. The app posts code/state/issuer or denial in an authenticated bounded request body to its recorded destination, with a per-attempt receiver capability. The callback cannot supply a server URL or use whichever server the UI happens to show later. The server validates and exchanges with its own verifier and the **original app redirect URI**.

Authenticate account/resource confirmation before activating credentials. Cancellation, expiry, authority revocation and disconnect invalidate unfinished attempts. App-side removal cancels when the server is reachable; offline removal relies on expiry and must never reassign delivery. Use pinned HTTPS for non-loopback code delivery, not plain LAN HTTP. Duplicate delivery is idempotent; a lost native code/receiver requires a fresh flow, not manual input.

Native desktop loopback is the initial receiver mechanism. Phone receiver handling must be verified separately: Android currently has pairing/approval links, not OAuth handling. A TCP receiver surviving browser handoff or a provider-permitted OS callback needs platform acceptance before support is claimed. This is a bounded feasibility gate, not a topology-detection service. Do not invent a universal custom scheme, app-association domain or provider registration.

## Browser-only → local Go server

The frontend requests begin with operator authorization. Go binds an IP loopback listener before declaring its real callback port, creates the transaction and returns an authorization URL. The browser opens consent and follows the callback directly to that listener. Go validates/exchanges and closes the listener after settlement, cancellation or expiry. The original UI polls authenticated status, so it needs no popup/cross-origin token bridge.

Do not bind LAN/wildcard interfaces or launch a browser on the Go host. A remote no-app browser times out without silently switching modes. If a fixed permitted port is occupied, report failure rather than using an unregistered address. A process restart losing the listener is an interrupted attempt, not proof that durable OAuth state can recreate a safe receiver.

## Registration still applies

Generate redirect declarations automatically through compatible pre-registration, usable CIMD or advertised DCR. A local callback does not make CIMD metadata provider-readable. The earlier per-instance public callback/CIMD publisher is no longer first-release scope. CIMD-only providers need an existing permitted public metadata profile, otherwise report the compatibility limit.

Loopback port exceptions and native callback scheme rules are provider-specific. Static-only app setup remains advanced; no returned code-bearing callback is pasted into Legato/conversation. Server-side PKCE prevents a receiver-only code thief from exchanging it, but does not replace redirect, issuer, caller and transaction checks.

## Existing seams and evidence limits

- `internal/engine/oauthclient/authorize.go:41-73`: existing Go helper binds loopback or prepares an explicit redirect. It is a reusable primitive, not the new authenticated web flow.
- `desktop/src-tauri/src/links.rs:21-30,43`: external consent can open in the system browser.
- `desktop/src-tauri/src/bridge.rs:58-95`: keyed origin/top-frame native bridge pattern. Add a narrow OAuth action; do not send codes through navigation/event payloads back into the webview.
- `desktop/src-tauri/src/applink.rs:22-43`: existing external links parse imports only, not an OAuth callback.
- `mobile/app/src/main/AndroidManifest.xml`: current external intents accept `legato://pair` and `legato://approval`, not OAuth.
- `mobile/app/src/main/java/dev/legato/mobile/PageContract.kt:12-35`: mobile keyed top-frame/server-origin bridge pattern. It currently advertises no OAuth receiver capability.

Historical local TLS scenarios in `leg-199-mcp-scenarios.md` remain useful for delayed callbacks, wrong instance, registration and SDK risks. They do not validate native delivery, phone background behavior or the new local-web entry point. New acceptance in the plan names these missing end-to-end paths explicitly.

References retained from the original research: RFC 8252 external browser/loopback guidance (https://www.rfc-editor.org/rfc/rfc8252.html) and current MCP registration constraints (https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/client-registration). App-assisted code delivery is the proposed Legato architecture; these sources do not guarantee every provider permits it.

The old 48-point estimate no longer describes unchanged work. Desktop/Android receiver and relay slices need decomposition and re-estimation. Overall plan approval remains pending. No installs, registrations, logins, deployments, live edits or running-process actions occurred.
