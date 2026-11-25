package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

var ErrWorkflowStateConflict = errors.New("workflow state conflict")

const workflowRunSelect = `SELECT id, workflow_name, workflow_title, definition_source,
	definition_digest, definition_json, inputs_json, status, current_step_id,
	COALESCE(task_id, '') AS task_id, error, created_at, started_at, completed_at,
	COALESCE(related_task_id, '') AS related_task_id,
	COALESCE(trigger_request_id, '') AS trigger_request_id,
	COALESCE(parent_run_id, '') AS parent_run_id, parent_step_path, outputs_json, schema_version,
	COALESCE((SELECT title FROM tasks WHERE tasks.id = workflow_runs.related_task_id), '') AS related_task_title
	FROM workflow_runs`

// workflowStepColumns are the columns of a step as WorkflowStep reads them.
const workflowStepColumns = `run_id, path, parent_path, step_id, pass, step_order, title, executor_type,
	definition_json, resolved_json, outputs_json, status, latest_attempt, error, advance_due,
	loop_state_json, wait_signals, wait_task_id, wait_slot, wait_execution_id, wait_watermark,
	wait_session_id, deadline_at, COALESCE(child_run_id, '') AS child_run_id, started_at, completed_at`

const workflowAttemptColumns = `id, run_id, path, step_id, attempt_number, executor_type, agent_session_id,
	task_id, tmux_session, agent_kind, tier, working_dir, status, error, started_at, ended_at`

const workflowArtifactColumns = `id, run_id, path, step_id, attempt_id, task_id, output_key, managed_path,
	original_path, media_type, size_bytes, digest, created_at`

type WorkflowRun struct {
	ID               string `db:"id" json:"id"`
	WorkflowName     string `db:"workflow_name" json:"workflow_name"`
	WorkflowTitle    string `db:"workflow_title" json:"workflow_title"`
	DefinitionSource string `db:"definition_source" json:"definition_source"`
	DefinitionDigest string `db:"definition_digest" json:"definition_digest"`
	DefinitionJSON   string `db:"definition_json" json:"definition_json"`
	InputsJSON       string `db:"inputs_json" json:"inputs_json"`
	Status           string `db:"status" json:"status"`
	// CurrentStepID is the path of the step the run is at.
	CurrentStepID    string `db:"current_step_id" json:"current_step_id"`
	TaskID           string `db:"task_id" json:"task_id,omitempty"`
	RelatedTaskID    string `db:"related_task_id" json:"related_task_id,omitempty"`
	RelatedTaskTitle string `db:"related_task_title" json:"related_task_title,omitempty"`
	// RelatedTaskKey is the key a person knows the related task by: its
	// ticket's, else its ID. The service fills it (WorkflowService.ListRuns).
	RelatedTaskKey   string `db:"-" json:"related_task_key,omitempty"`
	TriggerRequestID string `db:"trigger_request_id" json:"trigger_request_id,omitempty"`
	// ParentRunID and ParentStepPath name the workflow.run step that
	// started this run as its child (LEG-489); empty for any other run.
	ParentRunID    string `db:"parent_run_id" json:"parent_run_id,omitempty"`
	ParentStepPath string `db:"parent_step_path" json:"parent_step_path,omitempty"`
	// OutputsJSON is the run's own outputs, computed when it succeeds.
	OutputsJSON string `db:"outputs_json" json:"outputs_json,omitempty"`
	// SchemaVersion is the schema version of the definition the run runs.
	SchemaVersion   int     `db:"schema_version" json:"schema_version"`
	Error           string  `db:"error" json:"error,omitempty"`
	CreatedAt       string  `db:"created_at" json:"created_at"`
	StartedAt       *string `db:"started_at" json:"started_at,omitempty"`
	CompletedAt     *string `db:"completed_at" json:"completed_at,omitempty"`
	NeedsAttention  bool    `db:"-" json:"needs_attention,omitempty"`
	AttentionReason string  `db:"-" json:"attention_reason,omitempty"`
	// OutputWarning says, quietly, that an agent step of the run finished
	// with no output (LEG-532); the service fills it.
	OutputWarning string `db:"-" json:"output_warning,omitempty"`
	// Steps is filled by ListWorkflowRuns with WithSteps only (the runs list
	// of the API): each step's place and state, so a list of runs can say how
	// far each one is.
	Steps []WorkflowStepSummary `db:"-" json:"steps,omitempty"`
}

// WorkflowStepSummary is one step of a run as the runs list shows it.
type WorkflowStepSummary struct {
	StepID  string `db:"step_id" json:"step_id"`
	Title   string `db:"title" json:"title"`
	Status  string `db:"status" json:"status"`
	Trigger string `db:"step_trigger" json:"trigger,omitempty"`
}

