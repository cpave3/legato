package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Handoff states on a worker.
const (
	HandoffPending    = "pending"
	HandoffInProgress = "in_progress"
	HandoffDone       = "done"
	HandoffFailed     = "failed"
)

// Handoff is a task a main instance handed to this worker: the main's
// instance ID and handoff ID name it, TaskID is the worker's card.
type Handoff struct {
	MainInstanceID string  `db:"main_instance_id"`
	HandoffID      string  `db:"handoff_id"`
	MainName       string  `db:"main_name"`
	MainTaskID     string  `db:"main_task_id"`
	TaskID         string  `db:"task_id"`
	Purpose        string  `db:"purpose"`
	Kind           string  `db:"kind"`
	Repo           string  `db:"repo"`
	Branch         string  `db:"branch"`
	AgentKind      string  `db:"agent_kind"`
	State          string  `db:"state"`
	Failure        string  `db:"failure"`
	RequestHash    string  `db:"request_hash"`
	StartAttemptAt *string `db:"start_attempt_at"`
	CreatedAt      string  `db:"created_at"`
	UpdatedAt      string  `db:"updated_at"`
	ClosedAt       *string `db:"closed_at"`
}

// CreateHandoffCard makes a handed-off card in one transaction: the task,
// its notes and the handoff record. A handoff of the same main and ID
// already stored is ErrConflict, and nothing is made.
func (s *Store) CreateHandoffCard(ctx context.Context, task Task, notes []TaskNote, h Handoff) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // a committed transaction ignores it
	if _, err := tx.NamedExecContext(ctx, `
		INSERT INTO tasks (id, title, description, description_md, remote_description_md, status,
			priority, sort_order, provider, remote_id, remote_meta, metadata,
			workspace_id, ephemeral, swarm_working_dir, task_group, created_at, updated_at)
		VALUES (:id, :title, :description, :description_md, :remote_description_md, :status,
			:priority, :sort_order, :provider, :remote_id, :remote_meta, :metadata,
			:workspace_id, :ephemeral, :swarm_working_dir, :task_group, :created_at, :updated_at)`, task); err != nil {
		return err
	}
	for _, note := range notes {
		if _, err := tx.NamedExecContext(ctx, `INSERT INTO task_notes (id, task_id, body, source, remote_id) VALUES (:id, :task_id, :body, :source, :remote_id)`, note); err != nil {
			return err
		}
	}
	if _, err := tx.NamedExecContext(ctx, `INSERT INTO handoffs (main_instance_id, handoff_id, main_name, main_task_id, task_id, purpose, kind,
			repo, branch, agent_kind, state, failure, request_hash, created_at, updated_at)
		VALUES (:main_instance_id, :handoff_id, :main_name, :main_task_id, :task_id, :purpose, :kind,
			:repo, :branch, :agent_kind, :state, :failure, :request_hash, :created_at, :updated_at)`, h); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrConflict
		}
		return err
	}
	return tx.Commit()
}

