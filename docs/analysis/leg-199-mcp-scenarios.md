# LEG-199: scenario investigation

**Callback scope superseded:** Cameron now limits OAuth setup to the native app, or a browser on the Go server’s machine. See [the current decision](leg-199-app-local-callbacks.md) and proposal revision 3. The direct HTTPS callback scenarios below remain historical evidence, not verification or requirements for the new app-forwarding path.

Date: 2026-11-15. Baseline: `c8aafd13` plus research-only scenario files. No feature implementation or live setup.

## Result

**Cameron's correction is right: manual redirect setup must not be the normal experience.** Legato can generate the callback and declare it automatically through DCR or a Client ID Metadata Document (CIMD). The operator normally enters an MCP URL and presses Connect.

A redirect URI is still required inside the OAuth protocol. Automatic registration does not remove callback reachability, TLS or provider policy requirements. The evidence does not establish what percentage of providers support automatic registration, so “most” is a UX goal, not a measured compatibility claim.

The first proposal did not explain this distinction clearly. It also omitted a concrete CIMD publisher and made a host-file-only URL setting a prerequisite for phone setup.

## What ran

Research harness: `.legato/plans/leg-199-mcp-broker/scenarios/scenarios_test.go`.
Machine observations: `.legato/plans/leg-199-mcp-broker/scenarios/results.jsonl`.

Command, from the clone root:

```sh
CGO_ENABLED=0 GOPROXY=off GOTOOLCHAIN=local \
GOCACHE=/tmp/leg-199-scenario-gocache \
go test -count=1 -json ./.legato/plans/leg-199-mcp-broker/scenarios
```

**11 top-level scenarios and two nested CIMD branches passed their characterization assertions.** This does not mean the proposed broker passed acceptance. Several assertions deliberately confirm existing gaps. The harness lives in the plan bundle, outside normal `go test ./...` discovery; it must not become a production regression suite that requires unsafe behavior to remain.

The existing public Connect/Callback HTTP routes and service methods ran against temporary SQLite and local TLS OAuth endpoints. A separate test calls the pinned official Go SDK's authorization API. Every endpoint is a local fixture. All credentials are synthetic, held by a memory secret adapter; these tests establish server custody, not encryption-at-rest correctness. No agents, installed clients, real browser, VPN, provider account or running Legato instance participated.

The fake provider checks exact redirect matching, resource and PKCE before issuing a token. The browser simulation receives redirects over HTTP and calls the actual callback route without a Legato bearer header. The server performs the token exchange. Consent is automatic in the fixture; it does not prove a real provider accepts our registration.

## Executed scenarios

| Scenario | Observed result | Meaning for the plan |
| --- | --- | --- |
| 1. Operator supplies only MCP URL; provider supports DCR | Existing Connect automatically registers the server callback. The browser returns without Legato bearer; server exchanges and retains credentials. | **Works in local simulation.** No operator-entered redirect or provider-console step is needed for this provider behavior. |
| 2. External HTTPS is terminated at a proxy; backend is HTTP | Current handler registers the internal HTTP origin, even with forwarded HTTPS/host headers. | **Existing gap.** Choose a trusted canonical address. Do not “fix” this by blindly trusting forwarded headers. |
| 3. Provider advertises CIMD but has no DCR endpoint | Current IntegrationConnector refuses because it requires DCR. | **Existing gap.** Specifying CIMD support in prose does not connect the current code. Add publisher and selection behavior. |
| 4. DCR endpoint rejects this redirect | Rejection reaches caller; automatic registration does not override policy. | **Compatibility limit.** Explain the cause and supported alternatives. Do not ask for a code or silently change application type. |
| 5. Operator re-authenticates | Current flow creates a new client registration. | **Existing gap.** Reuse compatible registration by issuer, callback and client-auth profile. |
| 6. Two attempts complete newest first, then oldest | Current flow lets the old attempt replace the newer connection. | **Existing gap.** Final activation must compare the expected connection generation. |
| 7. Operator disconnects while consent is pending | Current pending callback can reconnect afterward. | **Existing gap.** Disconnect must invalidate attempts and staged credentials, not just active tokens. |
| 8. Callback reaches another service over the same database | Second service cannot find the first service's pending attempt. | **Existing gap.** Durable transaction state plus exchange ownership is required. |
| 9a. Generated CIMD document is available to provider | Pinned SDK completes authorization and token exchange with zero DCR requests and no manual redirect entry. Provider fetches generated metadata and checks its exact callback. | **Works at SDK boundary only.** Legato does not yet publish or integrate this document. |
| 9b. Same CIMD capability, but provider cannot obtain metadata | Authorization fails before token exchange. | **Compatibility limit.** A private-only Legato address cannot serve CIMD to an Internet-only provider. This branch injects provider-side reachability refusal; it does not test a real VPN. |
| 10. Phone route disappears after DCR and consent | Injected browser network failure prevents callback, token exchange and stored connection. Provider never calls Legato directly. | **Deployment limit.** Successful registration/consent does not prove the phone can reach the callback. No pasted-callback recovery. |
| 11. SDK cannot discover authorization-server metadata | Pinned SDK still makes requests to guessed `/register`, `/authorize` and `/token`, then completes with the fixture. | **Newly verified SDK gap.** Strict discovery preflight must prevent the legacy endpoint-guessing downgrade. |

