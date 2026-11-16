package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Sub-agent coverage of a total: whether every execution in it had an agent
// tool that reports its sub-agents.
const (
	// SubagentCoverageComplete: every execution reported its sub-agents.
	SubagentCoverageComplete = "complete"
	// SubagentCoveragePartial: some did; the total misses the others'.
	SubagentCoveragePartial = "partial"
	// SubagentCoverageNone: none did (an older execution, or Codex): the
	// total is the top-level agents' own time only.
	SubagentCoverageNone = "none"
)

// RecordSubagentActivityAt moves one sub-agent of a running execution to a
// state at a moment: working or waiting opens its span (or keeps it, in that
// state), "" ends it. A sub-agent is started under parentID, the sub-agent
// that started it ("" for the top-level agent, or when unknown); a later
// report keeps the parent it was started under. Ending a sub-agent with no
// open span does nothing.
func (s *Store) RecordSubagentActivityAt(ctx context.Context, executionID, subagentID, parentID, state string, at time.Time) error {
	if subagentID == "" {
		return errors.New("sub-agent id is required")
	}
	if state != "" && state != ActivityWorking && state != ActivityWaiting {
		return fmt.Errorf("invalid sub-agent activity %q", state)
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireRunningExecution(ctx, tx, executionID); err != nil {
		return err
	}
	var current struct {
		ID     int64  `db:"id"`
		State  string `db:"state"`
		Parent string `db:"parent_subagent_id"`
	}
	err = tx.GetContext(ctx, &current, `SELECT id, state, parent_subagent_id FROM subagent_activity
		WHERE execution_id = ? AND subagent_id = ? AND ended_at IS NULL`, executionID, subagentID)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	open := err == nil
	if open && current.State == state {
		return tx.Commit()
	}
	stamp := formatExecutionTime(at)
	if open {
		parentID = current.Parent
		if _, err := tx.ExecContext(ctx, `UPDATE subagent_activity SET ended_at = ? WHERE id = ?`, stamp, current.ID); err != nil {
			return err
		}
	}
	if state != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO subagent_activity
			(execution_id, subagent_id, parent_subagent_id, state, started_at) VALUES (?, ?, ?, ?, ?)`,
			executionID, subagentID, parentID, state, stamp); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// EndSubagentsAt ends the open span of every sub-agent of a running execution
// but those in keep: the sub-agents the agent tool says still run.
func (s *Store) EndSubagentsAt(ctx context.Context, executionID string, keep []string, at time.Time) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireRunningExecution(ctx, tx, executionID); err != nil {
		return err
	}
	kept := make(map[string]bool, len(keep))
	for _, id := range keep {
		kept[id] = true
	}
	var open []struct {
		ID       int64  `db:"id"`
		Subagent string `db:"subagent_id"`
	}
	if err := tx.SelectContext(ctx, &open, `SELECT id, subagent_id FROM subagent_activity
		WHERE execution_id = ? AND ended_at IS NULL`, executionID); err != nil {
		return err
	}
	stamp := formatExecutionTime(at)
	for _, span := range open {
		if kept[span.Subagent] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE subagent_activity SET ended_at = ? WHERE id = ?`, stamp, span.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// OpenSubagents lists the sub-agents of an execution with an open span.
func (s *Store) OpenSubagents(ctx context.Context, executionID string) ([]string, error) {
	var ids []string
	err := s.db.SelectContext(ctx, &ids, `SELECT subagent_id FROM subagent_activity
		WHERE execution_id = ? AND ended_at IS NULL ORDER BY started_at, id`, executionID)
	return ids, err
}

// SubagentParents maps each sub-agent of an execution to the sub-agent that
// started it ("" for the top-level agent, or when unknown).
func (s *Store) SubagentParents(ctx context.Context, executionID string) (map[string]string, error) {
	var rows []struct {
		Subagent string `db:"subagent_id"`
		Parent   string `db:"parent_subagent_id"`
	}
	if err := s.db.SelectContext(ctx, &rows, `SELECT subagent_id, MAX(parent_subagent_id) AS parent_subagent_id
		FROM subagent_activity WHERE execution_id = ? GROUP BY subagent_id`, executionID); err != nil {
		return nil, err
	}
	parents := make(map[string]string, len(rows))
	for _, row := range rows {
		parents[row.Subagent] = row.Parent
	}
	return parents, nil
}

type querier interface {
	GetContext(ctx context.Context, dest any, query string, args ...any) error
}

func requireRunningExecution(ctx context.Context, q querier, executionID string) error {
	var status string
	if err := q.GetContext(ctx, &status, `SELECT status FROM executions WHERE id = ?`, executionID); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if status != ExecutionRunning {
		return ErrExecutionTerminal
	}
	return nil
}

// AgentTime is agent time over whole agent trees: the top-level agents (the
// executions) and the sub-agents they ran, at any depth.
type AgentTime struct {
	// Own is the top-level agents' own working, waiting and error time.
	Own DurationResult
	// Subagents is their sub-agents' working and waiting time, each
	// sub-agent's own spans counted once, whatever its parent was doing.
	Subagents DurationResult
	// SubagentCount is how many sub-agents had a span in the range.
	SubagentCount int
	// Elapsed is wall-clock time during which any agent of the tree was
	// working, waiting or in error: agents active at once count once. Idle
	// and background time of the top-level agent is not in it.
	Elapsed time.Duration
	// Executions is how many executions had a span in the range, and
	// ExecutionsWithSubagentTelemetry how many of those report sub-agents.
	Executions                      int
	ExecutionsWithSubagentTelemetry int
}

// AggregateWorking is the working time of every agent in the tree added
// together: agents working at once each count.
func (a AgentTime) AggregateWorking() time.Duration { return a.Own.Working + a.Subagents.Working }

// SubagentCoverage says whether the sub-agent time is the whole tree's:
// SubagentCoverageComplete, SubagentCoveragePartial, SubagentCoverageNone,
// or "" when no execution had a span.
func (a AgentTime) SubagentCoverage() string {
	switch {
	case a.Executions == 0:
		return ""
	case a.ExecutionsWithSubagentTelemetry == a.Executions:
		return SubagentCoverageComplete
	case a.ExecutionsWithSubagentTelemetry == 0:
		return SubagentCoverageNone
	default:
		return SubagentCoveragePartial
	}
}

// AgentTimeReport is agent time in a range: in all, by attribution task, and
// by execution.
type AgentTimeReport struct {
	Total       AgentTime
	ByTask      map[string]AgentTime
	ByExecution map[string]AgentTime
}

// QueryAgentTime sums the top-level agents' and their sub-agents' spans in
// a range, each clipped to the range and to its execution's end (an open
// span runs to now). See AgentTime for what each figure means.
func (s *Store) QueryAgentTime(ctx context.Context, tr ReportRange) (AgentTimeReport, error) {
	type spanRow struct {
		ExecutionID string  `db:"execution_id"`
		TaskID      string  `db:"task_id"`
		Telemetry   bool    `db:"subagent_telemetry"`
		Subagent    string  `db:"subagent_id"`
		Sub         bool    `db:"sub"`
		State       string  `db:"state"`
		StartedAt   string  `db:"started_at"`
		EndedAt     *string `db:"ended_at"`
		ExecutionAt *string `db:"execution_ended_at"`
	}
	start, end := tr.StartUTC().Format(executionTimestamp), tr.EndUTC().Format(executionTimestamp)
	var rows []spanRow
	if err := s.db.SelectContext(ctx, &rows, `
		SELECT e.id AS execution_id, COALESCE(e.task_id, '') AS task_id, e.subagent_telemetry,
			'' AS subagent_id, 0 AS sub, a.state, CAST(a.started_at AS TEXT) AS started_at,
			CAST(a.ended_at AS TEXT) AS ended_at, CAST(e.ended_at AS TEXT) AS execution_ended_at
		FROM execution_activity a JOIN executions e ON e.id = a.execution_id
		WHERE a.started_at < ? AND (COALESCE(a.ended_at, e.ended_at) IS NULL OR COALESCE(a.ended_at, e.ended_at) > ?)
		UNION ALL
		SELECT e.id, COALESCE(e.task_id, ''), e.subagent_telemetry,
			s.subagent_id, 1, s.state, CAST(s.started_at AS TEXT),
			CAST(s.ended_at AS TEXT), CAST(e.ended_at AS TEXT)
		FROM subagent_activity s JOIN executions e ON e.id = s.execution_id
		WHERE s.started_at < ? AND (COALESCE(s.ended_at, e.ended_at) IS NULL OR COALESCE(s.ended_at, e.ended_at) > ?)`,
		end, start, end, start); err != nil {
		return AgentTimeReport{}, err
	}

	now := time.Now().UTC()
	type tally struct {
		time       AgentTime
		spans      []interval
		subagents  map[string]bool
		executions map[string]bool
	}
	newTally := func() *tally {
		return &tally{subagents: map[string]bool{}, executions: map[string]bool{}}
	}
	total := newTally()
	byTask := map[string]*tally{}
	byExecution := map[string]*tally{}
	for _, row := range rows {
		span, ok, err := clipSpan(row.StartedAt, row.EndedAt, row.ExecutionAt, tr, now)
		if err != nil {
			return AgentTimeReport{}, err
		}
		if !ok {
			continue
		}
		tallies := []*tally{total}
		if byExecution[row.ExecutionID] == nil {
			byExecution[row.ExecutionID] = newTally()
		}
		tallies = append(tallies, byExecution[row.ExecutionID])
		if row.TaskID != "" {
			if byTask[row.TaskID] == nil {
				byTask[row.TaskID] = newTally()
			}
			tallies = append(tallies, byTask[row.TaskID])
		}
		d := span.end.Sub(span.start)
		for _, t := range tallies {
			sums := &t.time.Own
			if row.Sub {
				sums = &t.time.Subagents
				t.subagents[row.ExecutionID+"\x00"+row.Subagent] = true
			}
			switch row.State {
			case ActivityWorking:
				sums.Working += d
			case ActivityWaiting:
				sums.Waiting += d
			case ActivityError:
				sums.Error += d
			}
			t.spans = append(t.spans, span)
			if !t.executions[row.ExecutionID] {
				t.executions[row.ExecutionID] = true
				t.time.Executions++
				if row.Telemetry {
					t.time.ExecutionsWithSubagentTelemetry++
				}
			}
		}
	}
	finish := func(t *tally) AgentTime {
		at := t.time
		at.SubagentCount = len(t.subagents)
		at.Elapsed = unionDuration(t.spans).Round(time.Second)
		for _, d := range []*time.Duration{&at.Own.Working, &at.Own.Waiting, &at.Own.Error, &at.Subagents.Working, &at.Subagents.Waiting, &at.Subagents.Error} {
			*d = d.Round(time.Second)
		}
		return at
	}
	report := AgentTimeReport{Total: finish(total), ByTask: map[string]AgentTime{}, ByExecution: map[string]AgentTime{}}
	for id, t := range byTask {
		report.ByTask[id] = finish(t)
	}
	for id, t := range byExecution {
		report.ByExecution[id] = finish(t)
	}
	return report, nil
}

// clipSpan is a span clipped to a range and to its execution's end; an open
// span of a running execution runs to now. ok is false when nothing is left.
func clipSpan(startedAt string, endedAt, executionEndedAt *string, tr ReportRange, now time.Time) (interval, bool, error) {
	start, err := parseStoredTime(startedAt)
	if err != nil {
		return interval{}, false, err
	}
	end := now
	if executionEndedAt != nil {
		executionEnd, err := parseStoredTime(*executionEndedAt)
		if err != nil {
			return interval{}, false, err
		}
		end = executionEnd
	}
	if endedAt != nil {
		spanEnd, err := parseStoredTime(*endedAt)
		if err != nil {
			return interval{}, false, err
		}
		if spanEnd.Before(end) {
			end = spanEnd
		}
	}
	if start.Before(tr.StartUTC()) {
		start = tr.StartUTC()
	}
	if end.After(tr.EndUTC()) {
		end = tr.EndUTC()
	}
	return interval{start: start, end: end}, end.After(start), nil
}

// storedTimeLayouts are the ways a timestamp is written in the database: as
// Legato writes it, and as SQLite's datetime() and the driver do.
var storedTimeLayouts = []string{executionTimestamp, time.RFC3339Nano, "2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05Z07:00", "2006-01-02 15:04:05.999999999-07:00"}

func parseStoredTime(value string) (time.Time, error) {
	for _, layout := range storedTimeLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("reading the time %q", value)
}
