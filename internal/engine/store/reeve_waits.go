package store

import (
	"context"
	"fmt"
	"time"
)

// Why a wait on the Reeve ended.
const (
	ReeveWaitReeveAnswered = "reeve_answered"
	ReeveWaitOperatorTurn  = "operator_turn"
)

// OpenReeveWait records that an execution's agent asked the Reeve at at and
// waits for its answer. An open wait keeps its first question's time; a
// cleared one opens again at at.
func (s *Store) OpenReeveWait(ctx context.Context, taskID, executionID string, at time.Time) error {
	if executionID == "" {
		return fmt.Errorf("a wait on the Reeve needs an execution")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO reeve_waits (execution_id, task_id, since) VALUES (?, ?, ?)
		ON CONFLICT(execution_id) DO UPDATE SET
			since = CASE WHEN reeve_waits.cleared_at IS NULL THEN reeve_waits.since ELSE excluded.since END,
			task_id = excluded.task_id, cleared_at = NULL, clear_reason = ''`,
		executionID, taskID, at.UTC().Format(time.RFC3339))
	return err
}

// ClearReeveWait ends an execution's open wait on the Reeve, saying why; it
// says whether there was one.
func (s *Store) ClearReeveWait(ctx context.Context, executionID, reason string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE reeve_waits SET cleared_at = ?, clear_reason = ? WHERE execution_id = ? AND cleared_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339), reason, executionID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ClearReeveWaitsForTask ends every open wait on the Reeve of a task's
// executions, saying why, and says how many it ended.
func (s *Store) ClearReeveWaitsForTask(ctx context.Context, taskID, reason string) (int, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE reeve_waits SET cleared_at = ?, clear_reason = ? WHERE task_id = ? AND cleared_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339), reason, taskID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// OpenReeveWaitsSince lists the times of the open waits on the Reeve of
// running executions, oldest first.
func (s *Store) OpenReeveWaitsSince(ctx context.Context) ([]time.Time, error) {
	var raw []string
	if err := s.db.SelectContext(ctx, &raw, `SELECT rw.since FROM reeve_waits rw JOIN executions e ON e.id = rw.execution_id
		WHERE rw.cleared_at IS NULL AND e.status = 'running' ORDER BY rw.since`); err != nil {
		return nil, err
	}
	out := make([]time.Time, 0, len(raw))
	for _, r := range raw {
		if t, err := time.Parse(time.RFC3339, r); err == nil {
			out = append(out, t)
		}
	}
	return out, nil
}
