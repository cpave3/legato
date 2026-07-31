ALTER TABLE workflow_trigger_requests
ADD COLUMN definition_json TEXT NOT NULL DEFAULT '{}';