// WorkflowStep is one step instance of a run, created when the run reaches
// it. Path keys it within the run: a top-level step's ID, or its scope's
// path, a slash and its ID.
type WorkflowStep struct {
	RunID      string `db:"run_id" json:"run_id"`
	Path       string `db:"path" json:"path"`
	ParentPath string `db:"parent_path" json:"parent_path,omitempty"`
	StepID     string `db:"step_id" json:"step_id"`
	// Pass is the loop pass whose scope the step is in; nil outside loops.
	Pass               *int    `db:"pass" json:"pass,omitempty"`
	StepOrder          int     `db:"step_order" json:"step_order"`
	Title              string  `db:"title" json:"title"`
	ExecutorType       string  `db:"executor_type" json:"executor_type"`
	DefinitionJSON     string  `db:"definition_json" json:"definition_json"`
	ResolvedInputsJSON string  `db:"resolved_json" json:"resolved_inputs_json"`
	OutputsJSON        string  `db:"outputs_json" json:"outputs_json"`
	Status             string  `db:"status" json:"status"`
	LatestAttempt      int     `db:"latest_attempt" json:"latest_attempt"`
	Error              string  `db:"error" json:"error,omitempty"`
	AdvanceDue         bool    `db:"advance_due" json:"advance_due,omitempty"`
	LoopStateJSON      string  `db:"loop_state_json" json:"-"`
	WaitSignals        string  `db:"wait_signals" json:"-"`
	WaitTaskID         string  `db:"wait_task_id" json:"-"`
	WaitSlot           string  `db:"wait_slot" json:"-"`
	WaitExecutionID    string  `db:"wait_execution_id" json:"-"`
	WaitWatermark      int64   `db:"wait_watermark" json:"-"`
	WaitSessionID      *int    `db:"wait_session_id" json:"-"`
	DeadlineAt         *string `db:"deadline_at" json:"deadline_at,omitempty"`
	ChildRunID         string  `db:"child_run_id" json:"child_run_id,omitempty"`
	StartedAt          *string `db:"started_at" json:"started_at,omitempty"`
	CompletedAt        *string `db:"completed_at" json:"completed_at,omitempty"`
	// OutputWarning says that this agent step finished with no output and
	// what became of its agent (LEG-532); the service fills it.
	OutputWarning string `db:"-" json:"output_warning,omitempty"`
	// ErrorCode and ErrorLink are what the error of a failed or paused
	// step carries beside its text (StepFailure), read from its outputs.
	ErrorCode string `db:"-" json:"error_code,omitempty"`
	ErrorLink string `db:"-" json:"error_link,omitempty"`
}

type WorkflowStepAttempt struct {
	ID    string `db:"id" json:"id"`
	RunID string `db:"run_id" json:"run_id"`
	// Path is the step instance's; StepID its ID alone.
	Path           string  `db:"path" json:"path"`
	StepID         string  `db:"step_id" json:"step_id"`
	AttemptNumber  int     `db:"attempt_number" json:"attempt_number"`
	ExecutorType   string  `db:"executor_type" json:"executor_type"`
	AgentSessionID *int    `db:"agent_session_id" json:"agent_session_id,omitempty"`
	TaskID         string  `db:"task_id" json:"task_id,omitempty"`
	TmuxSession    string  `db:"tmux_session" json:"tmux_session,omitempty"`
	AgentKind      string  `db:"agent_kind" json:"agent_kind,omitempty"`
	Tier           string  `db:"tier" json:"tier,omitempty"`
	WorkingDir     string  `db:"working_dir" json:"working_dir,omitempty"`
	Status         string  `db:"status" json:"status"`
	Error          string  `db:"error" json:"error,omitempty"`
	StartedAt      string  `db:"started_at" json:"started_at"`
	EndedAt        *string `db:"ended_at" json:"ended_at,omitempty"`
	AgentActivity  string  `db:"-" json:"agent_activity,omitempty"`
	// AgentID is the agent its session is (the session's task, with a named
	// slot's suffix): an interview's attempt keeps the run's task, not the
	// borrowed agent's. The service fills it.
	AgentID         string `db:"-" json:"agent_id,omitempty"`
	NeedsAttention  bool   `db:"-" json:"needs_attention,omitempty"`
	AttentionReason string `db:"-" json:"attention_reason,omitempty"`
	// Resumable says Resume continues the attempt: it is suspended, or an
	// earlier Legato failed it when its agent was interrupted and that
	// agent can still be resumed. The service fills it.
	Resumable bool `db:"-" json:"resumable,omitempty"`
	// AttentionActions are the actions a step waiting on its service
	// offers beyond the usual, such as message for a stuck review loop
	// with an agent author.
	AttentionActions []string `db:"-" json:"attention_actions,omitempty"`
}

type WorkflowArtifact struct {
	ID           string `db:"id" json:"id"`
	RunID        string `db:"run_id" json:"run_id"`
	Path         string `db:"path" json:"path"`
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
	Steps     []WorkflowStep        `json:"steps"`
	Attempts  []WorkflowStepAttempt `json:"attempts"`
	Artifacts []WorkflowArtifact    `json:"artifacts"`
	// Details are what a step's executor says of it, by step path, such as
	// where a review loop stands; the service fills them.
	Details map[string]any `json:"details,omitempty"`
	// TriggerKind is the kind of trigger that started the run, from its
	// trigger request; empty for a run started by hand. The service fills it.
	TriggerKind string `json:"trigger_kind"`
	// ResumeTaskID is the card whose agent Resume agent starts, or sends a
	// macro to: the waiting github.pr.wait step's task, else the run's
	// task input. ResumeAgentState is that card's agent: "" with none
	// running, "running", or "waiting" while it waits on the operator.
	// CanResumeAgent says whether Resume agent is offered now. The service
	// fills them (LEG-614).
	ResumeTaskID     string `json:"resume_task_id,omitempty"`
	ResumeAgentState string `json:"resume_agent_state,omitempty"`
	CanResumeAgent   bool   `json:"can_resume_agent,omitempty"`
}

