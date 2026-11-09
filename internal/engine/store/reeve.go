package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// TaskKindReeve is the kind of a Reeve session's hidden task.
const TaskKindReeve = "reeve"

// Reeve session states. A suspended session still counts as the
// instance's running one.
const (
	ReeveRunning   = "running"
	ReeveSuspended = "suspended"
	ReeveEnded     = "ended"
)

// ReeveSession is one session of the Reeve.
type ReeveSession struct {
	TaskID       string         `db:"task_id" json:"task_id"`
	State        string         `db:"state" json:"state"`
	AgentKind    string         `db:"agent_kind" json:"agent_kind"`
	Model        string         `db:"model" json:"model,omitempty"`
	LaunchArgs   string         `db:"launch_args" json:"-"`
	APIToken     string         `db:"api_token" json:"-"`
	APITokenHash string         `db:"api_token_hash" json:"-"`
	StartedBy    string         `db:"started_by" json:"started_by,omitempty"`
	CreatedAt    string         `db:"created_at" json:"created_at"`
	UpdatedAt    string         `db:"updated_at" json:"updated_at"`
	ActiveAt     string         `db:"active_at" json:"active_at"`
	EndedAt      sql.NullString `db:"ended_at" json:"-"`
	Title        string         `db:"title" json:"title"`
}

// CreateReeveTask makes the hidden task of a Reeve session: ephemeral (off
// the board) and of kind reeve.
func (s *Store) CreateReeveTask(ctx context.Context, title string) (string, error) {
	id, err := s.CreateEphemeralTask(ctx, title)
	if err != nil {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET kind = ? WHERE id = ?`, TaskKindReeve, id); err != nil {
		return "", err
	}
	return id, nil
}

// TaskKind is a task's kind: "" for an ordinary task, TaskKindReeve.
func (s *Store) TaskKind(ctx context.Context, taskID string) (string, error) {
	var kind string
	err := s.db.GetContext(ctx, &kind, `SELECT kind FROM tasks WHERE id = ?`, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return kind, err
}

const reeveSelect = `SELECT r.*, COALESCE(t.title, '') AS title FROM reeve_sessions r LEFT JOIN tasks t ON t.id = r.task_id`

// InsertReeveSession keeps a new session.
func (s *Store) InsertReeveSession(ctx context.Context, r ReeveSession) error {
	if r.LaunchArgs == "" {
		r.LaunchArgs = "[]"
	}
	_, err := s.db.NamedExecContext(ctx, `INSERT INTO reeve_sessions (task_id, state, agent_kind, model, launch_args, api_token, api_token_hash, started_by)
		VALUES (:task_id, :state, :agent_kind, :model, :launch_args, :api_token, :api_token_hash, :started_by)`, r)
	return err
}

// GetReeveSession reads one session; ErrNotFound when there is none.
func (s *Store) GetReeveSession(ctx context.Context, taskID string) (*ReeveSession, error) {
	var r ReeveSession
	err := s.db.GetContext(ctx, &r, reeveSelect+` WHERE r.task_id = ?`, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &r, err
}

// CurrentReeveSession is the instance's running or suspended session, or
// nil.
func (s *Store) CurrentReeveSession(ctx context.Context) (*ReeveSession, error) {
	var r ReeveSession
	err := s.db.GetContext(ctx, &r, reeveSelect+` WHERE r.state IN ('running', 'suspended') ORDER BY r.created_at DESC LIMIT 1`)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

// ListReeveSessions lists sessions, newest first.
func (s *Store) ListReeveSessions(ctx context.Context) ([]ReeveSession, error) {
	rows := []ReeveSession{}
	err := s.db.SelectContext(ctx, &rows, reeveSelect+` ORDER BY r.created_at DESC`)
	return rows, err
}

// ReeveSessionByToken finds the session an API token belongs to, by the
// token's hash; only a session that is not ended.
func (s *Store) ReeveSessionByToken(ctx context.Context, hash string) (*ReeveSession, error) {
	var r ReeveSession
	err := s.db.GetContext(ctx, &r, reeveSelect+` WHERE r.api_token_hash = ? AND r.api_token_hash != '' AND r.state != 'ended'`, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &r, err
}

// SetReeveSessionState moves a session to a state; ended revokes its token.
func (s *Store) SetReeveSessionState(ctx context.Context, taskID, state string) error {
	query := `UPDATE reeve_sessions SET state = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE task_id = ?`
	if state == ReeveEnded {
		query = `UPDATE reeve_sessions SET state = ?, api_token = '', api_token_hash = '', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), ended_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE task_id = ?`
	}
	res, err := s.db.ExecContext(ctx, query, state, taskID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ResumeReeveSession runs an ended or suspended session again, with a new
// API token, tool and model.
func (s *Store) ResumeReeveSession(ctx context.Context, r ReeveSession) error {
	res, err := s.db.ExecContext(ctx, `UPDATE reeve_sessions SET state = 'running', agent_kind = ?, model = ?, launch_args = ?,
		api_token = ?, api_token_hash = ?, ended_at = NULL,
		updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), active_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE task_id = ?`,
		r.AgentKind, r.Model, r.LaunchArgs, r.APIToken, r.APITokenHash, r.TaskID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("reeve session %s: %w", r.TaskID, ErrNotFound)
	}
	return nil
}

// TouchReeveSession records activity in a session (for the idle suspend).
func (s *Store) TouchReeveSession(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE reeve_sessions SET active_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE task_id = ?`, taskID)
	return err
}

// DeleteReeveSession removes an ended session and its task.
func (s *Store) DeleteReeveSession(ctx context.Context, taskID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM reeve_sessions WHERE task_id = ? AND state = 'ended'`, taskID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return s.DeleteTask(ctx, taskID)
}
