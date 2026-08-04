package store

import (
	"context"
	"time"
)

type ExecutionBreakdown struct {
	ID            string
	Kind          string
	Status        string
	TaskID        string
	TaskTitle     string
	WorkflowRunID string
	Working       time.Duration
	Waiting       time.Duration
	Error         time.Duration
	WallClock     time.Duration
	StartedAt     time.Time
	EndedAt       *time.Time
}

func (s *Store) QueryExecutionBreakdown(ctx context.Context, tr ReportRange) ([]ExecutionBreakdown, error) {
	type executionRow struct {
		ID            string     `db:"id"`
		Kind          string     `db:"kind"`
		Status        string     `db:"status"`
		TaskID        string     `db:"task_id"`
		TaskTitle     string     `db:"task_title"`
		WorkflowRunID string     `db:"workflow_run_id"`
		Working       int64      `db:"working_seconds"`
		Waiting       int64      `db:"waiting_seconds"`
		Error         int64      `db:"error_seconds"`
		WallClock     int64      `db:"wall_clock_seconds"`
		StartedAt     time.Time  `db:"started_at"`
		EndedAt       *time.Time `db:"ended_at"`
	}
	var rows []executionRow
	err := s.db.SelectContext(ctx, &rows, `
		SELECT e.id, e.kind, e.status, COALESCE(e.task_id, '') AS task_id,
			e.task_title, COALESCE(e.workflow_run_id, '') AS workflow_run_id,
			CAST(ROUND(COALESCE(SUM(CASE WHEN a.state='working' THEN MAX(0,(julianday(MIN(COALESCE(a.ended_at,e.ended_at,datetime('now')),COALESCE(e.ended_at,datetime('now')),?))-julianday(MAX(a.started_at,?)))*86400) ELSE 0 END),0)) AS INTEGER) AS working_seconds,
			CAST(ROUND(COALESCE(SUM(CASE WHEN a.state='waiting' THEN MAX(0,(julianday(MIN(COALESCE(a.ended_at,e.ended_at,datetime('now')),COALESCE(e.ended_at,datetime('now')),?))-julianday(MAX(a.started_at,?)))*86400) ELSE 0 END),0)) AS INTEGER) AS waiting_seconds,
			CAST(ROUND(COALESCE(SUM(CASE WHEN a.state='error' THEN MAX(0,(julianday(MIN(COALESCE(a.ended_at,e.ended_at,datetime('now')),COALESCE(e.ended_at,datetime('now')),?))-julianday(MAX(a.started_at,?)))*86400) ELSE 0 END),0)) AS INTEGER) AS error_seconds,
			CAST(ROUND(MAX(0,(julianday(MIN(COALESCE(e.ended_at,datetime('now')),?))-julianday(MAX(e.started_at,?)))*86400)) AS INTEGER) AS wall_clock_seconds,
			e.started_at, e.ended_at
		FROM executions e LEFT JOIN execution_activity a ON a.execution_id=e.id
		WHERE e.started_at < ? AND COALESCE(e.ended_at,datetime('now')) > ?
		GROUP BY e.id ORDER BY working_seconds DESC`,
		tr.EndUTC().Format(sqliteDatetime), tr.StartUTC().Format(sqliteDatetime),
		tr.EndUTC().Format(sqliteDatetime), tr.StartUTC().Format(sqliteDatetime),
		tr.EndUTC().Format(sqliteDatetime), tr.StartUTC().Format(sqliteDatetime),
		tr.EndUTC().Format(sqliteDatetime), tr.StartUTC().Format(sqliteDatetime),
		tr.EndUTC().Format(sqliteDatetime), tr.StartUTC().Format(sqliteDatetime))
	if err != nil {
		return nil, err
	}
	result := make([]ExecutionBreakdown, len(rows))
	for i, row := range rows {
		result[i] = ExecutionBreakdown{ID: row.ID, Kind: row.Kind, Status: row.Status, TaskID: row.TaskID, TaskTitle: row.TaskTitle, WorkflowRunID: row.WorkflowRunID, Working: time.Duration(row.Working) * time.Second, Waiting: time.Duration(row.Waiting) * time.Second, Error: time.Duration(row.Error) * time.Second, WallClock: time.Duration(row.WallClock) * time.Second, StartedAt: row.StartedAt, EndedAt: row.EndedAt}
	}
	return result, nil
}
