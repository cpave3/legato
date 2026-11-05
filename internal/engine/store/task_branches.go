package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

// BranchRef names a branch of a GitHub repository (owner/name).
type BranchRef struct {
	Repo   string
	Branch string
}

// TaskBranch is one branch a task tracks, at its place in the task's stack
// order, with the PR found for it (nil until discovery has looked).
type TaskBranch struct {
	TaskID   string
	Repo     string
	Branch   string
	Position int
	// Source says where the branch came from: code_context, stack, api,
	// manual or discovered.
	Source  string
	PR      *PRMeta
	AddedAt string
}

type taskBranchRow struct {
	TaskID   string         `db:"task_id"`
	Repo     string         `db:"repo"`
	Branch   string         `db:"branch"`
	Position int            `db:"position"`
	Source   string         `db:"source"`
	PR       sql.NullString `db:"pr"`
	AddedAt  string         `db:"added_at"`
}

func (r taskBranchRow) branch() (TaskBranch, error) {
	b := TaskBranch{TaskID: r.TaskID, Repo: r.Repo, Branch: r.Branch, Position: r.Position, Source: r.Source, AddedAt: r.AddedAt}
	if r.PR.Valid {
		pr, err := ParsePRMeta(&r.PR.String)
		if err != nil {
			return TaskBranch{}, err
		}
		b.PR = pr
	}
	return b, nil
}

const taskBranchSelect = `SELECT task_id, repo, branch, position, source, pr, added_at FROM task_branches`

// TrackTaskBranches adds branches to a task in the order given, and returns
// those it did not track before. Branches it tracks already are kept with
// their PRs; the given ones are put together, in the given order, where the
// first of them already was (at the end when none was), so a stack that
// grows or is reordered stays whole and in order.
func (s *Store) TrackTaskBranches(ctx context.Context, taskID string, branches []BranchRef, source string) ([]BranchRef, error) {
	given := make([]BranchRef, 0, len(branches))
	seen := map[BranchRef]bool{}
	for _, b := range branches {
		if b.Repo == "" || b.Branch == "" || seen[b] {
			continue
		}
		seen[b] = true
		given = append(given, b)
	}
	if len(given) == 0 {
		return nil, nil
	}
	var added []BranchRef
	err := withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		var rows []taskBranchRow
		if err := tx.SelectContext(ctx, &rows, taskBranchSelect+` WHERE task_id = ? ORDER BY position`, taskID); err != nil {
			return err
		}
		existing := map[BranchRef]bool{}
		for _, r := range rows {
			existing[BranchRef{r.Repo, r.Branch}] = true
		}
		order := make([]BranchRef, 0, len(rows)+len(given))
		placed := false
		for _, r := range rows {
			ref := BranchRef{r.Repo, r.Branch}
			if !seen[ref] {
				order = append(order, ref)
				continue
			}
			if !placed {
				order = append(order, given...)
				placed = true
			}
		}
		if !placed {
			order = append(order, given...)
		}
		now := time.Now().UTC().Format(time.RFC3339)
		for i, ref := range order {
			if existing[ref] {
				if _, err := tx.ExecContext(ctx, `UPDATE task_branches SET position = ? WHERE task_id = ? AND repo = ? AND branch = ?`,
					i, taskID, ref.Repo, ref.Branch); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO task_branches (task_id, repo, branch, position, source, added_at) VALUES (?, ?, ?, ?, ?, ?)`,
				taskID, ref.Repo, ref.Branch, i, source, now); err != nil {
				return err
			}
			added = append(added, ref)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return added, nil
}

// ListTaskBranches lists the branches a task tracks, in stack order.
func (s *Store) ListTaskBranches(ctx context.Context, taskID string) ([]TaskBranch, error) {
	var rows []taskBranchRow
	if err := s.db.SelectContext(ctx, &rows, taskBranchSelect+` WHERE task_id = ? ORDER BY position`, taskID); err != nil {
		return nil, err
	}
	out := make([]TaskBranch, 0, len(rows))
	for _, r := range rows {
		b, err := r.branch()
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// ListTrackedTaskBranches lists the branches of every task not archived, by
// task, each in stack order.
func (s *Store) ListTrackedTaskBranches(ctx context.Context) (map[string][]TaskBranch, error) {
	var rows []taskBranchRow
	if err := s.db.SelectContext(ctx, &rows, `SELECT b.task_id, b.repo, b.branch, b.position, b.source, b.pr, b.added_at
		FROM task_branches b JOIN tasks t ON t.id = b.task_id
		WHERE t.archived_at IS NULL ORDER BY b.task_id, b.position`); err != nil {
		return nil, err
	}
	out := map[string][]TaskBranch{}
	for _, r := range rows {
		b, err := r.branch()
		if err != nil {
			return nil, err
		}
		out[r.TaskID] = append(out[r.TaskID], b)
	}
	return out, nil
}

// SetTaskBranchPR replaces the PR cache of one of a task's branches (nil
// clears it). ErrNotFound when the task does not track the branch.
func (s *Store) SetTaskBranchPR(ctx context.Context, taskID, repo, branch string, pr *PRMeta) error {
	raw, err := MarshalPRMeta(pr)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE task_branches SET pr = ? WHERE task_id = ? AND repo = ? AND branch = ?`, raw, taskID, repo, branch)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RemoveTaskBranches stops tracking a task's branches of the given sources,
// or all of them when none is given.
func (s *Store) RemoveTaskBranches(ctx context.Context, taskID string, sources ...string) error {
	if len(sources) == 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM task_branches WHERE task_id = ?`, taskID)
		return err
	}
	query, args, err := sqlx.In(`DELETE FROM task_branches WHERE task_id = ? AND source IN (?)`, taskID, sources)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.db.Rebind(query), args...)
	return err
}

// RemoveTaskBranch stops tracking one of a task's branches.
func (s *Store) RemoveTaskBranch(ctx context.Context, taskID, repo, branch string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM task_branches WHERE task_id = ? AND repo = ? AND branch = ?`, taskID, repo, branch)
	return err
}
