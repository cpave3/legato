# LEG-199: server-owned MCP broker with app or local-web OAuth

## Solution

Legato remains the MCP/OAuth client and owns credentials. OAuth connection setup is supported through a native Legato app, or through a web browser on the same machine as the Go server. The app receives the authorization response and forwards it to the initiating server. Without an app, the frontend asks the Go server to start a loopback receiver, then opens consent in the current browser.

**Proposal revision 3; no feature implementation has started.** This follows Cameron's direct scope decision. It replaces the public server-callback design in revisions 1–2. Authenticated account/resource confirmation, selected on revision 1, remains required.

## Supported setup modes

| Mode | Receiver | Code delivery | Supported assumption |
| --- | --- | --- | --- |
| Native desktop app, local or remote Legato | App-owned loopback receiver on the desktop | Native app posts authorization response to its recorded paired server | Browser and receiver share the desktop; app can reach its server |
| Native phone app, local or remote Legato | App-owned native callback receiver | Native app posts authorization response to its recorded paired server | Platform and provider support the selected callback profile |
| Web UI with no native app | Temporary loopback listener owned by the Go server | Current browser redirects directly to that listener | Browser and Go server run on the same machine |
| Remote web UI/PWA, no native app | Receiver would be on the wrong machine | Browser localhost cannot reach server localhost | **Unsupported for OAuth setup** |

No topology inference, public HTTPS server callback, public callback-origin setting, tunnel, hosted relay or pasted-code fallback in this release. The server does not try to infer whether a web browser is local. Web mode displays: **“Browser-only connection setup requires Legato on this computer. For a remote server, use the Legato app.”** A timeout explains the same restriction and allows cancel/retry. Do not prevent a local browser just because it opened the page through a LAN name or proxy.

This restriction applies to obtaining/replacing OAuth authorization, not ordinary web use. Once authorized, Legato refreshes credentials and brokers permitted calls without keeping the app/listener open. Remote web users can still view status and manage grants under operator authorization. Anonymous MCPs do not need OAuth setup.

## App path

1. The server page asks the native shell to connect a named MCP connection. The shell bridge accepts only its own top-frame/server-origin request. A web-supplied app flag is not an authentication boundary.
2. The native app prepares its receiver before consent. On desktop it binds a temporary listener to an IP loopback address only. It reports the resulting allowed callback profile/port to the selected paired Legato through its existing authenticated, pinned connection. Use HTTPS for non-loopback server destinations; never forward a code over plain LAN/Internet HTTP. HTTP may be used only for an explicitly local IP-loopback destination.
3. Legato discovers the MCP resource and issuer and selects an allowed client registration. It generates state and PKCE on the server. The durable transaction records operator authority/generation, initiating instance, connection generation, exact issuer/resource/client registration/redirect, expiry and receiver binding. The verifier never leaves Legato.
4. The app keeps the transaction-to-server/receiver binding. It obtains the authorization URL directly from that server, not from untrusted callback content or an arbitrary webview URL, and opens the system browser.
5. The provider redirects the browser to the app's registered callback. The app checks the pending receiver/state mapping and strict callback parameter shape, then posts the code/state/issuer or denial in a bounded authenticated request body to the recorded server. A transaction-specific return capability binds completion to that receiver session; it is not a general credential or agent tool.
6. Legato validates caller authority, receiver capability, state, expiry, exact issuer, resource and connection generation. It atomically claims the transaction and exchanges the code at its validated token endpoint using the server-held verifier and **the same app redirect URI**. It must not substitute a server URL during exchange.
7. Legato seals pending credentials. The app/UI shows status and account/resource confirmation. Only authenticated operator confirmation activates the connection. Agent tool grants remain separate.

The callback must not choose the destination server, even when the operator has switched the app to another server. Resolve the destination only from the saved transaction binding. Reject arbitrary `server`, `return_to` or token-endpoint fields. Operator-authority revocation invalidates unfinished transactions. The app cancels its pending attempts before forgetting a server when reachable; offline removal cannot guarantee server-side cancellation, so those attempts expire and are never reassigned. Shared web tokens are not per-person/device identity; do not claim per-device revocation or cryptographic device attestation unless implemented.

