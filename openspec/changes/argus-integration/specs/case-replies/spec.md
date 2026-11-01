## ADDED Requirements

### Requirement: The reply_case tool
When `integrations.argus` is configured, `legato mcp serve` SHALL list `reply_case(delegation_id, outcome, text)` in every session. `outcome` SHALL be `done`, `finding`, `question` or `cannot`, and `text` 1 byte to 16 KiB. A call SHALL be refused with a tool error, storing nothing, unless the delegation exists, is `delivered`, and belongs to the caller's task. A different execution of that task SHALL be allowed. An accepted call SHALL add `{delegationId, outcome, text, agent: {taskId, agentId}}` to the outbox, where `agentId` is the caller's execution ID, and the host SHALL try it at once, waiting up to 10 seconds. The tool SHALL answer:
- `sent` on success;
- a tool error with Argus's code and message on a 4xx;
- `queued` when it will retry.

#### Scenario: Reply from the delegated task
- **WHEN** the agent of the delegation's task calls `reply_case` with outcome `finding`
- **THEN** Argus SHALL receive the reply with that task ID and the agent's execution ID, and the tool SHALL answer `sent`

#### Scenario: Reply from another task
- **WHEN** an agent calls `reply_case` with a delegation sent to a different task
- **THEN** the call SHALL fail and nothing SHALL be sent

#### Scenario: Argus down
- **WHEN** Argus does not answer
- **THEN** the tool SHALL answer `queued`, and the reply SHALL be sent when Argus answers again

### Requirement: Connecting to Argus
`legato integration connect argus <url>` SHALL:
- discover Argus's authorization server from `/.well-known/oauth-protected-resource` and `/.well-known/oauth-authorization-server`;
- register as a public client with the redirect `http://127.0.0.1/callback`;
- authorize with PKCE (S256), a random `state`, scope `argus:reply` and the `resource` named by the protected-resource metadata, through a loopback listener on a free port;
- exchange the code for tokens.

It SHALL store the refresh token in Legato's sealed store and the registration in the database, and never print or log a token. With `--no-browser`, it SHALL print the authorization address and accept the redirected address pasted back. It SHALL refuse to run when the sealed store's master key source is `passphrase`, saying why. `legato integration status argus` SHALL report the connection and the outbox. `legato integration disconnect argus` SHALL remove the refresh token and the registration.

#### Scenario: Connect
- **WHEN** the person runs `connect` and consents in Argus
- **THEN** `status` SHALL show `connected`, and the sealed store SHALL hold an `oauth-refresh` secret for Argus

#### Scenario: State mismatch
- **WHEN** the redirect carries a `state` other than the one sent
- **THEN** the command SHALL fail and store nothing

### Requirement: Access tokens
The host that holds the broker lock SHALL keep the access token in memory only. It SHALL refresh the token when it expires within 60 seconds or after a 401, one refresh at a time, and SHALL store the rotated refresh token before using the new access token. On `invalid_grant` it SHALL read the stored refresh token again and retry once if it changed. Otherwise it SHALL mark the integration `disconnected`, raise Attention `integration_disconnected`, and hold the outbox until the person connects again.

#### Scenario: Rotation stored
- **WHEN** a refresh returns a new refresh token
- **THEN** the sealed store SHALL hold the new token before the next reply is sent

#### Scenario: Refresh token expired
- **WHEN** Argus answers `invalid_grant` and the stored token has not changed
- **THEN** the integration SHALL be `disconnected`, Attention SHALL show `integration_disconnected`, and queued replies SHALL stay `pending`

### Requirement: The reply outbox
The host that holds the broker lock SHALL send outbox entries as `POST <url>/api/cases/{caseId}/replies` with the bearer access token and `Idempotency-Key: <outbox id>`. It SHALL:
- mark an entry `sent` on 2xx;
- refresh once and retry on 401;
- on a 401 after a fresh token, or a 403, mark the integration `disconnected` as for `invalid_grant` and keep the entry `pending`;
- mark it `failed` on any other 4xx;
- retry 5xx answers, network errors and timeouts with backoff from 30 seconds, doubling up to 1 hour.

An entry unsent after 24 hours SHALL become `failed` and raise Attention `case_reply_failed` on the task. Entries written by a process without a broker SHALL be sent by the host.

#### Scenario: Retry keeps the key
- **WHEN** a reply fails with a network error and is sent again
- **THEN** both attempts SHALL carry the same `Idempotency-Key`

#### Scenario: Refused by Argus
- **WHEN** Argus answers 409 because the case is closed
- **THEN** the entry SHALL be `failed` and SHALL NOT be retried
