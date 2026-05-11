-- Per-sub-task tier selection so the conductor can pick a small/medium/large
-- (or otherwise-named) launch profile per worker. Tier names are free-form
-- and validated against the configured tiers of the resolved adapter at
-- plan-validation time, not at the database layer.

ALTER TABLE swarm_subtasks ADD COLUMN tier TEXT NOT NULL DEFAULT '';
