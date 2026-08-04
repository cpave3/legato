package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

// LatestTaskWorkingDir returns the newest non-empty directory recorded by an
// execution attributed to the task.
func (s *Store) LatestTaskWorkingDir(ctx context.Context, taskID string) (string, error) {
	var dir string
	err := s.db.GetContext(ctx, &dir, `
		SELECT COALESCE(a.working_dir, e.working_dir)
		FROM executions e
		LEFT JOIN execution_activity a ON a.execution_id = e.id
		WHERE e.task_id = ?
		  AND COALESCE(a.working_dir, e.working_dir) != ''
		ORDER BY COALESCE(a.started_at, e.started_at) DESC LIMIT 1`, taskID)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return dir, err
}

// GetStateDurations returns execution-bounded activity durations attributed to a task.
func (s *Store) GetStateDurations(ctx context.Context, taskID string) (map[string]time.Duration, error) {
	batch, err := s.GetStateDurationsBatch(ctx, []string{taskID})
	if err != nil {
		return nil, err
	}
	if batch[taskID] == nil {
		return map[string]time.Duration{}, nil
	}
	return batch[taskID], nil
}

// GetStateTimeline returns the majority activity state in each bucket over the window.
func (s *Store) GetStateTimeline(ctx context.Context, taskID string, window time.Duration, buckets int) ([]string, error) {
	if buckets <= 0 {
		return []string{}, nil
	}
	now := time.Now().UTC()
	slotDuration := window / time.Duration(buckets)
	windowStart := now.Add(-window)

	var rows []struct {
		State     string `db:"state"`
		StartedAt int64  `db:"started_at"`
		EndedAt   int64  `db:"ended_at"`
	}
	err := s.db.SelectContext(ctx, &rows, `
		SELECT a.state,
			CAST(strftime('%s', a.started_at) AS INTEGER) AS started_at,
			CAST(strftime('%s', MIN(COALESCE(a.ended_at, e.ended_at, datetime('now')), COALESCE(e.ended_at, datetime('now')))) AS INTEGER) AS ended_at
		FROM execution_activity a
		JOIN executions e ON e.id = a.execution_id
		WHERE e.task_id = ? AND a.started_at < ?
		  AND MIN(COALESCE(a.ended_at, e.ended_at, datetime('now')), COALESCE(e.ended_at, datetime('now'))) > ?
		ORDER BY a.started_at`, taskID, formatExecutionTime(now), formatExecutionTime(windowStart))
	if err != nil {
		return nil, err
	}
	type slotAcc struct{ working, waiting, failed time.Duration }
	acc := make([]slotAcc, buckets)
	for _, row := range rows {
		start := time.Unix(row.StartedAt, 0)
		end := time.Unix(row.EndedAt, 0)
		for i := range acc {
			slotStart := windowStart.Add(time.Duration(i) * slotDuration)
			slotEnd := slotStart.Add(slotDuration)
			effectiveStart, effectiveEnd := start, end
			if effectiveStart.Before(slotStart) {
				effectiveStart = slotStart
			}
			if effectiveEnd.After(slotEnd) {
				effectiveEnd = slotEnd
			}
			if !effectiveEnd.After(effectiveStart) {
				continue
			}
			switch row.State {
			case ActivityWorking:
				acc[i].working += effectiveEnd.Sub(effectiveStart)
			case ActivityWaiting:
				acc[i].waiting += effectiveEnd.Sub(effectiveStart)
			case ActivityError:
				acc[i].failed += effectiveEnd.Sub(effectiveStart)
			}
		}
	}
	result := make([]string, buckets)
	for i := range acc {
		switch {
		case acc[i].failed > 0 && acc[i].failed >= acc[i].waiting && acc[i].failed >= acc[i].working:
			result[i] = ActivityError
		case acc[i].waiting > 0 && acc[i].waiting >= acc[i].working:
			result[i] = ActivityWaiting
		case acc[i].working > 0:
			result[i] = ActivityWorking
		}
	}
	return result, nil
}

func (s *Store) GetStateDurationsBatch(ctx context.Context, taskIDs []string) (map[string]map[string]time.Duration, error) {
	if len(taskIDs) == 0 {
		return map[string]map[string]time.Duration{}, nil
	}
	query, args, err := sqlx.In(`
		SELECT e.task_id, a.state,
			CAST(ROUND(SUM(MAX(0,
				(julianday(MIN(COALESCE(a.ended_at, e.ended_at, datetime('now')), COALESCE(e.ended_at, datetime('now')))) - julianday(a.started_at)) * 86400
			))) AS INTEGER) AS total_seconds
		FROM execution_activity a
		JOIN executions e ON e.id = a.execution_id
		WHERE e.task_id IN (?)
		GROUP BY e.task_id, a.state`, taskIDs)
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
		var seconds int64
		if err := rows.Scan(&taskID, &state, &seconds); err != nil {
			return nil, err
		}
		if result[taskID] == nil {
			result[taskID] = map[string]time.Duration{}
		}
		result[taskID][state] = time.Duration(seconds) * time.Second
	}
	return result, rows.Err()
}
