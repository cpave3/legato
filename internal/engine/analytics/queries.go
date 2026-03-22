package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// DurationResult holds aggregated working and waiting durations.
type DurationResult struct {
	Working time.Duration
	Waiting time.Duration
}

// DayEntry holds duration totals for a single calendar day.
type DayEntry struct {
	Date    time.Time
	Working time.Duration
	Waiting time.Duration
}

// TaskDuration holds duration totals for a single task.
type TaskDuration struct {
	TaskID  string
	Working time.Duration
	Waiting time.Duration
}

// Throughput holds task/session/PR counts for a time range.
type Throughput struct {
	TasksCreated   int
	TasksCompleted int
	AgentSessions  int
	PRsMerged      int
}

// WorkspaceBreakdown holds duration and count metrics for a workspace.
type WorkspaceBreakdown struct {
	WorkspaceID   *int
	WorkspaceName string
	Working       time.Duration
	Waiting       time.Duration
	TaskCount     int
}

// QueryDurations aggregates total working/waiting durations from state_intervals
// within the given time range. Intervals that span boundaries are clipped.
// Open intervals use the current time as end.
func QueryDurations(ctx context.Context, db *sqlx.DB, tr TimeRange) (DurationResult, error) {
	// Clip intervals to the range: effective_start = MAX(started_at, range_start),
	// effective_end = MIN(COALESCE(ended_at, now), range_end).
	// Duration = effective_end - effective_start (only if positive).
	rows, err := db.QueryContext(ctx, `
		SELECT state,
			SUM(
				CAST(ROUND(
					MAX(0,
						(julianday(MIN(COALESCE(ended_at, datetime('now')), ?)) -
						 julianday(MAX(started_at, ?))) * 86400
					)) AS INTEGER
				)
			) as total_seconds
		FROM state_intervals
		WHERE started_at < ? AND (ended_at IS NULL OR ended_at > ?)
		GROUP BY state`,
		tr.EndUTC().Format(sqliteDatetime),
		tr.StartUTC().Format(sqliteDatetime),
		tr.EndUTC().Format(sqliteDatetime),
		tr.StartUTC().Format(sqliteDatetime),
	)
	if err != nil {
		return DurationResult{}, err
	}
	defer rows.Close()

	var result DurationResult
	for rows.Next() {
		var state string
		var totalSeconds int64
		if err := rows.Scan(&state, &totalSeconds); err != nil {
			return DurationResult{}, err
		}
		d := time.Duration(totalSeconds) * time.Second
		switch state {
		case "working":
			result.Working = d
		case "waiting":
			result.Waiting = d
		}
	}
	return result, rows.Err()
}

// QueryDailyBreakdown returns per-day duration totals within the time range.
// Every calendar day in the range gets an entry, even if zero.
func QueryDailyBreakdown(ctx context.Context, db *sqlx.DB, tr TimeRange) ([]DayEntry, error) {
	// Query per-day aggregations using date(started_at, 'localtime') would be wrong
	// because intervals can span days. Instead, we iterate days and query each.
	// For simplicity and correctness with range clipping, we use a single query
	// that buckets by the effective start date.

	startDate := time.Date(tr.Start.Year(), tr.Start.Month(), tr.Start.Day(), 0, 0, 0, 0, tr.Start.Location())
	// End is exclusive — last included day is the day containing the last moment before End.
	lastMoment := tr.End.Add(-time.Second)
	endDate := time.Date(lastMoment.Year(), lastMoment.Month(), lastMoment.Day(), 0, 0, 0, 0, lastMoment.Location())

	// Build day list
	var days []DayEntry
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		days = append(days, DayEntry{Date: d})
	}

	// Query each day's clipped durations
	for i := range days {
		dayStart := days[i].Date
		dayEnd := dayStart.AddDate(0, 0, 1)
		// Clip to the overall range
		if dayStart.Before(tr.Start) {
			dayStart = tr.Start
		}
		if dayEnd.After(tr.End) {
			dayEnd = tr.End
		}
		dayRange := TimeRange{Start: dayStart, End: dayEnd}
		dur, err := QueryDurations(ctx, db, dayRange)
		if err != nil {
			return nil, err
		}
		days[i].Working = dur.Working
		days[i].Waiting = dur.Waiting
	}

	return days, nil
}

