-- Agent stages (LEG-440): where an agent place's work stands, inferred by
-- Legato apart from activity and from the card's column. A place is a task's
-- slot (task_id, slot); a workflow agent's place is its execution
-- (place_execution_id), '' for every other place. execution_id is the
-- execution that last changed it. See docs/ai/agent-states.md § Stages.
CREATE TABLE IF NOT EXISTS agent_stages (
    task_id            TEXT NOT NULL,
    slot               TEXT NOT NULL DEFAULT 'main',
    place_execution_id TEXT NOT NULL DEFAULT '',
    stage              TEXT NOT NULL CHECK (stage IN ('exploring', 'planning', 'implementing', 'reviewing', 'finished')),
    reason             TEXT NOT NULL DEFAULT '',
    source             TEXT NOT NULL CHECK (source IN ('start', 'operator', 'agent', 'milestone', 'evidence')),
    since              TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    execution_id       TEXT NOT NULL DEFAULT '',
    updated_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (task_id, slot, place_execution_id)
);

-- The history of a place's stage: one row for each change.
CREATE TABLE IF NOT EXISTS agent_stage_events (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id            TEXT NOT NULL,
    slot               TEXT NOT NULL DEFAULT 'main',
    place_execution_id TEXT NOT NULL DEFAULT '',
    from_stage         TEXT NOT NULL DEFAULT '',
    stage              TEXT NOT NULL,
    reason             TEXT NOT NULL DEFAULT '',
    source             TEXT NOT NULL,
    execution_id       TEXT NOT NULL DEFAULT '',
    created_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_agent_stage_events_place ON agent_stage_events (task_id, slot, place_execution_id, id);
