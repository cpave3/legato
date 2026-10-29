## ADDED Requirements

### Requirement: Enrollment with key attestation
The server SHALL enroll an Android device only from an attestation chain that verifies to a shipped Google hardware attestation root, contains no revoked certificate, has attestation security level `TrustedEnvironment` or `StrongBox`, carries the server's unused enrollment challenge of less than 5 minutes, and whose hardware-enforced list has purpose sign, algorithm EC, curve P-256, a user authentication type, no `noAuthRequired` and no `authTimeout`, for the application ID of the Legato app. The leaf's public key SHALL become the device key.

#### Scenario: Hardware key enrolled
- **WHEN** the app posts a chain from a StrongBox key that needs authentication for each use
- **THEN** the server SHALL enroll the device and record its security level as `StrongBox`

#### Scenario: Software key
- **WHEN** the chain's attestation security level is `Software`
- **THEN** the server SHALL refuse the enrollment

#### Scenario: Key usable without authentication
- **WHEN** the hardware-enforced list has `noAuthRequired`
- **THEN** the server SHALL refuse the enrollment

#### Scenario: Unknown root
- **WHEN** the chain does not verify to a shipped root
- **THEN** the server SHALL refuse the enrollment

#### Scenario: Reused challenge
- **WHEN** the attestation challenge was already used
- **THEN** the server SHALL refuse the enrollment

### Requirement: First device and later devices
The first device on a server SHALL be enrolled with the web token alone. Each later device and each desktop passkey SHALL be an approval request that completes only after a signed approval from an enrolled device. Removing a device SHALL be an approval request that needs a signature from an enrolled device.

#### Scenario: Second phone
- **WHEN** a second phone posts a valid chain while a device is enrolled
- **THEN** the server SHALL raise an approval request and SHALL NOT enroll the phone until a signed approval arrives

#### Scenario: Removal without a signature
- **WHEN** a client calls `DELETE /api/devices/{id}` and the approval is decided without a signature
- **THEN** the device SHALL stay enrolled

### Requirement: Canonical requests and challenges
`POST /api/approvals/{id}/challenge` SHALL return the approval's canonical object serialized by RFC 8785, a random 32-byte challenge bound to that approval and the calling device, and an expiry 5 minutes later. The canonical object SHALL hold the server ID, the approval ID and revision, the source, task, execution, credential, operation, resources, options, and the agent's reason as it was written.

#### Scenario: Same request, same bytes
- **WHEN** the challenge endpoint is called twice for an unchanged request
- **THEN** both responses SHALL carry identical canonical bytes and different challenges

### Requirement: Signed decisions
A decision on `POST /api/approvals/{id}/decision` with a `signature` SHALL be applied only when the challenge belongs to that approval and device, is unused and unexpired, the canonical object built at that moment is byte-equal to the one signed, and the ECDSA P-256 signature over the RFC 8785 form of `{v, device, request, option, challenge}` verifies with the device key. The challenge SHALL be spent whatever the outcome. The audit log SHALL record the device.

#### Scenario: Valid signature
- **WHEN** an enrolled phone signs option `session` with a fresh challenge
- **THEN** the server SHALL apply the decision and record the device

#### Scenario: Replay
- **WHEN** a signature is posted again with the same challenge
- **THEN** the server SHALL refuse it

#### Scenario: Request changed after the challenge
- **WHEN** the approval request's revision changed between the challenge and the decision
- **THEN** the server SHALL refuse the decision

#### Scenario: Option not signed
- **WHEN** the decision names an option other than the one in the signed message
- **THEN** the server SHALL refuse the decision

### Requirement: Signed rules and defaults
A rule with `signed: true` SHALL accept only a signed decision; while no device is enrolled, its requests SHALL not be approvable and their cards SHALL say a device must be enrolled. Once at least one device is enrolled, `request` rules and approval requests for changes to credential rules, profiles and devices SHALL need a signature unless the rule says `signed: false`. Before the first enrollment these defaults SHALL NOT apply.

#### Scenario: Explicit signed rule, no device
- **WHEN** a request under a `signed: true` rule is approved without a signature and no device is enrolled
- **THEN** the server SHALL refuse the decision with an error that says a device must be enrolled

#### Scenario: Default after enrollment
- **WHEN** a device is enrolled and a request under a `request` rule with no `signed` field is approved without a signature
- **THEN** the server SHALL refuse the decision

#### Scenario: Default before enrollment
- **WHEN** no device is enrolled and a request under a `request` rule with no `signed` field is approved without a signature
- **THEN** the server SHALL apply the decision

### Requirement: Devices in Settings
The web Settings SHALL list enrolled devices and passkeys with name, kind, security level and last use, SHALL offer Remove for each, and SHALL show a notice while no device is enrolled.

#### Scenario: Remove
- **WHEN** the person presses Remove on a device
- **THEN** the web UI SHALL raise the removal approval request and show that it needs a signature

### Requirement: Desktop passkeys
A desktop SHALL be enrollable as a WebAuthn platform authenticator after a signed approval from an enrolled device. For a decision with a passkey, the WebAuthn challenge SHALL be the SHA-256 of the signed message, and the server SHALL verify the assertion as it verifies a device signature. The web UI SHALL offer passkeys only in a secure context.

#### Scenario: Passkey decision
- **WHEN** an enrolled passkey asserts over the hash of a fresh signed message
- **THEN** the server SHALL apply the decision as a signed decision

#### Scenario: No secure context
- **WHEN** the web UI is opened over plain HTTP on an address other than `localhost`
- **THEN** it SHALL NOT offer passkey enrollment

### Requirement: Device reset at the host
`legato device reset` SHALL remove every enrolled device and passkey of the server, record the reset in the audit log, and raise an Attention item and an ntfy notice.

#### Scenario: Reset
- **WHEN** the person runs `legato device reset`
- **THEN** no device SHALL remain enrolled and the audit log SHALL hold a reset entry
