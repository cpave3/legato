-- Devices that can sign approval decisions: an Android Keystore key the
-- phone attested, or a desktop passkey. pending: waiting for an enrolled
-- device to approve it; removed: kept for the audit log.
CREATE TABLE IF NOT EXISTS approval_devices (
    id             TEXT PRIMARY KEY,
    kind           TEXT NOT NULL,
    name           TEXT NOT NULL DEFAULT '',
    public_key     BLOB NOT NULL,
    security_level TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL,
    approval_id    TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_used_at   TEXT,
    removed_at     TEXT
);

-- One-time challenges: for enrolling a device, and for each signed
-- decision (bound to its approval and device). Only their hashes are kept.
CREATE TABLE IF NOT EXISTS approval_challenges (
    hash        TEXT PRIMARY KEY,
    purpose     TEXT NOT NULL,
    approval_id TEXT NOT NULL DEFAULT '',
    device_id   TEXT NOT NULL DEFAULT '',
    expires_at  TEXT NOT NULL,
    used_at     TEXT
);

-- Values made once for this Legato and kept (the server ID devices sign).
CREATE TABLE IF NOT EXISTS instance_values (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

ALTER TABLE approval_requests ADD COLUMN signed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE approval_requests ADD COLUMN decided_device TEXT NOT NULL DEFAULT '';
