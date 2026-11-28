-- The managed hooks someone chose to turn off for an execution, and where the
-- choice came from: spawn (hooks_off on a spawn), and later step and handoff.
-- What a relaunch carries; the other layers are worked out again each spawn.
CREATE TABLE IF NOT EXISTS execution_managed_hooks_off (
    execution_id TEXT NOT NULL,
    name         TEXT NOT NULL,
    source       TEXT NOT NULL,
    PRIMARY KEY (execution_id, name, source)
);
