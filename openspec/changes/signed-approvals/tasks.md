Every task is test-first (red, green, refactor). This change needs `agent-approvals` (phase 1). The `signed` defaults for credential rules need `credential-broker` (phase 2).

## 1. Attestation

- [ ] 1.1 `internal/engine/attest`: parse the key description extension (security levels, challenge, hardware-enforced authorization list, application ID); tests against attestation chains captured from real devices and checked into `testdata/`
- [ ] 1.2 Chain verification to shipped Google roots; test with a test root injected, and that a chain to an unknown root is refused
- [ ] 1.3 Revocation list fetch and cache; test that a revoked serial is refused and that a failed fetch refuses a chain the cache has not seen
- [ ] 1.4 Policy checks of D2 as a pure function; table tests for software-only keys, `noAuthRequired`, an `authTimeout`, a wrong curve, a wrong application ID and a stale challenge

## 2. Devices and enrollment

- [ ] 2.1 Migration `approval_devices` and `approval_challenges`; store tests that a challenge is spent once
- [ ] 2.2 A server ID made once and kept; test it survives a restart
- [ ] 2.3 `POST /api/devices/challenge` and `POST /api/devices`: first device enrolled with the token alone; server tests with a test attestation chain
- [ ] 2.4 A later device is an approval request with source `devices`, completed only by a signed approval from an enrolled device; service test
- [ ] 2.5 `GET /api/devices` and `DELETE /api/devices/{id}` (an approval request that needs a signature); server tests
- [ ] 2.6 `legato device reset` at the host: removes every device and passkey, writes the audit log, raises an Attention item and an ntfy notice; CLI test

## 3. Signed decisions

- [ ] 3.1 Canonical approval object and RFC 8785 serialization; golden tests, including Unicode and numbers
- [ ] 3.2 `POST /api/approvals/{id}/challenge`: canonical bytes, a 32-byte challenge bound to the approval and the device, 5-minute expiry; tests with an injected clock
- [ ] 3.3 Signature check on `POST /api/approvals/{id}/decision`: device key, challenge unused and bound, canonical object unchanged, ECDSA verify; tests for a replayed challenge, a changed request and a key of another device
- [ ] 3.4 `signed: true` refuses unsigned decisions; defaults for `request` rules and for rule, profile and device changes apply once a device is enrolled, unless `signed: false`; service tests before and after the first enrollment
- [ ] 3.5 Audit log records the device for signed decisions; test against the hash chain

## 4. Web

- [ ] 4.1 Settings › Devices: list (name, kind, security level, last use), Remove raising a signed approval, the notice while none is enrolled; component tests
- [ ] 4.2 Tray cards for signed requests: "Needs your phone" outside the app, "Approve on this phone" inside it posting the bridge message, and the `legato:shell-approval` result; component tests
- [ ] 4.3 Passkey enrollment and assertion with `go-webauthn`, the challenge being the hash of the signed message; server tests with a virtual authenticator; web test that the option is hidden without a secure context

## 5. Android app

- [ ] 5.1 `ApprovalKey.kt`: key generation for API 30+ and API 29, StrongBox with fallback, invalidation on enrollment; JVM tests with a fake key provider that check the parameters
- [ ] 5.2 Enrollment flow: challenge, key with the challenge, chain posted, result shown; JVM tests with a fake server
- [ ] 5.3 `ApprovalSheet.kt`: draw from the fetched canonical bytes; the agent's reason quoted and labelled; JVM tests that the sheet ignores anything but the ID it was opened with
- [ ] 5.4 `BiometricPrompt` with a `CryptoObject`, the scope in its title and description and not the reason; sign, post, and handle `KeyPermanentlyInvalidatedException`; tests with a fake prompt
- [ ] 5.5 `PageContract.kt`: `approval: 1` in `window.legatoShell`, parse `{legatoShell: "approval", key, id}`, and `approvalEventScript`; unit tests for the key and top-frame rule
- [ ] 5.6 Deep link `legato://approval?url=…&id=…`: sheet for an enrolled saved server, the page for a saved server without a key, a notice for an unknown server; tests
- [ ] 5.7 `ApprovalList.kt`: open approvals of all saved servers, reloads on open, on return to the foreground and on a notification, unreachable servers marked; JVM tests with fake servers
- [ ] 5.8 Check on the phone: enroll, approve a signed request from a notification and from the tray, refuse with a new fingerprint enrolled

## 6. Docs

- [ ] 6.1 `mobile/README.md` § Approvals: the key, the sheet, the bridge message and event, the list
- [ ] 6.2 `docs/ai/approvals.md` (from phase 1): signed rules, defaults, devices, passkeys, reset
