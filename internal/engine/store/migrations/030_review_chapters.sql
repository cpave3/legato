ALTER TABLE review_tours ADD COLUMN head_sha TEXT NOT NULL DEFAULT '';

CREATE TABLE review_chapter_hunks (
    id          TEXT PRIMARY KEY,
    task_id     TEXT NOT NULL,
    step_id     TEXT NOT NULL,
    file_path   TEXT NOT NULL,
    hunk_anchor TEXT NOT NULL,
    seq         INTEGER NOT NULL,
    generated   BOOLEAN NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(step_id, file_path, hunk_anchor)
);
CREATE INDEX idx_review_chapter_hunks_step ON review_chapter_hunks(step_id, seq);
CREATE INDEX idx_review_chapter_hunks_task ON review_chapter_hunks(task_id);
