package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

var ErrWorkflowStateConflict = errors.New("workflow state conflict")

const workflowRunSelect = `SELECT id, workflow_name, workflow_title, definition_source,
	definition_digest, definition_json, inputs_json, status, current_step_id,
	COALESCE(task_id, '') AS task_id, error, created_at, started_at, completed_at,
	COALESCE(related_task_id, '') AS related_task_id FROM workflow_runs`

type WorkflowRun struct {
	ID               string  `db:"id" json:"id"`
	WorkflowName     string  `db:"workflow_name" json:"workflow_name"`
	WorkflowTitle    string  `db:"workflow_title" json:"workflow_title"`
	DefinitionSource string  `db:"definition_source" json:"definition_source"`
	DefinitionDigest string  `db:"definition_digest" json:"definition_digest"`
	DefinitionJSON   string  `db:"definition_json" json:"definition_json"`
	InputsJSON       string  `db:"inputs_json" json:"inputs_json"`
	Status           string  `db:"status" json:"status"`
	CurrentStepID    string  `db:"current_step_id" json:"current_step_id"`
	TaskID           string  `db:"task_id" json:"task_id,omitempty"`
	RelatedTaskID    string  `db:"related_task_id" json:"related_task_id,omitempty"`
	Error            string  `db:"error" json:"error,omitempty"`
	CreatedAt        string  `db:"created_at" json:"created_at"`
	StartedAt        *string `db:"started_at" json:"started_at,omitempty"`
	CompletedAt      *string `db:"completed_at" json:"completed_at,omitempty"`
	NeedsAttention   bool    `db:"-" json:"needs_attention,omitempty"`
	AttentionReason  string  `db:"-" json:"attention_reason,omitempty"`
}

type WorkflowStepRun struct {
	RunID              string  `db:"run_id" json:"run_id"`
	StepID             string  `db:"step_id" json:"step_id"`
	StepOrder          int     `db:"step_order" json:"step_order"`
	Title              string  `db:"title" json:"title"`
	ExecutorType       string  `db:"executor_type" json:"executor_type"`
	DefinitionJSON     string  `db:"definition_json" json:"definition_json"`
	ResolvedInputsJSON string  `db:"resolved_inputs_json" json:"resolved_inputs_json"`
	OutputsJSON        string  `db:"outputs_json" json:"outputs_json"`
	Status             string  `db:"status" json:"status"`
	LatestAttempt      int     `db:"latest_attempt" json:"latest_attempt"`
	Error              string  `db:"error" json:"error,omitempty"`
	StartedAt          *string `db:"started_at" json:"started_at,omitempty"`
	CompletedAt        *string `db:"completed_at" json:"completed_at,omitempty"`
}

type WorkflowStepAttempt struct {
	ID              string  `db:"id" json:"id"`
	RunID           string  `db:"run_id" json:"run_id"`
	StepID          string  `db:"step_id" json:"step_id"`
	AttemptNumber   int     `db:"attempt_number" json:"attempt_number"`
	ExecutorType    string  `db:"executor_type" json:"executor_type"`
	AgentSessionID  *int    `db:"agent_session_id" json:"agent_session_id,omitempty"`
	TaskID          string  `db:"task_id" json:"task_id,omitempty"`
	TmuxSession     string  `db:"tmux_session" json:"tmux_session,omitempty"`
	AgentKind       string  `db:"agent_kind" json:"agent_kind,omitempty"`
	Tier            string  `db:"tier" json:"tier,omitempty"`
	WorkingDir      string  `db:"working_dir" json:"working_dir,omitempty"`
	Status          string  `db:"status" json:"status"`
	Error           string  `db:"error" json:"error,omitempty"`
	StartedAt       string  `db:"started_at" json:"started_at"`
	EndedAt         *string `db:"ended_at" json:"ended_at,omitempty"`
	AgentActivity   string  `db:"-" json:"agent_activity,omitempty"`
	NeedsAttention  bool    `db:"-" json:"needs_attention,omitempty"`
	AttentionReason string  `db:"-" json:"attention_reason,omitempty"`
}

