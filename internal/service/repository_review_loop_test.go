package service

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// A handed-off card whose review loop is open: its job clone is the
// author's checkout every pass reviews, so the sweep keeps it however long
// the loop waits, and lets it go once the handoff closes.
func TestSweepKeepsTheCloneOfAnOpenHandoffReviewLoop(t *testing.T) {
	f, clone := jobFixture(t)
	ctx := context.Background()
	if err := f.store.CreateHandoffCard(ctx, store.Task{ID: "card-1", Title: "x", Status: "Doing"}, nil, store.Handoff{
		MainInstanceID: "srv-main", HandoffID: "ho-1", MainName: "workstation", MainTaskID: "T1", TaskID: "card-1",
		Purpose: HandoffMakeChange, Kind: HandoffNewBranch, Repo: "acme/widget", AgentKind: "claude-code", State: store.HandoffInProgress,
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.AttachJobCloneTask(ctx, clone.ID, "card-1", *f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveHandoffReview(ctx, store.HandoffReview{TaskID: "card-1", Phase: ReviewPhaseWaitingForAuthor, Pass: 1}); err != nil {
		t.Fatal(err)
	}
	f.end(t, "ex-abc")
	*f.now = f.now.Add(30 * 24 * time.Hour)

	f.sweep(t)
	if !exists(clone.Path) {
		t.Fatal("the sweep removed the clone of an open review loop")
	}

	if err := f.store.CloseHandoffAndArchive(ctx, "srv-main", "ho-1", f.now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	f.idle()
	f.sweep(t)
	if exists(clone.Path) {
		t.Fatal("the clone of a closed handoff's loop is still there")
	}
}
