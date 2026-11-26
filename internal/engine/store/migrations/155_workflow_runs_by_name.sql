-- The Workflows page reads each workflow's newest run and its runs of the
-- last week (LEG-657).
CREATE INDEX IF NOT EXISTS idx_workflow_runs_name ON workflow_runs(workflow_name, created_at DESC);
