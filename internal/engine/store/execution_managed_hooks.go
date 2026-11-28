package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ManagedHooksOwner is the execution an agent's managed hooks are recorded
// for, and the task slot whose agent it is.
type ManagedHooksOwner struct {
	ExecutionID string
	TaskID      string
	Slot        string
}

// ExecutionManagedHook is one managed hook as an execution's spawn decided
// it: on when Off is empty, else off for every reason in Off.
type ExecutionManagedHook struct {
	Name   string
	Events []string
	Off    []string
}

// ExecutionManagedHookOff is a managed hook someone chose to turn off for
// an execution, and where the choice came from (spawn; later step,
// handoff).
type ExecutionManagedHookOff struct {
	Name   string `db:"name"`
	Source string `db:"source"`
}

// RecordExecutionManagedHooks writes an execution's managed hooks and its
// chosen offs in one transaction, and the execution as its slot's newest
// decided, even when it has no hooks.
func (s *Store) RecordExecutionManagedHooks(ctx context.Context, owner ManagedHooksOwner, hooks []ExecutionManagedHook, offs []ExecutionManagedHookOff) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // a committed transaction ignores it
	executionID := owner.ExecutionID
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO execution_managed_hook_owners (execution_id, task_id, slot) VALUES (?, ?, ?)`,
		executionID, owner.TaskID, slotOrMain(owner.Slot)); err != nil {
		return err
	}
	for _, hook := range hooks {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO execution_managed_hooks (execution_id, name, events, off) VALUES (?, ?, ?, ?)`,
			executionID, hook.Name, strings.Join(hook.Events, ","), strings.Join(hook.Off, " ")); err != nil {
			return err
		}
	}
	for _, off := range offs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO execution_managed_hooks_off (execution_id, name, source) VALUES (?, ?, ?)`,
			executionID, off.Name, off.Source); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ExecutionManagedHooks is what an execution's spawn recorded, sorted by
// name; none for an execution from before managed hooks were recorded.
func (s *Store) ExecutionManagedHooks(ctx context.Context, executionID string) ([]ExecutionManagedHook, []ExecutionManagedHookOff, error) {
	var rows []struct {
		Name   string `db:"name"`
		Events string `db:"events"`
		Off    string `db:"off"`
	}
	if err := s.db.SelectContext(ctx, &rows, `SELECT name, events, off FROM execution_managed_hooks WHERE execution_id = ? ORDER BY name`, executionID); err != nil {
		return nil, nil, err
	}
	hooks := make([]ExecutionManagedHook, 0, len(rows))
	for _, row := range rows {
		hooks = append(hooks, ExecutionManagedHook{Name: row.Name, Events: splitNonEmpty(row.Events, ","), Off: strings.Fields(row.Off)})
	}
	var offs []ExecutionManagedHookOff
	if err := s.db.SelectContext(ctx, &offs, `SELECT name, source FROM execution_managed_hooks_off WHERE execution_id = ? ORDER BY name, source`, executionID); err != nil {
		return nil, nil, err
	}
	return hooks, offs, nil
}

// LatestManagedHooksExecution is the newest execution of a task slot whose
// managed hooks a spawn decided, hooks or none, whether or not its agent
// still runs.
func (s *Store) LatestManagedHooksExecution(ctx context.Context, taskID, slot string) (string, error) {
	var executionID string
	err := s.db.GetContext(ctx, &executionID,
		`SELECT execution_id FROM execution_managed_hook_owners WHERE task_id = ? AND slot = ? ORDER BY recorded_at DESC, rowid DESC LIMIT 1`, taskID, slotOrMain(slot))
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("managed hooks for task %s, slot %s: %w", taskID, slotOrMain(slot), ErrNotFound)
	}
	return executionID, err
}

func splitNonEmpty(text, sep string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(text, sep)
}
