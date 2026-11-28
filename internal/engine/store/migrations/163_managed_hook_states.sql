-- Whether each managed hook is given to agents, keyed by the hook's folder
-- name under the managed-hooks folder (managed_hooks.dir). A hook with no row
-- is on. The hooks themselves are files on disk, never in the database.
CREATE TABLE IF NOT EXISTS managed_hook_states (
    name       TEXT PRIMARY KEY,
    enabled    INTEGER NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
