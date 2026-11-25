package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

// The wait record of an agent step (LEG-485, spec LEG-482 §5.3): the
// signals that end it, the agent place it watches, the stage event
// watermark and the session it is bound to, kept on its workflow_steps row
// so a host finds every wait again from the tables after a restart.

// WorkStageSourceWorkflow: a workflow step reset the place before it sent
// its prompt, so only a Finished reported after it counts.
const WorkStageSourceWorkflow = "workflow"

// WorkflowWait is a wait an agent step begins.
type WorkflowWait struct {
	// Signals are the wait_for signals, any one of which ends the step.
	Signals []string
	// Place is the agent place the wait watches, when known before the
	// prompt is sent; empty for an agent not started yet (a new
	// execution's place has no events, so its watermark is 0).
	Place WorkStagePlace
	// Reset puts the place back to Implementing (source workflow), in the
	// same write as the watermark.
	Reset bool
	// Reason is the reset's reason.
	Reason string
}

// workflowDeadlineFormat is fixed width, so deadlines compare as text.
const workflowDeadlineFormat = "2006-01-02T15:04:05.000000000Z"

// BeginWorkflowWait writes a running step's wait record and returns its
// watermark: the newest stage event of the place, in one transaction with
// the place's reset. It is written before the step's prompt is sent.
func (s *Store) BeginWorkflowWait(ctx context.Context, runID, path string, wait WorkflowWait) (int64, error) {
	signals, err := json.Marshal(wait.Signals)
	if err != nil {
		return 0, err
	}
	place := wait.Place
	if place.TaskID != "" {
		place = place.normal()
	}
	var watermark int64
	err = withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		if place.TaskID != "" {
			if err := tx.GetContext(ctx, &watermark, `SELECT COALESCE(MAX(id), 0) FROM agent_stage_events
				WHERE task_id = ? AND slot = ? AND place_execution_id = ?`, place.TaskID, place.Slot, place.PlaceExecutionID); err != nil {
				return err
			}
			if wait.Reset {
				if err := setWorkStageTx(ctx, tx, WorkStage{WorkStagePlace: place, Stage: WorkStageImplementing, Reason: wait.Reason, Source: WorkStageSourceWorkflow}); err != nil {
					return err
				}
			}
		}
		result, err := tx.ExecContext(ctx, `UPDATE workflow_steps SET wait_signals = ?, wait_task_id = ?, wait_slot = ?, wait_execution_id = ?,
			wait_watermark = ?, wait_session_id = NULL WHERE run_id = ? AND path = ? AND status = 'running'`,
			string(signals), place.TaskID, place.Slot, place.PlaceExecutionID, watermark, runID, path)
		if err != nil {
			return err
		}
		if rows, err := result.RowsAffected(); err != nil {
			return err
		} else if rows != 1 {
			return ErrWorkflowStateConflict
		}
		return nil
	})
	return watermark, err
}

// BindWorkflowWait ties a step's wait to the agent it watches: its place
// and its session, on the step and on the attempt, with the folder the
// session works in (kept on the attempt, since an ended session is
// deleted). It is how a wait that began before its agent started, or
// whose agent was resumed, finds it.
func (s *Store) BindWorkflowWait(ctx context.Context, runID, path, attemptID string, place WorkStagePlace, sessionID int) error {
	place = place.normal()
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workflow_steps SET wait_task_id = ?, wait_slot = ?, wait_execution_id = ?, wait_session_id = ?
			WHERE run_id = ? AND path = ? AND status IN ('running', 'waiting_agent')`,
			place.TaskID, place.Slot, place.PlaceExecutionID, sessionID, runID, path)
		if err != nil {
			return err
		}
		if rows, err := result.RowsAffected(); err != nil {
			return err
		} else if rows != 1 {
			return ErrWorkflowStateConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE workflow_step_attempts SET agent_session_id = ?,
			working_dir = COALESCE(NULLIF((SELECT working_dir FROM executions WHERE agent_session_id = ? ORDER BY started_at DESC LIMIT 1), ''), working_dir)
			WHERE id = ? AND status IN ('running', 'waiting_agent')`, sessionID, sessionID, attemptID)
		return err
	})
}

