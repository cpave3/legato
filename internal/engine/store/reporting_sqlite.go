package store

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// ReportRange bounds a reporting query.
type ReportRange struct {
	Start time.Time
	End   time.Time
}

func (r ReportRange) StartUTC() time.Time { return r.Start.UTC() }
func (r ReportRange) EndUTC() time.Time   { return r.End.UTC() }

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

// DirectoryBreakdown holds duration and count metrics for a directory.
// The Directory field is the effective working directory (from the interval or
// the task's swarm working directory as a fallback), used as a proxy for project.
type DirectoryBreakdown struct {
	Directory string
	Working   time.Duration
	Waiting   time.Duration
	TaskCount int
}

// SwarmBreakdown holds aggregated durations and counts for a swarm (parent task
// plus all of its workers' sub-tasks) within a time range.
type SwarmBreakdown struct {
	ParentTaskID  string
	Title         string
	Working       time.Duration
	Waiting       time.Duration
	WallClock     time.Duration // earliest effective_start → latest effective_end across intervals
	ParallelRatio float64       // (Working+Waiting) / WallClock; 0 if WallClock==0
	WorkerCount   int
	SubtaskCount  int
}

// QueryDurations aggregates execution-bounded working/waiting activity within
// the range. Parallel executions are additive; open activity cannot outlive a
// terminal execution.
func (s *Store) QueryDurations(ctx context.Context, tr ReportRange) (DurationResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.state,
			CAST(ROUND(SUM(MAX(0,
				(julianday(MIN(COALESCE(a.ended_at, e.ended_at, datetime('now')), COALESCE(e.ended_at, datetime('now')), ?)) -
				 julianday(MAX(a.started_at, ?))) * 86400
			))) AS INTEGER) AS total_seconds
		FROM execution_activity a
		JOIN executions e ON e.id = a.execution_id
		WHERE a.started_at < ?
		  AND MIN(COALESCE(a.ended_at, e.ended_at, datetime('now')), COALESCE(e.ended_at, datetime('now'))) > ?
		GROUP BY a.state`,
		tr.EndUTC().Format(sqliteDatetime), tr.StartUTC().Format(sqliteDatetime),
		tr.EndUTC().Format(sqliteDatetime), tr.StartUTC().Format(sqliteDatetime))
	if err != nil {
		return DurationResult{}, err
	}
	defer rows.Close()
	var result DurationResult
	for rows.Next() {
		var state string
		var seconds int64
		if err := rows.Scan(&state, &seconds); err != nil {
			return DurationResult{}, err
		}
		if state == ActivityWorking {
			result.Working = time.Duration(seconds) * time.Second
		}
		if state == ActivityWaiting {
			result.Waiting = time.Duration(seconds) * time.Second
		}
	}
	return result, rows.Err()
}

// QueryDailyBreakdown returns per-day duration totals within the time range.
// Every calendar day in the range gets an entry, even if zero.
func (s *Store) QueryDailyBreakdown(ctx context.Context, tr ReportRange) ([]DayEntry, error) {
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
		dayRange := ReportRange{Start: dayStart, End: dayEnd}
		dur, err := s.QueryDurations(ctx, dayRange)
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
func (s *Store) QueryTaskBreakdown(ctx context.Context, tr ReportRange) ([]TaskDuration, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.task_id, a.state,
			CAST(ROUND(SUM(MAX(0,
				(julianday(MIN(COALESCE(a.ended_at, e.ended_at, datetime('now')), COALESCE(e.ended_at, datetime('now')), ?)) -
				 julianday(MAX(a.started_at, ?))) * 86400
			))) AS INTEGER) AS total_seconds
		FROM execution_activity a
		JOIN executions e ON e.id = a.execution_id
		WHERE e.task_id IS NOT NULL AND a.started_at < ?
		  AND MIN(COALESCE(a.ended_at, e.ended_at, datetime('now')), COALESCE(e.ended_at, datetime('now'))) > ?
		GROUP BY e.task_id, a.state`,
		tr.EndUTC().Format(sqliteDatetime), tr.StartUTC().Format(sqliteDatetime),
		tr.EndUTC().Format(sqliteDatetime), tr.StartUTC().Format(sqliteDatetime))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byTask := map[string]*TaskDuration{}
	for rows.Next() {
		var taskID, state string
		var seconds int64
		if err := rows.Scan(&taskID, &state, &seconds); err != nil {
			return nil, err
		}
		if byTask[taskID] == nil {
			byTask[taskID] = &TaskDuration{TaskID: taskID}
		}
		if state == ActivityWorking {
			byTask[taskID].Working = time.Duration(seconds) * time.Second
		}
		if state == ActivityWaiting {
			byTask[taskID].Waiting = time.Duration(seconds) * time.Second
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]TaskDuration, 0, len(byTask))
	for _, item := range byTask {
		result = append(result, *item)
	}
	return result, nil
}

