package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

// WorkflowSuspended is the status of an agent.run attempt, its step and its
// run while the agent is interrupted: Legato stopped with it, and its tmux
// session was gone when Legato started again. The step is still open:
// resuming the agent puts it back to waiting_agent on the same attempt.
const WorkflowSuspended = "suspended"

// SuspendWorkflowStep suspends an agent step that waits on its agent: the
// attempt, the step and the run, in one transaction. The attempt keeps no
// end time, since it goes on when its agent is resumed.
// ErrWorkflowStateConflict when the step no longer waits on its agent.
func (s *Store) SuspendWorkflowStep(ctx context.Context, runID, path, attemptID, reason string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		return execOneRowEach(ctx, tx,
			oneRowStmt{`UPDATE workflow_step_attempts SET status = 'suspended', error = ? WHERE id = ? AND run_id = ? AND path = ? AND status = 'waiting_agent'`, []any{reason, attemptID, runID, path}},
			oneRowStmt{`UPDATE workflow_steps SET status = 'suspended', error = ? WHERE run_id = ? AND path = ? AND status = 'waiting_agent'`, []any{reason, runID, path}},
			oneRowStmt{`UPDATE workflow_runs SET status = 'suspended', error = ? WHERE id = ? AND current_step_id = ? AND status = 'running'`, []any{reason, runID, path}},
		)
	})
}

// ResumeWorkflowStep puts a suspended agent step back to waiting on its
// agent, now session (the resumed agent's), and its run back to running.
func (s *Store) ResumeWorkflowStep(ctx context.Context, runID, path, attemptID string, session AgentSession) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		return execOneRowEach(ctx, tx,
			oneRowStmt{`UPDATE workflow_step_attempts SET status = 'waiting_agent', error = '', agent_session_id = ?, tmux_session = ?, agent_kind = ? WHERE id = ? AND run_id = ? AND path = ? AND status = 'suspended'`, []any{session.ID, session.TmuxSession, session.AgentKind, attemptID, runID, path}},
			oneRowStmt{`UPDATE workflow_steps SET status = 'waiting_agent', error = '' WHERE run_id = ? AND path = ? AND status = 'suspended'`, []any{runID, path}},
			oneRowStmt{`UPDATE workflow_runs SET status = 'running', error = '' WHERE id = ? AND current_step_id = ? AND status = 'suspended'`, []any{runID, path}},
		)
	})
}

// ReopenWorkflowStepSuspended suspends a failed agent step again: one an
// earlier Legato failed when its agent was interrupted, before steps were
// suspended. Its end times are cleared.
func (s *Store) ReopenWorkflowStepSuspended(ctx context.Context, runID, path, attemptID, reason string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		return execOneRowEach(ctx, tx,
			oneRowStmt{`UPDATE workflow_step_attempts SET status = 'suspended', error = ?, ended_at = NULL WHERE id = ? AND run_id = ? AND path = ? AND status = 'failed'`, []any{reason, attemptID, runID, path}},
			oneRowStmt{`UPDATE workflow_steps SET status = 'suspended', error = ?, completed_at = NULL WHERE run_id = ? AND path = ? AND status = 'failed' AND latest_attempt = (SELECT attempt_number FROM workflow_step_attempts WHERE id = ?)`, []any{reason, runID, path, attemptID}},
			oneRowStmt{`UPDATE workflow_runs SET status = 'suspended', error = ?, completed_at = NULL WHERE id = ? AND current_step_id = ? AND status = 'failed'`, []any{reason, runID, path}},
		)
	})
}

// ListSuspendedWorkflowAttemptsByAgentSession lists the suspended attempts
// that wait on an agent session.
func (s *Store) ListSuspendedWorkflowAttemptsByAgentSession(ctx context.Context, sessionID int) ([]WorkflowStepAttempt, error) {
	attempts := []WorkflowStepAttempt{}
	err := s.db.SelectContext(ctx, &attempts, "SELECT "+workflowAttemptColumns+` FROM workflow_step_attempts
		WHERE agent_session_id = ? AND status = 'suspended' ORDER BY started_at, id`, sessionID)
	return attempts, err
}

type oneRowStmt struct {
	query string
	args  []any
}

// execOneRowEach runs each statement in turn, each of which must change exactly
// one row: ErrWorkflowStateConflict otherwise.
func execOneRowEach(ctx context.Context, tx *sqlx.Tx, stmts ...oneRowStmt) error {
	for _, st := range stmts {
		result, err := tx.ExecContext(ctx, st.query, st.args...)
		if err != nil {
			return err
		}
		if err := oneRowChanged(result); err != nil {
			return err
		}
	}
	return nil
}

func oneRowChanged(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrWorkflowStateConflict
	}
	return nil
}