// RetitleWorkflowRun gives a run, and its task when it has one, a new title:
// a step that learns what the run is about once it starts (a review loop
// that found a stack, say).
func (s *Store) RetitleWorkflowRun(ctx context.Context, runID, title string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE workflow_runs SET workflow_title = ? WHERE id = ?`, title, runID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE tasks SET title = ? WHERE id = (SELECT task_id FROM workflow_runs WHERE id = ?)`, title, runID)
		return err
	})
}

// CreateWorkflowRun records a run and the step instances it has reached
// at its start: its top-level steps.
func (s *Store) CreateWorkflowRun(ctx context.Context, run WorkflowRun, steps []WorkflowStep) error {
	if run.SchemaVersion == 0 {
		run.SchemaVersion = 1
	}
	if run.OutputsJSON == "" {
		run.OutputsJSON = "{}"
	}
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		if _, err := tx.NamedExecContext(ctx, `INSERT INTO workflow_runs
			(id, workflow_name, workflow_title, definition_source, definition_digest, definition_json, inputs_json, status, current_step_id, task_id, related_task_id, trigger_request_id, parent_run_id, parent_step_path, outputs_json, schema_version, error, started_at)
			VALUES (:id, :workflow_name, :workflow_title, :definition_source, :definition_digest, :definition_json, :inputs_json, :status, :current_step_id, NULLIF(:task_id,''), NULLIF(:related_task_id,''), NULLIF(:trigger_request_id,''), NULLIF(:parent_run_id,''), :parent_step_path, :outputs_json, :schema_version, :error, datetime('now'))`, run); err != nil {
			return err
		}
		for _, step := range steps {
			if err := insertWorkflowStep(ctx, tx, step); err != nil {
				return err
			}
		}
		return nil
	})
}

// CreateWorkflowSteps records step instances a run has reached, such as
// a loop pass's (LEG-490). An instance that exists already is left as it is.
func (s *Store) CreateWorkflowSteps(ctx context.Context, steps []WorkflowStep) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		for _, step := range steps {
			if err := insertWorkflowStep(ctx, tx, step); err != nil {
				return err
			}
		}
		return nil
	})
}

func insertWorkflowStep(ctx context.Context, tx *sqlx.Tx, step WorkflowStep) error {
	if step.Path == "" {
		step.Path = step.StepID
	}
	if step.ResolvedInputsJSON == "" {
		step.ResolvedInputsJSON = "{}"
	}
	if step.OutputsJSON == "" {
		step.OutputsJSON = "{}"
	}
	if step.DefinitionJSON == "" {
		step.DefinitionJSON = "{}"
	}
	_, err := tx.NamedExecContext(ctx, `INSERT INTO workflow_steps
		(run_id, path, parent_path, step_id, pass, step_order, title, executor_type, definition_json, resolved_json, outputs_json, status, latest_attempt, error)
		VALUES (:run_id, :path, :parent_path, :step_id, :pass, :step_order, :title, :executor_type, :definition_json, :resolved_json, :outputs_json, :status, :latest_attempt, :error)
		ON CONFLICT (run_id, path) DO NOTHING`, step)
	return err
}

func (s *Store) GetWorkflowRun(ctx context.Context, id string) (*WorkflowRunView, error) {
	view := WorkflowRunView{
		Steps: make([]WorkflowStep, 0), Attempts: make([]WorkflowStepAttempt, 0), Artifacts: make([]WorkflowArtifact, 0),
	}
	if err := s.db.GetContext(ctx, &view.Run, workflowRunSelect+" WHERE id = ?", id); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	// Steps by scope: the run's own steps first, then each nested scope's
	// (a loop pass's), each in order.
	if err := s.db.SelectContext(ctx, &view.Steps, "SELECT "+workflowStepColumns+" FROM workflow_steps WHERE run_id = ? ORDER BY parent_path, step_order", id); err != nil {
		return nil, err
	}
	for i := range view.Steps {
		step := &view.Steps[i]
		if step.Status != "failed" && step.Status != WorkflowStepPaused {
			continue
		}
		var failure struct {
			Code string `json:"error_code"`
			Link string `json:"error_link"`
		}
		if json.Unmarshal([]byte(step.OutputsJSON), &failure) == nil {
			step.ErrorCode, step.ErrorLink = failure.Code, failure.Link
		}
	}
	if err := s.db.SelectContext(ctx, &view.Attempts, "SELECT "+workflowAttemptColumns+" FROM workflow_step_attempts WHERE run_id = ? ORDER BY path, attempt_number", id); err != nil {
		return nil, err
	}
	if err := s.db.SelectContext(ctx, &view.Artifacts, "SELECT "+workflowArtifactColumns+" FROM workflow_artifacts WHERE run_id = ? ORDER BY created_at, id", id); err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *Store) WaitWorkflowStepManual(ctx context.Context, runID, path, resolvedInputsJSON string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workflow_steps SET status = 'waiting_manual', resolved_json = ?, error = '' WHERE run_id = ? AND path = ? AND status = 'pending'`, resolvedInputsJSON, runID, path)
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
		result, err = tx.ExecContext(ctx, `UPDATE workflow_runs SET status = 'waiting' WHERE id = ? AND current_step_id = ? AND status = 'running'`, runID, path)
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

func (s *Store) TriggerWorkflowStep(ctx context.Context, runID, path string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workflow_steps SET status = 'pending', error = '' WHERE run_id = ? AND path = ? AND status = 'waiting_manual'`, runID, path)
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
		result, err = tx.ExecContext(ctx, `UPDATE workflow_runs SET status = 'running' WHERE id = ? AND current_step_id = ? AND status = 'waiting'`, runID, path)
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