// QueryThroughput counts tasks created, completed, agent sessions started,
// and PRs merged within the time range.
func (s *Store) QueryThroughput(ctx context.Context, tr ReportRange) (Throughput, error) {
	var result Throughput
	startFmt := tr.StartUTC().Format(sqliteDatetime)
	endFmt := tr.EndUTC().Format(sqliteDatetime)

	// Tasks created
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tasks WHERE created_at >= ? AND created_at < ?",
		startFmt, endFmt).Scan(&result.TasksCreated)
	if err != nil {
		return Throughput{}, err
	}

	// Tasks completed (archived)
	err = s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tasks WHERE archived_at >= ? AND archived_at < ?",
		startFmt, endFmt).Scan(&result.TasksCompleted)
	if err != nil {
		return Throughput{}, err
	}

	// Agent sessions started
	err = s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM agent_sessions WHERE started_at >= ? AND started_at < ?",
		startFmt, endFmt).Scan(&result.AgentSessions)
	if err != nil {
		return Throughput{}, err
	}

	// PRs merged — pr_meta is a JSON TEXT column, check state="MERGED" and updated_at in range
	err = s.db.QueryRowContext(ctx, `
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
func (s *Store) QueryWorkspaceBreakdown(ctx context.Context, tr ReportRange) ([]WorkspaceBreakdown, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.workspace_id, COALESCE(w.name, 'Unassigned'), a.state,
			CAST(ROUND(SUM(MAX(0,
				(julianday(MIN(COALESCE(a.ended_at, e.ended_at, datetime('now')), COALESCE(e.ended_at, datetime('now')), ?)) -
				 julianday(MAX(a.started_at, ?))) * 86400))) AS INTEGER),
			COUNT(DISTINCT e.task_id)
		FROM execution_activity a
		JOIN executions e ON e.id=a.execution_id
		JOIN tasks t ON t.id=e.task_id
		LEFT JOIN workspaces w ON w.id=t.workspace_id
		WHERE a.started_at < ? AND MIN(COALESCE(a.ended_at,e.ended_at,datetime('now')),COALESCE(e.ended_at,datetime('now'))) > ?
		GROUP BY t.workspace_id, a.state`, tr.EndUTC().Format(sqliteDatetime), tr.StartUTC().Format(sqliteDatetime), tr.EndUTC().Format(sqliteDatetime), tr.StartUTC().Format(sqliteDatetime))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	key := func(id *int) string {
		if id == nil {
			return "<nil>"
		}
		return fmt.Sprint(*id)
	}
	items := map[string]*WorkspaceBreakdown{}
	for rows.Next() {
		var id *int
		var name, state string
		var seconds int64
		var count int
		if err := rows.Scan(&id, &name, &state, &seconds, &count); err != nil {
			return nil, err
		}
		k := key(id)
		if items[k] == nil {
			items[k] = &WorkspaceBreakdown{WorkspaceID: id, WorkspaceName: name}
		}
		if state == ActivityWorking {
			items[k].Working = time.Duration(seconds) * time.Second
		}
		if state == ActivityWaiting {
			items[k].Waiting = time.Duration(seconds) * time.Second
		}
		if count > items[k].TaskCount {
			items[k].TaskCount = count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]WorkspaceBreakdown, 0, len(items))
	for _, v := range items {
		result = append(result, *v)
	}
	return result, nil
}

