-- Conductor-bound event log. Replaces the base64-encoded send-keys envelope
-- with a DB-backed inbox: SwarmService writes events here, the agent
-- receives a short plain-text pointer via send-keys, and the conductor
-- pulls events via `legato swarm inbox <parent-id>`.

CREATE TABLE swarm_events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_task_id  TEXT NOT NULL,
    subtask_id      TEXT,
    kind            TEXT NOT NULL,    -- 'progress' | 'built' | 'question' | 'died' | 'cap_deferred' | 'plan_rejected' | 'all_idle' | 'scope_warning' | 'message'
    worker_title    TEXT NOT NULL DEFAULT '',
    payload         TEXT NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    acked_at        DATETIME
);

CREATE INDEX idx_swarm_events_parent ON swarm_events(parent_task_id);
CREATE INDEX idx_swarm_events_unacked ON swarm_events(parent_task_id, acked_at);
