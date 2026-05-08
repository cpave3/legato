-- Swarm conductor v1: rewrite v0 lifecycle, add per-subtask agent + prompt + dispatched_at,
-- add parent-task swarm working directory.

-- Status enum value rewrite (v0 → v1):
--   building  → in_progress
--   review    → reporting
--   rejected  → cancelled
-- queued and done are unchanged.
UPDATE swarm_subtasks SET status = 'in_progress' WHERE status = 'building';
UPDATE swarm_subtasks SET status = 'reporting'   WHERE status = 'review';
UPDATE swarm_subtasks SET status = 'cancelled'   WHERE status = 'rejected';

ALTER TABLE swarm_subtasks ADD COLUMN agent_kind     TEXT NOT NULL DEFAULT '';
ALTER TABLE swarm_subtasks ADD COLUMN prompt         TEXT NOT NULL DEFAULT '';
ALTER TABLE swarm_subtasks ADD COLUMN dispatched_at  DATETIME;

ALTER TABLE tasks ADD COLUMN swarm_working_dir TEXT;
