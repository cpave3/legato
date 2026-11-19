package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// scheduleFixture is a database with a scheduled workflow and n
// coordinators on it, as the TUI and serve would each run one.
func scheduleFixture(t *testing.T, schedule string, n int) (*store.Store, []*WorkflowTriggerCoordinator, *fakeWorkflowExecutor) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	dir := t.TempDir()
	definition := `schema_version: 1
name: standup
title: Standup
inputs:
  since: {type: string, required: true}
  at: {type: string, required: true}
on:
  schedule:
` + schedule + `    inputs:
      since: ${{ schedule.previous_time }}
      at: ${{ schedule.time }}
steps:
  - {id: stop, title: Stop, type: agent.stop, with: {task: "x"}}
`
	if err := os.WriteFile(filepath.Join(dir, "standup.yaml"), []byte(definition), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "agent.stop", result: WorkflowExecutionResult{Status: "succeeded"}}
	var out []*WorkflowTriggerCoordinator
	for i := 0; i < n; i++ {
		workflows := NewWorkflowService(db, []WorkflowExecutor{executor})
		coordinator := NewWorkflowTriggerCoordinator(db, workflows, func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) })
		coordinator.SetHostZone(time.UTC)
		out = append(out, coordinator)
	}
	return db, out, executor
}

func TestEachDueTimeMakesOneRunWhateverTicksIt(t *testing.T) {
	db, coordinators, _ := scheduleFixture(t, "    every: weekdays\n    at: \"09:00\"\n", 2)
	ctx := context.Background()
	now := time.Date(2026, 11, 23, 8, 59, 30, 0, time.UTC) // a Monday
	for _, c := range coordinators {
		c.SetClock(func() time.Time { return now })
	}
	tick := func() {
		for _, c := range coordinators {
			if err := c.TickSchedules(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	tick()
	if n := len(requestsWith(t, db, "started")); n != 0 {
		t.Fatalf("before the due time: %d runs", n)
	}
	for _, step := range []time.Duration{30 * time.Second, time.Minute, time.Minute} {
		now = now.Add(step)
		tick()
	}
	started := requestsWith(t, db, "started")
	if len(started) != 1 {
		t.Fatalf("runs for 09:00 = %d, want 1", len(started))
	}
	if started[0].TriggerKind != workflowengine.TriggerSchedule || started[0].EventKey != "2026-11-23T09:00:00Z" {
		t.Fatalf("request = %+v", started[0])
	}
	run, err := db.FindWorkflowRunByTriggerRequest(ctx, started[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Run.InputsJSON == "" || !strings.Contains(run.Run.InputsJSON, `"at":"2026-11-23T09:00:00Z"`) || !strings.Contains(run.Run.InputsJSON, `"since":"2026-11-20T09:00:00Z"`) {
		t.Fatalf("inputs = %s", run.Run.InputsJSON)
	}
	// The next weekday's due time is a run of its own.
	now = time.Date(2026, 11, 24, 9, 0, 10, 0, time.UTC)
	tick()
	if n := len(requestsWith(t, db, "started")); n != 2 {
		t.Fatalf("runs after Tuesday = %d", n)
	}
}

func TestAPausedScheduleMakesNoRun(t *testing.T) {
	db, coordinators, _ := scheduleFixture(t, "    every: daily\n    at: \"09:00\"\n    enabled: false\n", 1)
	c := coordinators[0]
	c.SetClock(func() time.Time { return time.Date(2026, 11, 23, 9, 0, 5, 0, time.UTC) })
	if err := c.TickSchedules(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := requestCount(t, db); n != 0 {
		t.Fatalf("requests = %d", n)
	}
}

func TestTheWorkflowsPageSeesAScheduleNextRuns(t *testing.T) {
	_, coordinators, _ := scheduleFixture(t, "    every: weekdays\n    at: \"09:00\"\n", 1)
	c := coordinators[0]
	ctx := context.Background()
	now := time.Date(2026, 11, 20, 9, 0, 5, 0, time.UTC) // Friday
	c.SetClock(func() time.Time { return now })
	if err := c.TickSchedules(ctx); err != nil {
		t.Fatal(err)
	}
	states, err := c.TriggerStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s := states["standup"][0]
	if s.Schedule == nil || s.Schedule.Description != "Every weekday at 09:00" {
		t.Fatalf("state = %+v", s)
	}
	if len(s.Schedule.Next) != 3 || s.Schedule.Next[0] != "2026-11-23T09:00:00Z" || s.Schedule.Next[1] != "2026-11-24T09:00:00Z" {
		t.Fatalf("next = %v", s.Schedule.Next)
	}
	if s.Schedule.Last == nil || s.Schedule.Last.DueTime != "2026-11-20T09:00:00Z" || s.Schedule.Last.Status != "started" || s.Schedule.Last.RunID == "" {
		t.Fatalf("last = %+v", s.Schedule.Last)
	}
}

func TestMissedDueTimesAreRecordedAndCaughtUpOnce(t *testing.T) {
	for _, policy := range []string{"once", "skip"} {
		t.Run(policy, func(t *testing.T) {
			db, coordinators, _ := scheduleFixture(t, "    every: daily\n    at: \"09:00\"\n    catch_up: "+policy+"\n", 1)
			c := coordinators[0]
			ctx := context.Background()
			now := time.Date(2026, 11, 20, 9, 0, 5, 0, time.UTC)
			c.SetClock(func() time.Time { return now })
			if err := c.TickSchedules(ctx); err != nil {
				t.Fatal(err)
			}
			// Legato is down for three due times, and starts at noon on the 23rd.
			now = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
			for range 2 { // a restart does not repeat them
				if err := c.CatchUpSchedules(ctx); err != nil {
					t.Fatal(err)
				}
			}
			missed := requestsWith(t, db, "missed")
			if len(missed) != 3 {
				t.Fatalf("missed = %d, want 3", len(missed))
			}
			started := requestsWith(t, db, "started")
			want := 2
			if policy == "skip" {
				want = 1
			}
			if len(started) != want {
				t.Fatalf("started = %d, want %d", len(started), want)
			}
			if policy == "once" {
				late := started[len(started)-1]
				run, _ := db.FindWorkflowRunByTriggerRequest(ctx, late.ID)
				if !strings.Contains(run.Run.InputsJSON, `"since":"2026-11-20T09:00:00Z"`) || !strings.Contains(run.Run.InputsJSON, `"at":"2026-11-23T09:00:00Z"`) {
					t.Fatalf("the late run must cover every missed time: %s", run.Run.InputsJSON)
				}
			}
			states, _ := c.TriggerStates(ctx)
			if got := states["standup"][0].Schedule.Missed; len(got) != 3 || got[0].DueTime != "2026-11-23T09:00:00Z" || !strings.Contains(got[0].Error, "not running") {
				t.Fatalf("missed shown = %+v", got)
			}
		})
	}
}

func TestANewScheduleHasNothingToCatchUp(t *testing.T) {
	db, coordinators, _ := scheduleFixture(t, "    every: daily\n    at: \"09:00\"\n", 1)
	c := coordinators[0]
	c.SetClock(func() time.Time { return time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC) })
	if err := c.CatchUpSchedules(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := requestCount(t, db); n != 0 {
		t.Fatalf("requests = %d", n)
	}
}

func TestAScheduledRunThatCannotStartReachesAttention(t *testing.T) {
	db, coordinators, _ := scheduleFixture(t, "    every: daily\n    at: \"09:00\"\n", 1)
	c := coordinators[0]
	ctx := context.Background()
	c.SetClock(func() time.Time { return time.Date(2026, 11, 23, 9, 0, 5, 0, time.UTC) })
	// The workflow's file breaks its own run: its step names no executor.
	c.workflows = NewWorkflowService(db, nil)
	if err := c.TickSchedules(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := NewWorkflowTriggerAttentionAdapter(c, db).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].SignalKind != AttentionSignalWorkflowTriggerFailed {
		t.Fatalf("items = %+v, requests %+v", items, requestsWith(t, db, "started"))
	}
}