// RecordWorkflowAttemptAgent notes on a step attempt that waits on its
// service the agent session started for it (Resume agent on a waiting
// github.pr.wait), so the step stops that agent, and no other, when it
// ends. ErrWorkflowStateConflict when the attempt no longer waits.
func (s *Store) RecordWorkflowAttemptAgent(ctx context.Context, attemptID string, session AgentSession) error {
	result, err := s.db.ExecContext(ctx, `UPDATE workflow_step_attempts SET agent_session_id = ?, tmux_session = ?, agent_kind = ?
		WHERE id = ? AND status = 'waiting'`, session.ID, session.TmuxSession, session.AgentKind, attemptID)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return err
	} else if rows != 1 {
		return ErrWorkflowStateConflict
	}
	return nil
}

func (s *Store) ListActiveWorkflowAgentAttemptsByTask(ctx context.Context, taskID string) ([]WorkflowStepAttempt, error) {
	attempts := []WorkflowStepAttempt{}
	err := s.db.SelectContext(ctx, &attempts, "SELECT "+workflowAttemptColumns+` FROM workflow_step_attempts
		WHERE task_id = ? AND executor_type = 'agent.run' AND status = 'waiting_agent'
		ORDER BY started_at, id`, taskID)
	return attempts, err
}

// ListWaitingWorkflowAttemptsByAgentSession includes both owned agent.run
// sessions and borrowed agent.interview sessions, whose task IDs may differ.
func (s *Store) ListWaitingWorkflowAttemptsByAgentSession(ctx context.Context, sessionID int) ([]WorkflowStepAttempt, error) {
	attempts := []WorkflowStepAttempt{}
	err := s.db.SelectContext(ctx, &attempts, "SELECT "+workflowAttemptColumns+` FROM workflow_step_attempts
  WHERE agent_session_id = ? AND status = 'waiting_agent' ORDER BY started_at,id`, sessionID)
	return attempts, err
}

func (s *Store) StartWorkflowStepAttempt(ctx context.Context, attempt WorkflowStepAttempt, resolvedInputsJSON string) error {
	if attempt.Path == "" {
		attempt.Path = attempt.StepID
	}
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workflow_steps
			SET status = 'running', resolved_json = ?, latest_attempt = ?, started_at = COALESCE(started_at, datetime('now')), error = ''
			WHERE run_id = ? AND path = ? AND status IN ('pending','failed','interrupted')`,
			resolvedInputsJSON, attempt.AttemptNumber, attempt.RunID, attempt.Path)
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
			(id, run_id, path, step_id, attempt_number, executor_type, agent_session_id, task_id, tmux_session, agent_kind, tier, working_dir, status, error)
			VALUES (:id, :run_id, :path, :step_id, :attempt_number, :executor_type, :agent_session_id, :task_id, :tmux_session, :agent_kind, :tier, :working_dir, :status, :error)`, attempt)
		return err
	})
}

// CompleteWorkflowStep records a step's success and moves the run to the
// step at nextPath, or ends it succeeded when nextPath is empty.
func (s *Store) CompleteWorkflowStep(ctx context.Context, runID, path, attemptID, outputsJSON, nextPath string) error {
	return s.completeWorkflowStep(ctx, runID, path, attemptID, outputsJSON, nextPath, false)
}

// RecordWorkflowStepCompletion is CompleteWorkflowStep for a completion an
// agent reported from its own pane: the step is also marked advance due,
// for a host process to finish (or keep) its agent and start the next step
// (ClaimWorkflowAdvances).
func (s *Store) RecordWorkflowStepCompletion(ctx context.Context, runID, path, attemptID, outputsJSON, nextPath string) error {
	return s.completeWorkflowStep(ctx, runID, path, attemptID, outputsJSON, nextPath, true)
}

func (s *Store) completeWorkflowStep(ctx context.Context, runID, path, attemptID, outputsJSON, nextPath string, advanceDue bool) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workflow_step_attempts SET status = 'succeeded', ended_at = datetime('now')
			WHERE id = ? AND run_id = ? AND path = ? AND status IN ('running','waiting_agent','waiting')`, attemptID, runID, path)
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
		result, err = tx.ExecContext(ctx, `UPDATE workflow_steps SET status = 'succeeded', outputs_json = ?, completed_at = datetime('now'), error = '',
			advance_due = ?, advance_claimed_at = NULL
			WHERE run_id = ? AND path = ? AND status IN ('running','waiting_agent','waiting')`, outputsJSON, advanceDue, runID, path)
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
		if nextPath == "" {
			result, err = tx.ExecContext(ctx, `UPDATE workflow_runs SET status = 'succeeded', current_step_id = '', completed_at = datetime('now'), error = '' WHERE id = ? AND current_step_id = ? AND status = 'running'`, runID, path)
		} else {
			result, err = tx.ExecContext(ctx, `UPDATE workflow_runs SET current_step_id = ?, error = '' WHERE id = ? AND current_step_id = ? AND status = 'running'`, nextPath, runID, path)
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
		if nextPath == "" {
			result, err = tx.ExecContext(ctx, `UPDATE tasks SET status = 'Done', archived_at = datetime('now'), updated_at = datetime('now') WHERE id = (SELECT task_id FROM workflow_runs WHERE id = ?)`, runID)
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

// MarkWorkflowStepWaiting ties a running step to the agent it waits on.
// taskID, when not empty, is the task the agent works on, which replaces the
// run's task on the attempt: an agent.run step on a card's own agent
// (LEG-540), so the attempt's agent is found on that card.
func (s *Store) MarkWorkflowStepWaiting(ctx context.Context, runID, path, attemptID string, session *AgentSession, workingDir, taskID string) error {
	var sessionID any
	var tmuxName, agentKind string
	if session != nil {
		sessionID, tmuxName, agentKind = session.ID, session.TmuxSession, session.AgentKind
	}
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workflow_step_attempts SET status = 'waiting_agent', agent_session_id = ?, tmux_session = ?, agent_kind = ?, working_dir = ?, task_id = COALESCE(NULLIF(?, ''), task_id) WHERE id = ? AND run_id = ? AND path = ? AND status = 'running'`, sessionID, tmuxName, agentKind, workingDir, taskID, attemptID, runID, path)
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
		result, err = tx.ExecContext(ctx, `UPDATE workflow_steps SET status = 'waiting_agent' WHERE run_id = ? AND path = ? AND status = 'running'`, runID, path)
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

