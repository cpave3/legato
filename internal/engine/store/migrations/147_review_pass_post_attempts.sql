-- A pass whose post's outcome is unknown (GitHub answered with a 5xx, or
-- not at all) stays submitted and is tried again by its markers (LEG-561).
-- posting_started_at is when its first post was tried, and post_attempts
-- how many tries ended with the outcome unknown: after ten, or an hour
-- from the first try, the loop is stuck for the operator.
ALTER TABLE handoff_review_passes ADD COLUMN posting_started_at TEXT NOT NULL DEFAULT '';
ALTER TABLE handoff_review_passes ADD COLUMN post_attempts INTEGER NOT NULL DEFAULT 0;
