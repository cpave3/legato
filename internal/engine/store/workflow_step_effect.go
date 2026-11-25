package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// States of a WorkflowStepEffect.
const (
	// WorkflowEffectPending: an attempt began to make it and did not learn
	// whether it did.
	WorkflowEffectPending = "pending"
	// WorkflowEffectDone: it is made, and Key, RemoteID and URL name it.
	WorkflowEffectDone = "done"
)

// WorkflowStepEffect is something a step instance made outside Legato, such
// as a ticket, kept by run, step path and effect so that every attempt of
// the step finds it (LEG-640).
type WorkflowStepEffect struct {
	RunID     string
	Path      string
	Effect    string
	AttemptID string
	State     string
	Provider  string
	Target    string
	Summary   string
	Key       string
	RemoteID  string
	URL       string
	StartedAt time.Time
	UpdatedAt time.Time
}

type stepEffectRow struct {
	RunID     string `db:"run_id"`
	Path      string `db:"path"`
	Effect    string `db:"effect"`
	AttemptID string `db:"attempt_id"`
	State     string `db:"state"`
	Provider  string `db:"provider"`
	Target    string `db:"target"`
	Summary   string `db:"summary"`
	Key       string `db:"key"`
	RemoteID  string `db:"remote_id"`
	URL       string `db:"url"`
	StartedAt string `db:"started_at"`
	UpdatedAt string `db:"updated_at"`
}

func (r stepEffectRow) effect() WorkflowStepEffect {
	started, _ := time.Parse(time.RFC3339Nano, r.StartedAt)
	updated, _ := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	return WorkflowStepEffect{RunID: r.RunID, Path: r.Path, Effect: r.Effect, AttemptID: r.AttemptID, State: r.State,
		Provider: r.Provider, Target: r.Target, Summary: r.Summary, Key: r.Key, RemoteID: r.RemoteID, URL: r.URL,
		StartedAt: started, UpdatedAt: updated}
}

// BeginWorkflowStepEffect records a pending effect, unless the step already
// has a record of it: then it returns that one and begun is false.
func (s *Store) BeginWorkflowStepEffect(ctx context.Context, e WorkflowStepEffect) (WorkflowStepEffect, bool, error) {
	if e.StartedAt.IsZero() {
		e.StartedAt = time.Now()
	}
	stamp := e.StartedAt.UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `INSERT INTO workflow_step_effects
		(run_id, path, effect, attempt_id, state, provider, target, summary, started_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (run_id, path, effect) DO NOTHING`,
		e.RunID, e.Path, e.Effect, e.AttemptID, WorkflowEffectPending, e.Provider, e.Target, e.Summary, stamp, stamp)
	if err != nil {
		return WorkflowStepEffect{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return WorkflowStepEffect{}, false, err
	}
	got, err := s.GetWorkflowStepEffect(ctx, e.RunID, e.Path, e.Effect)
	if err != nil {
		return WorkflowStepEffect{}, false, err
	}
	if got == nil {
		return WorkflowStepEffect{}, false, errors.New("workflow step effect: the record vanished")
	}
	return *got, rows == 1, nil
}

// GetWorkflowStepEffect is a step's record of an effect, or nil.
func (s *Store) GetWorkflowStepEffect(ctx context.Context, runID, path, effect string) (*WorkflowStepEffect, error) {
	var row stepEffectRow
	err := s.db.GetContext(ctx, &row, `SELECT run_id, path, effect, attempt_id, state, provider, target, summary, key, remote_id, url,
		started_at, updated_at FROM workflow_step_effects WHERE run_id = ? AND path = ? AND effect = ?`, runID, path, effect)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e := row.effect()
	return &e, nil
}

// FinishWorkflowStepEffect records that the effect is made, and what it is.
func (s *Store) FinishWorkflowStepEffect(ctx context.Context, runID, path, effect, key, remoteID, url string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE workflow_step_effects SET state = ?, key = ?, remote_id = ?, url = ?, updated_at = ?
		WHERE run_id = ? AND path = ? AND effect = ?`,
		WorkflowEffectDone, key, remoteID, url, time.Now().UTC().Format(time.RFC3339Nano), runID, path, effect)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		if err != nil {
			return err
		}
		return ErrWorkflowStateConflict
	}
	return nil
}

// DropWorkflowStepEffect removes a step's record of an effect: the attempt
// learned it made nothing.
func (s *Store) DropWorkflowStepEffect(ctx context.Context, runID, path, effect string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM workflow_step_effects WHERE run_id = ? AND path = ? AND effect = ?`, runID, path, effect)
	return err
}
