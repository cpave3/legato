-- Repositories the person added in the web UI, beside those of the
-- configuration: offered by the spawn dialogs (a fresh clone, or a folder)
-- and as workflow repository presets.
CREATE TABLE IF NOT EXISTS repository_presets (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL UNIQUE,
    repo        TEXT NOT NULL DEFAULT '',
    primary_dir TEXT NOT NULL DEFAULT '',
    base_branch TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
