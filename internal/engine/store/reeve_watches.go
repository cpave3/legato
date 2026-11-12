package store

import (
	"context"
	"time"
)

// ReeveWatch is an agent the Reeve watches. ReeveTaskID is the session that
// asked; Events the comma-separated events it asked for ("" for all); Seen
// the watcher's own JSON record of what it last saw.
type ReeveWatch struct {
	TaskID      string `db:"task_id"`
	ReeveTaskID string `db:"reeve_task_id"`
	Events      string `db:"events"`
	Seen        string `db:"seen"`
	CreatedAt   string `db:"created_at"`
}

// WatchForReeve starts watching a task's agent for a Reeve session. A task
// already watched keeps what was seen, with the session and events now
// asked for.
func (s *Store) WatchForReeve(ctx context.Context, w ReeveWatch) error {
	if w.Seen == "" {
		w.Seen = "{}"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO reeve_watches (task_id, reeve_task_id, events, seen) VALUES (?, ?, ?, ?)
		ON CONFLICT (task_id) DO UPDATE SET reeve_task_id = excluded.reeve_task_id, events = excluded.events`, w.TaskID, w.ReeveTaskID, w.Events, w.Seen)
	return err
}

// ListReeveWatches lists every watched task, oldest first.
func (s *Store) ListReeveWatches(ctx context.Context) ([]ReeveWatch, error) {
	var out []ReeveWatch
	err := s.db.SelectContext(ctx, &out, `SELECT task_id, reeve_task_id, events, seen, created_at FROM reeve_watches ORDER BY created_at, task_id`)
	return out, err
}

// ReeveWatchEvent is something a watched agent did that the Reeve has not
// been told yet; Body is the watcher's JSON.
type ReeveWatchEvent struct {
	ID        int64  `db:"id"`
	TaskID    string `db:"task_id"`
	Body      string `db:"body"`
	CreatedAt string `db:"created_at"`
}

// QueueReeveWatchEvent keeps an event until the Reeve is told of it.
func (s *Store) QueueReeveWatchEvent(ctx context.Context, taskID, body string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO reeve_watch_events (task_id, body) VALUES (?, ?)`, taskID, body)
	return err
}

// ListReeveWatchEvents lists at most limit queued events, oldest first.
func (s *Store) ListReeveWatchEvents(ctx context.Context, limit int) ([]ReeveWatchEvent, error) {
	var out []ReeveWatchEvent
	err := s.db.SelectContext(ctx, &out, `SELECT id, task_id, body, created_at FROM reeve_watch_events ORDER BY id LIMIT ?`, limit)
	return out, err
}

// DeleteReeveWatchEventsThrough removes the queued events up to and
// including id: those the Reeve was told of.
func (s *Store) DeleteReeveWatchEventsThrough(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM reeve_watch_events WHERE id <= ?`, id)
	return err
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
