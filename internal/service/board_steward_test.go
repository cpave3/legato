package service

import (
	"context"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// A card whose pull requests a steward watches says so, for the TUI's card
// strip (spec LEG-559 story 22): stewarding, held or stuck.
func TestACardSaysItsStewardsState(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	ctx := context.Background()
	seedColumns(t, s)
	for _, id := range []string{"card-1", "host-1"} {
		if err := s.CreateTask(ctx, store.Task{ID: id, Title: id, Status: "In Progress"}); err != nil {
			t.Fatal(err)
		}
	}
	loop, _, err := s.OpenReviewLoop(ctx, store.ReviewLoopOpen{Loop: store.ReviewLoop{
		OwnerKind: store.ReviewLoopOwnerRun, RunID: "wr-1", StepPath: "open", AttemptID: "a1", HostTask: "host-1", Repo: "acme/widget",
		Scope: "stack", AgentAuthor: true, AuthorTask: "card-1", Steward: true, MaxPasses: 3, MaxReentries: 5, CreatedAt: "2026-11-24T10:00:00Z",
	}, PRs: []store.ReviewLoopPR{{Number: 12, Branch: "feature", Base: "main"}}})
	if err != nil {
		t.Fatal(err)
	}
	steward := func() string {
		t.Helper()
		cards, err := svc.ListCards(ctx, "In Progress")
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cards {
			if c.ID == "card-1" {
				return c.Steward
			}
		}
		t.Fatal("no card-1")
		return ""
	}
	if got := steward(); got != "" {
		t.Fatalf("before approval: %q", got)
	}
	set := func(change func(*store.ReviewLoop)) {
		t.Helper()
		rec, err := s.GetReviewLoop(ctx, loop.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpdateReviewLoop(ctx, rec.ID, rec.Rev, func(tx *store.ReviewLoopTx) error { change(tx.Loop); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	set(func(l *store.ReviewLoop) { l.Phase, l.IdleDeadline = ReviewPhaseClean, "2026-12-01T10:00:00Z" })
	if got := steward(); got != CardStewarding {
		t.Fatalf("watching: %q", got)
	}
	set(func(l *store.ReviewLoop) { l.Held = true })
	if got := steward(); got != CardStewardHeld {
		t.Fatalf("held: %q", got)
	}
	set(func(l *store.ReviewLoop) { l.Held, l.Phase = false, ReviewPhaseStuck })
	if got := steward(); got != CardStewardStuck {
		t.Fatalf("stuck: %q", got)
	}
	if _, _, err := s.CloseReviewLoop(ctx, loop.ID, ReviewOutcomeEnded, ""); err != nil {
		t.Fatal(err)
	}
	if got := steward(); got != "" {
		t.Fatalf("ended: %q", got)
	}
}
