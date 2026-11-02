package store

import (
	"context"
	"database/sql"
	"time"
)

// JobClone is a clone Legato made for a job in its data folder.
type JobClone struct {
	ID          int64
	ExecutionID string
	TaskID      string
	Repo        string // owner/name
	Ref         string // what was asked for; "" for the default branch
	Branch      string // the branch checked out; "" when detached
	Path        string
	CreatedAt   time.Time
	// RemoveAfter is set when the clone was kept for unpushed work.
	RemoveAfter *time.Time
	// LastUsedAt is when an agent last started or was seen working in it.
	LastUsedAt time.Time
	// TaskIDs are the tasks that work in it, the one it was made for first.
	TaskIDs []string
}

type jobCloneRow struct {
	ID          int64          `db:"id"`
	ExecutionID string         `db:"execution_id"`
	TaskID      string         `db:"task_id"`
	Repo        string         `db:"repo"`
	Ref         string         `db:"ref"`
	Branch      string         `db:"branch"`
	Path        string         `db:"path"`
	CreatedAt   string         `db:"created_at"`
	RemoveAfter sql.NullString `db:"remove_after"`
	LastUsedAt  sql.NullString `db:"last_used_at"`
}

func (r jobCloneRow) clone() (JobClone, error) {
	created, err := time.Parse(executionTimestamp, r.CreatedAt)
	if err != nil {
		return JobClone{}, err
	}
	c := JobClone{ID: r.ID, ExecutionID: r.ExecutionID, TaskID: r.TaskID, Repo: r.Repo, Ref: r.Ref, Branch: r.Branch, Path: r.Path, CreatedAt: created, LastUsedAt: created}
	if r.LastUsedAt.Valid {
		if c.LastUsedAt, err = time.Parse(executionTimestamp, r.LastUsedAt.String); err != nil {
			return JobClone{}, err
		}
	}
	if r.RemoveAfter.Valid {
		after, err := time.Parse(executionTimestamp, r.RemoveAfter.String)
		if err != nil {
			return JobClone{}, err
		}
		c.RemoveAfter = &after
	}
	return c, nil
}

const jobCloneColumns = `id, execution_id, task_id, repo, ref, branch, path, created_at, remove_after, last_used_at`

// CreateJobClone records a clone made for a job, with its task as the
// first that works in it, and returns its ID.
func (s *Store) CreateJobClone(ctx context.Context, c JobClone) (int64, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	made := formatExecutionTime(c.CreatedAt)
	result, err := tx.ExecContext(ctx, `INSERT INTO job_clones (execution_id, task_id, repo, ref, branch, path, created_at, last_used_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, c.ExecutionID, c.TaskID, c.Repo, c.Ref, c.Branch, c.Path, made, made)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO job_clone_tasks (clone_id, task_id, attached_at) VALUES (?, ?, ?)`, id, c.TaskID, made); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// AttachJobCloneTask records that a task works in a clone, from at; the
// clone was used then. A task already attached stays attached.
func (s *Store) AttachJobCloneTask(ctx context.Context, cloneID int64, taskID string, at time.Time) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	when := formatExecutionTime(at)
	result, err := tx.ExecContext(ctx, `UPDATE job_clones SET last_used_at = ? WHERE id = ? AND removed_at IS NULL`, when, cloneID)
	if err != nil {
		return err
	}
	if err := expectOneRow(result); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO job_clone_tasks (clone_id, task_id, attached_at) VALUES (?, ?, ?)`, cloneID, taskID, when); err != nil {
		return err
	}
	return tx.Commit()
}

// TouchJobClone records that a clone was in use at a time.
func (s *Store) TouchJobClone(ctx context.Context, id int64, at time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE job_clones SET last_used_at = ? WHERE id = ?`, formatExecutionTime(at), id)
	if err != nil {
		return err
	}
	return expectOneRow(result)
}

func (s *Store) liveJobClones(ctx context.Context, where string, args ...any) ([]JobClone, error) {
	var rows []jobCloneRow
	if err := s.db.SelectContext(ctx, &rows, `SELECT `+jobCloneColumns+` FROM job_clones WHERE removed_at IS NULL`+where, args...); err != nil {
		return nil, err
	}
	out := make([]JobClone, 0, len(rows))
	for _, r := range rows {
		c, err := r.clone()
		if err != nil {
			return nil, err
		}
		if err := s.db.SelectContext(ctx, &c.TaskIDs, `SELECT task_id FROM job_clone_tasks WHERE clone_id = ? ORDER BY attached_at, rowid`, c.ID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) oneLiveJobClone(ctx context.Context, where string, args ...any) (*JobClone, error) {
	clones, err := s.liveJobClones(ctx, where, args...)
	if err != nil {
		return nil, err
	}
	if len(clones) == 0 {
		return nil, ErrNotFound
	}
	return &clones[0], nil
}

// JobCloneByPath finds the clone at path that has not been removed.
func (s *Store) JobCloneByPath(ctx context.Context, path string) (*JobClone, error) {
	return s.oneLiveJobClone(ctx, ` AND path = ?`, path)
}

// JobCloneByID finds a clone that has not been removed.
func (s *Store) JobCloneByID(ctx context.Context, id int64) (*JobClone, error) {
	return s.oneLiveJobClone(ctx, ` AND id = ?`, id)
}

// LiveJobCloneForTask finds the newest clone of a repository that a task
// works in and that has not been removed.
func (s *Store) LiveJobCloneForTask(ctx context.Context, taskID, repo string) (*JobClone, error) {
	return s.oneLiveJobClone(ctx, ` AND repo = ? AND id IN (SELECT clone_id FROM job_clone_tasks WHERE task_id = ?) ORDER BY created_at DESC, id DESC`, repo, taskID)
}

// ListLiveJobClones lists the clones that have not been removed, oldest
// first.
func (s *Store) ListLiveJobClones(ctx context.Context) ([]JobClone, error) {
	return s.liveJobClones(ctx, ` ORDER BY created_at, id`)
}

// KeepJobCloneUntil records that a clone with unpushed work is kept until
// a time.
func (s *Store) KeepJobCloneUntil(ctx context.Context, id int64, until time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE job_clones SET remove_after = ? WHERE id = ?`, formatExecutionTime(until), id)
	if err != nil {
		return err
	}
	return expectOneRow(result)
}

// MarkJobCloneRemoved records that a clone's folder was removed.
func (s *Store) MarkJobCloneRemoved(ctx context.Context, id int64, at time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE job_clones SET removed_at = ? WHERE id = ?`, formatExecutionTime(at), id)
	if err != nil {
		return err
	}
	return expectOneRow(result)
}
