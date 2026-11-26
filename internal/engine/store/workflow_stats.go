package store

import (
	"context"
	"time"
)

// WorkflowRunMark is a run as the Workflows page counts it: its workflow,
// how it stands and when it started (RFC 3339, as WorkflowRun.CreatedAt).
type WorkflowRunMark struct {
	WorkflowName string `db:"workflow_name"`
	Status       string `db:"status"`
	CreatedAt    string `db:"created_at"`
}

// LatestWorkflowRuns is the newest run of each workflow that has run.
func (s *Store) LatestWorkflowRuns(ctx context.Context) ([]WorkflowRun, error) {
	var runs []WorkflowRun
	err := s.db.SelectContext(ctx, &runs, workflowRunSelect+` WHERE id IN (
		SELECT (SELECT id FROM workflow_runs latest WHERE latest.workflow_name = names.workflow_name ORDER BY created_at DESC, rowid DESC LIMIT 1)
		FROM (SELECT DISTINCT workflow_name FROM workflow_runs) names)`)
	return runs, err
}

// WorkflowRunsSince lists the runs started at or after since, oldest first.
func (s *Store) WorkflowRunsSince(ctx context.Context, since time.Time) ([]WorkflowRunMark, error) {
	var marks []WorkflowRunMark
	err := s.db.SelectContext(ctx, &marks, `SELECT workflow_name, status, created_at FROM workflow_runs
		WHERE created_at >= ? ORDER BY created_at, rowid`, since.UTC().Format(time.DateTime))
	return marks, err
}