// QueryDirectoryBreakdown returns working/waiting durations grouped by working
// directory within the time range. Per-interval working_dir takes precedence;
// if NULL for older rows, falls back to the task's swarm_working_dir.
func (s *Store) QueryDirectoryBreakdown(ctx context.Context, tr ReportRange) ([]DirectoryBreakdown, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(NULLIF(a.working_dir,''), NULLIF(e.working_dir,''), 'Unknown'), a.state,
			CAST(ROUND(SUM(MAX(0,(julianday(MIN(COALESCE(a.ended_at,e.ended_at,datetime('now')),COALESCE(e.ended_at,datetime('now')),?))-julianday(MAX(a.started_at,?)))*86400))) AS INTEGER),
			COUNT(DISTINCT COALESCE(e.task_id,e.id))
		FROM execution_activity a JOIN executions e ON e.id=a.execution_id
		WHERE a.started_at < ? AND MIN(COALESCE(a.ended_at,e.ended_at,datetime('now')),COALESCE(e.ended_at,datetime('now'))) > ?
		GROUP BY 1,a.state`, tr.EndUTC().Format(sqliteDatetime), tr.StartUTC().Format(sqliteDatetime), tr.EndUTC().Format(sqliteDatetime), tr.StartUTC().Format(sqliteDatetime))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := map[string]*DirectoryBreakdown{}
	for rows.Next() {
		var dir, state string
		var seconds int64
		var count int
		if err := rows.Scan(&dir, &state, &seconds, &count); err != nil {
			return nil, err
		}
		if items[dir] == nil {
			items[dir] = &DirectoryBreakdown{Directory: dir}
		}
		if state == ActivityWorking {
			items[dir].Working = time.Duration(seconds) * time.Second
		}
		if state == ActivityWaiting {
			items[dir].Waiting = time.Duration(seconds) * time.Second
		}
		if count > items[dir].TaskCount {
			items[dir].TaskCount = count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]DirectoryBreakdown, 0, len(items))
	for _, v := range items {
		result = append(result, *v)
	}
	return result, nil
}

// QuerySwarmBreakdown returns working/waiting durations aggregated per swarm
// (parent task plus all of its workers' sub-tasks) within the time range.
// Open intervals are clipped to the current time. Swarms whose parent task row
// has been deleted are skipped. Only swarms with non-zero working or waiting
// time in the range are returned.
func (s *Store) QuerySwarmBreakdown(ctx context.Context, tr ReportRange) ([]SwarmBreakdown, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT e.swarm_id,COALESCE(e.swarm_subtask_id,''),a.state,MAX(a.started_at,?),MIN(COALESCE(a.ended_at,e.ended_at,datetime('now')),COALESCE(e.ended_at,datetime('now')),?)
		FROM execution_activity a JOIN executions e ON e.id=a.execution_id
		JOIN tasks parent ON parent.id=e.swarm_id
		WHERE e.swarm_id IS NOT NULL AND a.started_at < ? AND MIN(COALESCE(a.ended_at,e.ended_at,datetime('now')),COALESCE(e.ended_at,datetime('now'))) > ?`, tr.StartUTC().Format(sqliteDatetime), tr.EndUTC().Format(sqliteDatetime), tr.EndUTC().Format(sqliteDatetime), tr.StartUTC().Format(sqliteDatetime))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type agg struct {
		working, waiting time.Duration
		workers          map[string]struct{}
		intervals        []interval
	}
	items := map[string]*agg{}
	for rows.Next() {
		var parent, subtask, state, startRaw, endRaw string
		if err := rows.Scan(&parent, &subtask, &state, &startRaw, &endRaw); err != nil {
			return nil, err
		}
		start, err := time.Parse(sqliteDatetime, startRaw)
		if err != nil {
			return nil, err
		}
		end, err := time.Parse(sqliteDatetime, endRaw)
		if err != nil {
			return nil, err
		}
		if !end.After(start) {
			continue
		}
		if items[parent] == nil {
			items[parent] = &agg{workers: map[string]struct{}{}}
		}
		d := end.Sub(start)
		if state == ActivityWorking {
			items[parent].working += d
		} else if state == ActivityWaiting {
			items[parent].waiting += d
		}
		if subtask != "" {
			items[parent].workers[subtask] = struct{}{}
		}
		items[parent].intervals = append(items[parent].intervals, interval{start: start, end: end})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := []SwarmBreakdown{}
	for parent, a := range items {
		var title string
		_ = s.db.GetContext(ctx, &title, `SELECT title FROM tasks WHERE id=?`, parent)
		var count int
		_ = s.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM swarm_subtasks WHERE parent_task_id=?`, parent)
		wall := unionDuration(a.intervals)
		ratio := 0.0
		if wall > 0 {
			ratio = float64(a.working+a.waiting) / float64(wall)
		}
		result = append(result, SwarmBreakdown{ParentTaskID: parent, Title: title, Working: a.working, Waiting: a.waiting, WallClock: wall, ParallelRatio: ratio, WorkerCount: len(a.workers), SubtaskCount: count})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Working > result[j].Working })
	return result, nil
}

// interval is a time span used for swarm aggregation and union calculations.
type interval struct {
	parentID  string
	state     string
	isSubtask bool
	taskID    string
	start     time.Time
	end       time.Time
}

// unionDuration merges a collection of intervals and returns the total duration
// of their union (overlapping and adjacent intervals are coalesced).
func unionDuration(ivs []interval) time.Duration {
	if len(ivs) == 0 {
		return 0
	}
	ivs2 := make([]interval, len(ivs))
	copy(ivs2, ivs)
	sort.Slice(ivs2, func(i, j int) bool {
		return ivs2[i].start.Before(ivs2[j].start)
	})
	curStart, curEnd := ivs2[0].start, ivs2[0].end
	var total time.Duration
	for i := 1; i < len(ivs2); i++ {
		next := ivs2[i]
		if next.start.After(curEnd) {
			// Disjoint — commit current and start new
			total += curEnd.Sub(curStart)
			curStart, curEnd = next.start, next.end
		} else if next.end.After(curEnd) {
			// Overlapping or adjacent — extend
			curEnd = next.end
		}
	}
	total += curEnd.Sub(curStart)
	return total
}

const sqliteDatetime = "2006-01-02 15:04:05"
