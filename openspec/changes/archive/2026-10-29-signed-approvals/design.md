## Context

This is phase 5 of the agent approvals and credentials plan (`.legato/plans/agent-approvals-credentials/plan.md`). It needs `agent-approvals` (phase 1): durable approval requests, `POST /api/approvals/{id}/decision`, the ntfy link `legato://approval?url=<server>&id=<id>`, and the web tray. It uses `credential-broker` (phase 2) for the `signed` field of rules, and for the approval requests that changes to rules and profiles raise.

What the Android app has today:

- It shows each saved server in a WebView. Before the page loads, it sets `window.legatoShell = {voice: 1, key}` with a key that is new each time the server opens (`PageContract.kt`, `bootstrapScript`). It acts on `{legatoShell: "voice", key, action}` only from the top frame and only with that key, and answers with `legato:shell-voice` events (`mobile/README.md` § Dictation).
- It keeps each server's token with a Keystore key.
- Its minimum is Android 10 (API 29); it targets API 36.

The threat is an agent on the same machine as Legato, running as the same user, with the web token. It can call every API. It cannot touch the person's phone.

## Goals / Non-Goals

**Goals:**
- A `signed` decision can be made only by a person at an enrolled device.
- What the person signs is what Legato built, never what a page or an agent supplied.
- A stolen phone without the person's biometric or PIN signs nothing.
- Enrolled devices are listed and removable, and adding one needs an existing one.