type WorkflowArtifact struct {
	ID           string `db:"id" json:"id"`
	RunID        string `db:"run_id" json:"run_id"`
	StepID       string `db:"step_id" json:"step_id"`
	AttemptID    string `db:"attempt_id" json:"attempt_id"`
	TaskID       string `db:"task_id" json:"task_id,omitempty"`
	OutputKey    string `db:"output_key" json:"output_key"`
	ManagedPath  string `db:"managed_path" json:"managed_path"`
	OriginalPath string `db:"original_path" json:"original_path"`
	MediaType    string `db:"media_type" json:"media_type"`
	SizeBytes    int64  `db:"size_bytes" json:"size_bytes"`
	Digest       string `db:"digest" json:"digest"`
	CreatedAt    string `db:"created_at" json:"created_at"`
}

type WorkflowRunView struct {
	Run       WorkflowRun           `json:"run"`
	Steps     []WorkflowStepRun     `json:"steps"`
	Attempts  []WorkflowStepAttempt `json:"attempts"`
	Artifacts []WorkflowArtifact    `json:"artifacts"`
}

func (s *Store) CreateWorkflowRun(ctx context.Context, run WorkflowRun, steps []WorkflowStepRun) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		if _, err := tx.NamedExecContext(ctx, `INSERT INTO workflow_runs
			(id, workflow_name, workflow_title, definition_source, definition_digest, definition_json, inputs_json, status, current_step_id, task_id, related_task_id, error, started_at)
			VALUES (:id, :workflow_name, :workflow_title, :definition_source, :definition_digest, :definition_json, :inputs_json, :status, :current_step_id, NULLIF(:task_id,''), NULLIF(:related_task_id,''), :error, datetime('now'))`, run); err != nil {
			return err
		}
		for _, step := range steps {
			if step.ResolvedInputsJSON == "" {
				step.ResolvedInputsJSON = "{}"
			}
			if step.OutputsJSON == "" {
				step.OutputsJSON = "{}"
			}
			if _, err := tx.NamedExecContext(ctx, `INSERT INTO workflow_step_runs
				(run_id, step_id, step_order, title, executor_type, definition_json, resolved_inputs_json, outputs_json, status, latest_attempt, error)
				VALUES (:run_id, :step_id, :step_order, :title, :executor_type, :definition_json, :resolved_inputs_json, :outputs_json, :status, :latest_attempt, :error)`, step); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) GetWorkflowRun(ctx context.Context, id string) (*WorkflowRunView, error) {
	view := WorkflowRunView{
		Steps: make([]WorkflowStepRun, 0), Attempts: make([]WorkflowStepAttempt, 0), Artifacts: make([]WorkflowArtifact, 0),
	}
	if err := s.db.GetContext(ctx, &view.Run, workflowRunSelect+" WHERE id = ?", id); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.db.SelectContext(ctx, &view.Steps, "SELECT * FROM workflow_step_runs WHERE run_id = ? ORDER BY step_order", id); err != nil {
		return nil, err
	}
	if err := s.db.SelectContext(ctx, &view.Attempts, "SELECT * FROM workflow_step_attempts WHERE run_id = ? ORDER BY step_id, attempt_number", id); err != nil {
		return nil, err
	}
	if err := s.db.SelectContext(ctx, &view.Artifacts, "SELECT * FROM workflow_artifacts WHERE run_id = ? ORDER BY created_at, id", id); err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *Store) WaitWorkflowStepManual(ctx context.Context, runID, stepID, resolvedInputsJSON string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workflow_step_runs SET status = 'waiting_manual', resolved_inputs_json = ?, error = '' WHERE run_id = ? AND step_id = ? AND status = 'pending'`, resolvedInputsJSON, runID, stepID)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkflowStateConflict
		}
		result, err = tx.ExecContext(ctx, `UPDATE workflow_runs SET status = 'waiting' WHERE id = ? AND current_step_id = ? AND status = 'running'`, runID, stepID)
		if err != nil {
			return err
		}
		rows, err = result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkflowStateConflict
		}
		return nil
	})
}

func (s *Store) TriggerWorkflowStep(ctx context.Context, runID, stepID string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workflow_step_runs SET status = 'pending', error = '' WHERE run_id = ? AND step_id = ? AND status = 'waiting_manual'`, runID, stepID)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkflowStateConflict
		}
		result, err = tx.ExecContext(ctx, `UPDATE workflow_runs SET status = 'running' WHERE id = ? AND current_step_id = ? AND status = 'waiting'`, runID, stepID)
		if err != nil {
			return err
		}
		rows, err = result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkflowStateConflict
		}
		return nil
	})
}

