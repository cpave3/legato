package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// SetExecutionEngram records the engram an execution runs as and its
// engram session key.
func (s *Store) SetExecutionEngram(ctx context.Context, executionID, engram, session string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE executions SET engram = ?, engram_session = ? WHERE id = ?`, engram, session, executionID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ExecutionEngram is the engram an execution ran as and its session key,
// both empty when it ran as none.
func (s *Store) ExecutionEngram(ctx context.Context, executionID string) (engram, session string, err error) {
	var row struct {
		Engram  string `db:"engram"`
		Session string `db:"engram_session"`
	}
	err = s.db.GetContext(ctx, &row, `SELECT engram, engram_session FROM executions WHERE id = ?`, executionID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return row.Engram, row.Session, err
}

// TaskEngram is the engram a task's execution ran as.
type TaskEngram struct {
	ExecutionID string `db:"id"`
	Engram      string `db:"engram"`
	Session     string `db:"engram_session"`
}

// LastTaskEngram is the engram of the task's latest execution that ran as
// one; zero when none did.
func (s *Store) LastTaskEngram(ctx context.Context, taskID string) (TaskEngram, error) {
	var last TaskEngram
	err := s.db.GetContext(ctx, &last, `SELECT id, engram, engram_session FROM executions
		WHERE execution_task_id = ? AND engram != '' ORDER BY started_at DESC, rowid DESC LIMIT 1`, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskEngram{}, nil
	}
	return last, err
}

// LastEngramSession is the session key of the task's latest execution
// that ran as engram, "" when none did.
func (s *Store) LastEngramSession(ctx context.Context, taskID, engram string) (string, error) {
	var session string
	err := s.db.GetContext(ctx, &session, `SELECT engram_session FROM executions
		WHERE execution_task_id = ? AND engram = ? AND engram_session != '' ORDER BY started_at DESC, rowid DESC LIMIT 1`, taskID, engram)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return session, err
}

// EngramBinding is an engram bound to a place key.
type EngramBinding struct {
	PlaceKey   string
	Engram     string
	IsDefault  bool
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

type engramBindingRow struct {
	PlaceKey   string         `db:"place_key"`
	Engram     string         `db:"engram"`
	IsDefault  bool           `db:"is_default"`
	LastUsedAt sql.NullString `db:"last_used_at"`
	CreatedAt  string         `db:"created_at"`
}

func (r engramBindingRow) binding() EngramBinding {
	b := EngramBinding{PlaceKey: r.PlaceKey, Engram: r.Engram, IsDefault: r.IsDefault}
	b.CreatedAt, _ = time.Parse(executionTimestamp, r.CreatedAt)
	if r.LastUsedAt.Valid {
		if at, err := time.Parse(executionTimestamp, r.LastUsedAt.String); err == nil {
			b.LastUsedAt = &at
		}
	}
	return b
}

// BindEngram binds an engram to a place, unless it is bound already, and
// with makeDefault makes it the place's default. The old default stays
// bound.
func (s *Store) BindEngram(ctx context.Context, place, engram string, makeDefault bool, at time.Time) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO engram_bindings (place_key, engram, created_at) VALUES (?, ?, ?)
		ON CONFLICT (place_key, engram) DO NOTHING`, place, engram, formatExecutionTime(at)); err != nil {
		return err
	}
	if makeDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE engram_bindings SET is_default = 0 WHERE place_key = ?`, place); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE engram_bindings SET is_default = 1 WHERE place_key = ? AND engram = ?`, place, engram); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetEngramDefault makes a bound engram its place's default; ErrNotFound
// when it is not bound there.
func (s *Store) SetEngramDefault(ctx context.Context, place, engram string) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err := tx.GetContext(ctx, &n, `SELECT COUNT(*) FROM engram_bindings WHERE place_key = ? AND engram = ?`, place, engram); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE engram_bindings SET is_default = 0 WHERE place_key = ?`, place); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE engram_bindings SET is_default = 1 WHERE place_key = ? AND engram = ?`, place, engram); err != nil {
		return err
	}
	return tx.Commit()
}

// ClearEngramDefault leaves a place with no default; its engrams stay
// bound.
func (s *Store) ClearEngramDefault(ctx context.Context, place string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE engram_bindings SET is_default = 0 WHERE place_key = ?`, place)
	return err
}

// UnbindEngram removes one binding; ErrNotFound when there is none.
func (s *Store) UnbindEngram(ctx context.Context, place, engram string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM engram_bindings WHERE place_key = ? AND engram = ?`, place, engram)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchEngramBinding records that a bound engram was spawned in its
// place. An engram not bound there is left alone.
func (s *Store) TouchEngramBinding(ctx context.Context, place, engram string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE engram_bindings SET last_used_at = ? WHERE place_key = ? AND engram = ?`,
		formatExecutionTime(at), place, engram)
	return err
}

// EngramBindings are the bindings of any of the places.
func (s *Store) EngramBindings(ctx context.Context, places []string) ([]EngramBinding, error) {
	if len(places) == 0 {
		return nil, nil
	}
	args := make([]any, len(places))
	for i, p := range places {
		args[i] = p
	}
	return s.engramBindings(ctx, `WHERE place_key IN (?`+strings.Repeat(", ?", len(places)-1)+`)`, args...)
}

// AllEngramBindings are every binding, by place.
func (s *Store) AllEngramBindings(ctx context.Context) ([]EngramBinding, error) {
	return s.engramBindings(ctx, "")
}

func (s *Store) engramBindings(ctx context.Context, where string, args ...any) ([]EngramBinding, error) {
	var rows []engramBindingRow
	if err := s.db.SelectContext(ctx, &rows, `SELECT place_key, engram, is_default, last_used_at, created_at
		FROM engram_bindings `+where+` ORDER BY place_key, engram`, args...); err != nil {
		return nil, err
	}
	out := make([]EngramBinding, len(rows))
	for i, r := range rows {
		out[i] = r.binding()
	}
	return out, nil
}

// SetExecutionEngramFailure records that an execution was asked to start
// as an engram that did not boot, and why.
func (s *Store) SetExecutionEngramFailure(ctx context.Context, executionID, engram, reason string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE executions SET engram_boot_failed = ?, engram_boot_error = ? WHERE id = ?`, engram, reason, executionID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

// EngramBootFailure is a running execution whose engram did not boot.
type EngramBootFailure struct {
	ExecutionID string `db:"id"`
	TaskID      string `db:"task_id"`
	TaskTitle   string `db:"task_title"`
	Engram      string `db:"engram_boot_failed"`
	Reason      string `db:"engram_boot_error"`
}

// ListEngramBootFailures lists the running executions whose engram did not
// boot.
func (s *Store) ListEngramBootFailures(ctx context.Context) ([]EngramBootFailure, error) {
	var out []EngramBootFailure
	err := s.db.SelectContext(ctx, &out, `SELECT id, COALESCE(task_id, '') AS task_id, task_title, engram_boot_failed, engram_boot_error
		FROM executions WHERE engram_boot_failed != '' AND status = ? ORDER BY started_at`, ExecutionRunning)
	return out, err
}