// WaitWorkflowStepOnService parks a running step until the service that owns
// its executor completes or fails it (CompleteWorkflowStep,
// FailWorkflowStep). No agent session is tied to it, so no agent's exit ends
// it, and a restart leaves it waiting.
func (s *Store) WaitWorkflowStepOnService(ctx context.Context, runID, path, attemptID string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		for _, update := range []struct {
			query string
			args  []any
		}{
			{`UPDATE workflow_step_attempts SET status = 'waiting' WHERE id = ? AND run_id = ? AND path = ? AND status = 'running'`, []any{attemptID, runID, path}},
			{`UPDATE workflow_steps SET status = 'waiting' WHERE run_id = ? AND path = ? AND status = 'running'`, []any{runID, path}},
		} {
			result, err := tx.ExecContext(ctx, update.query, update.args...)
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
		}
		return nil
	})
}

// ListWorkflowAttemptsForRuns lists every attempt of runIDs, oldest first.
func (s *Store) ListWorkflowAttemptsForRuns(ctx context.Context, runIDs []string) ([]WorkflowStepAttempt, error) {
	attempts := []WorkflowStepAttempt{}
	if len(runIDs) == 0 {
		return attempts, nil
	}
	query, args, err := sqlx.In("SELECT "+workflowAttemptColumns+` FROM workflow_step_attempts
		WHERE run_id IN (?) ORDER BY started_at, id`, runIDs)
	if err != nil {
		return nil, err
	}
	err = s.db.SelectContext(ctx, &attempts, s.db.Rebind(query), args...)
	return attempts, err
}

// ListWaitingWorkflowAttemptsByExecutor lists the attempts of one executor
// type that wait on its service, oldest first.
func (s *Store) ListWaitingWorkflowAttemptsByExecutor(ctx context.Context, executorType string) ([]WorkflowStepAttempt, error) {
	attempts := []WorkflowStepAttempt{}
	err := s.db.SelectContext(ctx, &attempts, "SELECT "+workflowAttemptColumns+` FROM workflow_step_attempts
		WHERE executor_type = ? AND status = 'waiting' ORDER BY started_at, id`, executorType)
	return attempts, err
}

// WorkflowStepPaused is a step that failed under on_failure: pause: the
// run waits (status waiting) at a gate with Retry, Skip and Stop.
const WorkflowStepPaused = "paused"

// What a step's failure did (StepFailure, FailWorkflowStepWith).
const (
	FailureFailed    = "fail"
	FailureContinued = "continue"
	FailurePaused    = "pause"
)

// StepFailure is a step's failure: its text, and the code and link an
// executor's error may carry (such as review_loop_active, and the other
// loop's run). InProcess is set by a caller that moves the run on itself
// after a continued failure; otherwise a continued step is left due an
// advance, for a host to start the next step (ClaimWorkflowAdvances).
type StepFailure struct {
	Message   string
	Code      string
	Link      string
	InProcess bool
	// Retired is set for an attempt the operator retries (a suspended
	// step's): the step fails as on_failure: fail would, whatever it says,
	// since its retry resets it at once and a continue would move the run
	// past it.
	Retired bool
}

// FailureOutputs are the outputs a failed step has: status failed, error,
// error_code and, when there is one, error_link.
func FailureOutputs(failure StepFailure) map[string]any {
	outputs := map[string]any{"status": "failed", "error": failure.Message, "error_code": failure.Code}
	if failure.Link != "" {
		outputs["error_link"] = failure.Link
	}
	return outputs
}

// FailWorkflowStep fails a step's attempt with errorMessage, and does
// what the step's on_failure says (FailWorkflowStepWith).
func (s *Store) FailWorkflowStep(ctx context.Context, runID, path, attemptID, errorMessage string) error {
	_, err := s.FailWorkflowStepWith(ctx, runID, path, attemptID, StepFailure{Message: errorMessage})
	return err
}