func (s *Store) StartWorkflowStepAttempt(ctx context.Context, attempt WorkflowStepAttempt, resolvedInputsJSON string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workflow_step_runs
			SET status = 'running', resolved_inputs_json = ?, latest_attempt = ?, started_at = COALESCE(started_at, datetime('now')), error = ''
			WHERE run_id = ? AND step_id = ? AND status IN ('pending','failed','interrupted')`,
			resolvedInputsJSON, attempt.AttemptNumber, attempt.RunID, attempt.StepID)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkflowStateConflict
		}
		_, err = tx.NamedExecContext(ctx, `INSERT INTO workflow_step_attempts
			(id, run_id, step_id, attempt_number, executor_type, agent_session_id, task_id, tmux_session, agent_kind, tier, working_dir, status, error)
			VALUES (:id, :run_id, :step_id, :attempt_number, :executor_type, :agent_session_id, :task_id, :tmux_session, :agent_kind, :tier, :working_dir, :status, :error)`, attempt)
		return err
	})
}

func (s *Store) CompleteWorkflowStep(ctx context.Context, runID, stepID, attemptID, outputsJSON, nextStepID string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workflow_step_attempts SET status = 'succeeded', ended_at = datetime('now')
			WHERE id = ? AND run_id = ? AND step_id = ? AND status IN ('running','waiting_agent')`, attemptID, runID, stepID)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkflowStateConflict
		}
		result, err = tx.ExecContext(ctx, `UPDATE workflow_step_runs SET status = 'succeeded', outputs_json = ?, completed_at = datetime('now'), error = ''
			WHERE run_id = ? AND step_id = ? AND status IN ('running','waiting_agent')`, outputsJSON, runID, stepID)
		if err != nil {
			return err
		}
		rows, err = result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkflowStateConflict
		}
		if nextStepID == "" {
			result, err = tx.ExecContext(ctx, `UPDATE workflow_runs SET status = 'succeeded', current_step_id = '', completed_at = datetime('now'), error = '' WHERE id = ? AND current_step_id = ? AND status = 'running'`, runID, stepID)
		} else {
			result, err = tx.ExecContext(ctx, `UPDATE workflow_runs SET current_step_id = ?, error = '' WHERE id = ? AND current_step_id = ? AND status = 'running'`, nextStepID, runID, stepID)
		}
		if err != nil {
			return err
		}
		rows, err = result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkflowStateConflict
		}
		if nextStepID == "" {
			result, err = tx.ExecContext(ctx, `UPDATE tasks SET status = 'Done', updated_at = datetime('now') WHERE id = (SELECT task_id FROM workflow_runs WHERE id = ?)`, runID)
			if err != nil {
				return err
			}
			rows, err = result.RowsAffected()
			if err != nil {
				return err
			}
			if rows != 1 {
				return ErrWorkflowStateConflict
			}
		}
		return nil
	})
}

func (s *Store) MarkWorkflowStepWaiting(ctx context.Context, runID, stepID, attemptID string, session *AgentSession, workingDir string) error {
	var sessionID any
	var tmuxName, agentKind string
	if session != nil {
		sessionID, tmuxName, agentKind = session.ID, session.TmuxSession, session.AgentKind
	}
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workflow_step_attempts SET status = 'waiting_agent', agent_session_id = ?, tmux_session = ?, agent_kind = ?, working_dir = ? WHERE id = ? AND run_id = ? AND step_id = ? AND status = 'running'`, sessionID, tmuxName, agentKind, workingDir, attemptID, runID, stepID)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkflowStateConflict
		}
		result, err = tx.ExecContext(ctx, `UPDATE workflow_step_runs SET status = 'waiting_agent' WHERE run_id = ? AND step_id = ? AND status = 'running'`, runID, stepID)
		if err != nil {
			return err
		}
		rows, err = result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkflowStateConflict
		}
		return nil
	})
}

func (s *Store) FailWorkflowStep(ctx context.Context, runID, stepID, attemptID, errorMessage string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workflow_step_attempts SET status = 'failed', error = ?, ended_at = datetime('now') WHERE id = ? AND run_id = ? AND step_id = ? AND status IN ('running','waiting_agent')`, errorMessage, attemptID, runID, stepID)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkflowStateConflict
		}
		result, err = tx.ExecContext(ctx, `UPDATE workflow_step_runs SET status = 'failed', error = ?, completed_at = datetime('now') WHERE run_id = ? AND step_id = ? AND status IN ('running','waiting_agent')`, errorMessage, runID, stepID)
		if err != nil {
			return err
		}
		rows, err = result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkflowStateConflict
		}
		result, err = tx.ExecContext(ctx, `UPDATE workflow_runs SET status = 'failed', error = ? WHERE id = ? AND current_step_id = ? AND status = 'running'`, errorMessage, runID, stepID)
		if err != nil {
			return err
		}
		rows, err = result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkflowStateConflict
		}
		return nil
	})
}

