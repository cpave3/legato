ALTER TABLE handoffs ADD COLUMN agent_tier TEXT NOT NULL DEFAULT '';
ALTER TABLE review_loops ADD COLUMN reviewer_override TEXT NOT NULL DEFAULT '';
ALTER TABLE review_loop_passes ADD COLUMN reviewer_settings TEXT NOT NULL DEFAULT '';
