package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// CloneStack is the stack of branches Staccato keeps in a job clone, as
// Legato last read it. Branches is JSON the service writes; Problem is why
// the stack could not be read, when it could not.
type CloneStack struct {
	CloneID  int64
	Root     string
	Branches string
	Problem  string
	ReadAt   time.Time
	// Repo and Path are the clone's; Live is false once its folder is
	// removed. They are filled when read, and ignored by PutCloneStack.
	Repo string
	Path string
	Live bool
}

type cloneStackRow struct {
	CloneID   int64          `db:"clone_id"`
	Root      string         `db:"root"`
	Branches  string         `db:"branches"`
	Problem   string         `db:"problem"`
	ReadAt    string         `db:"read_at"`
	Repo      string         `db:"repo"`
	Path      string         `db:"path"`
	RemovedAt sql.NullString `db:"removed_at"`
}

const cloneStackSelect = `SELECT s.clone_id, s.root, s.branches, s.problem, s.read_at, c.repo, c.path, c.removed_at
	FROM clone_stacks s JOIN job_clones c ON c.id = s.clone_id`

func (r cloneStackRow) stack() (CloneStack, error) {
	readAt, err := time.Parse(executionTimestamp, r.ReadAt)
	if err != nil {
		return CloneStack{}, err
	}
	return CloneStack{CloneID: r.CloneID, Root: r.Root, Branches: r.Branches, Problem: r.Problem,
		ReadAt: readAt, Repo: r.Repo, Path: r.Path, Live: !r.RemovedAt.Valid}, nil
}

// PutCloneStack records a clone's stack, replacing the one recorded before.
func (s *Store) PutCloneStack(ctx context.Context, st CloneStack) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO clone_stacks (clone_id, root, branches, problem, read_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (clone_id) DO UPDATE SET root = excluded.root, branches = excluded.branches,
			problem = excluded.problem, read_at = excluded.read_at`,
		st.CloneID, st.Root, st.Branches, st.Problem, formatExecutionTime(st.ReadAt))
	return err
}

// DeleteCloneStack forgets a clone's stack: the clone has none any more.
func (s *Store) DeleteCloneStack(ctx context.Context, cloneID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM clone_stacks WHERE clone_id = ?`, cloneID)
	return err
}

// CloneStackByID is the stack recorded for a clone, removed or not.
func (s *Store) CloneStackByID(ctx context.Context, cloneID int64) (CloneStack, error) {
	var row cloneStackRow
	err := s.db.GetContext(ctx, &row, cloneStackSelect+` WHERE s.clone_id = ?`, cloneID)
	if errors.Is(err, sql.ErrNoRows) {
		return CloneStack{}, ErrNotFound
	}
	if err != nil {
		return CloneStack{}, err
	}
	return row.stack()
}

// ListTaskStacks lists the stacks of the clones a task works or worked in,
// oldest clone first.
func (s *Store) ListTaskStacks(ctx context.Context, taskID string) ([]CloneStack, error) {
	var rows []cloneStackRow
	if err := s.db.SelectContext(ctx, &rows, cloneStackSelect+`
		WHERE s.clone_id IN (SELECT clone_id FROM job_clone_tasks WHERE task_id = ?)
		ORDER BY c.created_at, c.id`, taskID); err != nil {
		return nil, err
	}
	out := make([]CloneStack, 0, len(rows))
	for _, r := range rows {
		st, err := r.stack()
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// ListLiveTaskJobClones lists the clones a task works in whose folders
// have not been removed, oldest first.
func (s *Store) ListLiveTaskJobClones(ctx context.Context, taskID string) ([]JobClone, error) {
	return s.liveJobClones(ctx, ` AND id IN (SELECT clone_id FROM job_clone_tasks WHERE task_id = ?) ORDER BY created_at, id`, taskID)
}
