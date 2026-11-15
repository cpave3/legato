# LEG-199: server-owned MCP OAuth and controlled tools

## Solution

Legato will own MCP connections and OAuth credentials. An operator can add a server from a phone, authorize in the provider browser, and return to a callback on the running Legato server. Agents can use only explicitly granted tools through the existing host broker. Agents receive no upstream tokens or per-session login prompts.

**This is a proposal for approval. No feature implementation has started.** The later request, “Let's build it”, starts the build-planning workflow. Implementation still waits for this plan's approval.

## Recommended first release

- Remote Streamable HTTP upstreams and controlled agents on the owning instance.
- Operator-only Settings: add, inspect, enable, disable and remove named connections; Connect, confirm account/resource, re-authenticate and disconnect.
- Existing Claude plugin, Chimera mod and Codex MCP injection, with execution-authenticated host dispatch.
- Default-deny execution/task/repository/tool grants, host operation approvals and audit attribution.
- Existing sealed host secret store, not a parallel token vault.
- Explicit reload for tool additions; immediate denial for removal or revoked permission.
- No arbitrary subprocess MCPs, hosted callback relay, device grant adapter or cross-instance forwarding in this release.

The committed research document is `docs/analysis/leg-199-mcp-broker-oauth.md`. It contains the sequence diagram, exact repository evidence, official source links, deployment comparison and detailed acceptance procedure. An interactive architecture/sequence artifact accompanies this proposal in Legato. This plan uses prose rather than embedded diagram fences so validation does not install diagram dependencies.

## Phone flow and callback contract

1. The authenticated operator selects a named configured MCP connection and presses Connect.
2. Legato validates upstream egress and discovers its resource and issuer. It selects a compatible registration: supplied pre-registration, advertised CIMD, then supported DCR fallback.
3. Legato creates a durable, short-lived transaction. It binds state and sealed PKCE verifier to the operator authority, owning instance, connection/version, exact issuer, resource, redirect URI and client registration.
4. The phone opens the provider's authorization URL. Only the challenge and state leave Legato; the verifier and client secret remain server-side.
5. After consent, the provider redirects the **phone browser** to Legato's configured HTTPS callback. The provider does not need an inbound network route to that callback. The phone browser does.
6. The public callback validates and atomically claims the bound transaction. Legato exchanges the code with the pinned token endpoint, same redirect URI, verifier and resource.
7. Legato seals pending credentials and redirects to a clean completion page. The original UI or another authenticated device resumes from server status and confirms the account/resource before activation.
8. The operator grants selected tools separately. Two granted agents can use the connection without upstream credentials or another OAuth flow.

No normal or fallback path asks an operator to copy a callback URL or authorization code into a conversation.

Use the explicit `web.public_url` as the root HTTPS callback origin. Apply stricter OAuth validation than the current URL setting. Do not derive redirects from request Host or forwarded headers. First version rejects subpath deployment for this flow rather than claiming unsupported prefix behavior. If the URL is absent or cannot support a phone callback, show the missing requirement before authorization. Do not modify network or TLS settings automatically.

For LAN/VPN deployments, the provider must permit the exact redirect, and the external phone browser must trust TLS and reach that address. App certificate pinning is not browser trust. CIMD additionally requires the provider to fetch its HTTPS metadata document; private-only callback reachability does not satisfy that separate requirement. Pre-registration/DCR may be alternatives only when the provider supports them. Loopback-only mode is same-machine only, never the phone solution.

## Authorization and secret boundaries

Current web authentication represents one shared instance operator authority, not separate human accounts. Bind transactions to that authority and its credential generation. Add a verified device identity only where one exists. Account confirmation requires authenticated operator authority and cannot be performed using callback state or the completion handle alone. Cross-device resume does not transfer web tokens or codes.

Reuse `SecretStoreService` and the existing credential broker's grants, approvals, limits, read-only enforcement and audit. Add host-only MCP usage profiles that prohibit raw-secret read/exec. OAuth scopes authorize the connection; local operation grants authorize an agent. Neither implies the other.

Grant checks occur at discovery and every dispatch, including stale catalogs and manually constructed calls. Resolve caller identity from the execution broker token, not task IDs in arguments. Namespaced upstream tools must not shadow local tools. Native tool auto-allow does not bypass host operation approval. Unknown operations are not read-only because an upstream annotation says so.

Keep tokens, verifier and client secrets out of prompts, agent configs, arguments/environment, logs, localStorage/sessionStorage, tool results and API responses. Only the necessary provider callback request contains the short-lived code/state. Use a server-only callback, clean redirect, no third-party content, no-store and no-referrer headers, and proxy query-redaction requirements. Do not claim Legato can erase the provider/browser's necessary callback history.

Exclude all MCP authorization/admin/status APIs from service-worker caching. The existing API cache must not retain authorization URLs or transaction data.

## Lifecycle and failure behavior

