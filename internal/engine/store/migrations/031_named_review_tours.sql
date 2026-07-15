-- Recreate review_tours with surrogate id as primary key and a name column.
-- SQLite cannot ALTER TABLE DROP PRIMARY KEY, so we recreate the table.

CREATE TABLE review_tours_new (
    id               TEXT PRIMARY KEY,
    task_id          TEXT NOT NULL,
    name             TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'capturing',
    summary          TEXT NOT NULL DEFAULT '',
    base_sha         TEXT NOT NULL DEFAULT '',
    head_sha         TEXT NOT NULL DEFAULT '',
    repository_path  TEXT NOT NULL DEFAULT '',
    last_reviewed_sha TEXT NOT NULL DEFAULT '',
    ready_at         DATETIME,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO review_tours_new (id, task_id, name, status, summary, base_sha, head_sha,
    repository_path, last_reviewed_sha, ready_at, created_at, updated_at)
SELECT 'rt-' || task_id, task_id, '', status, summary, base_sha, head_sha,
    repository_path, last_reviewed_sha, ready_at, created_at, updated_at
FROM review_tours;

DROP TABLE review_tours;
ALTER TABLE review_tours_new RENAME TO review_tours;

CREATE UNIQUE INDEX idx_review_tours_task_name ON review_tours(task_id, name) WHERE name != '';
CREATE INDEX idx_review_tours_task ON review_tours(task_id);

-- Add tour_id to child tables for tour-scoped queries.
ALTER TABLE review_steps ADD COLUMN tour_id TEXT NOT NULL DEFAULT '';
ALTER TABLE review_transcript ADD COLUMN tour_id TEXT NOT NULL DEFAULT '';
ALTER TABLE review_hunk_notes ADD COLUMN tour_id TEXT NOT NULL DEFAULT '';
ALTER TABLE review_chapter_hunks ADD COLUMN tour_id TEXT NOT NULL DEFAULT '';

-- Backfill tour_id from task_id.
UPDATE review_steps SET tour_id = 'rt-' || task_id WHERE tour_id = '';
UPDATE review_transcript SET tour_id = 'rt-' || task_id WHERE tour_id = '';
UPDATE review_hunk_notes SET tour_id = 'rt-' || task_id WHERE tour_id = '';
UPDATE review_chapter_hunks SET tour_id = 'rt-' || task_id WHERE tour_id = '';

CREATE INDEX IF NOT EXISTS idx_review_steps_tour ON review_steps(tour_id);
CREATE INDEX IF NOT EXISTS idx_review_transcript_tour ON review_transcript(tour_id);
CREATE INDEX IF NOT EXISTS idx_review_hunk_notes_tour ON review_hunk_notes(tour_id);
CREATE INDEX IF NOT EXISTS idx_review_chapter_hunks_tour ON review_chapter_hunks(tour_id);
