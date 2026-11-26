-- The newest subscription usage reading of each provider (claude, codex),
-- as the service's JSON: the cache every process serves, written only by
-- the one that polls (LEG-659).
CREATE TABLE IF NOT EXISTS subscription_usage (
    provider   TEXT PRIMARY KEY,
    reading    TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