- Separate configured/enabled, authorized, connected and usable-for-this-execution status. Show safe actionable errors, not an empty tool list.
- Persist independent attempts and use connection generation checks. A late attempt cannot overwrite a newer activation. Handle denial, cancellation, expiry, reload and concurrent tabs explicitly.
- Exchange and refresh through the lock-owning host broker. Durable claims/fencing cover web processes and ownership takeover. Save rotated refresh state before using new access tokens.
- Reuse registrations by issuer and redirect/auth configuration. Validate exact issuer response, resource audience and advertised S256 support. Provider errors must not leak response bodies or descriptions.
- Support no-refresh tokens until expiry, then require operator re-auth. Treat transient network failure separately from `invalid_grant`.
- Disconnect blocks new dispatch and pending activation before network cleanup. Clear local grants/caches and revoke upstream tokens where advertised. Show unsupported or failed upstream revocation honestly.
- Do not retry an ambiguous mutation automatically. Report that its outcome is unknown.

Existing Burnlite ticket-source auth is not assumed to authorize broker mutations. Share it only through explicit operator choice and verified issuer/resource/scope compatibility; otherwise use a separate named connection. Do not silently elevate its read scope.

## Implementation phases and provisional estimate

Create implementation tickets only after plan approval. Split each Large phase into vertical TDD slices before starting it.

1. **A — callback origin and transaction:** configured origin, durable transaction, authenticated activation, concurrent/restart HTTP acceptance. Large / High uncertainty.
2. **B — registration and credential lifecycle:** current MCP discovery, registration options, issuer/resource/S256 validation, sealed versioned tokens, refresh/re-auth/disconnect. Depends on A. Large / High.
3. **C — broker tracer:** one authorized fake upstream, two granted controlled executions, denial/approval/audit and read-only boundaries. Depends on B. Large / Medium.
4. **D — upstream security gate:** egress/SSRF at every hop, bounded JSON Schema 2020-12 validation, local-only references, result validation, untrusted instructions and schema-change handling. Depends on B/C. Large / High. Complete before any real upstream is used.
5. **E — operator Settings:** administration, Connect/confirmation, status/recovery and grant controls; phone/external-browser return and cache exclusions. Depends on A–D. Large / Medium.
6. **F — adapters and acceptance:** Claude/Chimera/Codex discovery/reload contract, scratch phone/proxy/restart evidence, user/agent docs and regression checks. Depends on C–E. Large / High.

Total: 48 points, roughly 18 nominal engineering days under the repository's estimate guide. This is provisional, not a delivery commitment. Provider registration policy, SDK behavior, cross-process persistence and installed-client compatibility remain estimate risks.

Defer worker delegation and subprocess launches to separately approved designs. Existing paired main/worker scopes grant no MCP calls or administration. Secrets and callbacks stay on the owning instance. Worker forwarding would need an explicit delegation capability and routing protocol; current pairing is insufficient.

## Verification

Follow TDD through public service/HTTP/MCP entry points, one vertical behavior at a time. Use fake OAuth and MCP servers, real temporary databases and isolated secrets. Do not register or log in to real providers without separate permission.

Required acceptance outcomes:

- Phone browser reaches the configured server callback, not phone localhost. Server alone exchanges code/verifier. No pasted-code workflow.
- Original UI and another authenticated device can resume; unauthenticated callbacks cannot activate connections or disclose status.
- Two granted agents call one connection without tokens/login; ungranted callers, tools, repositories and forged identities are denied and audited.
- Denial/cancel/expiry/reload/replay/wrong issuer/concurrent attempts/restart are deterministic and cannot replace newer credentials.
- Refresh rotation, no-refresh expiry, secret-store failure, upstream outage and schema incompatibility show safe recovery states.
- Disable/disconnect prevents new dispatch and late activation. Provider revocation status is distinct from local disconnect.
- Spoofed Host/forwarded headers cannot alter redirects. Private egress, DNS rebinding and credential-forwarding redirects fail policy.
- Schemas/results/descriptions cannot grant authority or invoke unsupported sampling, roots, automatic elicitation or URL navigation.
- Sentinel secrets do not appear in logs, caches, model/config surfaces or results. Check the necessary callback request separately.
- Each supported adapter gains additions only through new session/explicit reload; revoked calls fail immediately even with stale definitions.
- Existing main/worker tokens cannot acquire MCP authority.

Run targeted Go and web tests, architecture tests, then repository checks suitable for changed areas. Validate route/config/site documentation through the existing generated-reference checks. Do not modify CI.

A real-phone and installed-agent browser pass needs scratch instances, pre-authorized HTTPS/network routing and explicit testing permission. This proposal does not authorize installations, deployment, DNS/firewall/tunnel changes, credentials, live settings edits, running-instance tests, or process restarts. If those block a promised verification, report the gap rather than claiming completion.

## Approval boundary

Approve this first-release scope and the required decisions in `plan.json` before implementation. After implementation and promised verification succeed, run `legato plan complete`. Do not mark the feature complete on research or plan submission.
