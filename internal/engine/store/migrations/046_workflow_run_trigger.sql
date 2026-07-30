ALTER TABLE workflow_runs ADD COLUMN trigger_request_id TEXT REFERENCES workflow_trigger_requests(id);
CREATE UNIQUE INDEX idx_workflow_runs_trigger_request ON workflow_runs(trigger_request_id) WHERE trigger_request_id IS NOT NULL;
