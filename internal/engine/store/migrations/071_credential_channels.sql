-- Placeholder keys of the http-proxy channel. Only the SHA-256 of a
-- placeholder is kept: the agent holds the placeholder, Legato the key.
CREATE TABLE IF NOT EXISTS credential_placeholders (
    hash         TEXT PRIMARY KEY,
    execution_id TEXT NOT NULL,
    task_id      TEXT NOT NULL DEFAULT '',
    credential   TEXT NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    revoked_at   DATETIME
);
CREATE INDEX IF NOT EXISTS credential_placeholders_execution ON credential_placeholders (execution_id);

-- What the channels measured of each use (requests, bytes, characters), for
-- the limits of rules. Kept here so a restart does not reset a daily limit.
CREATE TABLE IF NOT EXISTS credential_meter_totals (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    credential TEXT NOT NULL,
    meter      TEXT NOT NULL,
    amount     INTEGER NOT NULL,
    at         TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS credential_meter_totals_lookup ON credential_meter_totals (credential, meter, at);
