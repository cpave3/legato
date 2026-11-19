-- Trigger requests for every trigger kind (LEG-354), part 2: see
-- 123_workflow_trigger_held.sql.
ALTER TABLE workflow_trigger_requests ADD COLUMN trigger_kind TEXT NOT NULL DEFAULT 'task.entered_column';
ALTER TABLE workflow_trigger_requests ADD COLUMN item TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_trigger_requests ADD COLUMN event_key TEXT;
ALTER TABLE workflow_trigger_requests ADD COLUMN event_json TEXT NOT NULL DEFAULT '{}';

CREATE UNIQUE INDEX IF NOT EXISTS idx_workflow_trigger_requests_event ON workflow_trigger_requests(workflow_name, trigger_kind, event_key) WHERE event_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_workflow_trigger_requests_item ON workflow_trigger_requests(workflow_name, trigger_kind, item, created_at);
