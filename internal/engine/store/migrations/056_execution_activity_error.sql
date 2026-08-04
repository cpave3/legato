ALTER TABLE execution_activity RENAME TO execution_activity_old;

CREATE TABLE execution_activity (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
    state TEXT NOT NULL CHECK (state IN ('working', 'waiting', 'error')),
    started_at DATETIME NOT NULL,
    ended_at DATETIME,
    working_dir TEXT,
    CHECK (ended_at IS NULL OR ended_at >= started_at)
);

INSERT INTO execution_activity (id, execution_id, state, started_at, ended_at, working_dir)
SELECT id, execution_id, state, started_at, ended_at, working_dir
FROM execution_activity_old;

DROP TABLE execution_activity_old;

CREATE INDEX idx_execution_activity_execution_time ON execution_activity(execution_id, started_at);
CREATE INDEX idx_execution_activity_range ON execution_activity(started_at, ended_at);
CREATE UNIQUE INDEX idx_execution_activity_one_open ON execution_activity(execution_id) WHERE ended_at IS NULL;
