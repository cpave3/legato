package service

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// stageSoon waits for a place to reach stage, and returns it.
func stageSoon(t *testing.T, stages *WorkStageService, place WorkStagePlace, stage string) store.WorkStage {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := stages.Get(context.Background(), place)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil && got.Stage == stage {
			return *got
		}
		if time.Now().After(deadline) {
			t.Fatalf("stage = %+v, want %s", got, stage)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// stageStays checks a place is still at stage once the bus has settled.
func stageStays(t *testing.T, stages *WorkStageService, place WorkStagePlace, stage string) {
	t.Helper()
	time.Sleep(50 * time.Millisecond)
	got, err := stages.Get(context.Background(), place)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Stage != stage {
		t.Fatalf("stage = %+v, want it to stay %s", got, stage)
	}
}

func startMilestones(t *testing.T, db *store.Store, bus *events.Bus) *WorkStageService {
	t.Helper()
	stages := NewWorkStageService(db, bus)
	stop := StartWorkStageMilestones(context.Background(), bus, stages, db)
	t.Cleanup(stop)
	return stages
}

// S2: a plan-first agent goes Exploring, Planning while its plan waits,
// Implementing once approved, and Reviewing when its pull request opens.
func TestAPlanFirstAgentsStages(t *testing.T) {
	f := newStageFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "LEG-1")
	stages := startMilestones(t, f.store, f.bus)
	place := TaskPlace("LEG-1", "")
	_ = stages.Start(ctx, place, "ex-1")

	f.bus.Publish(events.Event{Type: events.EventPlanChanged, Payload: events.PlanChangedPayload{PlanID: "p1", TaskID: "LEG-1", Kind: "submitted"}})
	if got := stageSoon(t, stages, place, store.WorkStagePlanning); got.Source != store.WorkStageSourceMilestone || got.Reason != "plan submitted" {
		t.Fatalf("stage = %+v", got)
	}
	// A comment on the plan says nothing of the stage.
	f.bus.Publish(events.Event{Type: events.EventPlanChanged, Payload: events.PlanChangedPayload{PlanID: "p1", TaskID: "LEG-1", Kind: "comment"}})
	f.bus.Publish(events.Event{Type: events.EventPlanChanged, Payload: events.PlanChangedPayload{PlanID: "p1", TaskID: "LEG-1", Kind: "approved"}})
	stageSoon(t, stages, place, store.WorkStageImplementing)

	f.bus.Publish(events.Event{Type: events.EventWorkMilestone, Payload: events.WorkMilestonePayload{TaskID: "LEG-1", Stage: store.WorkStageReviewing, Reason: "PR #12 open"}})
	stageSoon(t, stages, place, store.WorkStageReviewing)

	// A plan submitted now is not news: milestones only move forward.
	f.bus.Publish(events.Event{Type: events.EventPlanChanged, Payload: events.PlanChangedPayload{PlanID: "p2", TaskID: "LEG-1", Kind: "submitted"}})
	stageStays(t, stages, place, store.WorkStageReviewing)
}

// A review tour marked ready puts its task's agent in review, the task
// found from the tour when the event does not name it (a CLI broadcast).
func TestAReadyReviewTourIsReviewing(t *testing.T) {
	f := newStageFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "LEG-1")
	tour, err := f.store.EnsureReviewTour(ctx, "LEG-1", "")
	if err != nil {
		t.Fatal(err)
	}
	stages := startMilestones(t, f.store, f.bus)
	place := TaskPlace("LEG-1", "")
	_ = stages.Start(ctx, place, "ex-1")

	f.bus.Publish(events.Event{Type: events.EventReviewChanged, Payload: events.ReviewChangedPayload{TourID: tour.ID, Kind: "synced"}})
	f.bus.Publish(events.Event{Type: events.EventReviewChanged, Payload: events.ReviewChangedPayload{TourID: tour.ID, Kind: "ready"}})
	if got := stageSoon(t, stages, place, store.WorkStageReviewing); got.Reason != "review tour ready" {
		t.Fatalf("stage = %+v", got)
	}
}

// Changes requested by a person on a task's pull request is feedback: the
// agent is back to Implementing.
func TestChangesRequestedIsFeedback(t *testing.T) {
	f := newStageFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "LEG-1")
	stages := startMilestones(t, f.store, f.bus)
	place := TaskPlace("LEG-1", "")
	_, _ = stages.Report(ctx, place, "ex-1", store.WorkStageReviewing, "PR open")

	f.bus.Publish(events.Event{Type: events.EventPRChangesRequested, Payload: events.PRChangesRequestedPayload{TaskID: "LEG-1", Repo: "acme/widget", Number: 12, Reviewer: "ana"}})
	if got := stageSoon(t, stages, place, store.WorkStageImplementing); got.Reason != "changes requested on PR #12" {
		t.Fatalf("stage = %+v", got)
	}
}

