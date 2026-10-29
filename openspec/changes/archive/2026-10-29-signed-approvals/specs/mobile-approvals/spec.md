## ADDED Requirements

### Requirement: A Keystore key for each server
The Android app SHALL make one EC P-256 signing key for each server it enrolls with, in StrongBox when the phone has it and in the TEE otherwise. The key SHALL need user authentication for each use: on API 30 and later through `setUserAuthenticationParameters(0, AUTH_BIOMETRIC_STRONG | AUTH_DEVICE_CREDENTIAL)`, and on API 29 through `setUserAuthenticationValidityDurationSeconds(-1)`. On API 29, where biometrics alone unlock it, the key SHALL be invalidated when a new biometric is enrolled. It SHALL carry the server's enrollment challenge in its attestation.

#### Scenario: StrongBox missing
- **WHEN** the phone has no StrongBox
- **THEN** the app SHALL make the key in the TEE and enroll it

#### Scenario: New fingerprint
- **WHEN** a new fingerprint has been enrolled since a key made on API 29
- **THEN** signing SHALL fail with an invalidated key, and the app SHALL offer to enroll again

### Requirement: The approval sheet shows what the app fetched
The app SHALL open the approval sheet with a server and an approval ID only. It SHALL fetch `GET /api/approvals/{id}` and the challenge with its own stored token for that server, and SHALL draw the sheet from the canonical bytes it received. It SHALL show the agent's reason as plain text in a quote under the label "Agent's reason". It SHALL NOT use any other content from the page or the link.

#### Scenario: Page sends extra fields
- **WHEN** a bridge message carries fields besides `legatoShell`, `key` and `id`
- **THEN** the app SHALL ignore them

#### Scenario: Reason with markup
- **WHEN** the agent's reason contains HTML or Markdown
- **THEN** the sheet SHALL show it as literal text

### Requirement: Biometric prompt repeats the scope
On approval, the app SHALL show `BiometricPrompt` with a `CryptoObject` over the signature, a title naming the operation and the first resource, and a description naming the chosen option and the task. The prompt SHALL NOT contain the agent's reason. After authentication, the app SHALL sign the message, post the decision with the signature, and show the result.

#### Scenario: Approve
- **WHEN** the person chooses "For this session" and authenticates
- **THEN** the app SHALL post a decision with option `session` and a signature that the server accepts

#### Scenario: Authentication cancelled
- **WHEN** the person cancels the prompt
- **THEN** the app SHALL post nothing and the request SHALL stay open

### Requirement: Opening the sheet from a notification
The app SHALL handle `legato://approval?url=<server>&id=<id>`. For a saved server with an enrolled key it SHALL open the sheet. For a saved server without a key it SHALL open that server's page at the request. For a server that is not saved it SHALL say so and do nothing else.

#### Scenario: Enrolled server
- **WHEN** the person taps an ntfy notification for an enrolled server
- **THEN** the app SHALL open the native sheet for that approval

#### Scenario: Unknown server
- **WHEN** the link names a server that is not saved
- **THEN** the app SHALL NOT contact it

### Requirement: Opening the sheet from the page
The app SHALL add `approval: 1` to `window.legatoShell` for the top frame. It SHALL act on `{legatoShell: "approval", key, id}` only when the message comes from the top frame and carries the page's current key. After the sheet closes, it SHALL dispatch a `legato:shell-approval` event in the page with `{id, status, option?}`, where `status` is `decided`, `cancelled` or `error`.

#### Scenario: Message from a sandboxed frame
- **WHEN** a frame other than the top frame posts an approval message
- **THEN** the app SHALL ignore it

#### Scenario: Wrong key
- **WHEN** the message's key is not the page's current key
- **THEN** the app SHALL ignore it

#### Scenario: Result to the page
- **WHEN** the person approves in the sheet opened from the page
- **THEN** the page SHALL receive `legato:shell-approval` with `status: "decided"` and the option

### Requirement: Open approvals from every server
The app SHALL show a list of the open approvals of all saved servers, grouped by server, read from each server's `GET /api/approvals?status=open`. It SHALL reload the list when it opens, when the app returns to the foreground, and when an approval notification arrives. A server that does not answer SHALL be marked unreachable with its last list.

#### Scenario: Two servers
- **WHEN** two saved servers each have an open approval
- **THEN** the list SHALL show both, under each server's name

#### Scenario: Tap a row
- **WHEN** the person taps an approval in the list
- **THEN** the app SHALL open the sheet for it
