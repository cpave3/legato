package store

import (
	"context"
	"database/sql"
	"fmt"
)

const agentSessionSelect = `SELECT a.*,
	COALESCE(e.id, '') AS execution_id,
	COALESCE(e.task_id, '') AS attribution_task_id,
	COALESCE(e.kind, '') AS execution_kind,
	COALESCE(e.workflow_run_id, '') AS workflow_run_id
	FROM agent_sessions a
	LEFT JOIN executions e ON e.agent_session_id = a.id`

func (s *Store) InsertAgentSession(ctx context.Context, a AgentSession) error {
	result, err := s.db.NamedExecContext(ctx, `
		INSERT INTO agent_sessions (task_id, slot, tmux_session, command, agent_kind, status, role, parent_task_id, subtask_id, sandboxed)
		SELECT :task_id, CASE WHEN :slot = '' THEN 'main' ELSE :slot END, :tmux_session, :command, :agent_kind, :status, :role, :parent_task_id, :subtask_id, :sandboxed
		WHERE :status != 'running' OR NOT EXISTS (SELECT 1 FROM chimera_cleanup_reservations WHERE task_id = :task_id)`, a)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrConflict
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	a.ID = int(id)
	return nil
}

// InsertAgentSessionReturningID inserts a session and returns the new row's ID.
func (s *Store) InsertAgentSessionReturningID(ctx context.Context, a AgentSession) (int, error) {
	result, err := s.db.NamedExecContext(ctx, `
		INSERT INTO agent_sessions (task_id, slot, tmux_session, command, agent_kind, status, role, parent_task_id, subtask_id, sandboxed)
		SELECT :task_id, CASE WHEN :slot = '' THEN 'main' ELSE :slot END, :tmux_session, :command, :agent_kind, :status, :role, :parent_task_id, :subtask_id, :sandboxed
		WHERE :status != 'running' OR NOT EXISTS (SELECT 1 FROM chimera_cleanup_reservations WHERE task_id = :task_id)`, a)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rows == 0 {
		return 0, ErrConflict
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(id), nil
}

func (s *Store) ListAgentSessions(ctx context.Context) ([]AgentSession, error) {
	var sessions []AgentSession
	err := s.db.SelectContext(ctx, &sessions,
		agentSessionSelect+" WHERE a.status = 'running' ORDER BY a.started_at DESC")
	return sessions, err
}

// GetAgentSessionByTaskID returns the running session of the task's main
// slot: the task's agent, to anything that addresses it without a slot.
func (s *Store) GetAgentSessionByTaskID(ctx context.Context, taskID string) (*AgentSession, error) {
	return s.GetAgentSessionInSlot(ctx, taskID, SlotMain)
}

// GetAgentSessionInSlot returns the running session of one of the task's slots.
func (s *Store) GetAgentSessionInSlot(ctx context.Context, taskID, slot string) (*AgentSession, error) {
	var a AgentSession
	err := s.db.GetContext(ctx, &a,
		agentSessionSelect+" WHERE a.task_id = ? AND a.slot = ? AND a.status = 'running' ORDER BY a.started_at DESC LIMIT 1", taskID, slotOrMain(slot))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent session for task %s, slot %s: %w", taskID, slotOrMain(slot), ErrNotFound)
	}
	return &a, err
}

// GetRunningAgentSession returns the running session a caller inside it
// names: the one of executionID on the task, in whichever slot, or the main
// slot's when executionID is empty.
func (s *Store) GetRunningAgentSession(ctx context.Context, taskID, executionID string) (*AgentSession, error) {
	if executionID == "" {
		return s.GetAgentSessionByTaskID(ctx, taskID)
	}
	var a AgentSession
	err := s.db.GetContext(ctx, &a,
		agentSessionSelect+" WHERE a.task_id = ? AND e.id = ? AND a.status = 'running' LIMIT 1", taskID, executionID)
	if err == sql.ErrNoRows {
		// A session whose execution is not linked yet, or a legacy one: the
		// main slot's, which the caller checks against executionID.
		return s.GetAgentSessionByTaskID(ctx, taskID)
	}
	return &a, err
}

