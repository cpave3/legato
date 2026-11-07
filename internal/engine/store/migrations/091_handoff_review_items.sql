-- What the author of a reviewed handoff must handle: the reviewer's
-- findings, counted comments and failing required checks, each answered.
CREATE TABLE IF NOT EXISTS handoff_review_items (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    -- blocking, nit, comment, check
    kind TEXT NOT NULL,
    -- the pass whose round it belongs to; raised again, it moves on
    pass INTEGER NOT NULL,
    repo TEXT NOT NULL DEFAULT '',
    pr INTEGER NOT NULL DEFAULT 0,
    path TEXT NOT NULL DEFAULT '',
    line INTEGER NOT NULL DEFAULT 0,
    url TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1,
    outdated INTEGER NOT NULL DEFAULT 0,
    author TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL DEFAULT '',
    -- the thread an answer replies in; 0 answers on the conversation
    thread_comment_id INTEGER NOT NULL DEFAULT 0,
    source_id TEXT NOT NULL DEFAULT '',
    -- fixed, replied, skipped; '' unanswered
    answer TEXT NOT NULL DEFAULT '',
    answer_note TEXT NOT NULL DEFAULT '',
    answered_at TEXT NOT NULL DEFAULT '',
    posted INTEGER NOT NULL DEFAULT 0,
    closed INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_handoff_review_items_task ON handoff_review_items(task_id, pass);
ALTER TABLE handoff_reviews ADD COLUMN delivered_pass INTEGER NOT NULL DEFAULT 0;