// QueryTaskBreakdown returns working/waiting durations grouped by task
// within the time range.
func QueryTaskBreakdown(ctx context.Context, db *sqlx.DB, tr TimeRange) ([]TaskDuration, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT task_id, state,
			SUM(
				CAST(ROUND(
					MAX(0,
						(julianday(MIN(COALESCE(ended_at, datetime('now')), ?)) -
						 julianday(MAX(started_at, ?))) * 86400
					)) AS INTEGER
				)
			) as total_seconds
		FROM state_intervals
		WHERE started_at < ? AND (ended_at IS NULL OR ended_at > ?)
		GROUP BY task_id, state`,
		tr.EndUTC().Format(sqliteDatetime),
		tr.StartUTC().Format(sqliteDatetime),
		tr.EndUTC().Format(sqliteDatetime),
		tr.StartUTC().Format(sqliteDatetime),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	taskMap := make(map[string]*TaskDuration)
	for rows.Next() {
		var taskID, state string
		var totalSeconds int64
		if err := rows.Scan(&taskID, &state, &totalSeconds); err != nil {
			return nil, err
		}
		if taskMap[taskID] == nil {
			taskMap[taskID] = &TaskDuration{TaskID: taskID}
		}
		d := time.Duration(totalSeconds) * time.Second
		switch state {
		case "working":
			taskMap[taskID].Working = d
		case "waiting":
			taskMap[taskID].Waiting = d
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]TaskDuration, 0, len(taskMap))
	for _, td := range taskMap {
		result = append(result, *td)
	}
	return result, nil
}

// QueryThroughput counts tasks created, completed, agent sessions started,
// and PRs merged within the time range.
func QueryThroughput(ctx context.Context, db *sqlx.DB, tr TimeRange) (Throughput, error) {
	var result Throughput
	startFmt := tr.StartUTC().Format(sqliteDatetime)
	endFmt := tr.EndUTC().Format(sqliteDatetime)

	// Tasks created
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tasks WHERE created_at >= ? AND created_at < ?",
		startFmt, endFmt).Scan(&result.TasksCreated)
	if err != nil {
		return Throughput{}, err
	}

	// Tasks completed (archived)
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tasks WHERE archived_at >= ? AND archived_at < ?",
		startFmt, endFmt).Scan(&result.TasksCompleted)
	if err != nil {
		return Throughput{}, err
	}

	// Agent sessions started
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM agent_sessions WHERE started_at >= ? AND started_at < ?",
		startFmt, endFmt).Scan(&result.AgentSessions)
	if err != nil {
		return Throughput{}, err
	}

	// PRs merged — pr_meta is a JSON TEXT column, check state="MERGED" and updated_at in range
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM tasks
		WHERE pr_meta IS NOT NULL
		  AND json_extract(pr_meta, '$.state') = 'MERGED'
		  AND datetime(json_extract(pr_meta, '$.updated_at')) >= ?
		  AND datetime(json_extract(pr_meta, '$.updated_at')) < ?`,
		startFmt, endFmt).Scan(&result.PRsMerged)
	if err != nil {
		return Throughput{}, err
	}

	return result, nil
}

// QueryWorkspaceBreakdown returns duration and task count metrics grouped by workspace.
func QueryWorkspaceBreakdown(ctx context.Context, db *sqlx.DB, tr TimeRange) ([]WorkspaceBreakdown, error) {
	startFmt := tr.StartUTC().Format(sqliteDatetime)
	endFmt := tr.EndUTC().Format(sqliteDatetime)

	rows, err := db.QueryContext(ctx, `
		SELECT t.workspace_id, COALESCE(w.name, 'Unassigned') as workspace_name,
			si.state,
			SUM(
				CAST(ROUND(
					MAX(0,
						(julianday(MIN(COALESCE(si.ended_at, datetime('now')), ?)) -
						 julianday(MAX(si.started_at, ?))) * 86400
					)) AS INTEGER
				)
			) as total_seconds
		FROM state_intervals si
		JOIN tasks t ON si.task_id = t.id
		LEFT JOIN workspaces w ON t.workspace_id = w.id
		WHERE si.started_at < ? AND (si.ended_at IS NULL OR si.ended_at > ?)
		GROUP BY t.workspace_id, si.state`,
		endFmt, startFmt, endFmt, startFmt,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Use workspace_id string as map key to avoid name collisions (e.g. user-created "Unassigned")
	wsIDKey := func(id *int) string {
		if id == nil {
			return "<nil>"
		}
		return fmt.Sprintf("%d", *id)
	}

	wsMap := make(map[string]*WorkspaceBreakdown)
	for rows.Next() {
		var wsID *int
		var wsName, state string
		var totalSeconds int64
		if err := rows.Scan(&wsID, &wsName, &state, &totalSeconds); err != nil {
			return nil, err
		}
		key := wsIDKey(wsID)
		if wsMap[key] == nil {
			wsMap[key] = &WorkspaceBreakdown{
				WorkspaceID:   wsID,
				WorkspaceName: wsName,
			}
		}
		d := time.Duration(totalSeconds) * time.Second
		switch state {
		case "working":
			wsMap[key].Working = d
		case "waiting":
			wsMap[key].Waiting = d
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Count distinct tasks per workspace in the range
	countRows, err := db.QueryContext(ctx, `
		SELECT t.workspace_id,
			COUNT(DISTINCT si.task_id) as task_count
		FROM state_intervals si
		JOIN tasks t ON si.task_id = t.id
		LEFT JOIN workspaces w ON t.workspace_id = w.id
		WHERE si.started_at < ? AND (si.ended_at IS NULL OR si.ended_at > ?)
		GROUP BY t.workspace_id`,
		endFmt, startFmt,
	)
	if err != nil {
		return nil, err
	}
	defer countRows.Close()

	for countRows.Next() {
		var wsID *int
		var count int
		if err := countRows.Scan(&wsID, &count); err != nil {
			return nil, err
		}
		key := wsIDKey(wsID)
		if ws := wsMap[key]; ws != nil {
			ws.TaskCount = count
		}
	}
	if err := countRows.Err(); err != nil {
		return nil, err
	}

	result := make([]WorkspaceBreakdown, 0, len(wsMap))
	for _, ws := range wsMap {
		result = append(result, *ws)
	}
	return result, nil
}

const sqliteDatetime = "2006-01-02 15:04:05"
