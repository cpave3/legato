ALTER TABLE workflow_runs ADD COLUMN related_task_id TEXT REFERENCES tasks(id);
CREATE INDEX idx_workflow_runs_related_task ON workflow_runs(related_task_id, created_at DESC);
