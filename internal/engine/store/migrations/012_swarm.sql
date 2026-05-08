CREATE TABLE swarm_subtasks (
    id                  TEXT PRIMARY KEY,
    parent_task_id      TEXT NOT NULL,
    title               TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    scope_globs         TEXT NOT NULL DEFAULT '[]',
    role                TEXT NOT NULL DEFAULT 'builder',
    status              TEXT NOT NULL DEFAULT 'queued',
    builder_agent_id    INTEGER,
    reviewer_agent_id   INTEGER,
    created_at          DATETIME NOT NULL DEFAULT (datetime('now')),
    started_at          DATETIME,
    completed_at        DATETIME,
    FOREIGN KEY (parent_task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE INDEX idx_swarm_subtasks_parent ON swarm_subtasks(parent_task_id);
CREATE INDEX idx_swarm_subtasks_parent_status ON swarm_subtasks(parent_task_id, status);
