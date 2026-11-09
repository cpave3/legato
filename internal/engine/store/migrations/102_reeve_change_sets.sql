-- The Reeve's change sets: the calls it proposed together for one step,
-- approved or denied as one (approval source reeve_change_set) and run by
-- Legato in order. calls is the set as the approval shows it; results what
-- each call did. waiting is 1 while propose_changes waits for the decision,
-- so a denial then is reported by the tool rather than by a message.
CREATE TABLE IF NOT EXISTS reeve_change_sets (
    id          TEXT PRIMARY KEY,
    task_id     TEXT NOT NULL,
    approval_id TEXT NOT NULL DEFAULT '',
    reason      TEXT NOT NULL DEFAULT '',
    level       TEXT NOT NULL DEFAULT '',
    calls       TEXT NOT NULL DEFAULT '[]',
    state       TEXT NOT NULL DEFAULT 'pending',
    results     TEXT NOT NULL DEFAULT '[]',
    waiting     INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_reeve_change_sets_state ON reeve_change_sets(state);
