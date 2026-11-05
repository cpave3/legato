-- Handing tasks from a main instance to a worker (LEG-19). See
-- docs/ai/integrations.md § Handing off a task.

-- On a worker: each task a main handed to it, keyed by the main's instance ID
-- and the handoff ID the main made (the idempotency key). task_id is the
-- worker's card. state is pending (accepted, agent not running yet),
-- in_progress, done (the card is in a column named Done) or failed (with
-- failure, the reason). start_attempt_at is when a Legato last began starting
-- the agent: a claim, so two Legatos on one database do not both start it,
-- and one that died mid-start is retried once the claim is stale. closed_at
-- is set once the handoff is over (returned, dropped, unpaired); a card has
-- at most one open handoff.
CREATE TABLE IF NOT EXISTS handoffs (
    main_instance_id TEXT NOT NULL,
    handoff_id       TEXT NOT NULL,
    main_name        TEXT NOT NULL,
    main_task_id     TEXT NOT NULL,
    task_id          TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    purpose          TEXT NOT NULL,
    kind             TEXT NOT NULL,
    repo             TEXT NOT NULL,
    branch           TEXT NOT NULL,
    agent_kind       TEXT NOT NULL,
    state            TEXT NOT NULL,
    failure          TEXT NOT NULL DEFAULT '',
    request_hash     TEXT NOT NULL,
    start_attempt_at TEXT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    closed_at        TEXT,
    PRIMARY KEY (main_instance_id, handoff_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_handoffs_open_task ON handoffs(task_id) WHERE closed_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_handoffs_state ON handoffs(state) WHERE closed_at IS NULL;

-- On a worker: the workspace each main's tasks go in, made on demand and
-- named after the main, but found by its instance ID.
CREATE TABLE IF NOT EXISTS main_workspaces (
    main_instance_id TEXT PRIMARY KEY,
    workspace_id     INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE
);

-- On a main: each handoff of one of its tasks. state is sending (the Handoff
-- call has not been answered; the task is not away, and a new attempt to the
-- same worker reuses the handoff ID) or away. closed_at is set once the task
-- is back; a task has at most one open handoff.
CREATE TABLE IF NOT EXISTS away_tasks (
    handoff_id         TEXT PRIMARY KEY,
    task_id            TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    worker_instance_id TEXT NOT NULL,
    worker_task_id     TEXT NOT NULL DEFAULT '',
    state              TEXT NOT NULL,
    purpose            TEXT NOT NULL,
    kind               TEXT NOT NULL,
    repo               TEXT NOT NULL,
    branch             TEXT NOT NULL,
    agent_kind         TEXT NOT NULL DEFAULT '',
    left_column        TEXT NOT NULL,
    last_summary       TEXT,
    last_summary_at    TEXT,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    closed_at          TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_away_tasks_open_task ON away_tasks(task_id) WHERE closed_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_away_tasks_worker ON away_tasks(worker_instance_id) WHERE closed_at IS NULL;
