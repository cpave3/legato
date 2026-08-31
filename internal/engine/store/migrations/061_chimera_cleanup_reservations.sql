CREATE TABLE IF NOT EXISTS chimera_cleanup_reservations (
    task_id TEXT PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
    workflow_run_id TEXT NOT NULL,
    workflow_step_id TEXT NOT NULL,
    chimera_session_id TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(workflow_run_id, workflow_step_id)
);
