-- Clones Legato made for jobs, which borrow the repository cache's objects.
-- A clone is removed when its agent has ended and nothing in it is
-- unpushed; one with unpushed commits is kept until remove_after.
CREATE TABLE IF NOT EXISTS job_clones (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    execution_id TEXT NOT NULL,
    task_id      TEXT NOT NULL,
    repo         TEXT NOT NULL,
    ref          TEXT NOT NULL DEFAULT '',
    branch       TEXT NOT NULL DEFAULT '',
    path         TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    remove_after TEXT,
    removed_at   TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_job_clones_live_path ON job_clones(path) WHERE removed_at IS NULL;