// GetHandoff is a main's handoff by its ID, or ErrNotFound.
func (s *Store) GetHandoff(ctx context.Context, mainInstanceID, handoffID string) (*Handoff, error) {
	var h Handoff
	err := s.db.GetContext(ctx, &h, `SELECT * FROM handoffs WHERE main_instance_id = ? AND handoff_id = ?`, mainInstanceID, handoffID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

// ListOpenHandoffs lists the handoffs not closed, oldest first.
func (s *Store) ListOpenHandoffs(ctx context.Context) ([]Handoff, error) {
	var out []Handoff
	err := s.db.SelectContext(ctx, &out, `SELECT * FROM handoffs WHERE closed_at IS NULL ORDER BY created_at, rowid`)
	return out, err
}

// SetHandoffState moves an open handoff from one state to another, with a
// failure reason for failed, and says whether it moved: it changes nothing
// when the handoff is closed or no longer in from.
func (s *Store) SetHandoffState(ctx context.Context, mainInstanceID, handoffID, from, to, failure, at string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE handoffs SET state = ?, failure = ?, updated_at = ?
		WHERE main_instance_id = ? AND handoff_id = ? AND closed_at IS NULL AND state = ?`,
		to, failure, at, mainInstanceID, handoffID, from)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// ClaimHandoffStart claims a pending handoff's agent start for this Legato:
// true when no start was begun, or the last began before staleBefore (its
// Legato died mid-start). Times are RFC 3339 UTC text, which compares as
// time does.
func (s *Store) ClaimHandoffStart(ctx context.Context, mainInstanceID, handoffID, at, staleBefore string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE handoffs SET start_attempt_at = ?
		WHERE main_instance_id = ? AND handoff_id = ? AND closed_at IS NULL AND state = 'pending'
			AND (start_attempt_at IS NULL OR start_attempt_at < ?)`, at, mainInstanceID, handoffID, staleBefore)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// CountOpenHandoffsByMain counts each main's open handoffs, by instance ID.
func (s *Store) CountOpenHandoffsByMain(ctx context.Context) (map[string]int, error) {
	var rows []struct {
		MainInstanceID string `db:"main_instance_id"`
		N              int    `db:"n"`
	}
	if err := s.db.SelectContext(ctx, &rows, `SELECT main_instance_id, COUNT(*) AS n FROM handoffs WHERE closed_at IS NULL GROUP BY main_instance_id`); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, r := range rows {
		out[r.MainInstanceID] = r.N
	}
	return out, nil
}

// EnsureMainWorkspace is the workspace a main's handed-off tasks go in,
// made on the first call: named name, or name and the end of the instance
// ID when another workspace has that name. It is found by the instance ID
// afterwards, whatever it is called.
func (s *Store) EnsureMainWorkspace(ctx context.Context, mainInstanceID, name string) (int, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // a committed transaction ignores it
	var id int
	err = tx.GetContext(ctx, &id, `SELECT workspace_id FROM main_workspaces WHERE main_instance_id = ?`, mainInstanceID)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	suffix := mainInstanceID
	if len(suffix) > 4 {
		suffix = suffix[len(suffix)-4:]
	}
	var made bool
	for _, candidate := range []string{name, name + "-" + suffix, mainInstanceID} {
		result, err := tx.ExecContext(ctx, `INSERT INTO workspaces (name, sort_order) VALUES (?, (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM workspaces))
			ON CONFLICT(name) DO NOTHING`, candidate)
		if err != nil {
			return 0, err
		}
		if n, _ := result.RowsAffected(); n == 1 {
			last, err := result.LastInsertId()
			if err != nil {
				return 0, err
			}
			id, made = int(last), true
			break
		}
	}
	if !made {
		return 0, fmt.Errorf("no free workspace name for main instance %s", mainInstanceID)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO main_workspaces (main_instance_id, workspace_id) VALUES (?, ?)`, mainInstanceID, id); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// Away task states on a main.
const (
	AwaySending = "sending"
	AwayAway    = "away"
)

// AwayTask is a handoff of one of a main's tasks. WorkerName is read from
// workers ("" when the worker is no longer paired).
type AwayTask struct {
	HandoffID        string  `db:"handoff_id"`
	TaskID           string  `db:"task_id"`
	WorkerInstanceID string  `db:"worker_instance_id"`
	WorkerName       string  `db:"worker_name"`
	WorkerTaskID     string  `db:"worker_task_id"`
	State            string  `db:"state"`
	Purpose          string  `db:"purpose"`
	Kind             string  `db:"kind"`
	Repo             string  `db:"repo"`
	Branch           string  `db:"branch"`
	AgentKind        string  `db:"agent_kind"`
	LeftColumn       string  `db:"left_column"`
	LastSummary      *string `db:"last_summary"`
	LastSummaryAt    *string `db:"last_summary_at"`
	CreatedAt        string  `db:"created_at"`
	UpdatedAt        string  `db:"updated_at"`
	ClosedAt         *string `db:"closed_at"`
}

const awayTaskSelect = `SELECT a.*, COALESCE(w.name, '') AS worker_name FROM away_tasks a LEFT JOIN workers w ON w.instance_id = a.worker_instance_id`

// InsertAwayTask records a handoff of a task as it is sent. A task with an
// open handoff already is ErrConflict.
func (s *Store) InsertAwayTask(ctx context.Context, a AwayTask) error {
	_, err := s.db.NamedExecContext(ctx, `INSERT INTO away_tasks (handoff_id, task_id, worker_instance_id, worker_task_id, state, purpose, kind,
			repo, branch, agent_kind, left_column, created_at, updated_at)
		VALUES (:handoff_id, :task_id, :worker_instance_id, :worker_task_id, :state, :purpose, :kind,
			:repo, :branch, :agent_kind, :left_column, :created_at, :updated_at)`, a)
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrConflict
	}
	return err
}

// OpenAwayTask is a task's open handoff, sending or away, or ErrNotFound.
func (s *Store) OpenAwayTask(ctx context.Context, taskID string) (*AwayTask, error) {
	var a AwayTask
	err := s.db.GetContext(ctx, &a, awayTaskSelect+` WHERE a.task_id = ? AND a.closed_at IS NULL`, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ListAwayTasks lists the tasks away on workers, oldest handoff first.
func (s *Store) ListAwayTasks(ctx context.Context) ([]AwayTask, error) {
	var out []AwayTask
	err := s.db.SelectContext(ctx, &out, awayTaskSelect+` WHERE a.closed_at IS NULL AND a.state = 'away' ORDER BY a.created_at, a.rowid`)
	return out, err
}

// SetAwayTaskAway records that the worker accepted a handoff: the task is
// away, as the worker's task workerTaskID.
func (s *Store) SetAwayTaskAway(ctx context.Context, handoffID, workerTaskID, at string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE away_tasks SET state = 'away', worker_task_id = ?, updated_at = ? WHERE handoff_id = ? AND closed_at IS NULL`,
		workerTaskID, at, handoffID)
	return err
}

// DeleteAwayTask forgets a handoff the worker refused.
func (s *Store) DeleteAwayTask(ctx context.Context, handoffID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM away_tasks WHERE handoff_id = ?`, handoffID)
	return err
}
