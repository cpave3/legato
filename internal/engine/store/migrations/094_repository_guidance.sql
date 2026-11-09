-- Guidance the person saved in the web UI for the agents that work in a
-- repository, beside the guidance of the configuration. repo is
-- host/owner/name, as config.NormalizeRepository writes it.
CREATE TABLE IF NOT EXISTS repository_guidance (
    repo       TEXT PRIMARY KEY,
    text       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
