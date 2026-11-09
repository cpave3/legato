-- Entries of the lists of named entries that had no table of their own
-- (workspaces, macros, agent tools, credentials, credential sources, tmux
-- options, sandbox mounts, swarm prompts), kept beside config.yaml's. value
-- is the entry as JSON in config.yaml's shape. An entry whose name the file
-- has is never kept here.
CREATE TABLE IF NOT EXISTS setting_entries (
    list       TEXT NOT NULL,
    name       TEXT NOT NULL,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_by TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (list, name)
);
