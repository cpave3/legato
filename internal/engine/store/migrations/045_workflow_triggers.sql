CREATE TABLE task_transitions (
    id          TEXT PRIMARY KEY,
    task_id     TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    source      TEXT NOT NULL,
    from_column TEXT NOT NULL,
    to_column   TEXT NOT NULL,
    task_json   TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_task_transitions_task ON task_transitions(task_id, created_at);

CREATE TABLE workflow_trigger_requests (
    id                 TEXT PRIMARY KEY,
    transition_id      TEXT NOT NULL REFERENCES task_transitions(id) ON DELETE CASCADE,
    workflow_name      TEXT NOT NULL,
    definition_source  TEXT NOT NULL,
    definition_digest  TEXT NOT NULL,
    definition_json    TEXT NOT NULL,
    inputs_json        TEXT NOT NULL DEFAULT '{}',
    missing_inputs_json TEXT NOT NULL DEFAULT '[]',
    status             TEXT NOT NULL CHECK (status IN ('pending','waiting_input','started','skipped','failed')),
    run_id             TEXT NOT NULL DEFAULT '',
    error              TEXT NOT NULL DEFAULT '',
    created_at         DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at         DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE (transition_id, workflow_name, definition_digest)
);

CREATE INDEX idx_workflow_trigger_requests_status ON workflow_trigger_requests(status, updated_at);
