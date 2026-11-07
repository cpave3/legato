-- A reviewed handoff's counted checks: when they were last read, on which
-- pull request heads, and how many had not finished.
ALTER TABLE handoff_reviews ADD COLUMN checks_polled_at TEXT NOT NULL DEFAULT '';
ALTER TABLE handoff_reviews ADD COLUMN checks_heads TEXT NOT NULL DEFAULT '';
ALTER TABLE handoff_reviews ADD COLUMN checks_pending INTEGER NOT NULL DEFAULT 0;
