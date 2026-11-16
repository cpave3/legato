-- Whether the agent tool of an execution reports its sub-agents to Legato.
-- Executions from before this migration, and those of tools that report none
-- (Codex), have 0: their totals cover the top-level agent only.
ALTER TABLE executions ADD COLUMN subagent_telemetry INTEGER NOT NULL DEFAULT 0;

-- The working and waiting spans of the sub-agents an execution's agent ran,
-- at any depth. A sub-agent belongs to the execution (and so the task) of the
-- session that ran it; parent_subagent_id is the sub-agent that started it,
-- '' for one the top-level agent started or when the tool does not say.
CREATE TABLE IF NOT EXISTS subagent_activity (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
    subagent_id TEXT NOT NULL,
    parent_subagent_id TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK (state IN ('working', 'waiting')),
    started_at DATETIME NOT NULL,
    ended_at DATETIME,
    CHECK (ended_at IS NULL OR ended_at >= started_at)
);

CREATE INDEX IF NOT EXISTS idx_subagent_activity_execution ON subagent_activity(execution_id, subagent_id, started_at);
CREATE INDEX IF NOT EXISTS idx_subagent_activity_range ON subagent_activity(started_at, ended_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_subagent_activity_one_open ON subagent_activity(execution_id, subagent_id) WHERE ended_at IS NULL;
