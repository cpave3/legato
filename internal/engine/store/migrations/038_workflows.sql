CREATE TABLE workflow_runs (
    id TEXT PRIMARY KEY,
    workflow_name TEXT NOT NULL,
    workflow_title TEXT NOT NULL,
    definition_source TEXT NOT NULL,
    definition_digest TEXT NOT NULL,
    definition_json TEXT NOT NULL,
    inputs_json TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL,
    current_step_id TEXT NOT NULL DEFAULT '',
    task_id TEXT,
    error TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    started_at DATETIME,
    completed_at DATETIME
);
CREATE INDEX idx_workflow_runs_status ON workflow_runs(status, created_at DESC);

CREATE TABLE workflow_step_runs (
    run_id TEXT NOT NULL,
    step_id TEXT NOT NULL,
    step_order INTEGER NOT NULL,
    title TEXT NOT NULL,
    executor_type TEXT NOT NULL,
    definition_json TEXT NOT NULL,
    resolved_inputs_json TEXT NOT NULL DEFAULT '{}',
    outputs_json TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL,
    latest_attempt INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    started_at DATETIME,
    completed_at DATETIME,
    PRIMARY KEY (run_id, step_id),
    UNIQUE (run_id, step_order),
    FOREIGN KEY (run_id) REFERENCES workflow_runs(id) ON DELETE CASCADE
);

CREATE TABLE workflow_step_attempts (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    step_id TEXT NOT NULL,
    attempt_number INTEGER NOT NULL,
    executor_type TEXT NOT NULL,
    agent_session_id INTEGER,
    task_id TEXT NOT NULL DEFAULT '',
    tmux_session TEXT NOT NULL DEFAULT '',
    agent_kind TEXT NOT NULL DEFAULT '',
    tier TEXT NOT NULL DEFAULT '',
    working_dir TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    started_at DATETIME NOT NULL DEFAULT (datetime('now')),
    ended_at DATETIME,
    UNIQUE (run_id, step_id, attempt_number),
    FOREIGN KEY (run_id, step_id) REFERENCES workflow_step_runs(run_id, step_id) ON DELETE CASCADE
);
CREATE INDEX idx_workflow_attempts_step ON workflow_step_attempts(run_id, step_id, attempt_number);

CREATE TABLE workflow_artifacts (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    step_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    task_id TEXT NOT NULL DEFAULT '',
    output_key TEXT NOT NULL,
    managed_path TEXT NOT NULL,
    original_path TEXT NOT NULL,
    media_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    digest TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE (attempt_id, output_key),
    FOREIGN KEY (run_id, step_id) REFERENCES workflow_step_runs(run_id, step_id) ON DELETE CASCADE,
    FOREIGN KEY (attempt_id) REFERENCES workflow_step_attempts(id) ON DELETE CASCADE
);
