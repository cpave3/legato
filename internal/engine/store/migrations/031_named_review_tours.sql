-- Add surrogate ID and name to review_tours, shift PK from task_id to id.
ALTER TABLE review_tours ADD COLUMN id TEXT NOT NULL DEFAULT '';
ALTER TABLE review_tours ADD COLUMN name TEXT NOT NULL DEFAULT '';

-- Backfill id for existing rows (one per task, since task_id was the PK).
UPDATE review_tours SET id = 'rt-' || task_id WHERE id = '';

-- Create the unique index on the new id column and drop the old PK.
-- SQLite doesn't support ALTER TABLE DROP PRIMARY KEY directly, so we
-- recreate via the index: the id column becomes the de facto key.
CREATE UNIQUE INDEX IF NOT EXISTS idx_review_tours_id ON review_tours(id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_review_tours_task_name ON review_tours(task_id, name) WHERE name != '';
CREATE INDEX IF NOT EXISTS idx_review_tours_task ON review_tours(task_id);

-- Add tour_id to child tables for efficient scoping.
ALTER TABLE review_steps ADD COLUMN tour_id TEXT NOT NULL DEFAULT '';
UPDATE review_steps SET tour_id = (SELECT id FROM review_tours WHERE review_tours.task_id = review_steps.task_id);
CREATE INDEX IF NOT EXISTS idx_review_steps_tour ON review_steps(tour_id);

ALTER TABLE review_transcript ADD COLUMN tour_id TEXT NOT NULL DEFAULT '';
UPDATE review_transcript SET tour_id = (SELECT id FROM review_tours WHERE review_tours.task_id = review_transcript.task_id);
CREATE INDEX IF NOT EXISTS idx_review_transcript_tour ON review_transcript(tour_id);

ALTER TABLE review_hunk_notes ADD COLUMN tour_id TEXT NOT NULL DEFAULT '';
UPDATE review_hunk_notes SET tour_id = (SELECT id FROM review_tours WHERE review_tours.task_id = review_hunk_notes.task_id);
CREATE INDEX IF NOT EXISTS idx_review_hunk_notes_tour ON review_hunk_notes(tour_id);

ALTER TABLE review_chapter_hunks ADD COLUMN tour_id TEXT NOT NULL DEFAULT '';
UPDATE review_chapter_hunks SET tour_id = (SELECT id FROM review_tours WHERE review_tours.task_id = review_chapter_hunks.task_id);
CREATE INDEX IF NOT EXISTS idx_review_chapter_hunks_tour ON review_chapter_hunks(tour_id);
