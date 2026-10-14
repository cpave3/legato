-- Directories the person saved from a spawn dialog, offered there beside the
-- repositories named in the configuration. The path is absolute and is the
-- identity: saving one again renames it.
CREATE TABLE IF NOT EXISTS directory_presets (
    path       TEXT PRIMARY KEY CHECK (path != ''),
    name       TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
