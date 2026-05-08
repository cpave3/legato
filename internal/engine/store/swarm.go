package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// MarshalScopeGlobs serializes a []string of glob patterns to a JSON string for storage.
func MarshalScopeGlobs(globs []string) (string, error) {
	if globs == nil {
		globs = []string{}
	}
	b, err := json.Marshal(globs)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ParseScopeGlobs deserializes a scope_globs JSON string into a []string.
func ParseScopeGlobs(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	var globs []string
	if err := json.Unmarshal([]byte(raw), &globs); err != nil {
		return nil, fmt.Errorf("parsing scope_globs: %w", err)
	}
	return globs, nil
}

// CreateSubtask inserts a new sub-task row.
func (s *Store) CreateSubtask(ctx context.Context, st Subtask) error {
	if st.Status == "" {
		st.Status = "queued"
	}
	if st.ScopeGlobs == "" {
		st.ScopeGlobs = "[]"
	}
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO swarm_subtasks (id, parent_task_id, title, description, prompt, scope_globs,
			role, agent_kind, status, builder_agent_id, reviewer_agent_id,
			created_at, dispatched_at, started_at, completed_at)
		VALUES (:id, :parent_task_id, :title, :description, :prompt, :scope_globs,
			:role, :agent_kind, :status, :builder_agent_id, :reviewer_agent_id,
			COALESCE(NULLIF(:created_at, ''), datetime('now')),
			:dispatched_at, :started_at, :completed_at)`, st)
	return err
}

// GetSubtask returns a sub-task by its ID.
func (s *Store) GetSubtask(ctx context.Context, id string) (*Subtask, error) {
	var st Subtask
	err := s.db.GetContext(ctx, &st, "SELECT * FROM swarm_subtasks WHERE id = ?", id)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// ListSubtasksByParent returns all sub-tasks for the given parent task, ordered by created_at.
func (s *Store) ListSubtasksByParent(ctx context.Context, parentID string) ([]Subtask, error) {
	var subtasks []Subtask
	err := s.db.SelectContext(ctx, &subtasks,
		"SELECT * FROM swarm_subtasks WHERE parent_task_id = ? ORDER BY created_at ASC, id ASC", parentID)
	return subtasks, err
}

// ListSubtasksByParentAndStatus returns sub-tasks for a parent filtered by status.
func (s *Store) ListSubtasksByParentAndStatus(ctx context.Context, parentID, status string) ([]Subtask, error) {
	var subtasks []Subtask
	err := s.db.SelectContext(ctx, &subtasks,
		"SELECT * FROM swarm_subtasks WHERE parent_task_id = ? AND status = ? ORDER BY created_at ASC, id ASC",
		parentID, status)
	return subtasks, err
}

// UpdateSubtaskStatus transitions a sub-task to a new status.
//   - "in_progress" sets started_at if null (first observed worker activity).
//   - "done" or "cancelled" sets completed_at.
//   - All other statuses leave timestamps untouched.
func (s *Store) UpdateSubtaskStatus(ctx context.Context, id, status string) error {
	var query string
	switch status {
	case "in_progress":
		query = `UPDATE swarm_subtasks SET status = ?,
			started_at = COALESCE(started_at, datetime('now'))
			WHERE id = ?`
	case "done", "cancelled":
		query = `UPDATE swarm_subtasks SET status = ?,
			completed_at = datetime('now')
			WHERE id = ?`
	default:
		query = `UPDATE swarm_subtasks SET status = ? WHERE id = ?`
	}
	result, err := s.db.ExecContext(ctx, query, status, id)
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

// SetSubtaskDispatched marks a sub-task as dispatched (status `dispatched`,
// dispatched_at set to now). Returns ErrNotFound if the row doesn't exist.
func (s *Store) SetSubtaskDispatched(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE swarm_subtasks
		SET status = 'dispatched',
		    dispatched_at = datetime('now')
		WHERE id = ?`, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateSubtaskDescription replaces the description (used by Review --reject to append notes).
func (s *Store) UpdateSubtaskDescription(ctx context.Context, id, description string) error {
	result, err := s.db.ExecContext(ctx,
		"UPDATE swarm_subtasks SET description = ? WHERE id = ?", description, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// SetSubtaskBuilderAgent assigns the given agent_session id as the builder for a sub-task.
func (s *Store) SetSubtaskBuilderAgent(ctx context.Context, subtaskID string, agentID *int) error {
	result, err := s.db.ExecContext(ctx,
		"UPDATE swarm_subtasks SET builder_agent_id = ? WHERE id = ?", agentID, subtaskID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// SetSubtaskReviewerAgent assigns the given agent_session id as the reviewer for a sub-task.
func (s *Store) SetSubtaskReviewerAgent(ctx context.Context, subtaskID string, agentID *int) error {
	result, err := s.db.ExecContext(ctx,
		"UPDATE swarm_subtasks SET reviewer_agent_id = ? WHERE id = ?", agentID, subtaskID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSubtask removes a sub-task row.
func (s *Store) DeleteSubtask(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM swarm_subtasks WHERE id = ?", id)
	return err
}

// InsertSwarmEvent appends a conductor-bound event to the inbox. Returns the
// generated ID so the producer can include it in the send-keys pointer.
func (s *Store) InsertSwarmEvent(ctx context.Context, e SwarmEvent) (int, error) {
	res, err := s.db.NamedExecContext(ctx, `
		INSERT INTO swarm_events (parent_task_id, subtask_id, kind, worker_title, payload)
		VALUES (:parent_task_id, :subtask_id, :kind, :worker_title, :payload)`, e)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

// ListUnackedSwarmEvents returns the swarm_events rows for a parent that have
// not yet been acked, ordered chronologically.
func (s *Store) ListUnackedSwarmEvents(ctx context.Context, parentID string) ([]SwarmEvent, error) {
	var events []SwarmEvent
	err := s.db.SelectContext(ctx, &events, `
		SELECT * FROM swarm_events
		WHERE parent_task_id = ? AND acked_at IS NULL
		ORDER BY id ASC`, parentID)
	return events, err
}

// AckSwarmEvents marks the given event IDs as acked (read by the conductor).
func (s *Store) AckSwarmEvents(ctx context.Context, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	// SQLite param placeholders: build the IN clause.
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := "UPDATE swarm_events SET acked_at = datetime('now') WHERE id IN (" + strings.Join(placeholders, ",") + ")"
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}
