package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

// RecordStateTransition closes any open interval for the task and opens a new one
// if state is non-empty. Idempotent: if the current open interval already has the
// requested state, no changes are made.
func (s *Store) RecordStateTransition(ctx context.Context, taskID, state string) error {
	// Check for an existing open interval
	var current StateInterval
	err := s.db.GetContext(ctx, &current,
		"SELECT * FROM state_intervals WHERE task_id = ? AND ended_at IS NULL", taskID)

	hasOpen := err == nil
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	// Idempotent: same state already open, nothing to do
	if hasOpen && current.State == state {
		return nil
	}

	// Close the open interval if one exists
	if hasOpen {
		_, err := s.db.ExecContext(ctx,
			"UPDATE state_intervals SET ended_at = datetime('now') WHERE id = ?", current.ID)
		if err != nil {
			return err
		}
	}

	// Open a new interval if the new state is non-empty
	if state != "" {
		_, err := s.db.ExecContext(ctx,
			"INSERT INTO state_intervals (task_id, state) VALUES (?, ?)", taskID, state)
		if err != nil {
			return err
		}
	}

	return nil
}

// GetStateDurations returns aggregated durations per state for a task.
// Open intervals use the current time for their duration.
func (s *Store) GetStateDurations(ctx context.Context, taskID string) (map[string]time.Duration, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT state,
			SUM(
				CAST((julianday(COALESCE(ended_at, datetime('now'))) - julianday(started_at)) * 86400 AS INTEGER)
			) as total_seconds
		FROM state_intervals
		WHERE task_id = ?
		GROUP BY state`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]time.Duration)
	for rows.Next() {
		var state string
		var totalSeconds int64
		if err := rows.Scan(&state, &totalSeconds); err != nil {
			return nil, err
		}
		result[state] = time.Duration(totalSeconds) * time.Second
	}
	return result, rows.Err()
}

// GetStateDurationsBatch returns aggregated durations for multiple tasks in a single query.
// Returns map[taskID]map[state]duration.
func (s *Store) GetStateDurationsBatch(ctx context.Context, taskIDs []string) (map[string]map[string]time.Duration, error) {
	if len(taskIDs) == 0 {
		return make(map[string]map[string]time.Duration), nil
	}

	query, args, err := sqlx.In(`
		SELECT task_id, state,
			SUM(
				CAST((julianday(COALESCE(ended_at, datetime('now'))) - julianday(started_at)) * 86400 AS INTEGER)
			) as total_seconds
		FROM state_intervals
		WHERE task_id IN (?)
		GROUP BY task_id, state`, taskIDs)
	if err != nil {
		return nil, err
	}
	query = s.db.Rebind(query)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]map[string]time.Duration)
	for rows.Next() {
		var taskID, state string
		var totalSeconds int64
		if err := rows.Scan(&taskID, &state, &totalSeconds); err != nil {
			return nil, err
		}
		if result[taskID] == nil {
			result[taskID] = make(map[string]time.Duration)
		}
		result[taskID][state] = time.Duration(totalSeconds) * time.Second
	}
	return result, rows.Err()
}
