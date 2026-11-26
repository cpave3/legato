package handofftest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The steward's mergeability items and merged layers (spec LEG-559 § The
// steward, D14, Q6; LEG-591).

// openFeedback is the author's open feedback items of a kind.
func (r reviewRun) openFeedback(t *testing.T, kind string) []store.HandoffReviewItem {
	t.Helper()
	feedback, err := service.ReviewFeedback(context.Background(), r.worker.Store, "board-1")
	if err != nil {
		t.Fatal(err)
	}
	var out []store.HandoffReviewItem
	for _, item := range feedback.Items {
		if item.Kind == kind {
			out = append(out, item)
		}
	}
	return out
}

// told moves the clock past the quiet time and ticks, so the author is told
// what the watch routed.
func (r reviewRun) told() {
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
}

func TestAConflictRaisesOneMergeabilityItemThatClosesOnceMergeable(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)

	r.worker.ReviewGitHub.SetMergeable(12, "CONFLICTING", "DIRTY")
	r.watchTick()
	r.told()
	r.watchTick()
	r.watchTick()

	typed := r.worker.typedTo("legato-board-1")
	if n := strings.Count(typed, "[legato steward] 1 mergeability problem"); n != 1 {
		t.Fatalf("the author was told %d times; typed %q", n, typed)
	}
	items := r.openFeedback(t, service.ItemMergeability)
	if len(items) != 1 || items[0].ID != "m-12" || items[0].PR != 12 || items[0].Version != 1 {
		t.Fatalf("mergeability items %+v", items)
	}
	if !strings.Contains(items[0].Title, "conflicts") {
		t.Fatalf("title %q", items[0].Title)
	}
	for _, want := range []string{"merge", "restack", "git.push"} {
		if !strings.Contains(items[0].Operation, want) {
			t.Fatalf("the item names no %s: %q", want, items[0].Operation)
		}
	}
	// Routing starts no pass, and the approval stands.
	if rec := r.record(t, r.show(t, view.Run.ID)); rec.Pass != 1 || rec.Reentries != 0 {
		t.Fatalf("loop at pass %d with %d re-entries", rec.Pass, rec.Reentries)
	}

	// GitHub reports it mergeable: the item closes by itself.
	r.worker.ReviewGitHub.SetMergeable(12, "MERGEABLE", "CLEAN")
	r.watchTick()
	if items := r.openFeedback(t, service.ItemMergeability); len(items) != 0 {
		t.Fatalf("still open once mergeable: %+v", items)
	}

	// It conflicts again: the one item is raised again, and told again.
	r.worker.ReviewGitHub.SetMergeable(12, "CONFLICTING", "DIRTY")
	r.watchTick()
	r.told()
	items = r.openFeedback(t, service.ItemMergeability)
	if len(items) != 1 || items[0].ID != "m-12" || items[0].Version != 2 {
		t.Fatalf("raised again as %+v", items)
	}
	if n := strings.Count(r.worker.typedTo("legato-board-1"), "[legato steward] 1 mergeability problem"); n != 2 {
		t.Fatalf("told %d times in all", n)
	}
	if got := r.show(t, view.Run.ID); !r.watching(t, got) {
		t.Fatalf("not watching: %s", stepStates(got))
	}
}

// BEHIND is routed only when GitHub's mergeStateStatus says so (Q6: no
// branch protection read); BLOCKED, say, is not a mergeability problem.
func TestBehindIsRoutedOnlyWhenTheMergeStateIsBehind(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	r.stewarded(t, stewardInputs)

	r.worker.ReviewGitHub.SetMergeable(12, "MERGEABLE", "BLOCKED")
	r.watchTick()
	r.told()
	if items := r.openFeedback(t, service.ItemMergeability); len(items) != 0 {
		t.Fatalf("BLOCKED routed as %+v", items)
	}

	r.worker.ReviewGitHub.SetMergeable(12, "MERGEABLE", "BEHIND")
	r.watchTick()
	r.told()
	items := r.openFeedback(t, service.ItemMergeability)
	if len(items) != 1 || !strings.Contains(items[0].Title, "behind its base main") {
		t.Fatalf("BEHIND routed as %+v", items)
	}
	if typed := r.worker.typedTo("legato-board-1"); !strings.Contains(typed, "[legato steward] 1 mergeability problem") {
		t.Fatalf("typed %q", typed)
	}

	r.worker.ReviewGitHub.SetMergeable(12, "MERGEABLE", "CLEAN")
	r.watchTick()
	if items := r.openFeedback(t, service.ItemMergeability); len(items) != 0 {
		t.Fatalf("still open once up to date: %+v", items)
	}
}

// While GitHub is still working mergeability out (UNKNOWN), the item stays
// as it was: neither raised nor closed.
func TestAnUnknownMergeabilityLeavesTheItemAsItWas(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	r.stewarded(t, stewardInputs)

	r.worker.ReviewGitHub.SetMergeable(12, "UNKNOWN", "UNKNOWN")
	r.watchTick()
	r.told()
	if items := r.openFeedback(t, service.ItemMergeability); len(items) != 0 {
		t.Fatalf("UNKNOWN raised %+v", items)
	}
	r.worker.ReviewGitHub.SetMergeable(12, "CONFLICTING", "DIRTY")
	r.watchTick()
	r.worker.ReviewGitHub.SetMergeable(12, "UNKNOWN", "UNKNOWN")
	r.watchTick()
	r.watchTick()
	if items := r.openFeedback(t, service.ItemMergeability); len(items) != 1 || items[0].Version != 1 {
		t.Fatalf("UNKNOWN after a conflict: %+v", items)
	}
}
