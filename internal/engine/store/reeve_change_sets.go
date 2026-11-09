package store

import (
	"context"
	"database/sql"
	"errors"
)

// Change set states.
const (
	ChangeSetPending = "pending"
	ChangeSetQueued  = "queued"
	// ChangeSetQueuedNoRestart: approved, its optional restart left out.
	ChangeSetQueuedNoRestart = "queued_no_restart"
	ChangeSetRunning         = "running"
	ChangeSetDone            = "done"
	ChangeSetFailed          = "failed"
	ChangeSetDenied          = "denied"
	ChangeSetExpired         = "expired"
	ChangeSetRestarting      = "restarting"
)

// ReeveChangeSet is one change set of the Reeve. Calls and Results are
// JSON.
type ReeveChangeSet struct {
	ID         string `db:"id"`
	TaskID     string `db:"task_id"`
	ApprovalID string `db:"approval_id"`
	Reason     string `db:"reason"`
	Level      string `db:"level"`
	Calls      string `db:"calls"`
	State      string `db:"state"`
	Results    string `db:"results"`
	Waiting    bool   `db:"waiting"`
	CreatedAt  string `db:"created_at"`
	UpdatedAt  string `db:"updated_at"`
}

// InsertReeveChangeSet keeps a proposed set.
func (s *Store) InsertReeveChangeSet(ctx context.Context, c ReeveChangeSet) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO reeve_change_sets (id, task_id, approval_id, reason, level, calls, state, waiting) VALUES (?, ?, ?, ?, ?, ?, 'pending', ?)`,
		c.ID, c.TaskID, c.ApprovalID, c.Reason, c.Level, c.Calls, c.Waiting)
	return err
}

// GetReeveChangeSet reads one set; ErrNotFound when there is none.
func (s *Store) GetReeveChangeSet(ctx context.Context, id string) (*ReeveChangeSet, error) {
	var c ReeveChangeSet
	err := s.db.GetContext(ctx, &c, `SELECT * FROM reeve_change_sets WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

// ListReeveChangeSets lists a session's sets, oldest first; every set when
// taskID is "".
func (s *Store) ListReeveChangeSets(ctx context.Context, taskID string) ([]ReeveChangeSet, error) {
	sets := []ReeveChangeSet{}
	query, args := `SELECT * FROM reeve_change_sets ORDER BY created_at, id`, []any{}
	if taskID != "" {
		query, args = `SELECT * FROM reeve_change_sets WHERE task_id = ? ORDER BY created_at, id`, []any{taskID}
	}
	err := s.db.SelectContext(ctx, &sets, query, args...)
	return sets, err
}

// SetReeveChangeSetApproval records the approval a set waits on.
func (s *Store) SetReeveChangeSetApproval(ctx context.Context, id, approvalID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE reeve_change_sets SET approval_id = ? WHERE id = ?`, approvalID, id)
	return err
}

// MoveReeveChangeSet moves a set from one state to another, reporting false
// when it was not in from (another process moved it first).
func (s *Store) MoveReeveChangeSet(ctx context.Context, id, from, to string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE reeve_change_sets SET state = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ? AND state = ?`, to, id, from)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// FinishReeveChangeSet records a set's results and its final state.
func (s *Store) FinishReeveChangeSet(ctx context.Context, id, state, results string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE reeve_change_sets SET state = ?, results = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, state, results, id)
	return err
}

// SetReeveChangeSetWaiting marks whether propose_changes is waiting on it.
func (s *Store) SetReeveChangeSetWaiting(ctx context.Context, id string, waiting bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE reeve_change_sets SET waiting = ? WHERE id = ?`, waiting, id)
	return err
}

// ExpireReeveChangeSets expires a session's sets still pending.
func (s *Store) ExpireReeveChangeSets(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE reeve_change_sets SET state = 'expired', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE task_id = ? AND state = 'pending'`, taskID)
	return err
}
