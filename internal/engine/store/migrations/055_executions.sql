CREATE TABLE IF NOT EXISTS executions (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('interactive', 'workflow_agent', 'swarm_conductor', 'swarm_worker')),
    status TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled', 'lost')),
    task_id TEXT,
    execution_task_id TEXT,
    task_title TEXT NOT NULL DEFAULT '',
    workflow_run_id TEXT,
    workflow_attempt_id TEXT,
    swarm_id TEXT,
    swarm_subtask_id TEXT,
    agent_session_id INTEGER,
    agent_kind TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT '',
    working_dir TEXT NOT NULL DEFAULT '',
    provider_session_id TEXT NOT NULL DEFAULT '',
    started_at DATETIME NOT NULL,
    ended_at DATETIME,
    CHECK ((status = 'running' AND ended_at IS NULL) OR (status != 'running' AND ended_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS idx_executions_status_time ON executions(status, started_at);
CREATE INDEX IF NOT EXISTS idx_executions_task_time ON executions(task_id, started_at);
CREATE INDEX IF NOT EXISTS idx_executions_directory_time ON executions(working_dir, started_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_executions_workflow_attempt ON executions(workflow_attempt_id) WHERE workflow_attempt_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_executions_swarm ON executions(swarm_id, swarm_subtask_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_executions_agent_session ON executions(agent_session_id) WHERE agent_session_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS execution_activity (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
    state TEXT NOT NULL CHECK (state IN ('working', 'waiting')),
    started_at DATETIME NOT NULL,
    ended_at DATETIME,
    working_dir TEXT,
    CHECK (ended_at IS NULL OR ended_at >= started_at)
);

CREATE INDEX IF NOT EXISTS idx_execution_activity_execution_time ON execution_activity(execution_id, started_at);
CREATE INDEX IF NOT EXISTS idx_execution_activity_range ON execution_activity(started_at, ended_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_execution_activity_one_open ON execution_activity(execution_id) WHERE ended_at IS NULL;

CREATE TABLE IF NOT EXISTS reporting_metadata (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    activity_epoch DATETIME NOT NULL
);
INSERT INTO reporting_metadata (id, activity_epoch) VALUES (1, datetime('now')) ON CONFLICT(id) DO NOTHING;

DROP TABLE IF EXISTS state_intervals;