The app handles the short-lived code only in native memory. It does not exchange tokens or receive provider access/refresh tokens or client secrets. A temporarily disconnected app may retry delivery within the transaction lifetime with the same binding; repeated submissions report the original transaction status and never exchange twice. If the app loses the code/receiver state after termination, cancel/expire and begin again rather than paste a code or persist it in browser storage. Do not promise seamless process-death recovery without explicit secure native persistence design.

### Phone receiver compatibility gate

The app is the receiver owner; it does not follow that every phone can keep an arbitrary TCP listener alive while the browser is in front. Verify Android receiver/background behavior before calling that mode supported. A provider-supported OS callback/session can serve the same app-forwarding contract. Any claimed HTTPS app link or other callback scheme needs its own provider registration and platform handling; it is not already implemented by the pairing link.

Do not invent a callback endpoint, assume a public app-association domain exists, or silently substitute a custom URI scheme where the provider/current MCP profile requires HTTPS or loopback. Settle the actual phone callback profile in a bounded native feasibility slice. If a platform/provider combination cannot receive a permitted callback, show unsupported rather than adding a public-server fallback. Desktop loopback success is not proof of phone support.

## Web path: local machine only

1. The authenticated frontend asks the Go server to begin the connection in web mode.
2. The Go server binds a short-lived IP loopback listener **before** building/registering the redirect. Use the actual successfully bound port, an expected callback path, a bounded request size and timeout. Never bind `0.0.0.0` or a LAN interface.
3. Legato creates the server-owned transaction and registration, then returns the authorization URL and status handle. The frontend opens that URL in its current browser; it does not ask the server to launch a browser.
4. After consent, that browser requests the loopback callback. The listener validates state/issuer/transaction and settles through the same host service. Legato performs token exchange and sealed storage.
5. The loopback response gives a minimal clean completion page without provider error text, third-party assets, tokens or raw query reflection. The original web UI polls authenticated status and asks for account/resource confirmation. No cross-origin cookie, `window.opener`, CORS token transfer or automatic navigation to a request-derived server URL is needed.

If the web UI is remote, the server may still start the receiver: the product deliberately does not detect this case. Consent will redirect to the remote browser's own localhost and fail to reach the server. The pending attempt expires or the operator cancels. UI copy states the supported assumption before navigation and provides app/retry guidance after timeout. Never ask the operator to copy the failed redirect.

Cancel, expiry and receiver-owner exit close the temporary listener. Restart may preserve durable transaction metadata, but a lost listener must be reported as interrupted and restarted with a new transaction; do not promise to rebind the same port safely. If a fixed port is required and occupied, fail explicitly rather than silently selecting an unrelated redirect.

## Registration without routine manual redirect setup

The normal form asks for name/MCP URL and Connect. Generate the redirect from the active receiver profile; do not derive it from `web.public_url`, request Host or forwarded headers.

- Reuse compatible supplied registration first; use advertised CIMD only when a provider-readable metadata document already describes the selected receiver profile; otherwise use advertised DCR. Local callback addresses do not make a private CIMD document publicly readable.
- DCR declares the native/loopback redirect automatically with the appropriate application type. Handle provider-specific loopback port rules honestly. Arbitrary loopback host substitutions, ports, paths or custom schemes are not universally accepted.
- Registration reuse is keyed by issuer and callback/client-auth profile. A new ephemeral port can require new registration where no loopback-port exception is allowed. Prefer a stable allowed callback profile where necessary, but never bind an occupied port or claim all providers allow wildcard ports.
- Remove the proposed per-instance callback-origin onboarding and public CIMD publisher from the first release. A future shipped public metadata document can list an approved native/loopback profile, but a hosted service/domain is a separate decision, not implicit infrastructure. CIMD-only providers without usable metadata are a visible compatibility limit.
- Static-only providers get **Advanced: pre-registered OAuth client** with generated bare redirect, client ID and sealed secret input where required. Registering a bare URI in a provider console is different from pasting a returned code-bearing callback into Legato or conversation. No provider app registration or login is authorized during this planning task.
- Strict metadata/issuer/S256/resource validation must precede consent and exchange. Prevent the pinned SDK's guessed legacy-endpoint fallback. Do not fall back across security failures or promise universal native-client acceptance.