// S3 and S9: a stack of four pull requests is in review when they open,
// and finished only once every one is merged; a partial merge stays in
// review. A new pull request after that is new work in review, and the
// old merged ones do not finish it again.
func TestAStackIsFinishedOnlyWhenEveryPullRequestIsMerged(t *testing.T) {
	gh := newFakeGitHub(t)
	open := func(n int, head, base string, merged bool) fakePR {
		pr := fakePR{Number: n, Head: head, Base: base}
		if merged {
			pr.State, pr.Merged = "closed", true
		}
		return pr
	}
	gh.set("acme/widget",
		open(11, "leg-1-a", "", false), open(12, "leg-1-b", "leg-1-a", false),
		open(13, "leg-1-c", "leg-1-b", false), fakePR{Number: 14, Head: "leg-1-d", Base: "leg-1-c", Draft: true},
	)
	s, svc, bus := newBranchTracking(t, gh)
	ctx := context.Background()
	createPRTask(t, s, "LEG-1")
	setRepoContext(t, s, "LEG-1", "acme/widget", "")
	stages := startMilestones(t, s, bus)
	place := TaskPlace("LEG-1", "")
	_ = stages.Start(ctx, place, "ex-1")
	if _, err := svc.AddTaskBranches(ctx, "LEG-1", "acme/widget", []string{"leg-1-a", "leg-1-b", "leg-1-c", "leg-1-d"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.PollAll(ctx); err != nil {
		t.Fatal(err)
	}
	stageSoon(t, stages, place, store.WorkStageReviewing)

	// Feedback delivered, then a new pass: Implementing, then Reviewing.
	bus.Publish(events.Event{Type: events.EventWorkMilestone, Payload: events.WorkMilestonePayload{TaskID: "LEG-1", Stage: store.WorkStageImplementing, Reason: "fixing review feedback · pass 1", Back: true}})
	stageSoon(t, stages, place, store.WorkStageImplementing)
	// The draft marked ready is a ready edge.
	gh.set("acme/widget",
		open(11, "leg-1-a", "", false), open(12, "leg-1-b", "leg-1-a", false),
		open(13, "leg-1-c", "leg-1-b", false), open(14, "leg-1-d", "leg-1-c", false),
	)
	if err := svc.PollAll(ctx); err != nil {
		t.Fatal(err)
	}
	if got := stageSoon(t, stages, place, store.WorkStageReviewing); got.Reason != "PR #14 ready for review" {
		t.Fatalf("stage = %+v", got)
	}

	// Bottom up: two merged is not all.
	gh.set("acme/widget",
		open(11, "leg-1-a", "", true), open(12, "leg-1-b", "leg-1-a", true),
		open(13, "leg-1-c", "leg-1-b", false), open(14, "leg-1-d", "leg-1-c", false),
	)
	if err := svc.PollAll(ctx); err != nil {
		t.Fatal(err)
	}
	stageStays(t, stages, place, store.WorkStageReviewing)

	gh.set("acme/widget",
		open(11, "leg-1-a", "", true), open(12, "leg-1-b", "leg-1-a", true),
		open(13, "leg-1-c", "leg-1-b", true), open(14, "leg-1-d", "leg-1-c", true),
	)
	if err := svc.PollAll(ctx); err != nil {
		t.Fatal(err)
	}
	if got := stageSoon(t, stages, place, store.WorkStageFinished); got.Reason != "every PR merged" {
		t.Fatalf("stage = %+v", got)
	}

	// S9: a new pull request on the same task.
	gh.set("acme/widget",
		open(11, "leg-1-a", "", true), open(12, "leg-1-b", "leg-1-a", true),
		open(13, "leg-1-c", "leg-1-b", true), open(14, "leg-1-d", "leg-1-c", true),
		open(15, "leg-1-e", "main", false),
	)
	if _, err := svc.AddTaskBranches(ctx, "LEG-1", "acme/widget", []string{"leg-1-e"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.PollAll(ctx); err != nil {
		t.Fatal(err)
	}
	stageSoon(t, stages, place, store.WorkStageReviewing)
	if err := svc.PollAll(ctx); err != nil {
		t.Fatal(err)
	}
	stageStays(t, stages, place, store.WorkStageReviewing)
}