A fixture correction was needed during testing: the fake DCR response initially omitted its public-client token authentication method. The SDK then selected a different default. The fixture now returns `token_endpoint_auth_method: none`; the final run succeeded. No production code changed to make tests pass.

## Additional deployment walkthroughs

These are source-backed walkthroughs, **not executed integrations**.

### Public HTTPS + automatic registration

The instance already has a trusted canonical address. Operator enters the MCP URL. Legato generates its callback and client metadata. If the provider supports usable CIMD, it reads that metadata; otherwise supported DCR sends the same callback in its registration request. The operator consents and confirms in Legato.

**Manual work:** none per provider, apart from consent and tool grants. A provider can still reject an unknown client/domain or require administrative approval.

### VPN-only Legato + provider supporting DCR

The provider receives a private HTTPS redirect as data in the DCR request. If its policy accepts it, the phone browser uses its VPN route to deliver the callback. The provider does not require an inbound route to Legato.

**Manual work:** no per-provider redirect definition when DCR accepts the generated URI. The phone needs working VPN/DNS/browser TLS trust. A self-fetch by Legato cannot prove any of these browser conditions.

### VPN-only Legato + CIMD-only Internet provider

The phone can reach Legato, but the provider cannot fetch Legato's private metadata. This does not work by publishing the document only on that private server. A public metadata host could describe a private callback only if provider policy permits that relationship; the CIMD draft allows same-origin restrictions.

**First-release outcome:** explain unsupported topology. Try supported DCR only when available, or offer an explicit pre-registered-client option. Do not introduce a hosted service or expose the instance automatically. This is a real limit, not a setup field we can hide.

### Static-registration-only provider

If a matching client registration already exists, reuse it. Otherwise show **Advanced: pre-registered OAuth client**. Display the generated **bare redirect URI**, client ID input and sealed-secret input where required. The operator registers that URI in the provider's console and supplies the resulting client information through Settings.

This is different from copying a returned callback URL containing an authorization code into Legato or conversation. Returned codes/callbacks are never accepted as an operator workflow. Do not copy a packaged client's fixed registration and assume it accepts Legato's server callback.

**Manual work:** provider app setup, only in this compatibility branch. If the provider forbids third-party registration entirely, say unsupported. Device authorization is another option only with a documented compatible grant/client/resource.

### Desktop or PWA starts consent in another browser

The selected Legato server owns the transaction, even when the desktop shell and server share a machine. The external browser can deliver the callback without a Legato cookie. The original authenticated shell/PWA resumes the attempt and confirms the account/resource. No popup-opener or token transfer is necessary.

**Remaining gap:** real app-to-system-browser certificate trust and foreground behavior need installed-client acceptance. Loopback-only server mode cannot become phone-safe by changing the UI; first release should fail phone setup clearly rather than promise it.

### No OAuth required

An anonymous upstream needs no OAuth redirect or client registration. Tool grants, egress controls, schema handling and audit still apply. A provider-supported API-key connection also does not use OAuth redirects; a future/existing supported secret profile must own that key. Do not force every MCP through Connect OAuth merely because an OAuth provider is common. Exact anonymous/key UX needs acceptance coverage, not a claim that every protected provider accepts API keys.

## Plan changes required

