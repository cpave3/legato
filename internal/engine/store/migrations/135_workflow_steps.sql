-- Workflows v2 (LEG-483, spec LEG-482 §2): a run's steps are a tree. Each
-- step instance is keyed by its path (a top-level step's path is its ID; a
-- step in a loop pass is <loop>/<pass>/<id>), created when the run reaches
-- it. Existing rows move over with path = step_id, so runs in flight and
-- their agents carry on.

ALTER TABLE workflow_runs ADD COLUMN parent_run_id TEXT REFERENCES workflow_runs(id);
ALTER TABLE workflow_runs ADD COLUMN parent_step_path TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_runs ADD COLUMN outputs_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE workflow_runs ADD COLUMN schema_version INTEGER NOT NULL DEFAULT 1;

CREATE TABLE workflow_steps (
    run_id TEXT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    parent_path TEXT NOT NULL DEFAULT '',
    step_id TEXT NOT NULL,
    -- The pass of the loop whose scope the step is in; NULL outside loops.
    pass INTEGER,
    step_order INTEGER NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    executor_type TEXT NOT NULL DEFAULT '',
    definition_json TEXT NOT NULL DEFAULT '{}',
    resolved_json TEXT NOT NULL DEFAULT '{}',
    outputs_json TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    latest_attempt INTEGER NOT NULL DEFAULT 0,
    -- Host-side advance (§5.4): a completion recorded from an agent's pane
    -- sets advance_due; a host process claims it (advance_claimed_at),
    -- finishes or keeps the agent, advances the run, then clears it.
    advance_due INTEGER NOT NULL DEFAULT 0,
    advance_claimed_at TEXT,
    -- A loop's over: snapshot and the keys its passes used (LEG-490).
    loop_state_json TEXT NOT NULL DEFAULT '{}',
    -- The wait record of an agent step (LEG-485): its signals, its place,
    -- the stage event watermark, the session, the deadline, and a
    -- workflow.run step's child run (LEG-489). Empty until then.
    wait_signals TEXT NOT NULL DEFAULT '[]',
    wait_task_id TEXT NOT NULL DEFAULT '',
    wait_slot TEXT NOT NULL DEFAULT '',
    wait_execution_id TEXT NOT NULL DEFAULT '',
    wait_watermark INTEGER NOT NULL DEFAULT 0,
    wait_session_id INTEGER,
    deadline_at TEXT,
    child_run_id TEXT,
    started_at DATETIME,
    completed_at DATETIME,
    PRIMARY KEY (run_id, path),
    UNIQUE (run_id, parent_path, step_order)
);
CREATE INDEX idx_workflow_steps_advance_due ON workflow_steps(advance_due) WHERE advance_due = 1;

INSERT INTO workflow_steps
    (run_id, path, parent_path, step_id, pass, step_order, title, executor_type, definition_json,
     resolved_json, outputs_json, status, error, latest_attempt, started_at, completed_at)
SELECT run_id, step_id, '', step_id, NULL, step_order, title, executor_type, definition_json,
       resolved_inputs_json, outputs_json, status, error, latest_attempt, started_at, completed_at
FROM workflow_step_runs
WHERE run_id IN (SELECT id FROM workflow_runs);

CREATE TABLE workflow_step_attempts_v2 (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    path TEXT NOT NULL,
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
    UNIQUE (run_id, path, attempt_number),
    FOREIGN KEY (run_id, path) REFERENCES workflow_steps(run_id, path) ON DELETE CASCADE
);
INSERT INTO workflow_step_attempts_v2
    (id, run_id, path, step_id, attempt_number, executor_type, agent_session_id, task_id, tmux_session,
     agent_kind, tier, working_dir, status, error, started_at, ended_at)
SELECT a.id, a.run_id, a.step_id, a.step_id, a.attempt_number, a.executor_type, a.agent_session_id, a.task_id,
       a.tmux_session, a.agent_kind, a.tier, a.working_dir, a.status, a.error, a.started_at, a.ended_at
FROM workflow_step_attempts a
WHERE EXISTS (SELECT 1 FROM workflow_steps s WHERE s.run_id = a.run_id AND s.path = a.step_id);

CREATE TABLE workflow_artifacts_v2 (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    path TEXT NOT NULL,
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
    FOREIGN KEY (run_id, path) REFERENCES workflow_steps(run_id, path) ON DELETE CASCADE,
    FOREIGN KEY (attempt_id) REFERENCES workflow_step_attempts_v2(id) ON DELETE CASCADE
);
INSERT INTO workflow_artifacts_v2
    (id, run_id, path, step_id, attempt_id, task_id, output_key, managed_path, original_path, media_type,
     size_bytes, digest, created_at)
SELECT f.id, f.run_id, f.step_id, f.step_id, f.attempt_id, f.task_id, f.output_key, f.managed_path,
       f.original_path, f.media_type, f.size_bytes, f.digest, f.created_at
FROM workflow_artifacts f
WHERE EXISTS (SELECT 1 FROM workflow_step_attempts_v2 a WHERE a.id = f.attempt_id AND a.run_id = f.run_id AND a.path = f.step_id);

DROP TABLE workflow_artifacts;
DROP TABLE workflow_step_attempts;
DROP TABLE workflow_step_runs;
ALTER TABLE workflow_step_attempts_v2 RENAME TO workflow_step_attempts;
ALTER TABLE workflow_artifacts_v2 RENAME TO workflow_artifacts;
CREATE INDEX idx_workflow_attempts_step ON workflow_step_attempts(run_id, path, attempt_number);
CREATE INDEX idx_workflow_attempts_session ON workflow_step_attempts(agent_session_id, status);
