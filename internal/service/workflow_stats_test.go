package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestWorkflowStatsCountTodaysRunsAndFailuresAndGiveTheNewestRun(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for _, run := range []store.WorkflowRun{
		{ID: "first", Status: "failed"},
		{ID: "second", Status: "succeeded"},
	} {
		run.WorkflowName, run.WorkflowTitle, run.DefinitionSource, run.DefinitionDigest = "triage", "Triage", "test", run.ID
		run.DefinitionJSON, run.InputsJSON, run.CurrentStepID = `{}`, `{}`, "a"
		if err := s.CreateWorkflowRun(ctx, run, nil); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := NewWorkflowService(s, nil).Stats(ctx, time.Now(), time.Local, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Days) != 7 || stats.Days[6] != time.Now().In(time.Local).Format(time.DateOnly) {
		t.Fatalf("days = %v, want the last 7 ending today", stats.Days)
	}
	triage := stats.Workflows["triage"]
	if len(triage.Days) != 7 || triage.Days[6] != (WorkflowDayCount{Runs: 2, Failed: 1}) || triage.Days[0] != (WorkflowDayCount{}) {
		t.Fatalf("triage days = %+v", triage.Days)
	}
	if triage.Last == nil || triage.Last.ID != "second" {
		t.Fatalf("last = %+v, want second", triage.Last)
	}
}
