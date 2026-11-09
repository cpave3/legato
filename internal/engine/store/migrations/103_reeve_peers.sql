-- Reeve to Reeve. A paired main's token gets the reeve scope (on by
-- default; the worker's person can take it away). reeve_peers are the
-- other instances this Reeve can message: by a Reeve pairing, as a worker
-- of this main, or from the reply contact a Reeve prime sent; token_secret
-- names the secret holding the token for them. reeve_outbox holds replies
-- to a peer that could not be reached, given on its next connection.
-- reeve_inbox holds a peer's message waiting for the person (reeve.peer:
-- ask).
UPDATE integration_tokens SET scopes = '["handoff","reeve"]' WHERE kind = 'main' AND scopes = '["handoff"]';
CREATE TABLE IF NOT EXISTS reeve_peers (
    instance_id    TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    via            TEXT NOT NULL,
    address        TEXT NOT NULL DEFAULT '',
    ca_certificate TEXT NOT NULL DEFAULT '',
    token_secret   TEXT NOT NULL DEFAULT '',
    reply_given    INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE TABLE IF NOT EXISTS reeve_outbox (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id TEXT NOT NULL,
    text        TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE TABLE IF NOT EXISTS reeve_inbox (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id TEXT NOT NULL,
    name        TEXT NOT NULL,
    role        TEXT NOT NULL DEFAULT '',
    text        TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
