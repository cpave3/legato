package store

import (
	"context"
	"time"
)

// ReeveWatch is an agent a Reeve session started or messaged, whose work
// it is told of. Seen is the watcher's own JSON record of what it last
// reported.
type ReeveWatch struct {
	TaskID      string `db:"task_id"`
	ReeveTaskID string `db:"reeve_task_id"`
	Seen        string `db:"seen"`
	CreatedAt   string `db:"created_at"`
}

// WatchForReeve starts watching a task's agent for a Reeve session. A task
// already watched keeps what was seen, for the session now watching.
func (s *Store) WatchForReeve(ctx context.Context, w ReeveWatch) error {
	if w.Seen == "" {
		w.Seen = "{}"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO reeve_watches (task_id, reeve_task_id, seen) VALUES (?, ?, ?)
		ON CONFLICT (task_id) DO UPDATE SET reeve_task_id = excluded.reeve_task_id`, w.TaskID, w.ReeveTaskID, w.Seen)
	return err
}

// ListReeveWatches lists every watched task, oldest first.
func (s *Store) ListReeveWatches(ctx context.Context) ([]ReeveWatch, error) {
	var out []ReeveWatch
	err := s.db.SelectContext(ctx, &out, `SELECT task_id, reeve_task_id, seen, created_at FROM reeve_watches ORDER BY created_at, task_id`)
	return out, err
}

// SetReeveWatchSeen records what the watcher reported of a task.
func (s *Store) SetReeveWatchSeen(ctx context.Context, taskID, seen string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE reeve_watches SET seen = ? WHERE task_id = ?`, seen, taskID)
	return err
}

// DeleteReeveWatch stops watching a task.
func (s *Store) DeleteReeveWatch(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM reeve_watches WHERE task_id = ?`, taskID)
	return err
}

// TaskWorkedSince reports whether an execution for the task was working at
// any moment after since.
func (s *Store) TaskWorkedSince(ctx context.Context, taskID string, since time.Time) (bool, error) {
	var worked bool
	err := s.db.GetContext(ctx, &worked, `
		SELECT EXISTS (
			SELECT 1 FROM execution_activity a JOIN executions e ON e.id = a.execution_id
			WHERE e.task_id = ? AND a.state = 'working' AND (a.ended_at IS NULL OR a.ended_at > ?))`,
		taskID, formatExecutionTime(since))
	return worked, err
}
