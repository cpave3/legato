CREATE TABLE review_hunk_notes (
    id          TEXT PRIMARY KEY,
    task_id     TEXT NOT NULL,
    step_id     TEXT NOT NULL,
    file_path   TEXT NOT NULL,
    hunk_anchor TEXT NOT NULL,
    body        TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_review_hunk_notes_task ON review_hunk_notes(task_id, created_at, id);
CREATE INDEX idx_review_hunk_notes_step ON review_hunk_notes(step_id);