// FailWorkflowStepWith fails a step's attempt, and with it the step, whose
// outputs become the failure: status failed, error, error_code and, when
// there is one, error_link. Then the step's on_failure (in the definition
// it keeps) decides, and the result says which it did: fail (the default)
// fails the run; continue moves the run to the next step in the step's
// scope (or ends it succeeded when there is none); pause leaves the step
// paused and the run waiting.
func (s *Store) FailWorkflowStepWith(ctx context.Context, runID, path, attemptID string, failure StepFailure) (string, error) {
	encoded, _ := json.Marshal(FailureOutputs(failure))
	mode := FailureFailed
	err := withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		var step struct {
			ParentPath     string `db:"parent_path"`
			StepOrder      int    `db:"step_order"`
			DefinitionJSON string `db:"definition_json"`
		}
		if err := tx.GetContext(ctx, &step, `SELECT parent_path, step_order, definition_json FROM workflow_steps WHERE run_id = ? AND path = ?`, runID, path); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrWorkflowStateConflict
			}
			return err
		}
		var definition struct {
			OnFailure string `json:"on_failure"`
		}
		_ = json.Unmarshal([]byte(step.DefinitionJSON), &definition)
		if !failure.Retired && (definition.OnFailure == FailureContinued || definition.OnFailure == FailurePaused) {
			mode = definition.OnFailure
		}
		if err := execOne(ctx, tx, `UPDATE workflow_step_attempts SET status = 'failed', error = ?, ended_at = datetime('now') WHERE id = ? AND run_id = ? AND path = ? AND status IN ('running','waiting_agent','waiting','suspended')`, failure.Message, attemptID, runID, path); err != nil {
			return err
		}
		stepStatus := "failed"
		if mode == FailurePaused {
			stepStatus = WorkflowStepPaused
		}
		advanceDue := mode == FailureContinued && !failure.InProcess
		if err := execOne(ctx, tx, `UPDATE workflow_steps SET status = ?, error = ?, outputs_json = ?, completed_at = datetime('now'), advance_due = ?, advance_claimed_at = NULL WHERE run_id = ? AND path = ? AND status IN ('running','waiting_agent','waiting','suspended')`, stepStatus, failure.Message, string(encoded), advanceDue, runID, path); err != nil {
			return err
		}
		switch mode {
		case FailurePaused:
			return execOne(ctx, tx, `UPDATE workflow_runs SET status = 'waiting', error = '' WHERE id = ? AND current_step_id = ? AND status IN ('running','suspended')`, runID, path)
		case FailureContinued:
			next, err := nextStepPath(ctx, tx, runID, step.ParentPath, step.StepOrder)
			if err != nil {
				return err
			}
			return moveRunPast(ctx, tx, runID, path, next, "'running','suspended'")
		}
		return execOne(ctx, tx, `UPDATE workflow_runs SET status = 'failed', error = ? WHERE id = ? AND current_step_id = ? AND status IN ('running','suspended')`, failure.Message, runID, path)
	})
	return mode, err
}

// SkipWorkflowStep skips a step: one whose if: is false, which the run
// reached (pending), or one paused after a failure, which the operator
// skips. Its outputs are missing. The run moves to nextPath, or ends
// succeeded when it is empty.
func (s *Store) SkipWorkflowStep(ctx context.Context, runID, path, nextPath string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		if err := execOne(ctx, tx, `UPDATE workflow_steps SET status = 'skipped', outputs_json = '{}', completed_at = datetime('now') WHERE run_id = ? AND path = ? AND status IN ('pending', 'paused')`, runID, path); err != nil {
			return err
		}
		return moveRunPast(ctx, tx, runID, path, nextPath, "'running','waiting'")
	})
}

// nextStepPath is the path of the step after the one at order in its
// scope, or "" for the last.
func nextStepPath(ctx context.Context, tx *sqlx.Tx, runID, parent string, order int) (string, error) {
	var next string
	err := tx.GetContext(ctx, &next, `SELECT path FROM workflow_steps WHERE run_id = ? AND parent_path = ? AND step_order > ? ORDER BY step_order LIMIT 1`, runID, parent, order)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return next, err
}

// moveRunPast moves a run at path, in one of statuses, on to nextPath, or
// ends it succeeded (its task done) when nextPath is empty.
func moveRunPast(ctx context.Context, tx *sqlx.Tx, runID, path, nextPath, statuses string) error {
	if nextPath != "" {
		return execOne(ctx, tx, `UPDATE workflow_runs SET status = 'running', current_step_id = ?, error = '' WHERE id = ? AND current_step_id = ? AND status IN (`+statuses+`)`, nextPath, runID, path)
	}
	if err := execOne(ctx, tx, `UPDATE workflow_runs SET status = 'succeeded', current_step_id = '', completed_at = datetime('now'), error = '' WHERE id = ? AND current_step_id = ? AND status IN (`+statuses+`)`, runID, path); err != nil {
		return err
	}
	return execOne(ctx, tx, `UPDATE tasks SET status = 'Done', archived_at = datetime('now'), updated_at = datetime('now') WHERE id = (SELECT task_id FROM workflow_runs WHERE id = ?)`, runID)
}

