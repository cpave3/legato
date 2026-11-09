-- A restart of Legato that can be rolled back: what the restart changed
-- (the config.yaml backup, the database settings with their old values), and
-- how it ended. A start with a pending record that does not load its
-- configuration, or is not healthy in time, puts both back.
CREATE TABLE IF NOT EXISTS restart_records (
    id            TEXT PRIMARY KEY,
    reason        TEXT NOT NULL DEFAULT '',
    requested_by  TEXT NOT NULL DEFAULT '',
    version       TEXT NOT NULL DEFAULT '',
    config_path   TEXT NOT NULL DEFAULT '',
    config_backup TEXT NOT NULL DEFAULT '',
    settings      TEXT NOT NULL DEFAULT '[]',
    state         TEXT NOT NULL DEFAULT 'pending',
    failure       TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    finished_at   TEXT
);
