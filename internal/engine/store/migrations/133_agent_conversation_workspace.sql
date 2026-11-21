-- The workspace of a conversation's task when it was last recorded, so a
-- conversation restored into a new task lands in it even once the task is
-- gone. NULL for a task in none.
ALTER TABLE agent_conversations ADD COLUMN workspace_id INTEGER;
UPDATE agent_conversations SET workspace_id = (SELECT t.workspace_id FROM tasks t WHERE t.id = agent_conversations.task_id);
