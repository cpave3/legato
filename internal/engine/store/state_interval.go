package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

// LatestTaskWorkingDir returns the newest non-empty repository directory
// recorded for a task's agent activity.
func (s *Store) LatestTaskWorkingDir(ctx context.Context, taskID string) (string, error) {
	var dir string
	err := s.db.GetContext(ctx, &dir, `
		SELECT working_dir FROM state_intervals
		WHERE task_id = ? AND working_dir IS NOT NULL AND working_dir != ''
		ORDER BY id DESC LIMIT 1`, taskID)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return dir, err
}

func (s *Store) RecordStateTransition(ctx context.Context, taskID, state, workingDir string) error {
	var current StateInterval
	err := s.db.GetContext(ctx, &current,
		"SELECT * FROM state_intervals WHERE task_id = ? AND ended_at IS NULL", taskID)

	hasOpen := err == nil
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	if hasOpen && current.State == state {
		return nil
	}

	if hasOpen {
		_, err := s.db.ExecContext(ctx,
			"UPDATE state_intervals SET ended_at = datetime('now') WHERE id = ?", current.ID)
		if err != nil {
			return err
		}
	}

	if state != "" {
		if workingDir != "" {
			_, err := s.db.ExecContext(ctx,
				"INSERT INTO state_intervals (task_id, state, working_dir) VALUES (?, ?, ?)",
				taskID, state, workingDir)
			if err != nil {
				return err
			}
		} else {
			_, err := s.db.ExecContext(ctx,
				"INSERT INTO state_intervals (task_id, state) VALUES (?, ?)", taskID, state)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

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

// GetStateTimeline returns a per-bucket state label sequence over the last
// window. The task ID may be either a tasks.id or a swarm_subtasks.id —
// state_intervals lost its FK in migration 021 so both are valid lookups.
func (s *Store) GetStateTimeline(ctx context.Context, taskID string, window time.Duration, buckets int) ([]string, error) {
	if buckets <= 0 {
		return []string{}, nil
	}

	now := time.Now().UTC()
	slotDuration := window / time.Duration(buckets)

	var rows []struct {
		State     string `db:"state"`
		StartedAt int64  `db:"started_at"`
		EndedAt   *int64 `db:"ended_at"`
	}
	err := s.db.SelectContext(ctx, &rows, `
		SELECT state,
			CAST(strftime('%s', started_at) AS INTEGER) as started_at,
			CAST(strftime('%s', ended_at) AS INTEGER) as ended_at
		FROM state_intervals
		WHERE task_id = ?
		ORDER BY started_at ASC`, taskID)
	if err != nil {
		return nil, err
	}

	type slotAcc struct {
		working time.Duration
		waiting time.Duration
	}
	acc := make([]slotAcc, buckets)

	nowUnix := now.Unix()
	for _, r := range rows {
		var ended int64
		if r.EndedAt != nil {
			ended = *r.EndedAt
		} else {
			ended = nowUnix
		}

		for b := 0; b < buckets; b++ {
			windowStart := nowUnix - int64(window.Seconds())
			slotStart := windowStart + int64(b)*int64(slotDuration.Seconds())
			slotEnd := slotStart + int64(slotDuration.Seconds())

			// Interval is entirely before window
			if ended <= windowStart {
				continue
			}
			// Interval is entirely after window
			if r.StartedAt >= nowUnix {
				continue
			}

			interStart := r.StartedAt
			if interStart < slotStart {
				interStart = slotStart
			}
			interEnd := ended
			if interEnd > slotEnd {
				interEnd = slotEnd
			}
			if interEnd <= interStart {
				continue
			}

			dur := time.Duration(interEnd-interStart) * time.Second
			switch r.State {
			case "working":
				acc[b].working += dur
			case "waiting":
				acc[b].waiting += dur
			}
		}
	}

	timeline := make([]string, buckets)
	for i := 0; i < buckets; i++ {
		state := ""
		if acc[i].working > acc[i].waiting {
			state = "working"
		} else if acc[i].waiting > acc[i].working {
			state = "waiting"
		}
		timeline[i] = state
	}

	return timeline, nil
}

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