1. **Normal form:** MCP URL and Connect. Do not display a generic redirect URI editor, client ID or secret form unless capabilities require the advanced branch.
2. **Once per instance, not once per MCP:** reuse a valid pinned `web.public_url`. If absent, let the operator approve/save the canonical HTTPS address from phone Settings. This record is new proposed behavior; it is not currently provided by the file-only URL setting. Do not automatically trust Host/forwarded headers or overwrite pinned config. Changing the address invalidates pending transactions and requires compatible client metadata/registration before new consent.
3. **Publish CIMD concretely:** propose a public `GET /oauth/mcp/client-metadata/<profile>.json` route on the owning instance. Generate exact `client_id`, `client_name`, redirect list, grant/response types and `token_endpoint_auth_method: none`. Never publish client secrets, tokens, transaction IDs, PKCE/state or grant details. Keep the document small. Use a stable versioned profile when callback metadata changes so stale provider caches do not misidentify a registration. Same-origin callbacks avoid one class of provider restriction, but domain trust policy can still reject the client. No hosted vendor service is implied.
4. **Choose a usable automatic strategy:** supplied compatible pre-registration first; then CIMD only with a usable publication origin and advertised support; then advertised DCR. For a private-only publisher, explain CIMD's limitation instead of sending the operator into predictable failure. Provider fetch failure is still possible for a nominally public origin; show it and offer a new DCR attempt only when supported. Never silently fall back across issuer/PKCE/security validation failures.
5. **Static-only recovery:** expose generated redirect as read-only, with provider-console instructions and sealed client credentials. “No pasted callback” must mean no returned code-bearing callback, not a prohibition on registering the bare URI in an exceptional provider console.
6. **Readiness:** before navigation, show the selected instance/registration strategy. Test non-secret browser reachability to the approved canonical origin where practical. This is only a current browser check; it does not establish provider metadata reachability or guarantee the route survives consent. Do not send operator tokens to an untrusted suggested address.
7. **SDK security gate:** prohibit guessed legacy endpoints when metadata is absent. Preserve exact issuer/resource/S256 validation, explicit registration choice and persistence adapters.
8. **Lifecycle acceptance:** promote out-of-order activation, disconnect-then-callback and cross-process return into first vertical acceptance tests. They are demonstrated failures today, not hypothetical concerns.
9. **Preserve the actual decision:** Cameron selected `activation=confirm` on proposal revision 1. Keep authenticated confirmation. Other required decisions and overall approval remain unresolved.

The 48-point estimate remains provisional. Canonical-address onboarding and CIMD publishing are explicit scope now; split and re-estimate phase A/B before implementation.

## Sources

- Current MCP registration, including automatic CIMD/DCR and static fallback: https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/client-registration
- CIMD draft version cited by MCP, especially §§4.3–4.5, 6.1, 6.5–6.6: https://datatracker.ietf.org/doc/html/draft-ietf-oauth-client-id-metadata-document-00 . This is a work-in-progress draft; the datatracker also lists later revisions. Do not treat a newer draft as silently changing MCP's referenced contract.
- Official Notion guide: https://developers.notion.com/guides/mcp/build-mcp-client . Documents CIMD and DCR, not acceptance of every private Legato redirect.
- Official GitHub local stdio OAuth guide: https://github.com/github/github-mcp-server/blob/main/docs/oauth-login.md . Its packaged client, loopback and device flow are explicitly for the local server. They do not prove remote GitHub MCP accepts Legato's callback or supports DCR/CIMD.
- Official GitHub remote setup: https://docs.github.com/en/copilot/how-tos/provide-context/use-mcp-in-your-ide/set-up-the-github-mcp-server . Supported-client one-click setup is not universal third-party registration.
- Cached official Go SDK v1.8.0: `auth/authorization_code.go:310-338` contains metadata-missing legacy fallback; `:545-594` selects registration. The local SDK scenarios execute those public entry points rather than asserting on source constants.
- Existing production entry points exercised: `internal/server/burnlite_settings.go`, `internal/service/burnlite_settings.go`, `internal/service/integration_connect.go` and `internal/engine/oauthclient/`.

## Test audit

Reviewed 11 research test declarations: kept nine and strengthened two. Replaced one weak “URL generation is not reachability” probe with an actual browser-transport failure scenario and storage/exchange assertions. Strengthened CIMD-failure assertions to require the intended failure reason and zero token exchanges. No production seams or fixes were added. Existing production tests remain unchanged.

These are observations of the baseline. Once implementation starts, convert the unsafe-behavior observations to desired-outcome regression tests; do not retain assertions that disconnect must allow reconnection or that older attempts must win.
