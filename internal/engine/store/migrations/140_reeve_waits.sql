-- An agent's wait on the Reeve (LEG-613): it asked the Reeve with
-- message_reeve and no answer, nor turn of the operator's, has come since.
-- One row per execution, so a new or restarted execution starts with none;
-- since is the first open question's time (RFC 3339, UTC), and a cleared
-- row says why (reeve_answered, operator_turn).
CREATE TABLE IF NOT EXISTS reeve_waits (
    execution_id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL,
    since TEXT NOT NULL,
    cleared_at TEXT,
    clear_reason TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_reeve_waits_task ON reeve_waits(task_id) WHERE cleared_at IS NULL;