**Non-Goals:**
- iOS. Face ID is iOS only; this change is Android.
- Face unlock on Android. Only strong biometrics can unlock the key, and many phones class face unlock as weak.
- Protecting a person whose phone and PIN are both taken.
- Containment of an unsandboxed agent in other ways (it can still change Legato's binary). The sandbox closes that.

## Decisions

### D1. A Keystore key for each server, needing authentication for each use

The app makes one key per saved server, alias `approval-<server id>`:

- EC P-256 (`secp256r1`), purpose sign, digest SHA-256;
- `setIsStrongBoxBacked(true)`, retried without it on `StrongBoxUnavailableException`;
- `setUserAuthenticationRequired(true)`;
- API 30 and later: `setUserAuthenticationParameters(0, AUTH_BIOMETRIC_STRONG | AUTH_DEVICE_CREDENTIAL)`; the person decided the device PIN is an allowed fallback;
- API 29: `setUserAuthenticationValidityDurationSeconds(-1)`, the older form of authentication for each use;
- `setInvalidatedByBiometricEnrollment(true)` on API 29 only. Android honours it only for a key that biometrics alone unlock, so on API 30 and later, where the PIN unlocks the key too, it has no effect (found on a Pixel 9 Pro). It would add nothing there: enrolling a fingerprint needs the PIN, which already unlocks the key;
- `setAttestationChallenge(<enrollment challenge>)`.

A key per server keeps servers apart: one server cannot ask for a signature that another would accept.

### D2. The server checks the attestation chain

`internal/engine/attest` parses the chain and the key description extension (OID `1.3.6.1.4.1.11129.2.1.17`). The server accepts an enrollment only when:

1. the chain verifies to one of Google's hardware attestation roots shipped in the binary, and no certificate in it is on Google's revocation list (fetched and cached; a failed fetch is a warning, not a pass, for a chain the cache has not seen);
2. the attestation security level is `TrustedEnvironment` or `StrongBox`;
3. the attestation challenge is the server's enrollment challenge, unused and less than 5 minutes old;
4. the hardware-enforced list has purpose sign, algorithm EC, curve P-256, a user authentication type, no `noAuthRequired`, and no `authTimeout` (authentication for each use);
5. the attestation application ID names `dev.legato.mobile` and the app's signing certificate digest from the configuration of the build.

The leaf's public key becomes the device key. The device record keeps its security level, so Settings can show StrongBox or TEE.

*Alternative:* trust any public key the app sends. Rejected: an agent could then enroll a key it made itself.

### D3. The first device, and the ones after it

The first device on a server is enrolled with the web token alone, because no device exists to approve it. An agent with the token cannot use this: it has no attested hardware key. Settings shows a notice while no device is enrolled.

Each later device, and each desktop passkey, is an approval request (`source: devices`). Its enrollment completes only after a signed approval from a device that is already enrolled. Removing a device is the same: an approval request signed by any enrolled device, the removed one included.

Recovery from a lost only device is `legato device reset`, run at the host, which removes every device and passkey and records the reset in the audit log. Outside the sandbox an agent could run it too; see Risks.

### D4. Canonical requests, built by the server

For a signature, the server builds a canonical object from the approval request and serializes it with the JSON Canonicalization Scheme (RFC 8785):

```json
{"v":1,"server":"<server id>","approval":"ap-7f3c","revision":2,
 "source":"credentials","task":"edu1ugzz","execution":"ex-…",
 "credential":"github","operation":"git.push",
 "resources":["github:cpave3/legato@refs/heads/legato/edu1ugzz-fix-sort"],
 "options":[{"id":"session","label":"For this session"},{"id":"once","label":"Once"},{"id":"deny","label":"Deny"}],
 "reason":"Push the attention sort fix for review"}
```

`server` is a random ID made once for each server. `reason` is the agent's text, kept as it is, so the person sees it beside what they sign.

### D5. A challenge for each decision, used once

`POST /api/approvals/{id}/challenge` returns the canonical bytes (base64), a 32-byte challenge, and its expiry (5 minutes). The device signs the JCS form of:

```json
{"v":1,"device":"<device id>","request":<canonical object>,"option":"session","challenge":"<base64>"}
```

`POST /api/approvals/{id}/decision` carries `signature: {device, challenge, value}`. The server applies the decision only when the challenge belongs to this approval and this device, is unused and unexpired, the canonical object built now is byte-equal to the one signed (the request has not changed), and the ECDSA signature verifies with the device key. The challenge is then spent, whatever the outcome.

### D6. Which decisions need a signature

- A rule with `signed: true` accepts only a signed decision. With no device enrolled, such a request cannot be approved; its card says so and links to Settings.
- Once at least one device is enrolled, `request` rules and approval requests for changes to credential rules, profiles and devices need a signature unless the rule says `signed: false`.
- Before the first enrollment, those defaults do not apply, so Legato keeps working as phase 2 left it.
- A signed decision is accepted for any request. The audit log records the device either way.

*Alternative:* apply the defaults from the start. Rejected: every `request` rule would stop working until the person enrolls a phone.

### D7. The app shows what it fetched, never what the page sent

The sheet opens with a server and an approval ID only. The app calls `GET /api/approvals/{id}` and then the challenge endpoint with its own stored token for that server, parses the canonical bytes, and draws the sheet from them. Fields that Legato wrote (operation, resources, task, grant) are shown plainly. The agent's reason is shown in a quote block under the label "Agent's reason", as text, never as markup.

The biometric prompt is `BiometricPrompt` with a `CryptoObject` over the `Signature`, so the key is unlocked for that one signature. Its title is the operation and the first resource; its description is the chosen option and the agent's task. It does not show the agent's reason, because the prompt's text is the system's voice.

### D8. Two ways to open the sheet

**Notification.** The ntfy link `legato://approval?url=<server>&id=<id>` opens the sheet when `url` is a saved server with an enrolled key. For a saved server with no key, it opens the server's page at the request, as in phase 1. For a server that is not saved, the app says so and does nothing.

**Page bridge.** The bootstrap script adds `approval: 1` to `window.legatoShell`. The page posts `{legatoShell: "approval", key, id}`. The app acts only on the top frame and the current key, the same rule as dictation, and reads only `id` from the message. After the sheet closes, it dispatches `legato:shell-approval` with `{id, status, option?}`, where `status` is `decided`, `cancelled` or `error`. In the app, the tray's buttons on a signed request become "Approve on this phone", which posts the bridge message. Outside the app, the card says "Needs your phone".

### D9. Open approvals from every server

A native screen lists the open approvals of all saved servers, grouped by server, from each server's `GET /api/approvals?status=open`. It reloads when it opens, when the app returns to the foreground, and when a notification arrives. A server that does not answer shows as unreachable with its last list. Tapping a row opens the sheet.

### D10. Desktop passkeys

A desktop can be enrolled as a WebAuthn platform authenticator (a passkey), after a signed approval from a phone (D3). For a decision, the WebAuthn challenge is the SHA-256 of the message of D5, so the assertion binds the same request, option and challenge. The browser's own prompt shows no scope, so the web card shows the canonical fields next to the button. A passkey needs a secure context and a domain name, so it works on `localhost` or on a host name with TLS, not on a bare IP address.

## Risks / Trade-offs

- [Android 10 cannot unlock a key for each use with the PIN] → On API 29 the key needs a strong biometric; the sheet says so when the phone has none enrolled. See Open Questions.
- [A new fingerprint invalidates the key, on API 29] → The app detects `KeyPermanentlyInvalidatedException` and offers to enroll again, which needs another enrolled device or `legato device reset`. From API 30 the key survives a new fingerprint (see D1).
- [`legato device reset` is reachable by an unsandboxed agent] → Recorded in the audit log and announced through Attention and ntfy. The sandbox removes the agent's access to the host command.
- [Google rotates or revokes attestation roots] → Roots are shipped in the binary and updated with releases; the revocation list is fetched at runtime.
- [The person approves without reading] → The sheet puts the operation and resources first and the option on the button; a broader pattern is marked, as in phase 1.
- [A page in the app spoofs the bridge] → Only the top frame with the current key is heard, and the app reads only the ID; it fetches everything else itself.

## Migration Plan

- Additive: tables `approval_devices` (id, server, kind, name, public key, security level, created, last used, removed) and `approval_challenges` (challenge hash, approval or enrollment, device, expiry, used).
- No behaviour changes until the first device is enrolled (D6).
- The app update adds the key, the sheet, the list and the bridge; an older app keeps opening the page from the link.
- Rollback: remove every device with signed approvals, or `legato device reset`. Explicit `signed: true` rules then cannot be approved until a device is enrolled again.

## Open Questions

- On Android 10 (API 29), can a key with validity `-1` be unlocked by the device PIN through any API? The person accepted the PIN fallback; if Android 10 cannot do it, Android 10 phones are biometric-only.
- Does the attestation of a key made with `setUserAuthenticationParameters(0, …)` omit `authTimeout`, or carry it as 0? The check in D2 must match what real devices report.
- Which app signing certificate digest should the server expect: the release key only, or a list that includes debug builds?
- Should the web tray inside the app show the request at all, or only "Open on this phone", to keep agent text away from the signing flow?
- How should a phone that is enrolled with several servers show which server a notification came from, when two servers share a name?
