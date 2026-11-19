package store

import "context"

// WorkflowTriggerSwitch is whether a workflow's trigger is on here.
type WorkflowTriggerSwitch struct {
	WorkflowName string `db:"workflow_name"`
	Trigger      string `db:"trigger"`
	Enabled      bool   `db:"enabled"`
	UpdatedAt    string `db:"updated_at"`
}

// WorkflowTriggerSwitches lists every trigger switched here.
func (s *Store) WorkflowTriggerSwitches(ctx context.Context) ([]WorkflowTriggerSwitch, error) {
	rows := []WorkflowTriggerSwitch{}
	err := s.db.SelectContext(ctx, &rows, `SELECT workflow_name, trigger, enabled, updated_at FROM workflow_trigger_switches ORDER BY workflow_name, trigger`)
	return rows, err
}

// WorkflowTriggerSwitch reads one; ok is false when it was never switched.
func (s *Store) WorkflowTriggerSwitchFor(ctx context.Context, workflowName, trigger string) (enabled, ok bool, err error) {
	var rows []bool
	if err := s.db.SelectContext(ctx, &rows, `SELECT enabled FROM workflow_trigger_switches WHERE workflow_name = ? AND trigger = ?`, workflowName, trigger); err != nil {
		return false, false, err
	}
	if len(rows) == 0 {
		return false, false, nil
	}
	return rows[0], true, nil
}

// SetWorkflowTriggerSwitch switches a trigger on or off here.
func (s *Store) SetWorkflowTriggerSwitch(ctx context.Context, workflowName, trigger string, enabled bool) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO workflow_trigger_switches (workflow_name, trigger, enabled) VALUES (?, ?, ?)
		ON CONFLICT(workflow_name, trigger) DO UPDATE SET enabled = excluded.enabled, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, workflowName, trigger, enabled)
	return err
}

// DeleteWorkflowTriggerSwitch leaves a trigger to its definition again.
func (s *Store) DeleteWorkflowTriggerSwitch(ctx context.Context, workflowName, trigger string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM workflow_trigger_switches WHERE workflow_name = ? AND trigger = ?`, workflowName, trigger)
	return err
}
