-- The stack of branches Staccato keeps in a job clone, as Legato last read
-- it from the clone's files. One row per clone, replaced at each read; it
-- outlives the clone's folder, so the person can still take the stack home
-- after the clone is removed. A task's stacks are those of the clones it
-- works in (job_clone_tasks). branches is JSON the service writes, from which
-- it makes st's import document; problem is why the stack could not be read.
CREATE TABLE IF NOT EXISTS clone_stacks (
    clone_id INTEGER PRIMARY KEY REFERENCES job_clones(id) ON DELETE CASCADE,
    root     TEXT NOT NULL,
    branches TEXT NOT NULL,
    problem  TEXT NOT NULL DEFAULT '',
    read_at  TEXT NOT NULL
);
