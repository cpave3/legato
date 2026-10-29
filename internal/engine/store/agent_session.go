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
		INSERT INTO agent_sessions (task_id, tmux_session, command, agent_kind, status, role, parent_task_id, subtask_id, sandboxed)
		SELECT :task_id, :tmux_session, :command, :agent_kind, :status, :role, :parent_task_id, :subtask_id, :sandboxed
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
		INSERT INTO agent_sessions (task_id, tmux_session, command, agent_kind, status, role, parent_task_id, subtask_id, sandboxed)
		SELECT :task_id, :tmux_session, :command, :agent_kind, :status, :role, :parent_task_id, :subtask_id, :sandboxed
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

func (s *Store) GetAgentSessionByTaskID(ctx context.Context, taskID string) (*AgentSession, error) {
	var a AgentSession
	err := s.db.GetContext(ctx, &a,
		agentSessionSelect+" WHERE a.task_id = ? AND a.status = 'running' ORDER BY a.started_at DESC LIMIT 1", taskID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent session for task %s: %w", taskID, ErrNotFound)
	}
	return &a, err
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

func (s *Store) UpdateAgentSessionStatus(ctx context.Context, taskID string, status string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE agent_sessions SET status = ?, ended_at = datetime('now')
		WHERE task_id = ? AND status = 'running'`, status, taskID)
	return err
}

// UpdateAgentSessionSwarmRole updates swarm metadata on the currently running
// session for taskID. Use nil subtaskID for conductors.
func (s *Store) UpdateAgentSessionSwarmRole(ctx context.Context, taskID, role, parentTaskID string, subtaskID *string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE agent_sessions
		SET role = ?, parent_task_id = ?, subtask_id = ?
		WHERE task_id = ? AND status = 'running'`,
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

func (s *Store) UpdateAgentActivity(ctx context.Context, taskID string, activity string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE agent_sessions SET activity = ?
		WHERE task_id = ? AND status = 'running'`, activity, taskID)
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

func (s *Store) DeleteDeadAgentSessions(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM agent_sessions WHERE task_id = ? AND status != 'running'`, taskID)
	return err
}

// UpdateChimeraDiscovery stores the Chimera session ID and endpoint discovered
// from the lockfile after spawning a Chimera agent via tmux. The endpoint is
// private (loopback only) and must not be exposed in public API DTOs.
func (s *Store) UpdateChimeraDiscovery(ctx context.Context, taskID, chimeraSessionID, endpoint string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE agent_sessions
		SET chimera_session_id = ?, chimera_endpoint = ?
		WHERE task_id = ? AND status = 'running'`,
		chimeraSessionID, endpoint, taskID)
	return err
}

// UpdateChimeraDiscoveryForSession binds discovery to the exact running agent
// row observed before endpoint verification. This prevents a stale endpoint
// from being attached to a replacement process for the same task.
func (s *Store) UpdateChimeraDiscoveryForSession(ctx context.Context, agentSessionID int, chimeraSessionID, endpoint string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE agent_sessions
		SET chimera_session_id = ?, chimera_endpoint = ?
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
		WHERE task_id = ? AND status = 'running'`, eventID, taskID)
	return err
}
