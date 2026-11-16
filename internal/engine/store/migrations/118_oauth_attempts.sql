-- OAuth response delivery and account confirmation share one durable claim.
-- Secret material lives in the sealed store, under the attempt's opaque ID.
CREATE TABLE IF NOT EXISTS oauth_connections (
    connection TEXT PRIMARY KEY,
    generation INTEGER NOT NULL DEFAULT 0,
    active_attempt TEXT NOT NULL DEFAULT '',
    -- One process at a time refreshes a connection's tokens: it holds this
    -- lease until it has sealed the result.
    refresh_holder TEXT NOT NULL DEFAULT '',
    refresh_lease_until INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS oauth_attempts (
    id TEXT PRIMARY KEY,
    connection TEXT NOT NULL REFERENCES oauth_connections(connection),
    generation INTEGER NOT NULL,
    owner TEXT NOT NULL,
    instance_id TEXT NOT NULL DEFAULT '',
    state_hash TEXT NOT NULL UNIQUE,
    receiver_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'exchanging', 'awaiting_confirmation', 'authorized', 'cancelled', 'superseded', 'failed', 'denied', 'expired', 'interrupted', 'reauth_required')),
    error_code TEXT NOT NULL DEFAULT '',
    -- The scope the attempt asked for, shown to the operator at confirmation.
    scope TEXT NOT NULL DEFAULT '',
    -- A web attempt's loopback receiver lives in one process, which renews
    -- this lease while it listens; a lapsed lease means that process is gone.
    receiver_owner TEXT NOT NULL DEFAULT '',
    receiver_lease_until INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_oauth_attempts_connection ON oauth_attempts(connection, status);
CREATE INDEX IF NOT EXISTS idx_oauth_attempts_expiry ON oauth_attempts(status, expires_at);