// ListTaskAgentSessions returns the running sessions of every slot of a task,
// the main slot's first.
func (s *Store) ListTaskAgentSessions(ctx context.Context, taskID string) ([]AgentSession, error) {
	var sessions []AgentSession
	err := s.db.SelectContext(ctx, &sessions,
		agentSessionSelect+" WHERE a.task_id = ? AND a.status = 'running' ORDER BY a.slot != 'main', a.slot, a.started_at DESC", taskID)
	return sessions, err
}

func (s *Store) GetAgentSessionByID(ctx context.Context, id int) (*AgentSession, error) {
	var a AgentSession
	err := s.db.GetContext(ctx, &a, agentSessionSelect+" WHERE a.id = ?", id)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return &a, err
}

func (s *Store) GetAgentSessionByTmuxName(ctx context.Context, tmuxSession string) (*AgentSession, error) {
	var a AgentSession
	err := s.db.GetContext(ctx, &a,
		agentSessionSelect+" WHERE a.tmux_session = ?", tmuxSession)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent session %s: %w", tmuxSession, ErrNotFound)
	}
	return &a, err
}

// UpdateAgentSessionStatus ends the task's main slot's running session.
func (s *Store) UpdateAgentSessionStatus(ctx context.Context, taskID string, status string) error {
	return s.UpdateAgentSessionStatusInSlot(ctx, taskID, SlotMain, status)
}

// UpdateAgentSessionStatusInSlot ends one slot's running session.
func (s *Store) UpdateAgentSessionStatusInSlot(ctx context.Context, taskID, slot, status string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE agent_sessions SET status = ?, ended_at = datetime('now')
		WHERE task_id = ? AND slot = ? AND status = 'running'`, status, taskID, slotOrMain(slot))
	return err
}

// UpdateAgentSessionSwarmRole updates swarm metadata on the currently running
// session for taskID. Use nil subtaskID for conductors.
func (s *Store) UpdateAgentSessionSwarmRole(ctx context.Context, taskID, role, parentTaskID string, subtaskID *string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE agent_sessions
		SET role = ?, parent_task_id = ?, subtask_id = ?
		WHERE task_id = ? AND slot = 'main' AND status = 'running'`,
		role, parentTaskID, subtaskID, taskID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateAgentActivity sets the activity of the task's main slot's session.
func (s *Store) UpdateAgentActivity(ctx context.Context, taskID string, activity string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE agent_sessions SET activity = ?
		WHERE task_id = ? AND slot = 'main' AND status = 'running'`, activity, taskID)
	return err
}

// UpdateAgentActivityForSession sets the activity of one running session.
func (s *Store) UpdateAgentActivityForSession(ctx context.Context, id int, activity string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE agent_sessions SET activity = ? WHERE id = ? AND status = 'running'`, activity, id)
	return err
}

// GetAgentActivityCounts returns the count of running agent sessions grouped by activity state.
// If excludeTaskID is non-empty, that task's session is excluded from counts.
func (s *Store) GetAgentActivityCounts(ctx context.Context, excludeTaskID string) (working, waiting, errors, idle int, err error) {
	type row struct {
		Activity string `db:"activity"`
		Count    int    `db:"cnt"`
	}

	var query string
	var rows []row

	if excludeTaskID != "" {
		query = `SELECT activity, COUNT(*) AS cnt FROM agent_sessions WHERE status = 'running' AND task_id != ? GROUP BY activity`
		err = s.db.SelectContext(ctx, &rows, query, excludeTaskID)
	} else {
		query = `SELECT activity, COUNT(*) AS cnt FROM agent_sessions WHERE status = 'running' GROUP BY activity`
		err = s.db.SelectContext(ctx, &rows, query)
	}
	if err != nil {
		return 0, 0, 0, 0, err
	}

	for _, r := range rows {
		switch r.Activity {
		case "working":
			working = r.Count
		case "waiting":
			waiting = r.Count
		case "error":
			errors = r.Count
		default:
			idle += r.Count
		}
	}
	return working, waiting, errors, idle, nil
}

