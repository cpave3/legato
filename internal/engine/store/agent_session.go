package store

import (
	"context"
	"database/sql"
	"fmt"
)

func (s *Store) InsertAgentSession(ctx context.Context, a AgentSession) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO agent_sessions (ticket_id, tmux_session, command, status)
		VALUES (:ticket_id, :tmux_session, :command, :status)`, a)
	return err
}

func (s *Store) ListAgentSessions(ctx context.Context) ([]AgentSession, error) {
	var sessions []AgentSession
	err := s.db.SelectContext(ctx, &sessions,
		"SELECT * FROM agent_sessions WHERE status = 'running' ORDER BY started_at DESC")
	return sessions, err
}

func (s *Store) GetAgentSessionByTicketID(ctx context.Context, ticketID string) (*AgentSession, error) {
	var a AgentSession
	err := s.db.GetContext(ctx, &a,
		"SELECT * FROM agent_sessions WHERE ticket_id = ? AND status = 'running' ORDER BY started_at DESC LIMIT 1", ticketID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent session for ticket %s: %w", ticketID, ErrNotFound)
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

func (s *Store) UpdateAgentSessionStatus(ctx context.Context, ticketID string, status string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE agent_sessions SET status = ?, ended_at = datetime('now')
		WHERE ticket_id = ? AND status = 'running'`, status, ticketID)
	return err
}

func (s *Store) DeleteDeadAgentSessions(ctx context.Context, ticketID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM agent_sessions WHERE ticket_id = ? AND status != 'running'`, ticketID)
	return err
}