// execOne runs an update that must change exactly one row:
// ErrWorkflowStateConflict otherwise.
func execOne(ctx context.Context, tx *sqlx.Tx, query string, args ...any) error {
	result, err := tx.ExecContext(ctx, query, args...)
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

func (s *Store) ResetWorkflowStepForRetry(ctx context.Context, runID, path string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workflow_steps SET status = 'pending', error = '', outputs_json = '{}', completed_at = NULL WHERE run_id = ? AND path = ? AND status IN ('failed','interrupted','paused')`, runID, path)
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
		// A failed run, or one waiting at this step paused after a failure.
		result, err = tx.ExecContext(ctx, `UPDATE workflow_runs SET status = 'running', current_step_id = ?, error = '', completed_at = NULL WHERE id = ? AND (status = 'failed' OR (status = 'waiting' AND current_step_id = ?))`, path, runID, path)
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
	if err := s.db.GetContext(ctx, &artifact, "SELECT "+workflowArtifactColumns+" FROM workflow_artifacts WHERE attempt_id = ? AND output_key = ?", attemptID, outputKey); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &artifact, nil
}

func (s *Store) GetWorkflowArtifact(ctx context.Context, id string) (*WorkflowArtifact, error) {
	var artifact WorkflowArtifact
	if err := s.db.GetContext(ctx, &artifact, "SELECT "+workflowArtifactColumns+" FROM workflow_artifacts WHERE id = ?", id); err != nil {
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
	if artifact.Path == "" {
		artifact.Path = artifact.StepID
	}
	_, err := s.db.NamedExecContext(ctx, `INSERT INTO workflow_artifacts
		(id, run_id, path, step_id, attempt_id, task_id, output_key, managed_path, original_path, media_type, size_bytes, digest)
		VALUES (:id, :run_id, :path, :step_id, :attempt_id, :task_id, :output_key, :managed_path, :original_path, :media_type, :size_bytes, :digest)`, artifact)
	return err
}

func (s *Store) ListWorkflowRunsByRelatedTask(ctx context.Context, taskID string) ([]WorkflowRun, error) {
	var runs []WorkflowRun
	err := s.db.SelectContext(ctx, &runs, workflowRunSelect+" WHERE related_task_id = ? ORDER BY created_at DESC", taskID)
	return runs, err
}

type WorkflowRunFilter struct {
	Status       string
	WorkflowName string
	Inputs       map[string]string
	Limit        int
	// WithSteps fills each run's Steps. The runs list asks for it; callers
	// that only count or scan runs do not.
	WithSteps bool
}

func (s *Store) ListWorkflowRuns(ctx context.Context, filter WorkflowRunFilter) ([]WorkflowRun, error) {
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	var runs []WorkflowRun
	query := workflowRunSelect
	args := []any{}
	conditions := []string{}
	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.WorkflowName != "" {
		conditions = append(conditions, "workflow_name = ?")
		args = append(args, filter.WorkflowName)
	}
	keys := make([]string, 0, len(filter.Inputs))
	for key := range filter.Inputs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		conditions = append(conditions, `CASE json_type(inputs_json, ?)
			WHEN 'true' THEN 'true'
			WHEN 'false' THEN 'false'
			ELSE CAST(json_extract(inputs_json, ?) AS TEXT)
		END = ?`)
		path := "$." + key
		args = append(args, path, path, filter.Inputs[key])
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, filter.Limit)
	if err := s.db.SelectContext(ctx, &runs, query, args...); err != nil {
		return nil, err
	}
	if filter.WithSteps {
		if err := s.attachStepSummaries(ctx, runs); err != nil {
			return nil, err
		}
	}
	return runs, nil
}

// WorkflowAttemptInUse reports whether an attempt still runs, or the agent
// it started still does (a step that kept its agent): what a sweep of its
// scratch folder must leave alone (LEG-530).
func (s *Store) WorkflowAttemptInUse(ctx context.Context, attemptID string) (bool, error) {
	var used bool
	err := s.db.GetContext(ctx, &used, `SELECT EXISTS (
		SELECT 1 FROM workflow_step_attempts a
		LEFT JOIN agent_sessions s ON s.id = a.agent_session_id
		WHERE a.id = ? AND (a.status IN ('running', 'waiting_agent', 'waiting') OR s.status = 'running'))`, attemptID)
	return used, err
}

// ListSucceededAgentStepsWithoutArtifacts returns the agent.run and
// agent.dispatch steps of runIDs that succeeded and kept no artifact, for
// the caller to check their outputs (LEG-532).
func (s *Store) ListSucceededAgentStepsWithoutArtifacts(ctx context.Context, runIDs []string) ([]WorkflowStep, error) {
	if len(runIDs) == 0 {
		return nil, nil
	}
	query, args, err := sqlx.In(`SELECT run_id, path, step_id, title, executor_type, resolved_json, outputs_json, status
		FROM workflow_steps s
		WHERE run_id IN (?) AND status = 'succeeded' AND executor_type IN ('agent.run', 'agent.dispatch')
			AND NOT EXISTS (SELECT 1 FROM workflow_artifacts a WHERE a.run_id = s.run_id AND a.path = s.path)
		ORDER BY run_id, step_order, path`, runIDs)
	if err != nil {
		return nil, err
	}
	var steps []WorkflowStep
	err = s.db.SelectContext(ctx, &steps, s.db.Rebind(query), args...)
	return steps, err
}

func (s *Store) attachStepSummaries(ctx context.Context, runs []WorkflowRun) error {
	if len(runs) == 0 {
		return nil
	}
	ids := make([]string, len(runs))
	index := make(map[string]int, len(runs))
	for i, run := range runs {
		ids[i] = run.ID
		index[run.ID] = i
	}
	// The runs list shows each run's top-level steps. A step's definition
	// says start; one kept before schema version 2 says trigger.
	query, args, err := sqlx.In(`SELECT run_id, step_id, title, status,
		COALESCE(json_extract(definition_json, '$.start'), json_extract(definition_json, '$.trigger'), '') AS step_trigger
		FROM workflow_steps WHERE run_id IN (?) AND parent_path = '' ORDER BY run_id, step_order`, ids)
	if err != nil {
		return err
	}
	var rows []struct {
		RunID string `db:"run_id"`
		WorkflowStepSummary
	}
	if err := s.db.SelectContext(ctx, &rows, s.db.Rebind(query), args...); err != nil {
		return err
	}
	for _, row := range rows {
		i := index[row.RunID]
		runs[i].Steps = append(runs[i].Steps, row.WorkflowStepSummary)
	}
	return nil
}

// CancelWorkflowRun cancels a run that has not ended, or one that failed:
// a failed run keeps what it holds (a review loop and its claims) for
// Retry, and Cancel is how the operator gives it up. A failed step keeps
// its error; ErrWorkflowStateConflict for a run that succeeded, was
// stopped or was cancelled.
func (s *Store) CancelWorkflowRun(ctx context.Context, runID, reason string) error {
	return s.endWorkflowRun(ctx, runID, "cancelled", reason)
}

// StopWorkflowRun ends a running run as stopped, its unfinished steps and
// attempts cancelled, as CancelWorkflowRun does: what a gate's Stop does
// (LEG-486).
func (s *Store) StopWorkflowRun(ctx context.Context, runID, reason string) error {
	return s.endWorkflowRun(ctx, runID, "stopped", reason)
}

// endWorkflowRun ends a running, waiting or suspended run with status
// (cancelled or stopped), or a failed one with cancelled, and cancels its
// unfinished steps and attempts.
func (s *Store) endWorkflowRun(ctx context.Context, runID, status, reason string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		// Only Cancel gives up a failed run; Stop is a gate's, on a run
		// that has not ended.
		result, err := tx.ExecContext(ctx, `UPDATE workflow_runs SET status = ?, error = ?, completed_at = datetime('now') WHERE id = ? AND (status IN ('running','waiting','suspended') OR (? = 'cancelled' AND status = 'failed'))`, status, reason, runID, status)
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
		if _, err := tx.ExecContext(ctx, `UPDATE workflow_steps SET status = 'cancelled', error = ?, completed_at = datetime('now') WHERE run_id = ? AND status IN ('pending','running','waiting_agent','waiting_manual','waiting','suspended','paused')`, reason, runID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE workflow_step_attempts SET status = 'cancelled', error = ?, ended_at = datetime('now') WHERE run_id = ? AND status IN ('running','waiting_agent','waiting','suspended')`, reason, runID)
		return err
	})
}

