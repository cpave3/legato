-- Persistent pending-plan storage so the web PWA can rediscover plans
-- that arrived while the client was offline or the browser tab was suspended.

CREATE TABLE swarm_pending_plans (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_task_id  TEXT NOT NULL UNIQUE,
    plan_path       TEXT NOT NULL,
    reply_socket    TEXT NOT NULL,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_swarm_pending_plans_parent ON swarm_pending_plans(parent_task_id);
