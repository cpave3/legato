package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// treeExecution starts a running execution of a task whose agent tool
// reports its sub-agents.
func treeExecution(t *testing.T, s *Store, id, taskID string, started time.Time) {
	t.Helper()
	execution := executionAt(id, taskID, started)
	execution.SubagentTelemetry = true
	if err := s.CreateExecution(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
}

func mustActivity(t *testing.T, s *Store, executionID, state string, at time.Time) {
	t.Helper()
	if err := s.RecordExecutionActivityAt(context.Background(), executionID, state, "", at); err != nil {
		t.Fatal(err)
	}
}

func mustSubagent(t *testing.T, s *Store, executionID, subagentID, parentID, state string, at time.Time) {
	t.Helper()
	if err := s.RecordSubagentActivityAt(context.Background(), executionID, subagentID, parentID, state, at); err != nil {
		t.Fatal(err)
	}
}

func agentTime(t *testing.T, s *Store, start, end time.Time) AgentTimeReport {
	t.Helper()
	report, err := s.QueryAgentTime(context.Background(), ReportRange{Start: start, End: end})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

var subagentNames = []string{"sub-1", "sub-2", "sub-3", "sub-4", "sub-5"}

func TestAgentTime_ParentAndFiveSubagentsMakeSixtyWorkingAgentMinutes(t *testing.T) {
	s := newTestStore(t)
	t0 := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	treeExecution(t, s, "exec-1", "task-1", t0)
	// The parent works 10 minutes, then waits in the background while its
	// five sub-agents work 10 minutes each, one after another.
	mustActivity(t, s, "exec-1", ActivityWorking, t0)
	mustActivity(t, s, "exec-1", "", t0.Add(10*time.Minute))
	for i, name := range subagentNames {
		start := t0.Add(time.Duration(10+10*i) * time.Minute)
		mustSubagent(t, s, "exec-1", name, "", ActivityWorking, start)
		mustSubagent(t, s, "exec-1", name, "", "", start.Add(10*time.Minute))
	}
	if err := s.TerminalizeExecutionAt(context.Background(), "exec-1", ExecutionSucceeded, t0.Add(70*time.Minute)); err != nil {
		t.Fatal(err)
	}

	report := agentTime(t, s, t0.Add(-time.Hour), t0.Add(2*time.Hour))

	task := report.ByTask["task-1"]
	if task.Own.Working != 10*time.Minute || task.Subagents.Working != 50*time.Minute {
		t.Fatalf("own %v, sub-agents %v; want 10m and 50m", task.Own.Working, task.Subagents.Working)
	}
	if got := task.AggregateWorking(); got != 60*time.Minute {
		t.Fatalf("aggregate working = %v, want 60m", got)
	}
	if task.Elapsed != 60*time.Minute {
		t.Fatalf("elapsed = %v, want 60m: nothing ran at once", task.Elapsed)
	}
	if task.SubagentCount != 5 || task.SubagentCoverage() != SubagentCoverageComplete {
		t.Fatalf("sub-agents = %d, coverage %q; want 5, complete", task.SubagentCount, task.SubagentCoverage())
	}
	if report.Total.AggregateWorking() != 60*time.Minute {
		t.Fatalf("total aggregate = %v, want 60m", report.Total.AggregateWorking())
	}
}

func TestAgentTime_ConcurrentSubagentsAddAgentTimeButNotElapsedTime(t *testing.T) {
	s := newTestStore(t)
	t0 := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	treeExecution(t, s, "exec-1", "task-1", t0)
	// The parent works 5 minutes and goes idle; five sub-agents then work
	// together from 10 to 20 minutes.
	mustActivity(t, s, "exec-1", ActivityWorking, t0)
	mustActivity(t, s, "exec-1", "", t0.Add(5*time.Minute))
	for _, name := range subagentNames {
		mustSubagent(t, s, "exec-1", name, "", ActivityWorking, t0.Add(10*time.Minute))
	}
	for _, name := range subagentNames {
		mustSubagent(t, s, "exec-1", name, "", "", t0.Add(20*time.Minute))
	}

	window := agentTime(t, s, t0.Add(10*time.Minute), t0.Add(20*time.Minute)).ByTask["task-1"]

	if window.AggregateWorking() != 50*time.Minute || window.Elapsed != 10*time.Minute {
		t.Fatalf("window: aggregate %v, elapsed %v; want 50m and 10m", window.AggregateWorking(), window.Elapsed)
	}
	if window.Own.Working != 0 {
		t.Fatalf("own working in the window = %v, want 0: the parent was idle", window.Own.Working)
	}

	whole := agentTime(t, s, t0, t0.Add(time.Hour)).ByTask["task-1"]
	if whole.AggregateWorking() != 55*time.Minute || whole.Elapsed != 15*time.Minute {
		t.Fatalf("whole: aggregate %v, elapsed %v; want 55m and 15m", whole.AggregateWorking(), whole.Elapsed)
	}
}

func TestAgentTime_ANestedSubagentCountsItsOwnTimeOnce(t *testing.T) {
	s := newTestStore(t)
	t0 := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	treeExecution(t, s, "exec-1", "task-1", t0)
	mustSubagent(t, s, "exec-1", "outer", "", ActivityWorking, t0)
	mustSubagent(t, s, "exec-1", "inner", "outer", ActivityWorking, t0.Add(2*time.Minute))
	mustSubagent(t, s, "exec-1", "inner", "", "", t0.Add(8*time.Minute))
	mustSubagent(t, s, "exec-1", "outer", "", "", t0.Add(10*time.Minute))

	task := agentTime(t, s, t0, t0.Add(time.Hour)).ByTask["task-1"]

	// The outer sub-agent's 10 minutes and the inner one's 6: the inner one
	// running inside the outer one adds no time to it.
	if task.Subagents.Working != 16*time.Minute || task.Elapsed != 10*time.Minute || task.SubagentCount != 2 {
		t.Fatalf("sub-agents %v, elapsed %v, count %d; want 16m, 10m, 2", task.Subagents.Working, task.Elapsed, task.SubagentCount)
	}
	parents, err := s.SubagentParents(context.Background(), "exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if parents["inner"] != "outer" || parents["outer"] != "" {
		t.Fatalf("parents = %v, want inner under outer", parents)
	}
}

func TestAgentTime_ASubagentWaitingOnTheOperatorIsNotWorking(t *testing.T) {
	s := newTestStore(t)
	t0 := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	treeExecution(t, s, "exec-1", "task-1", t0)
	mustSubagent(t, s, "exec-1", "sub-1", "", ActivityWorking, t0)
	mustSubagent(t, s, "exec-1", "sub-1", "", ActivityWaiting, t0.Add(3*time.Minute))
	mustSubagent(t, s, "exec-1", "sub-1", "", ActivityWorking, t0.Add(7*time.Minute))
	mustSubagent(t, s, "exec-1", "sub-1", "", "", t0.Add(9*time.Minute))

	task := agentTime(t, s, t0, t0.Add(time.Hour)).ByTask["task-1"]

	if task.Subagents.Working != 5*time.Minute || task.Subagents.Waiting != 4*time.Minute || task.SubagentCount != 1 {
		t.Fatalf("sub-agent working %v, waiting %v, count %d; want 5m, 4m, 1", task.Subagents.Working, task.Subagents.Waiting, task.SubagentCount)
	}
}

func TestSubagentActivity_EndsWithItsExecution(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	treeExecution(t, s, "exec-1", "task-1", t0)
	mustSubagent(t, s, "exec-1", "sub-1", "", ActivityWorking, t0)
	if err := s.TerminalizeExecutionAt(ctx, "exec-1", ExecutionCancelled, t0.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}

	open, err := s.OpenSubagents(ctx, "exec-1")
	if err != nil || len(open) != 0 {
		t.Fatalf("open sub-agents = %v (%v), want none once the execution ended", open, err)
	}
	if err := s.RecordSubagentActivityAt(ctx, "exec-1", "sub-1", "", ActivityWorking, t0.Add(5*time.Minute)); !errors.Is(err, ErrExecutionTerminal) {
		t.Fatalf("late sub-agent report: %v, want ErrExecutionTerminal", err)
	}
	if got := agentTime(t, s, t0, t0.Add(time.Hour)).ByTask["task-1"].Subagents.Working; got != 4*time.Minute {
		t.Fatalf("sub-agent working = %v, want 4m", got)
	}
}

func TestSubagentActivity_EndingAllButTheOnesStillRunning(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	treeExecution(t, s, "exec-1", "task-1", t0)
	mustSubagent(t, s, "exec-1", "foreground", "", ActivityWorking, t0)
	mustSubagent(t, s, "exec-1", "background", "", ActivityWorking, t0)

	if err := s.EndSubagentsAt(ctx, "exec-1", []string{"background"}, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	open, err := s.OpenSubagents(ctx, "exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0] != "background" {
		t.Fatalf("open = %v, want only the one still running", open)
	}
}

func TestAgentTime_ExecutionsWithoutSubagentTelemetryAreNotComplete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	// A Codex agent (or one from before sub-agents were recorded) and a
	// Claude Code agent on one task.
	if err := s.CreateExecution(ctx, executionAt("exec-old", "task-1", t0)); err != nil {
		t.Fatal(err)
	}
	mustActivity(t, s, "exec-old", ActivityWorking, t0)
	treeExecution(t, s, "exec-new", "task-1", t0)
	mustActivity(t, s, "exec-new", ActivityWorking, t0)
	if err := s.CreateExecution(ctx, executionAt("exec-other", "task-2", t0)); err != nil {
		t.Fatal(err)
	}
	mustActivity(t, s, "exec-other", ActivityWorking, t0)
	end := t0.Add(10 * time.Minute)
	for _, id := range []string{"exec-old", "exec-new", "exec-other"} {
		if err := s.TerminalizeExecutionAt(ctx, id, ExecutionSucceeded, end); err != nil {
			t.Fatal(err)
		}
	}

	report := agentTime(t, s, t0, t0.Add(time.Hour))

	if got := report.ByTask["task-1"].SubagentCoverage(); got != SubagentCoveragePartial {
		t.Fatalf("task-1 coverage = %q, want partial", got)
	}
	if got := report.ByTask["task-2"].SubagentCoverage(); got != SubagentCoverageNone {
		t.Fatalf("task-2 coverage = %q, want none", got)
	}
	if got := report.Total.SubagentCoverage(); got != SubagentCoveragePartial {
		t.Fatalf("total coverage = %q, want partial", got)
	}
	if report.ByExecution["exec-new"].SubagentCoverage() != SubagentCoverageComplete || report.ByExecution["exec-old"].SubagentCoverage() != SubagentCoverageNone {
		t.Fatalf("by execution = %+v", report.ByExecution)
	}
	// Two agents working the same 10 minutes: 20 agent-minutes, 10 elapsed.
	if got := report.ByTask["task-1"]; got.AggregateWorking() != 20*time.Minute || got.Elapsed != 10*time.Minute {
		t.Fatalf("task-1 aggregate %v, elapsed %v; want 20m and 10m", got.AggregateWorking(), got.Elapsed)
	}
}
