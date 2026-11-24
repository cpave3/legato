package service

import (
	"context"
	"testing"
)

// A clone for an agent in a named slot of a card is its own: new beside
// the card's, never joined by the card's next agent, and leaving the card's
// code context the author's (LEG-618).
func TestASlottedCloneIsNewWhenTheCardHasALiveOne(t *testing.T) {
	f, first := jobFixture(t) // task1's clone, ex-abc running in it
	before := f.codeContext(t, "task1")
	triage, err := f.svc.JobClone(context.Background(), JobCloneRequest{ExecutionID: "ex-triage", TaskID: "task1", Repo: "acme/widget",
		Slot: "triage", FreshBranch: "legato/task1-triage"})
	if err != nil {
		t.Fatal(err)
	}
	if triage.ID == first.ID || triage.Path == first.Path || triage.Slot != "triage" {
		t.Fatalf("triage clone = %+v, the card's live clone is %+v", triage, first)
	}
	if triage.Branch != "legato/task1-triage" {
		t.Fatalf("triage clone on %q", triage.Branch)
	}
	if after := f.codeContext(t, "task1"); after.Branch != before.Branch || after.WorkingDir != before.WorkingDir {
		t.Fatalf("the card's code context = %+v, was %+v", after, before)
	}
}

func TestTheCardsNextAgentNeverJoinsASlottedClone(t *testing.T) {
	ctx := context.Background()
	f, first := jobFixture(t)
	triage, err := f.svc.JobClone(ctx, JobCloneRequest{ExecutionID: "ex-triage", TaskID: "task1", Repo: "acme/widget", Slot: "triage"})
	if err != nil {
		t.Fatal(err)
	}
	// The triage clone is the card's newest; the card's agent works in its
	// own clone again all the same.
	again, err := f.svc.JobClone(ctx, JobCloneRequest{ExecutionID: "ex-next", TaskID: "task1", Repo: "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID {
		t.Fatalf("the card's next agent works in clone %d, want its own %d (triage's is %d)", again.ID, first.ID, triage.ID)
	}
	// A second agent in the slot gets a clone of its own too.
	second, err := f.svc.JobClone(ctx, JobCloneRequest{ExecutionID: "ex-triage-2", TaskID: "task1", Repo: "acme/widget", Slot: "triage"})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == triage.ID || second.ID == first.ID {
		t.Fatalf("second triage clone = %d", second.ID)
	}
}

// With only a slot's clone, the card's agent gets a new one.
func TestACardWithOnlyASlottedCloneGetsANewOne(t *testing.T) {
	ctx := context.Background()
	f, _ := jobFixture(t)
	f.task(t, "task2")
	triage, err := f.svc.JobClone(ctx, JobCloneRequest{ExecutionID: "ex-triage", TaskID: "task2", Repo: "acme/widget", Slot: "triage"})
	if err != nil {
		t.Fatal(err)
	}
	main, err := f.svc.JobClone(ctx, JobCloneRequest{ExecutionID: "ex-main", TaskID: "task2", Repo: "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	if main.ID == triage.ID || main.Slot != "main" {
		t.Fatalf("the card's agent got %+v; the slot's clone is %d", main, triage.ID)
	}
}