func (s *Store) ResetWorkflowStepForRetry(ctx context.Context, runID, stepID string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workflow_step_runs SET status = 'pending', error = '', completed_at = NULL WHERE run_id = ? AND step_id = ? AND status IN ('failed','interrupted')`, runID, stepID)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkflowStateConflict
		}
		result, err = tx.ExecContext(ctx, `UPDATE workflow_runs SET status = 'running', current_step_id = ?, error = '', completed_at = NULL WHERE id = ? AND status = 'failed'`, stepID, runID)
		if err != nil {
			return err
		}
		rows, err = result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkflowStateConflict
		}
		return nil
	})
}

func (s *Store) GetWorkflowArtifactByAttemptOutput(ctx context.Context, attemptID, outputKey string) (*WorkflowArtifact, error) {
	var artifact WorkflowArtifact
	if err := s.db.GetContext(ctx, &artifact, "SELECT * FROM workflow_artifacts WHERE attempt_id = ? AND output_key = ?", attemptID, outputKey); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &artifact, nil
}

func (s *Store) GetWorkflowArtifact(ctx context.Context, id string) (*WorkflowArtifact, error) {
	var artifact WorkflowArtifact
	if err := s.db.GetContext(ctx, &artifact, "SELECT * FROM workflow_artifacts WHERE id = ?", id); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &artifact, nil
}

func (s *Store) DeleteWorkflowArtifactsByAttempt(ctx context.Context, attemptID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM workflow_artifacts WHERE attempt_id = ?", attemptID)
	return err
}

func (s *Store) InsertWorkflowArtifact(ctx context.Context, artifact WorkflowArtifact) error {
	_, err := s.db.NamedExecContext(ctx, `INSERT INTO workflow_artifacts
		(id, run_id, step_id, attempt_id, task_id, output_key, managed_path, original_path, media_type, size_bytes, digest)
		VALUES (:id, :run_id, :step_id, :attempt_id, :task_id, :output_key, :managed_path, :original_path, :media_type, :size_bytes, :digest)`, artifact)
	return err
}

func (s *Store) ListWorkflowRunsByRelatedTask(ctx context.Context, taskID string) ([]WorkflowRun, error) {
	var runs []WorkflowRun
	err := s.db.SelectContext(ctx, &runs, workflowRunSelect+" WHERE related_task_id = ? ORDER BY created_at DESC", taskID)
	return runs, err
}

func (s *Store) ListWorkflowRuns(ctx context.Context, status string, limit int) ([]WorkflowRun, error) {
	if limit <= 0 {
		limit = 100
	}
	var runs []WorkflowRun
	query := workflowRunSelect
	args := []any{}
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, limit)
	if err := s.db.SelectContext(ctx, &runs, query, args...); err != nil {
		return nil, err
	}
	return runs, nil
}

func (s *Store) CancelWorkflowRun(ctx context.Context, runID, reason string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workflow_runs SET status = 'cancelled', error = ?, completed_at = datetime('now') WHERE id = ? AND status IN ('running','waiting')`, reason, runID)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkflowStateConflict
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workflow_step_runs SET status = 'cancelled', error = ?, completed_at = datetime('now') WHERE run_id = ? AND status IN ('pending','running','waiting_agent','waiting_manual')`, reason, runID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE workflow_step_attempts SET status = 'cancelled', error = ?, ended_at = datetime('now') WHERE run_id = ? AND status IN ('running','waiting_agent')`, reason, runID)
		return err
	})
}

func (s *Store) UpdateWorkflowAttemptStatus(ctx context.Context, attemptID, from, to, errorMessage string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE workflow_step_attempts SET status = ?, error = ?, ended_at = CASE WHEN ? IN ('failed','interrupted','cancelled','succeeded') THEN datetime('now') ELSE ended_at END WHERE id = ? AND status = ?`, to, errorMessage, to, attemptID, from)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrWorkflowStateConflict
	}
	return nil
}

func withTx(ctx context.Context, db *sqlx.DB, fn func(*sqlx.Tx) error) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
