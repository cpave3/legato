-- Each execution whose managed hooks a spawn decided, even when the folder
-- held none, and whose agent it was (task_id, slot): a slot's latest
-- execution can still be found after its agent stops and its session row
-- is gone.
CREATE TABLE IF NOT EXISTS execution_managed_hook_owners (
    execution_id TEXT PRIMARY KEY,
    task_id      TEXT NOT NULL,
    slot         TEXT NOT NULL DEFAULT 'main',
    recorded_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_execution_managed_hook_owners_slot ON execution_managed_hook_owners (task_id, slot, recorded_at);

-- The managed hooks each execution was decided at its spawn: one row per hook
-- in the managed-hooks folder then, on (off = '') or off with every reason
-- that turned it off, space separated (off:global off:spawn ...). events are
-- the hook's events, comma separated. Written once per execution and never
-- changed: a hook turned off later leaves a running agent's record alone.
CREATE TABLE IF NOT EXISTS execution_managed_hooks (
    execution_id TEXT NOT NULL,
    name         TEXT NOT NULL,
    events       TEXT NOT NULL DEFAULT '',
    off          TEXT NOT NULL DEFAULT '',
    recorded_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (execution_id, name)
);
