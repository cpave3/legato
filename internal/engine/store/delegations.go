package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Delegation states.
const (
	DelegationPending          = "pending"
	DelegationAwaitingApproval = "awaiting_approval"
	DelegationDelivered        = "delivered"
	DelegationFailed           = "failed"
)

// Delegation is a brief an integration handed to a task's agent.
type Delegation struct {
	ID             string  `db:"id"`
	Integration    string  `db:"integration"`
	IdempotencyKey string  `db:"idempotency_key"`
	RequestHash    string  `db:"request_hash"`
	CaseID         string  `db:"case_id"`
	Mode           string  `db:"mode"`
	Target         string  `db:"target"`
	Brief          string  `db:"brief"`
	Title          string  `db:"title"`
	TaskID         string  `db:"task_id"`
	ExecutionID    string  `db:"execution_id"`
	Route          string  `db:"route"`
	State          string  `db:"state"`
	Enforced       bool    `db:"enforced"`
	ErrorCode      string  `db:"error_code"`
	ErrorMessage   string  `db:"error_message"`
	ApprovalID     string  `db:"approval_id"`
	CreatedAt      string  `db:"created_at"`
	DeliveredAt    *string `db:"delivered_at"`
}

// DelegationMessage is a further message from the integration to a
// delegation's agent.
type DelegationMessage struct {
	ID             string  `db:"id"`
	DelegationID   string  `db:"delegation_id"`
	IdempotencyKey string  `db:"idempotency_key"`
	RequestHash    string  `db:"request_hash"`
	Text           string  `db:"text"`
	State          string  `db:"state"`
	ErrorCode      string  `db:"error_code"`
	ErrorMessage   string  `db:"error_message"`
	CreatedAt      string  `db:"created_at"`
	DeliveredAt    *string `db:"delivered_at"`
}

func uniqueConflict(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrConflict
	}
	return err
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func rowChanged(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

// InsertDelegation stores a delegation. A key the integration already used
// is ErrConflict.
func (s *Store) InsertDelegation(ctx context.Context, d Delegation) error {
	_, err := s.db.NamedExecContext(ctx, `INSERT INTO delegations
		(id, integration, idempotency_key, request_hash, case_id, mode, target, brief, title, task_id, execution_id, route, state, enforced, approval_id)
		VALUES (:id, :integration, :idempotency_key, :request_hash, :case_id, :mode, :target, :brief, :title, :task_id, :execution_id, :route, :state, :enforced, :approval_id)`, d)
	return uniqueConflict(err)
}

func (s *Store) GetDelegation(ctx context.Context, id string) (*Delegation, error) {
	var d Delegation
	if err := s.db.GetContext(ctx, &d, `SELECT * FROM delegations WHERE id = ?`, id); err != nil {
		return nil, notFound(err)
	}
	return &d, nil
}

func (s *Store) GetDelegationByKey(ctx context.Context, integration, key string) (*Delegation, error) {
	var d Delegation
	if err := s.db.GetContext(ctx, &d, `SELECT * FROM delegations WHERE integration = ? AND idempotency_key = ?`, integration, key); err != nil {
		return nil, notFound(err)
	}
	return &d, nil
}

// ListDelegationsByState lists delegations in a state, oldest first.
func (s *Store) ListDelegationsByState(ctx context.Context, state string) ([]Delegation, error) {
	delegations := []Delegation{}
	err := s.db.SelectContext(ctx, &delegations, `SELECT * FROM delegations WHERE state = ? ORDER BY created_at, rowid`, state)
	return delegations, err
}

// ListDelegationsForTask lists a task's delegations, newest first.
func (s *Store) ListDelegationsForTask(ctx context.Context, taskID string) ([]Delegation, error) {
	delegations := []Delegation{}
	err := s.db.SelectContext(ctx, &delegations, `SELECT * FROM delegations WHERE task_id = ? ORDER BY created_at DESC, rowid DESC`, taskID)
	return delegations, err
}

// SetDelegationTask records the task a delegation was routed to.
func (s *Store) SetDelegationTask(ctx context.Context, id, taskID string) error {
	return rowChanged(s.db.ExecContext(ctx, `UPDATE delegations SET task_id = ? WHERE id = ?`, taskID, id))
}

// SetDelegationAwaitingApproval records the approval a delegation waits on.
func (s *Store) SetDelegationAwaitingApproval(ctx context.Context, id, approvalID string) error {
	return rowChanged(s.db.ExecContext(ctx, `UPDATE delegations SET state = 'awaiting_approval', approval_id = ? WHERE id = ?`, approvalID, id))
}

// SetDelegationPending puts a delegation back to pending, to deliver.
func (s *Store) SetDelegationPending(ctx context.Context, id string) error {
	return rowChanged(s.db.ExecContext(ctx, `UPDATE delegations SET state = 'pending' WHERE id = ?`, id))
}

// SetDelegationDelivered records how a delegation reached its agent.
func (s *Store) SetDelegationDelivered(ctx context.Context, id, route, executionID string, enforced bool) error {
	return rowChanged(s.db.ExecContext(ctx, `UPDATE delegations SET state = 'delivered', route = ?, execution_id = ?, enforced = ?,
		delivered_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, route, executionID, enforced, id))
}

// SetDelegationFailed records why a delegation failed.
func (s *Store) SetDelegationFailed(ctx context.Context, id, code, message string) error {
	return rowChanged(s.db.ExecContext(ctx, `UPDATE delegations SET state = 'failed', error_code = ?, error_message = ? WHERE id = ?`, code, message, id))
}

// InsertDelegationMessage stores a message. A key already used for the same
// delegation is ErrConflict.
func (s *Store) InsertDelegationMessage(ctx context.Context, m DelegationMessage) error {
	_, err := s.db.NamedExecContext(ctx, `INSERT INTO delegation_messages (id, delegation_id, idempotency_key, request_hash, text, state)
		VALUES (:id, :delegation_id, :idempotency_key, :request_hash, :text, :state)`, m)
	return uniqueConflict(err)
}

func (s *Store) GetDelegationMessageByKey(ctx context.Context, delegationID, key string) (*DelegationMessage, error) {
	var m DelegationMessage
	if err := s.db.GetContext(ctx, &m, `SELECT * FROM delegation_messages WHERE delegation_id = ? AND idempotency_key = ?`, delegationID, key); err != nil {
		return nil, notFound(err)
	}
	return &m, nil
}

// SetDelegationMessageState records whether a message was delivered.
func (s *Store) SetDelegationMessageState(ctx context.Context, id, state, code, message string) error {
	return rowChanged(s.db.ExecContext(ctx, `UPDATE delegation_messages SET state = ?, error_code = ?, error_message = ?,
		delivered_at = CASE WHEN ? = 'delivered' THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now') ELSE delivered_at END WHERE id = ?`, state, code, message, state, id))
}
