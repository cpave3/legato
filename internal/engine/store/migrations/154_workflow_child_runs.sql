-- Calling a workflow (LEG-489, spec LEG-482 §1): a workflow.run step's
-- child run is linked to the step by parent_run_id and parent_step_path
-- (migration 135) and to the step's attempt by parent_attempt. The three
-- are unique together, so a crash between creating the child and marking
-- the step waiting can't start it twice: the reconcile finds the child.
ALTER TABLE workflow_runs ADD COLUMN parent_attempt INTEGER;
CREATE UNIQUE INDEX IF NOT EXISTS idx_workflow_runs_child ON workflow_runs(parent_run_id, parent_step_path, parent_attempt)
    WHERE parent_run_id IS NOT NULL;
