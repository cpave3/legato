-- Drop the FK on state_intervals.task_id so that sub-task IDs (which exist
-- only in swarm_subtasks, not the tasks table) can record state intervals.
-- SQLite does not support ALTER TABLE DROP CONSTRAINT, so we table-recreate.

-- Preserve the current schema with working_dir (migration 016).
CREATE TABLE state_intervals_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL,
    state TEXT NOT NULL,
    started_at DATETIME NOT NULL DEFAULT (datetime('now')),
    ended_at DATETIME,
    working_dir TEXT,
    CONSTRAINT valid_state CHECK (state IN ('working', 'waiting'))
);

-- Migrate existing data
INSERT INTO state_intervals_new (id, task_id, state, started_at, ended_at, working_dir)
SELECT id, task_id, state, started_at, ended_at, working_dir FROM state_intervals;

-- Drop old table and rename
DROP TABLE state_intervals;
ALTER TABLE state_intervals_new RENAME TO state_intervals;

-- Restore the index
CREATE INDEX idx_state_intervals_task ON state_intervals(task_id);
