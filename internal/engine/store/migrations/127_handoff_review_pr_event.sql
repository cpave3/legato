-- The event of each pull request's review in a pass (LEG-302): APPROVE or
-- COMMENT as posted, or kept when the pass posted nothing because the pull
-- request's earlier approval stands (its head unchanged, nothing new on it).
-- '' for a review not posted yet, and for reviews posted before this column.
-- See docs/ai/integrations.md § Review passes.
ALTER TABLE handoff_review_prs ADD COLUMN event TEXT NOT NULL DEFAULT '';