// WorkflowStepWaitSignals are the signals a step waits for; none for a
// step that began no wait (one started before waits were recorded).
func (s *Store) WorkflowStepWaitSignals(ctx context.Context, runID, path string) ([]string, error) {
	var raw string
	err := s.db.GetContext(ctx, &raw, `SELECT wait_signals FROM workflow_steps WHERE run_id = ? AND path = ?`, runID, path)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var signals []string
	if err := json.Unmarshal([]byte(raw), &signals); err != nil {
		return nil, err
	}
	return signals, nil
}

// ListWorkflowAgentWaits lists the steps that wait on an agent with a wait
// record, oldest first: what a host checks for signals.
func (s *Store) ListWorkflowAgentWaits(ctx context.Context) ([]WorkflowStep, error) {
	steps := []WorkflowStep{}
	err := s.db.SelectContext(ctx, &steps, "SELECT "+workflowStepColumns+` FROM workflow_steps
		WHERE status = 'waiting_agent' AND wait_signals NOT IN ('', '[]') ORDER BY started_at, run_id, path`)
	return steps, err
}

// WorkStageEventAbove is the newest event of a place above an event ID
// that reached stage, or nil.
func (s *Store) WorkStageEventAbove(ctx context.Context, place WorkStagePlace, after int64, stage string) (*WorkStageEvent, error) {
	place = place.normal()
	var event WorkStageEvent
	err := s.db.GetContext(ctx, &event, `SELECT id, task_id, slot, place_execution_id, from_stage, stage, reason, source, execution_id, created_at
		FROM agent_stage_events WHERE task_id = ? AND slot = ? AND place_execution_id = ? AND id > ? AND stage = ?
		ORDER BY id DESC LIMIT 1`, place.TaskID, place.Slot, place.PlaceExecutionID, after, stage)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// SetWorkflowStepDeadline gives a step its timeout's deadline: absolute, so
// time Legato is down counts.
func (s *Store) SetWorkflowStepDeadline(ctx context.Context, runID, path string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE workflow_steps SET deadline_at = ? WHERE run_id = ? AND path = ?`,
		at.UTC().Format(workflowDeadlineFormat), runID, path)
	return err
}

// ListWorkflowStepsPastDeadline lists the steps under way whose deadline
// is at or before now.
func (s *Store) ListWorkflowStepsPastDeadline(ctx context.Context, now time.Time) ([]WorkflowStep, error) {
	steps := []WorkflowStep{}
	err := s.db.SelectContext(ctx, &steps, "SELECT "+workflowStepColumns+` FROM workflow_steps
		WHERE deadline_at IS NOT NULL AND deadline_at <= ? AND status IN ('running', 'waiting_agent', 'waiting')
		ORDER BY deadline_at, run_id, path`, now.UTC().Format(workflowDeadlineFormat))
	return steps, err
}

// ClaimWorkflowStepTimeout claims a step's timeout: it moves the deadline
// it was read with to until, so of the hosts that sweep it, one times it
// out. The claim goes stale: a host that stops, or fails to stop the
// step's agent, before the step's failure is recorded leaves the step
// past its deadline again at until, for a later sweep to take. Recording
// the failure ends the step, which no sweep lists after.
func (s *Store) ClaimWorkflowStepTimeout(ctx context.Context, runID, path, deadline string, until time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE workflow_steps SET deadline_at = ?
		WHERE run_id = ? AND path = ? AND deadline_at = ? AND status IN ('running', 'waiting_agent', 'waiting')`,
		until.UTC().Format(workflowDeadlineFormat), runID, path, deadline)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

// MergeWorkflowStepOutputs sets a recorded step's outputs: what a host
// fills in for an agent's completion before it advances the run.
func (s *Store) MergeWorkflowStepOutputs(ctx context.Context, runID, path, outputsJSON string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE workflow_steps SET outputs_json = ? WHERE run_id = ? AND path = ?`, outputsJSON, runID, path)
	return err
}

// FailRecordedWorkflowStep fails a step recorded as completed whose run has
// not moved on yet (advance due, the run at the step after it, which has
// not started): the host could not fill the outputs Legato owes it. The
// attempt and the step fail as FailWorkflowStepWith fails them, and the
// step's on_failure decides: fail sends the run back to the step, failed;
// pause sends it back waiting; continue leaves it at the next step, with
// the step still due its advance (the caller holds that claim and goes on).
// Each update is a compare-and-swap, so of two hosts one fails the step and
// the other gets ErrWorkflowStateConflict, as does a run that has ended.
func (s *Store) FailRecordedWorkflowStep(ctx context.Context, runID, path, attemptID string, failure StepFailure) (string, error) {
	encoded, _ := json.Marshal(FailureOutputs(failure))
	mode := FailureFailed
	err := withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		var step struct {
			ParentPath     string `db:"parent_path"`
			StepOrder      int    `db:"step_order"`
			DefinitionJSON string `db:"definition_json"`
		}
		if err := tx.GetContext(ctx, &step, `SELECT parent_path, step_order, definition_json FROM workflow_steps
			WHERE run_id = ? AND path = ? AND status = 'succeeded' AND advance_due = 1`, runID, path); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrWorkflowStateConflict
			}
			return err
		}
		var definition struct {
			OnFailure string `json:"on_failure"`
		}
		_ = json.Unmarshal([]byte(step.DefinitionJSON), &definition)
		if definition.OnFailure == FailureContinued || definition.OnFailure == FailurePaused {
			mode = definition.OnFailure
		}
		next, err := nextStepPath(ctx, tx, runID, step.ParentPath, step.StepOrder)
		if err != nil {
			return err
		}
		if next == "" {
			// The record ended the run: there is nothing to send it back to.
			return ErrWorkflowStateConflict
		}
		if err := execOne(ctx, tx, `UPDATE workflow_step_attempts SET status = 'failed', error = ?, ended_at = datetime('now')
			WHERE id = ? AND run_id = ? AND path = ? AND status = 'succeeded'`, failure.Message, attemptID, runID, path); err != nil {
			return err
		}
		stepStatus := "failed"
		if mode == FailurePaused {
			stepStatus = WorkflowStepPaused
		}
		// A continued step stays due and claimed: its caller moves the run on.
		if err := execOne(ctx, tx, `UPDATE workflow_steps SET status = ?, error = ?, outputs_json = ?, completed_at = datetime('now'),
			advance_due = ?, advance_claimed_at = CASE WHEN ? THEN advance_claimed_at END
			WHERE run_id = ? AND path = ? AND status = 'succeeded' AND advance_due = 1`,
			stepStatus, failure.Message, string(encoded), mode == FailureContinued, mode == FailureContinued, runID, path); err != nil {
			return err
		}
		atNext := ` WHERE id = ? AND current_step_id = ? AND status = 'running'
			AND EXISTS (SELECT 1 FROM workflow_steps WHERE run_id = ? AND path = ? AND status = 'pending')`
		switch mode {
		case FailureContinued:
			return execOne(ctx, tx, `UPDATE workflow_runs SET error = ''`+atNext, runID, next, runID, next)
		case FailurePaused:
			return execOne(ctx, tx, `UPDATE workflow_runs SET status = 'waiting', current_step_id = ?, error = ''`+atNext, path, runID, next, runID, next)
		}
		return execOne(ctx, tx, `UPDATE workflow_runs SET status = 'failed', current_step_id = ?, error = ?`+atNext, path, failure.Message, runID, next, runID, next)
	})
	return mode, err
}
