-- Integrations: services outside Legato that act on it with a scoped token
-- (Argus first). See openspec/changes/argus-integration.

-- Bearer tokens with scopes. Only the SHA-256 of a token is kept.
CREATE TABLE IF NOT EXISTS integration_tokens (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    scopes       TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_used_at TEXT,
    revoked_at   TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_integration_tokens_live_name ON integration_tokens(name) WHERE revoked_at IS NULL;

-- Legato's registration as an OAuth client of an integration. The refresh
-- token is in Legato's sealed secret store, not here.
CREATE TABLE IF NOT EXISTS integrations (
    name           TEXT PRIMARY KEY,
    url            TEXT NOT NULL,
    issuer         TEXT NOT NULL DEFAULT '',
    token_endpoint TEXT NOT NULL,
    resource       TEXT NOT NULL DEFAULT '',
    client_id      TEXT NOT NULL,
    state          TEXT NOT NULL DEFAULT 'connected',
    connected_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- A brief an integration handed to a task's agent.
CREATE TABLE IF NOT EXISTS delegations (
    id              TEXT PRIMARY KEY,
    integration     TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash    TEXT NOT NULL,
    case_id         TEXT NOT NULL,
    mode            TEXT NOT NULL,
    target          TEXT NOT NULL,
    brief           TEXT NOT NULL,
    title           TEXT NOT NULL DEFAULT '',
    task_id         TEXT NOT NULL DEFAULT '',
    execution_id    TEXT NOT NULL DEFAULT '',
    route           TEXT NOT NULL,
    state           TEXT NOT NULL,
    enforced        INTEGER NOT NULL DEFAULT 0,
    error_code      TEXT NOT NULL DEFAULT '',
    error_message   TEXT NOT NULL DEFAULT '',
    approval_id     TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    delivered_at    TEXT,
    UNIQUE (integration, idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_delegations_task ON delegations(task_id);
CREATE INDEX IF NOT EXISTS idx_delegations_state ON delegations(state);

-- Further messages from the integration to a delegation's agent.
CREATE TABLE IF NOT EXISTS delegation_messages (
    id              TEXT PRIMARY KEY,
    delegation_id   TEXT NOT NULL REFERENCES delegations(id) ON DELETE CASCADE,
    idempotency_key TEXT NOT NULL,
    request_hash    TEXT NOT NULL,
    text            TEXT NOT NULL,
    state           TEXT NOT NULL,
    error_code      TEXT NOT NULL DEFAULT '',
    error_message   TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    delivered_at    TEXT,
    UNIQUE (delegation_id, idempotency_key)
);

-- Replies from agents waiting to be sent to the integration.
CREATE TABLE IF NOT EXISTS case_reply_outbox (
    id              TEXT PRIMARY KEY,
    delegation_id   TEXT NOT NULL,
    case_id         TEXT NOT NULL,
    task_id         TEXT NOT NULL,
    body            TEXT NOT NULL,
    state           TEXT NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT NOT NULL,
    last_error      TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    sent_at         TEXT
);
CREATE INDEX IF NOT EXISTS idx_case_reply_outbox_due ON case_reply_outbox(state, next_attempt_at);

-- An execution that may look but not change: its checkout is mounted
-- read-only and the credential broker refuses its pushes and pull request
-- writes (a find-out delegation).
ALTER TABLE executions ADD COLUMN read_only INTEGER NOT NULL DEFAULT 0;
