package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Approval statuses. Every status but pending is final.
const (
	ApprovalPending   = "pending"
	ApprovalApproved  = "approved"
	ApprovalDenied    = "denied"
	ApprovalAnswered  = "answered"
	ApprovalDismissed = "dismissed"
	ApprovalWithdrawn = "withdrawn"
	ApprovalExpired   = "expired"
	ApprovalFailed    = "failed"
)

// Approval is one request that needs the person.
type Approval struct {
	ID           string  `db:"id" json:"id"`
	Source       string  `db:"source" json:"source"`
	SourceKey    string  `db:"source_key" json:"source_key"`
	TaskID       string  `db:"task_id" json:"task_id,omitempty"`
	ExecutionID  string  `db:"execution_id" json:"execution_id,omitempty"`
	Title        string  `db:"title" json:"title"`
	Summary      string  `db:"summary" json:"summary,omitempty"`
	DetailsJSON  string  `db:"details_json" json:"-"`
	OptionsJSON  string  `db:"options_json" json:"-"`
	Blocking     bool    `db:"blocking" json:"blocking"`
	Status       string  `db:"status" json:"status"`
	DecisionJSON string  `db:"decision_json" json:"-"`
	DecidedBy    string  `db:"decided_by" json:"decided_by,omitempty"`
	Error        string  `db:"error" json:"error,omitempty"`
	Revision     int     `db:"revision" json:"revision"`
	CreatedAt    string  `db:"created_at" json:"created_at"`
	UpdatedAt    string  `db:"updated_at" json:"updated_at"`
	DecidedAt    *string `db:"decided_at" json:"decided_at,omitempty"`
	// Signed: only a decision signed by an enrolled device may allow it.
	Signed bool `db:"signed" json:"signed,omitempty"`
	// DecidedDevice is the device that signed the decision.
	DecidedDevice string `db:"decided_device" json:"decided_device,omitempty"`
}

// ApprovalClose is how a pending approval ends.
type ApprovalClose struct {
	Status        string
	DecisionJSON  string
	DecidedBy     string
	DecidedDevice string
	Error         string
}

// ApprovalFilter selects approvals. Empty fields match everything.
type ApprovalFilter struct {
	Status      string
	TaskID      string
	ExecutionID string
	Source      string
	Limit       int
}

// InsertApproval stores a new pending approval, or returns the one already
// stored for its source and source key. created reports which.
func (s *Store) InsertApproval(ctx context.Context, a Approval) (*Approval, bool, error) {
	if a.DetailsJSON == "" {
		a.DetailsJSON = "{}"
	}
	if a.OptionsJSON == "" {
		a.OptionsJSON = "[]"
	}
	result, err := s.db.NamedExecContext(ctx, `
		INSERT INTO approval_requests
			(id, source, source_key, task_id, execution_id, title, summary, details_json, options_json, blocking, signed)
		VALUES
			(:id, :source, :source_key, :task_id, :execution_id, :title, :summary, :details_json, :options_json, :blocking, :signed)
		ON CONFLICT(source, source_key) DO NOTHING`, a)
	if err != nil {
		return nil, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	stored, err := s.GetApprovalBySource(ctx, a.Source, a.SourceKey)
	return stored, rows > 0, err
}

func (s *Store) GetApproval(ctx context.Context, id string) (*Approval, error) {
	return s.getApproval(ctx, "SELECT * FROM approval_requests WHERE id = ?", id)
}

func (s *Store) GetApprovalBySource(ctx context.Context, source, sourceKey string) (*Approval, error) {
	return s.getApproval(ctx, "SELECT * FROM approval_requests WHERE source = ? AND source_key = ?", source, sourceKey)
}

func (s *Store) getApproval(ctx context.Context, query string, args ...any) (*Approval, error) {
	var a Approval
	if err := s.db.GetContext(ctx, &a, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

// CloseApproval ends a pending approval. It returns ErrConflict when the
// approval is no longer pending, so of two decisions only the first counts.
func (s *Store) CloseApproval(ctx context.Context, id string, close ApprovalClose) (*Approval, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE approval_requests SET
			status = ?, decision_json = ?, decided_by = ?, decided_device = ?, error = ?,
			revision = revision + 1,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
			decided_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ? AND status = 'pending'`,
		close.Status, close.DecisionJSON, close.DecidedBy, close.DecidedDevice, close.Error, id)
	if err != nil {
		return nil, err
	}
	if err := expectOneRow(result); err != nil {
		if _, getErr := s.GetApproval(ctx, id); getErr != nil {
			return nil, getErr
		}
		return nil, err
	}
	return s.GetApproval(ctx, id)
}

// MarkApprovalFailed records that a decided approval could not be applied to
// its source.
func (s *Store) MarkApprovalFailed(ctx context.Context, id, message string) (*Approval, error) {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE approval_requests SET status = 'failed', error = ?, revision = revision + 1,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?`, message, id); err != nil {
		return nil, err
	}
	return s.GetApproval(ctx, id)
}

// ListApprovals returns approvals, oldest first.
func (s *Store) ListApprovals(ctx context.Context, f ApprovalFilter) ([]Approval, error) {
	var where []string
	var args []any
	for _, clause := range []struct {
		column, value string
	}{{"status", f.Status}, {"task_id", f.TaskID}, {"execution_id", f.ExecutionID}, {"source", f.Source}} {
		if clause.value != "" {
			where = append(where, clause.column+" = ?")
			args = append(args, clause.value)
		}
	}
	query := "SELECT * FROM approval_requests"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at, rowid"
	if f.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, f.Limit)
	}
	approvals := []Approval{}
	if err := s.db.SelectContext(ctx, &approvals, query, args...); err != nil {
		return nil, err
	}
	return approvals, nil
}

// CountApprovals counts approvals with a status.
func (s *Store) CountApprovals(ctx context.Context, status string) (int, error) {
	var n int
	err := s.db.GetContext(ctx, &n, "SELECT COUNT(*) FROM approval_requests WHERE status = ?", status)
	return n, err
}
