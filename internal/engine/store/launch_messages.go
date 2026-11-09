package store

import "context"

// LaunchMessage is a message an agent was launched with that has not been
// delivered yet.
type LaunchMessage struct {
	ExecutionID string `db:"execution_id"`
	Position    int    `db:"position"`
	TaskID      string `db:"task_id"`
	Slot        string `db:"slot"`
	TmuxSession string `db:"tmux_session"`
	AgentKind   string `db:"agent_kind"`
	PromptFile  string `db:"prompt_file"`
	Message     string `db:"message"`
	Owner       string `db:"owner"`
}

// InsertLaunchMessages keeps an execution's launch messages, in order.
func (s *Store) InsertLaunchMessages(ctx context.Context, messages []LaunchMessage) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, m := range messages {
		if _, err := tx.ExecContext(ctx, `INSERT INTO launch_messages (execution_id, position, task_id, slot, tmux_session, agent_kind, prompt_file, message, owner)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, m.ExecutionID, m.Position, m.TaskID, m.Slot, m.TmuxSession, m.AgentKind, m.PromptFile, m.Message, m.Owner); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListLaunchMessages lists every message not yet delivered, by execution
// and in order.
func (s *Store) ListLaunchMessages(ctx context.Context) ([]LaunchMessage, error) {
	messages := []LaunchMessage{}
	err := s.db.SelectContext(ctx, &messages, `SELECT execution_id, position, task_id, slot, tmux_session, agent_kind, prompt_file, message, owner
		FROM launch_messages ORDER BY created_at, execution_id, position`)
	return messages, err
}

// TakeLaunchMessage removes one message for delivery by owner, reporting
// false when it is gone or another owner has it.
func (s *Store) TakeLaunchMessage(ctx context.Context, executionID string, position int, owner string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM launch_messages WHERE execution_id = ? AND position = ? AND owner = ?`, executionID, position, owner)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// AdoptLaunchMessages gives an execution's messages from their owner to
// another, reporting false when another process took them first.
func (s *Store) AdoptLaunchMessages(ctx context.Context, executionID, from, to string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE launch_messages SET owner = ? WHERE execution_id = ? AND owner = ?`, to, executionID, from)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DeleteLaunchMessages drops an execution's messages.
func (s *Store) DeleteLaunchMessages(ctx context.Context, executionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM launch_messages WHERE execution_id = ?`, executionID)
	return err
}
