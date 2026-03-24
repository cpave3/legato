package store

import (
	"context"
	"database/sql"
	"fmt"
)

func (s *Store) InsertAgentSession(ctx context.Context, a AgentSession) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO agent_sessions (task_id, tmux_session, command, status)
		VALUES (:task_id, :tmux_session, :command, :status)`, a)
	return err
}

func (s *Store) ListAgentSessions(ctx context.Context) ([]AgentSession, error) {
	var sessions []AgentSession
	err := s.db.SelectContext(ctx, &sessions,
		"SELECT * FROM agent_sessions WHERE status = 'running' ORDER BY started_at DESC")
	return sessions, err
}

func (s *Store) GetAgentSessionByTaskID(ctx context.Context, taskID string) (*AgentSession, error) {
	var a AgentSession
	err := s.db.GetContext(ctx, &a,
		"SELECT * FROM agent_sessions WHERE task_id = ? AND status = 'running' ORDER BY started_at DESC LIMIT 1", taskID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent session for task %s: %w", taskID, ErrNotFound)
	}
	return &a, err
}

func (s *Store) GetAgentSessionByTmuxName(ctx context.Context, tmuxSession string) (*AgentSession, error) {
	var a AgentSession
	err := s.db.GetContext(ctx, &a,
		"SELECT * FROM agent_sessions WHERE tmux_session = ?", tmuxSession)
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

func (s *Store) UpdateAgentActivity(ctx context.Context, taskID string, activity string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE agent_sessions SET activity = ?
		WHERE task_id = ? AND status = 'running'`, activity, taskID)
	return err
}

// GetAgentActivityCounts returns the count of running agent sessions grouped by activity state.
// If excludeTaskID is non-empty, that task's session is excluded from counts.
func (s *Store) GetAgentActivityCounts(ctx context.Context, excludeTaskID string) (working, waiting, idle int, err error) {
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
		return 0, 0, 0, err
	}

	for _, r := range rows {
		switch r.Activity {
		case "working":
			working = r.Count
		case "waiting":
			waiting = r.Count
		default:
			idle += r.Count
		}
	}
	return working, waiting, idle, nil
}

func (s *Store) DeleteDeadAgentSessions(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM agent_sessions WHERE task_id = ? AND status != 'running'`, taskID)
	return err
}
