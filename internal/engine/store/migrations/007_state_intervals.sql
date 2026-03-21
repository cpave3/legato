CREATE TABLE state_intervals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    state TEXT NOT NULL,
    started_at DATETIME NOT NULL DEFAULT (datetime('now')),
    ended_at DATETIME,
    CONSTRAINT valid_state CHECK (state IN ('working', 'waiting'))
);
CREATE INDEX idx_state_intervals_task ON state_intervals(task_id);
