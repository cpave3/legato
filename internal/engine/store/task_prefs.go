package store

import (
	"context"
	"database/sql"
	"fmt"
)

// UpdateTaskNotifyEnabled sets or clears the notify preference for a task.
func (s *Store) UpdateTaskNotifyEnabled(ctx context.Context, taskID string, enabled bool) error {
	val := 0
	if enabled {
		val = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO task_prefs (task_id, notify_enabled) VALUES (?, ?)
		 ON CONFLICT(task_id) DO UPDATE SET notify_enabled = excluded.notify_enabled`,
		taskID, val)
	return err
}

// GetTaskNotifyEnabled returns whether notifications are enabled for a task.
func (s *Store) GetTaskNotifyEnabled(ctx context.Context, taskID string) (bool, error) {
	var val int
	err := s.db.GetContext(ctx, &val,
		`SELECT notify_enabled FROM task_prefs WHERE task_id = ?`, taskID)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get task notify: %w", err)
	}
	return val != 0, nil
}
