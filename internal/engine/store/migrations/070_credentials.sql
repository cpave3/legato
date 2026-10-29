-- Legato's own secret store: values sealed under a master key that is kept
-- outside the database (systemd-creds, the Secret Service, the Keychain, a
-- passphrase or a key file). Nothing here opens without it.
CREATE TABLE IF NOT EXISTS secrets (
    name       TEXT PRIMARY KEY,
    type       TEXT NOT NULL,
    nonce      BLOB NOT NULL,
    ciphertext BLOB NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- What an agent session may do with a credential: operations on resources,
-- until an expiry, for a number of uses, or until the session ends.
CREATE TABLE IF NOT EXISTS credential_grants (
    id              TEXT PRIMARY KEY,
    execution_id    TEXT NOT NULL,
    task_id         TEXT NOT NULL DEFAULT '',
    credential      TEXT NOT NULL,
    operations_json TEXT NOT NULL DEFAULT '[]',
    resources_json  TEXT NOT NULL DEFAULT '[]',
    mode            TEXT NOT NULL,
    uses_left       INTEGER,
    expires_at      DATETIME,
    approval_id     TEXT NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ended_at        DATETIME
);
CREATE INDEX IF NOT EXISTS credential_grants_live ON credential_grants (execution_id, credential, ended_at);

-- Every request, decision, grant, use and refusal, append-only. Each row
-- holds the hash of the row before it, so a gap or an edit shows.
CREATE TABLE IF NOT EXISTS credential_audit (
    seq          INTEGER PRIMARY KEY AUTOINCREMENT,
    -- TEXT, not DATETIME: the time is part of the hash, so it must come
    -- back exactly as it was written.
    at           TEXT NOT NULL,
    event        TEXT NOT NULL,
    execution_id TEXT NOT NULL DEFAULT '',
    task_id      TEXT NOT NULL DEFAULT '',
    agent_kind   TEXT NOT NULL DEFAULT '',
    workspace_id TEXT NOT NULL DEFAULT '',
    credential   TEXT NOT NULL DEFAULT '',
    operation    TEXT NOT NULL DEFAULT '',
    resource     TEXT NOT NULL DEFAULT '',
    amount       TEXT NOT NULL DEFAULT '',
    approval_id  TEXT NOT NULL DEFAULT '',
    decided_by   TEXT NOT NULL DEFAULT '',
    detail       TEXT NOT NULL DEFAULT '',
    prev_hash    TEXT NOT NULL DEFAULT '',
    hash         TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS credential_audit_credential ON credential_audit (credential, event, at);
CREATE INDEX IF NOT EXISTS credential_audit_task ON credential_audit (task_id, at);
