ALTER TABLE swarm_subtasks ADD COLUMN step_index INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_swarm_subtasks_parent_step ON swarm_subtasks(parent_task_id, step_index);