## Broker and authorization boundaries

Remote Streamable HTTP upstreams and controlled agents on the owning instance remain the first broker scope. Native callback forwarding is an operator app-to-owning-server path, **not main/worker tool forwarding**. Do not copy OAuth credentials to apps, agents or workers.

Reuse `SecretStoreService` and credential broker grants, approvals, limits, read-only enforcement and audit. Host-only MCP usage profiles prohibit raw-secret read/exec. OAuth authorization does not grant agent operations. Apply default-deny execution/task/repository/tool/resource checks on discovery and every dispatch, including stale catalog calls. Attribute actual execution/slot/task/repository and deny unauthenticated or forged identities.

Keep upstream names separate from local tool names. Native tool auto-allow does not bypass host approval. Upstream annotations, schemas, instructions and results are untrusted. Bound schemas/results, validate JSON Schema 2020-12 with no network references, and invalidate/review grants after material tool changes. Egress/SSRF controls cover every upstream metadata/registration/token/MCP hop; loopback receiver traffic is a narrowly scoped inbound path, not permission to access arbitrary loopback upstreams. No automatic sampling, roots, credential elicitation or result URL fetches.

Catalog additions need a new session or explicit operator reload initially. Revocation blocks host dispatch immediately. No automatic agent interruption/restart. Existing paired main/worker tokens gain no MCP administration or forwarding permission. Subprocess MCPs, device grants, public relay and worker routing remain deferred.

## Lifecycle and secret hygiene

- Separate configured/enabled, authorized, connected and usable-for-this-execution states. Report receiver-start failure, authorization denial, delivery pending, expiry, interrupted receiver, re-auth required and unsupported callback profiles distinctly.
- Keep independent attempts and connection generations. Old callbacks cannot overwrite newer activation; disconnect invalidates active, pending and staged credentials and return capabilities before cleanup.
- The host broker owns exchange/refresh. Serialize across processes with durable claims/fencing; write rotated refresh state before publishing access tokens. Receiver-owner loss is a recoverable interrupted attempt, not a reason to create a public listener.
- Support no-refresh tokens until expiry; re-auth then needs the app or local web path. Never launch consent from an agent call. Preserve existing Burnlite read authorization only through explicit compatible issuer/resource/scope selection; no silent scope elevation.
- Tokens, client secrets and verifier stay server-side. App codes/return capabilities must not reach webview scripts, native navigation logs, OS-intent debug dumps, analytics, localStorage/sessionStorage, agent configs, tool payloads or conversations. Use redacted native/HTTP logging, no-store auth APIs and service-worker cache exclusions.
- The provider's necessary callback URL can contain a short-lived code/state. Clean completion responses minimize further exposure; do not claim to erase browser or OS history. Registered callback validation and PKCE are both required.
- Disconnect blocks new calls and attempts first, then revokes upstream where supported and clears caches. Local disconnect is not proof of upstream revocation. Do not automatically retry ambiguous mutations.

## Implementation slices and estimate

Split into vertical TDD tickets after approval:

1. **Local-web callback and durable transaction:** authenticated begin starts Go loopback receiver; real local-browser-shaped HTTP callback exchanges server-side; denial/cancel/expiry/replay and interrupted listener. Large / Medium uncertainty.
2. **Native receiver feasibility:** prove desktop loopback and the selected Android callback profile with fake provider/isolated app state; establish provider-compatible registration constraints and failure behavior before promising phone support. Meta research, unestimated.
3. **App-to-server response delivery:** native shell begin/receiver lifecycle, transaction-to-paired-instance binding, authenticated bounded return, wrong-server/replay/multi-server/foreground tests. Depends on 2. Split desktop and Android work; provisional Large / High each.
4. **Registration and tokens:** strict discovery, usable pre-registration/CIMD/DCR selection, receiver-specific redirects/ports, sealed versioned credentials, refresh/re-auth/disconnect. Large / High.
5. **Controlled broker and upstream security:** two granted executions, deny/approval/audit, schema/result validation and SSRF controls. Two Large / High slices; complete before real upstream use.
6. **Settings, adapters and acceptance:** app vs no-app path, explicit local-web copy, status/confirmation/grants, Claude/Chimera/Codex reload and docs. Split UI and installed-client acceptance after receiver contracts settle.

The earlier 48-point estimate covered a different callback design and is no longer a valid unchanged total. Re-estimate after native phone feasibility and ticket decomposition. No delivery date or universal provider coverage is promised.

## Verification and evidence limits

Existing research: `docs/analysis/leg-199-mcp-broker-oauth.md` and `docs/analysis/leg-199-mcp-scenarios.md` record the previous direct HTTPS callback design and baseline tests. Their public-origin recommendations are superseded by this revision. Those tests do not verify app relay or native phone background handling.

Required new acceptance:

- Native app connects to an isolated remote Legato with no public OAuth callback. Device browser returns to app; authenticated delivery reaches the recorded server; only that server exchanges/verifies and stores tokens. Switching app server mid-flow cannot redirect delivery.
- Desktop and phone are separate platform acceptance paths. Verify phone browser handoff, background/resume/process death, denied provider profile and fresh-flow recovery. A webview app marker alone cannot bypass binding checks.
- Local web begin starts a loopback receiver on the Go host, actual local browser follows consent and callback, and original UI resumes by authenticated polling. No server browser launch or app installation required.
- Remote browser/PWA without app follows the same no-detection path, fails to deliver to the server, then times out with app guidance. There is no public callback, canonical-origin onboarding or copy/paste fallback.
- Bound-only loopback port, occupied fixed port, unrelated localhost request, missing/mismatched/duplicated state or issuer and unsolicited app deep link cannot settle another attempt. Only the selected receiver profile is accepted.
- Denial/cancel/expiry/reload/old-after-new/disconnect-before-callback and duplicate app submissions do not exchange twice or reactivate a disconnected connection. Receiver/process loss is explicit, not falsely marked successful.
- DCR automatically declares the receiver callback; supported port exceptions and static-only limits are tested. Unusable CIMD fails clearly. Missing AS metadata fails before guessed endpoints. Anonymous MCPs do not invoke OAuth.
- Two granted agents call one server-owned connection without credentials/login; ungranted callers/tools/repositories are refused and audited. Refresh works with the app closed; re-auth uses a supported operator path.
- Sentinel scan native logs, callback responses, request bodies outside protected delivery, web storage/caches, agent/config/tool/audit surfaces. Codes only traverse the necessary provider return and protected app-to-server body; no raw secrets enter other surfaces.

Use real temporary stores and fake OAuth/MCP servers. Tests follow public entry points, one vertical behavior at a time. Existing code has a loopback helper, but its presence is not acceptance of the new web/native workflow. No feature implementation or new native flow was tested in this plan revision.

Real app/browser tests use scratch instances and isolated app state only. No installs, upstream registrations/logins, credential access, deployment, DNS/firewall/tunnel changes, live settings edits or running-process restarts are authorized here. Report any blocked native verification rather than claiming it works.

## Approval boundary

The app-or-local-web product restriction is settled by Cameron's direct instruction. Authenticated confirmation is also settled. Overall plan approval remains pending; keep unresolved broker scope/catalog decisions visible. Do not implement or run `legato plan complete` until actual approval and successful implementation/verification.
