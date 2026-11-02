-- A job clone can be shared: several tasks, one after another or at once,
-- work in it (a stack of branches in one directory). job_clones.task_id is
-- the task it was made for; every task that works in it is listed here.
-- last_used_at is when an agent last started or was seen working in it, so
-- one left idle is removed after repository_cache.keep_idle.
ALTER TABLE job_clones ADD COLUMN last_used_at TEXT;

CREATE TABLE IF NOT EXISTS job_clone_tasks (
    clone_id    INTEGER NOT NULL REFERENCES job_clones(id) ON DELETE CASCADE,
    task_id     TEXT NOT NULL,
    attached_at TEXT NOT NULL,
    PRIMARY KEY (clone_id, task_id)
);

INSERT OR IGNORE INTO job_clone_tasks (clone_id, task_id, attached_at)
    SELECT id, task_id, created_at FROM job_clones;
