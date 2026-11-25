package store

import (
	"context"
	"testing"
)

// LEG-626: when a place's conversation began is its newest start event,
// whatever came after it; a place never started has none.
func TestWorkStageStartedAtIsTheNewestStart(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	place := WorkStagePlace{TaskID: "LEG-1"}
	other := WorkStagePlace{TaskID: "LEG-1", Slot: "reviewer"}

	if got, err := s.WorkStageStartedAt(ctx, place); err != nil || got != "" {
		t.Fatalf("never started = %q, %v; want none", got, err)
	}
	set := func(place WorkStagePlace, stage, source string) {
		t.Helper()
		if _, err := s.SetWorkStage(ctx, WorkStage{WorkStagePlace: place, Stage: stage, Source: source}); err != nil {
			t.Fatal(err)
		}
	}
	set(place, WorkStageExploring, WorkStageSourceStart)
	set(place, WorkStageImplementing, WorkStageSourceAgent)
	set(place, WorkStageExploring, WorkStageSourceStart)
	set(place, WorkStageReviewing, WorkStageSourceMilestone)
	set(other, WorkStageReviewing, WorkStageSourceStart)
	if _, err := s.DB().Exec(`UPDATE agent_stage_events SET created_at = '2026-01-0' || id || 'T00:00:00.000Z'`); err != nil {
		t.Fatal(err)
	}

	got, err := s.WorkStageStartedAt(ctx, place)
	if err != nil {
		t.Fatal(err)
	}
	if got != "2026-01-03T00:00:00.000Z" {
		t.Fatalf("started at = %q, want the second start's", got)
	}
}
