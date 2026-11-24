package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// Why a wait on the Reeve ended.
const (
	ReeveWaitReeveAnswered = "reeve_answered"
	ReeveWaitOperatorTurn  = "operator_turn"
)

// OpenReeveWait records that an execution's agent asked the Reeve at at and
// waits for its answer. An open wait keeps its first question's time and
// when the Reeve was told of it; a cleared one opens again at at, not yet
// told.
func (s *Store) OpenReeveWait(ctx context.Context, taskID, executionID string, at time.Time) error {
	if executionID == "" {
		return fmt.Errorf("a wait on the Reeve needs an execution")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO reeve_waits (execution_id, task_id, since) VALUES (?, ?, ?)
		ON CONFLICT(execution_id) DO UPDATE SET
			since = CASE WHEN reeve_waits.cleared_at IS NULL THEN reeve_waits.since ELSE excluded.since END,
			told_at = CASE WHEN reeve_waits.cleared_at IS NULL THEN reeve_waits.told_at ELSE NULL END,
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

// MarkReeveWaitsTold records that the Reeve was told at at of the open
// waits of these executions that it had not been told of yet.
func (s *Store) MarkReeveWaitsTold(ctx context.Context, executionIDs []string, at time.Time) error {
	if len(executionIDs) == 0 {
		return nil
	}
	query, args, err := sqlx.In(`UPDATE reeve_waits SET told_at = ? WHERE cleared_at IS NULL AND told_at IS NULL AND execution_id IN (?)`,
		at.UTC().Format(time.RFC3339), executionIDs)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.db.Rebind(query), args...)
	return err
}

// OpenReeveWaitTimes is an open wait on the Reeve: when its agent asked,
// and when the Reeve was told (nil while it has not been).
type OpenReeveWaitTimes struct {
	ExecutionID string
	Since       time.Time
	ToldAt      *time.Time
}

// OpenReeveWaits lists the open waits on the Reeve of running executions,
// oldest first.
func (s *Store) OpenReeveWaits(ctx context.Context) ([]OpenReeveWaitTimes, error) {
	var rows []struct {
		ExecutionID string `db:"execution_id"`
		Since       string `db:"since"`
		ToldAt      string `db:"told_at"`
	}
	if err := s.db.SelectContext(ctx, &rows, `SELECT rw.execution_id, rw.since, COALESCE(rw.told_at, '') AS told_at
		FROM reeve_waits rw JOIN executions e ON e.id = rw.execution_id
		WHERE rw.cleared_at IS NULL AND e.status = 'running' ORDER BY rw.since`); err != nil {
		return nil, err
	}
	out := make([]OpenReeveWaitTimes, 0, len(rows))
	for _, r := range rows {
		since, err := time.Parse(time.RFC3339, r.Since)
		if err != nil {
			continue
		}
		w := OpenReeveWaitTimes{ExecutionID: r.ExecutionID, Since: since}
		if told, err := time.Parse(time.RFC3339, r.ToldAt); err == nil {
			w.ToldAt = &told
		}
		out = append(out, w)
	}
	return out, nil
}
