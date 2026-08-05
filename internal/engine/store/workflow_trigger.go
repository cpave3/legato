package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

type TaskTransition struct {
	ID         string `db:"id" json:"id"`
	TaskID     string `db:"task_id" json:"task_id"`
	Source     string `db:"source" json:"source"`
	FromColumn string `db:"from_column" json:"from_column"`
	ToColumn   string `db:"to_column" json:"to_column"`
	TaskJSON   string `db:"task_json" json:"task_json"`
	CreatedAt  string `db:"created_at" json:"created_at"`
}

type WorkflowTriggerRequest struct {
	ID                string `db:"id" json:"id"`
	TransitionID      string `db:"transition_id" json:"transition_id"`
	WorkflowName      string `db:"workflow_name" json:"workflow_name"`
	DefinitionSource  string `db:"definition_source" json:"definition_source"`
	DefinitionDigest  string `db:"definition_digest" json:"definition_digest"`
	DefinitionJSON    string `db:"definition_json" json:"definition_json"`
	InputsJSON        string `db:"inputs_json" json:"inputs_json"`
	MissingInputsJSON string `db:"missing_inputs_json" json:"missing_inputs_json"`
	Status            string `db:"status" json:"status"`
	RunID             string `db:"run_id" json:"run_id,omitempty"`
	Error             string `db:"error" json:"error,omitempty"`
	CreatedAt         string `db:"created_at" json:"created_at"`
	UpdatedAt         string `db:"updated_at" json:"updated_at"`
}

func (s *Store) RecordTaskTransition(ctx context.Context, transition TaskTransition, task Task) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET status = ?, sort_order = ?, remote_meta = ?, updated_at = ? WHERE id = ?`, task.Status, task.SortOrder, task.RemoteMeta, task.UpdatedAt, task.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO task_transitions (id, task_id, source, from_column, to_column, task_json) VALUES (?, ?, ?, ?, ?, ?)`, transition.ID, transition.TaskID, transition.Source, transition.FromColumn, transition.ToColumn, transition.TaskJSON)
		return err
	})
}
func (s *Store) LatestTaskTransition(ctx context.Context, taskID string) (*TaskTransition, error) {
	var transition TaskTransition
	if err := s.db.GetContext(ctx, &transition, `SELECT * FROM task_transitions WHERE task_id = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`, taskID); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &transition, nil
}

func (s *Store) ListTaskTransitions(ctx context.Context, limit int) ([]TaskTransition, error) {
	if limit <= 0 {
		limit = 100
	}
	var transitions []TaskTransition
	err := s.db.SelectContext(ctx, &transitions, `SELECT * FROM task_transitions ORDER BY created_at, id LIMIT ?`, limit)
	return transitions, err
}

func (s *Store) GetTaskTransition(ctx context.Context, id string) (*TaskTransition, error) {
	var transition TaskTransition
	if err := s.db.GetContext(ctx, &transition, `SELECT * FROM task_transitions WHERE id = ?`, id); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &transition, nil
}

func (s *Store) ListWorkflowTriggerRequestsForTransition(ctx context.Context, transitionID string) ([]WorkflowTriggerRequest, error) {
	var requests []WorkflowTriggerRequest
	err := s.db.SelectContext(ctx, &requests, `SELECT * FROM workflow_trigger_requests WHERE transition_id = ? ORDER BY created_at, id`, transitionID)
	return requests, err
}

func (s *Store) ListWorkflowTriggerRequestsForTask(ctx context.Context, taskID string) ([]WorkflowTriggerRequest, error) {
	var requests []WorkflowTriggerRequest
	err := s.db.SelectContext(ctx, &requests, `SELECT r.* FROM workflow_trigger_requests r JOIN task_transitions t ON t.id = r.transition_id WHERE t.task_id = ? ORDER BY r.created_at, r.id`, taskID)
	return requests, err
}

func (s *Store) UpsertWorkflowTriggerRequest(ctx context.Context, request WorkflowTriggerRequest) (*WorkflowTriggerRequest, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO workflow_trigger_requests (id, transition_id, workflow_name, definition_source, definition_digest, definition_json, inputs_json, missing_inputs_json, status, run_id, error) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(transition_id, workflow_name, definition_digest) DO NOTHING`, request.ID, request.TransitionID, request.WorkflowName, request.DefinitionSource, request.DefinitionDigest, request.DefinitionJSON, request.InputsJSON, request.MissingInputsJSON, request.Status, request.RunID, request.Error)
	if err != nil {
		return nil, err
	}
	var stored WorkflowTriggerRequest
	err = s.db.GetContext(ctx, &stored, `SELECT * FROM workflow_trigger_requests WHERE transition_id = ? AND workflow_name = ? AND definition_digest = ?`, request.TransitionID, request.WorkflowName, request.DefinitionDigest)
	return &stored, err
}

func (s *Store) UpdateWorkflowTriggerRequest(ctx context.Context, id, status, runID, inputsJSON, missingJSON, errorText string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE workflow_trigger_requests SET status = ?, run_id = ?, inputs_json = ?, missing_inputs_json = ?, error = ?, updated_at = datetime('now') WHERE id = ?`, status, runID, inputsJSON, missingJSON, errorText, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) TransitionWorkflowTriggerRequest(ctx context.Context, id, fromStatus, toStatus string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE workflow_trigger_requests SET status = ?, updated_at = datetime('now') WHERE id = ? AND status = ?`, toStatus, id, fromStatus)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrWorkflowStateConflict
	}
	return nil
}

func (s *Store) GetWorkflowTriggerRequest(ctx context.Context, id string) (*WorkflowTriggerRequest, error) {
	var request WorkflowTriggerRequest
	if err := s.db.GetContext(ctx, &request, `SELECT * FROM workflow_trigger_requests WHERE id = ?`, id); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &request, nil
}

func (s *Store) FindWorkflowRunByTriggerRequest(ctx context.Context, requestID string) (*WorkflowRunView, error) {
	var id string
	if err := s.db.GetContext(ctx, &id, `SELECT id FROM workflow_runs WHERE trigger_request_id = ?`, requestID); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.GetWorkflowRun(ctx, id)
}

func (s *Store) ListWorkflowTriggerRequests(ctx context.Context, status string) ([]WorkflowTriggerRequest, error) {
	var requests []WorkflowTriggerRequest
	query := `SELECT * FROM workflow_trigger_requests`
	args := []any{}
	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at, id`
	err := s.db.SelectContext(ctx, &requests, query, args...)
	return requests, err
}
