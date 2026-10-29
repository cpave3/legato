## Why

Any client that holds the web token can answer an approval request. An agent that runs outside the sandbox can read that token from Legato's data directory, and so can approve its own request for access. Before this change, the only defence is to keep sensitive rules at `deny`.

A decision that needs the person must be made by something an agent cannot use: a key in a phone's secure hardware that unlocks only with the person's fingerprint or PIN.

## What Changes

- **Device enrollment per server, with key attestation.** The Legato Android app makes an EC P-256 key in the Android Keystore (in StrongBox when the phone has it) that needs a strong biometric or the device PIN for each use and is invalidated when a new fingerprint is enrolled. It sends the key's attestation chain. The server accepts the key only when the chain shows hardware-backed storage and authentication for each use.
- **Signed decisions.** The server builds a canonical JSON form of an approval request and gives a one-time challenge. The device signs the canonical request, the chosen option and the challenge. The server checks the signature before it applies the decision.
- **`signed` rules.** A rule with `signed: true` accepts only a signed decision. Once a device is enrolled, `request` rules and changes to credential rules, profiles and devices need a signature by default.
- **More devices.** A second Android device or a desktop passkey (WebAuthn) is enrolled only with a signed approval from a device already enrolled.
- **Devices in web Settings.** A list of enrolled devices with their names, kinds and last use, and Remove, which itself needs a signature.
- **Android approval sheet.** A native sheet shows the request from the canonical form the app fetched itself, with the agent's reason quoted and labelled as the agent's text. The biometric prompt repeats the scope. The app signs and posts the decision.
- **Two ways in.** The ntfy notification's link `legato://approval?url=<server>&id=<id>` opens the sheet. A page in the app can open it with the bridge message `{legatoShell: "approval", key, id}`; the app answers with a `legato:shell-approval` event.
- **Open approvals from every server.** A native list in the app of the open approvals of all saved servers.

## Capabilities

### New Capabilities
- `signed-approvals`: Enrollment with attestation checks, canonical requests, challenges, signature checks, `signed` rules and defaults, enrolling more devices and passkeys, and the device list.
- `mobile-approvals`: The Android Keystore key, the native approval sheet, the biometric prompt, the `approval` bridge message, notification links, and the list of open approvals across servers.

### Modified Capabilities
- None in the main specs. `approvals` (from `agent-approvals`) gains signed decisions through its existing decision endpoint.

## Impact

- **Code:** `internal/engine/attest` (new: Android key attestation parsing and chain checks, self-contained); `internal/service` (`device.go`, signed decisions in `ApprovalService`, signed defaults in `CredentialBroker`); `internal/engine/store` (migration for `approval_devices`, `approval_challenges`); `internal/server` (device and challenge endpoints, passkey ceremonies); `web/src` (Settings › Devices, signed cards in the tray, passkey enrollment); `mobile/app/src/main/java/dev/legato/mobile/` (`ApprovalKey.kt`, `ApprovalSheet.kt`, `ApprovalList.kt`, `PageContract.kt` and `ShellApproval.kt` for the bridge).
- **APIs:** `POST /api/devices/challenge`, `POST /api/devices`, `GET /api/devices`, `DELETE /api/devices/{id}`, `POST /api/approvals/{id}/challenge`, a `signature` field on `POST /api/approvals/{id}/decision`, passkey registration endpoints, the `approval` bridge message and the `legato:shell-approval` event.
- **Config:** none required. A rule's `signed` field now takes effect.
- **Dependencies:** `androidx.biometric` in the app; `github.com/go-webauthn/webauthn` on the server; Google's hardware attestation root certificates, shipped in the binary.
- **Unchanged:** approvals from rules that are not signed are answered as before, from any client with the token. Phase 1's page-based approval link keeps working for a phone that is not enrolled.
