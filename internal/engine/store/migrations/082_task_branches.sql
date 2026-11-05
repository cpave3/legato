-- The branches a task tracks, in stack order, each with the PR found for it.
-- A task built as a Staccato stack has several; one built on a single branch
-- has one. pr is the PR cache of that branch as JSON (number, URL, state,
-- draft, checks, review decision, comments, pin, match method and the last
-- outcome), NULL until discovery has looked. tasks.pr_meta stays beside it as
-- a summary of the first PR, for the readers that have not moved yet.
CREATE TABLE IF NOT EXISTS task_branches (
    task_id  TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    repo     TEXT NOT NULL,
    branch   TEXT NOT NULL,
    position INTEGER NOT NULL,
    source   TEXT NOT NULL,
    pr       TEXT,
    added_at TEXT NOT NULL,
    PRIMARY KEY (task_id, repo, branch)
);
CREATE INDEX IF NOT EXISTS task_branches_task_position ON task_branches (task_id, position);