func (s *Store) FinishAgentSession(ctx context.Context, id int) error {
	result, err := s.db.ExecContext(ctx, `UPDATE agent_sessions SET status = 'dead', ended_at = datetime('now') WHERE id = ? AND status = 'running'`, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) DeleteAgentSession(ctx context.Context, id int) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM agent_sessions WHERE id = ?", id)
	return err
}

// DeleteDeadAgentSessions removes the ended sessions of the task's main slot.
func (s *Store) DeleteDeadAgentSessions(ctx context.Context, taskID string) error {
	return s.DeleteDeadAgentSessionsInSlot(ctx, taskID, SlotMain)
}

// DeleteDeadAgentSessionsInSlot removes one slot's ended sessions.
func (s *Store) DeleteDeadAgentSessionsInSlot(ctx context.Context, taskID, slot string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM agent_sessions WHERE task_id = ? AND slot = ? AND status != 'running'`, taskID, slotOrMain(slot))
	return err
}

// UpdateChimeraDiscovery stores the Chimera session ID and endpoint discovered
// from the lockfile after spawning a Chimera agent via tmux. The endpoint is
// private (loopback only) and must not be exposed in public API DTOs.
func (s *Store) UpdateChimeraDiscovery(ctx context.Context, taskID, chimeraSessionID, endpoint string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE agent_sessions
		SET chimera_session_id = ?, chimera_endpoint = ?, native_off = 0
		WHERE task_id = ? AND slot = 'main' AND status = 'running'`,
		chimeraSessionID, endpoint, taskID)
	return err
}

// UpdateChimeraDiscoveryForSession binds discovery to the exact running agent
// row observed before endpoint verification. This prevents a stale endpoint
// from being attached to a replacement process for the same task.
func (s *Store) UpdateChimeraDiscoveryForSession(ctx context.Context, agentSessionID int, chimeraSessionID, endpoint string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE agent_sessions
		SET chimera_session_id = ?, chimera_endpoint = ?, native_off = 0
		WHERE id = ? AND status = 'running'`,
		chimeraSessionID, endpoint, agentSessionID)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return ErrNotFound
	}
	return nil
}

// ClearChimeraDiscoveryForSession clears a stale binding only if the exact
// running row still has the endpoint and Chimera session observed by the caller.
func (s *Store) ClearChimeraDiscoveryForSession(ctx context.Context, agentSessionID int, chimeraSessionID, endpoint string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE agent_sessions
		SET chimera_session_id = NULL, chimera_endpoint = NULL, last_event_id = NULL
		WHERE id = ? AND status = 'running'
			AND chimera_session_id = ? AND chimera_endpoint = ?`,
		agentSessionID, chimeraSessionID, endpoint)
	if err != nil {
		return false, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return updated == 1, nil
}

// UpdateChimeraLastEventID stores the latest SSE event ID consumed for this
// agent session, enabling gap-free replay on reconnect.
func (s *Store) UpdateChimeraLastEventID(ctx context.Context, taskID, eventID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE agent_sessions SET last_event_id = ?
		WHERE task_id = ? AND slot = 'main' AND status = 'running'`, eventID, taskID)
	return err
}

// KeepAgentSessionOnTmux clears a running agent's native binding, as
// ClearChimeraDiscoveryForSession does, and marks it as switched to its
// terminal on purpose (native_off), so a restart does not bind it again.
func (s *Store) KeepAgentSessionOnTmux(ctx context.Context, agentSessionID int, chimeraSessionID, endpoint string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE agent_sessions
		SET chimera_session_id = NULL, chimera_endpoint = NULL, last_event_id = NULL, native_off = 1
		WHERE id = ? AND status = 'running'
			AND chimera_session_id = ? AND chimera_endpoint = ?`,
		agentSessionID, chimeraSessionID, endpoint)
	if err != nil {
		return false, err
	}
	updated, err := result.RowsAffected()
	return updated == 1, err
}
