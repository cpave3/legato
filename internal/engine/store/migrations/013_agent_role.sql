ALTER TABLE agent_sessions ADD COLUMN role TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_sessions ADD COLUMN parent_task_id TEXT;
ALTER TABLE agent_sessions ADD COLUMN subtask_id TEXT;