// WorkflowAdvance is a recorded step whose run a host process must move
// on: the step, and the attempt that completed it.
type WorkflowAdvance struct {
	Step    WorkflowStep
	Attempt WorkflowStepAttempt
}

// ClaimWorkflowAdvances claims the steps whose advance is due (or, with
// runID and path, that one step only): each one no other process holds,
// or whose claim is older than stale, which a crashed process left. A
// claimed step stays due until FinishWorkflowAdvance, so a claim lost to
// a crash is taken again.
func (s *Store) ClaimWorkflowAdvances(ctx context.Context, runID, path string, stale time.Duration) ([]WorkflowAdvance, error) {
	query := "SELECT " + workflowStepColumns + " FROM workflow_steps WHERE advance_due = 1"
	args := []any{}
	if runID != "" {
		query += " AND run_id = ? AND path = ?"
		args = append(args, runID, path)
	}
	var steps []WorkflowStep
	if err := s.db.SelectContext(ctx, &steps, query+" ORDER BY completed_at, run_id, path", args...); err != nil {
		return nil, err
	}
	// A fixed width, so the times compare as text.
	const claimTime = "2006-01-02T15:04:05.000000000Z"
	cutoff := time.Now().Add(-stale).UTC().Format(claimTime)
	now := time.Now().UTC().Format(claimTime)
	var claimed []WorkflowAdvance
	for _, step := range steps {
		result, err := s.db.ExecContext(ctx, `UPDATE workflow_steps SET advance_claimed_at = ?
			WHERE run_id = ? AND path = ? AND advance_due = 1 AND (advance_claimed_at IS NULL OR advance_claimed_at < ?)`,
			now, step.RunID, step.Path, cutoff)
		if err != nil {
			return claimed, err
		}
		if rows, _ := result.RowsAffected(); rows != 1 {
			continue
		}
		var attempt WorkflowStepAttempt
		if err := s.db.GetContext(ctx, &attempt, "SELECT "+workflowAttemptColumns+` FROM workflow_step_attempts
			WHERE run_id = ? AND path = ? AND attempt_number = ?`, step.RunID, step.Path, step.LatestAttempt); err != nil {
			return claimed, fmt.Errorf("workflow step %s/%s attempt %d: %w", step.RunID, step.Path, step.LatestAttempt, err)
		}
		claimed = append(claimed, WorkflowAdvance{Step: step, Attempt: attempt})
	}
	return claimed, nil
}

// FinishWorkflowAdvance clears a claimed step's advance: its run has moved on.
func (s *Store) FinishWorkflowAdvance(ctx context.Context, runID, path string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE workflow_steps SET advance_due = 0, advance_claimed_at = NULL WHERE run_id = ? AND path = ?`, runID, path)
	return err
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

// TasksWithMetadataText lists the tasks whose metadata has text at path
// (dotted keys), newest first.
func (s *Store) TasksWithMetadataText(ctx context.Context, path []string, value string) ([]string, error) {
	ids := []string{}
	err := s.db.SelectContext(ctx, &ids, `SELECT id FROM tasks WHERE json_extract(metadata, ?) = ? ORDER BY updated_at DESC, id DESC`,
		"$."+strings.Join(path, "."), value)
	return ids, err
}
