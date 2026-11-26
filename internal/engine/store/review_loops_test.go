package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"testing"
)

const loopAt = "2026-11-24T10:00:00Z"

// runLoop is a run-owned loop on repo's pull requests, its host task made.
func runLoop(t *testing.T, s *Store, runID, repo string, prs ...int) ReviewLoopOpen {
	t.Helper()
	host := "host-" + runID
	if _, err := s.GetTask(context.Background(), host); errors.Is(err, ErrNotFound) {
		createTestTask(t, s, host)
	}
	open := ReviewLoopOpen{Loop: ReviewLoop{
		OwnerKind: ReviewLoopOwnerRun, RunID: runID, StepPath: "review", AttemptID: "a1",
		HostTask: host, Repo: repo, Scope: "stack", MaxPasses: 3, MaxReentries: 5, CreatedAt: loopAt,
	}}
	for _, n := range prs {
		open.PRs = append(open.PRs, ReviewLoopPR{Number: n, Branch: fmt.Sprintf("b%d", n), Base: "main"})
	}
	return open
}

func mustOpen(t *testing.T, s *Store, open ReviewLoopOpen) *ReviewLoop {
	t.Helper()
	loop, _, err := s.OpenReviewLoop(context.Background(), open)
	if err != nil {
		t.Fatal(err)
	}
	return loop
}

func TestReviewLoopIDIsRlAndTwelveHex(t *testing.T) {
	s := newTestStore(t)
	loop := mustOpen(t, s, runLoop(t, s, "run-1", "o/r", 7))
	if !regexp.MustCompile(`^rl-[0-9a-f]{12}$`).MatchString(loop.ID) {
		t.Fatalf("id = %q", loop.ID)
	}
	if loop.State != ReviewLoopOpenState || loop.MarkerKey != loop.ID {
		t.Fatalf("loop = %+v", loop)
	}
}

func TestConcurrentOpensOnOnePullRequestGiveOneClaim(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const n = 6
	opens := make([]ReviewLoopOpen, n)
	for i := range opens {
		// Repositories differ in case only: the claim ignores it.
		repo := "Owner/Repo"
		if i%2 == 1 {
			repo = "owner/repo"
		}
		opens[i] = runLoop(t, s, fmt.Sprintf("run-%d", i), repo, 10, 11)
	}
	var wg sync.WaitGroup
	loops := make([]*ReviewLoop, n)
	errs := make([]error, n)
	for i := range opens {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			loops[i], _, errs[i] = s.OpenReviewLoop(ctx, opens[i])
		}(i)
	}
	wg.Wait()
	var winner *ReviewLoop
	for i, err := range errs {
		if err == nil {
			if winner != nil {
				t.Fatalf("two loops opened: %s and %s", winner.ID, loops[i].ID)
			}
			winner = loops[i]
		}
	}
	if winner == nil {
		t.Fatalf("no loop opened: %v", errs)
	}
	for _, err := range errs {
		if err == nil {
			continue
		}
		var conflict *ReviewLoopConflictError
		if !errors.Is(err, ErrReviewLoopActive) || !errors.As(err, &conflict) || conflict.LoopID != winner.ID {
			t.Fatalf("refusal = %v, want review_loop_active naming %s", err, winner.ID)
		}
	}
	for _, pr := range []int{10, 11} {
		active, err := s.ActiveReviewLoop(ctx, "OWNER/REPO", pr)
		if err != nil || active == nil || active.ID != winner.ID {
			t.Fatalf("active on #%d = %+v, %v; want %s", pr, active, err, winner.ID)
		}
	}
	open, err := s.ListOpenReviewLoops(ctx)
	if err != nil || len(open) != 1 {
		t.Fatalf("open loops = %d, %v", len(open), err)
	}
}

func TestOneOpenAgentAuthoredLoopPerAuthor(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "author")
	first := runLoop(t, s, "run-a", "o/r", 1)
	second := runLoop(t, s, "run-b", "o/other", 2)
	for _, o := range []*ReviewLoopOpen{&first, &second} {
		o.Loop.AgentAuthor = true
		o.Loop.AuthorTask = "author"
	}
	var wg sync.WaitGroup
	results := make([]error, 2)
	ids := make([]string, 2)
	for i, o := range []ReviewLoopOpen{first, second} {
		wg.Add(1)
		go func(i int, o ReviewLoopOpen) {
			defer wg.Done()
			loop, _, err := s.OpenReviewLoop(ctx, o)
			results[i] = err
			if loop != nil {
				ids[i] = loop.ID
			}
		}(i, o)
	}
	wg.Wait()
	won, lost := 0, 1
	if results[0] != nil {
		won, lost = 1, 0
	}
	if results[won] != nil {
		t.Fatalf("both refused: %v", results)
	}
	var conflict *ReviewLoopConflictError
	if !errors.Is(results[lost], ErrAuthorLoopActive) || !errors.As(results[lost], &conflict) || conflict.LoopID != ids[won] {
		t.Fatalf("refusal = %v, want author_loop_active naming %s", results[lost], ids[won])
	}
	// The refused loop claimed nothing.
	pr := 2
	if lost == 0 {
		pr = 1
	}
	repo := map[int]string{1: "o/r", 2: "o/other"}[pr]
	if active, err := s.ActiveReviewLoop(ctx, repo, pr); err != nil || active != nil {
		t.Fatalf("refused loop's pull request is claimed: %+v, %v", active, err)
	}
	// A person-authored loop of the same author task is not limited.
	third := runLoop(t, s, "run-c", "o/third", 3)
	third.Loop.AuthorTask = "author"
	mustOpen(t, s, third)
}

func TestOpenIsIdempotentByRunAndStepPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	open := runLoop(t, s, "run-1", "o/r", 4)
	first, created, err := s.OpenReviewLoop(ctx, open)
	if err != nil || !created {
		t.Fatalf("first open: created=%v err=%v", created, err)
	}
	again, created, err := s.OpenReviewLoop(ctx, open)
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("second open = %+v created=%v err=%v; want %s again", again, created, err, first.ID)
	}
	// Another step of the same run may not open a second loop.
	other := runLoop(t, s, "run-1", "o/r2", 5)
	other.Loop.StepPath = "review-again"
	if _, _, err := s.OpenReviewLoop(ctx, other); !errors.Is(err, ErrRunLoopActive) {
		t.Fatalf("second loop of one run: %v", err)
	}
}

func TestItemsWithOneIDInTwoLoopsStaySeparate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// The same pull request number and check name in two repositories.
	a := mustOpen(t, s, runLoop(t, s, "run-a", "o/api", 12))
	b := mustOpen(t, s, runLoop(t, s, "run-b", "o/web", 12))
	id := "check:12:ci/test"
	for _, l := range []*ReviewLoop{a, b} {
		if err := s.UpsertReviewLoopItem(ctx, ReviewLoopItem{LoopID: l.ID, ID: id, Kind: "check", Pass: 1, Repo: l.Repo, PR: 12, Title: "ci/test in " + l.Repo}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AnswerReviewLoopItem(ctx, a.ID, id, "fixed", "flaky", loopAt); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkReviewLoopItemsDelivered(ctx, a.ID, []string{id}); err != nil {
		t.Fatal(err)
	}
	gotA, err := s.GetReviewLoopItem(ctx, a.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	gotB, err := s.GetReviewLoopItem(ctx, b.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if gotA.Title != "ci/test in o/api" || gotA.Answer != "fixed" || !gotA.Delivered {
		t.Fatalf("loop a's item = %+v", gotA)
	}
	if gotB.Title != "ci/test in o/web" || gotB.Answer != "" || gotB.Delivered {
		t.Fatalf("loop b's item = %+v, touched by loop a's answer", gotB)
	}
	for _, l := range []*ReviewLoop{a, b} {
		items, err := s.ListReviewLoopItems(ctx, l.ID)
		if err != nil || len(items) != 1 {
			t.Fatalf("%s items = %+v, %v", l.ID, items, err)
		}
	}
}

func TestStaleCompareAndSwapIsRefusedAndChangesNothing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	loop := mustOpen(t, s, runLoop(t, s, "run-1", "o/r", 1))
	moved, err := s.UpdateReviewLoop(ctx, loop.ID, loop.Rev, func(tx *ReviewLoopTx) error {
		tx.Loop.Phase = "reviewing"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Rev == loop.Rev || moved.Phase != "reviewing" {
		t.Fatalf("after one transition: %+v", moved)
	}
	// A writer still holding the first rev is refused, and none of its
	// writes land.
	_, err = s.UpdateReviewLoop(ctx, loop.ID, loop.Rev, func(tx *ReviewLoopTx) error {
		tx.Loop.Phase = "stuck"
		if err := tx.UpsertItem(ReviewLoopItem{ID: "x", Kind: "comment", Pass: 1}); err != nil {
			return err
		}
		return tx.AddPR(ReviewLoopPR{Number: 2})
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("stale write: %v", err)
	}
	after, _ := s.GetReviewLoop(ctx, loop.ID)
	if after.Rev != moved.Rev || after.Phase != "reviewing" {
		t.Fatalf("stale write changed the loop: %+v", after)
	}
	if items, _ := s.ListReviewLoopItems(ctx, loop.ID); len(items) != 0 {
		t.Fatalf("stale write left items: %+v", items)
	}
	if active, _ := s.ActiveReviewLoop(ctx, "o/r", 2); active != nil {
		t.Fatalf("stale write claimed #2")
	}

	// Two writers racing from one rev: exactly one wins.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = s.UpdateReviewLoop(ctx, loop.ID, moved.Rev, func(tx *ReviewLoopTx) error {
				tx.Loop.Reason = fmt.Sprintf("writer %d", i)
				return nil
			})
		}(i)
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("racing writers: %v", errs)
	}
	for _, err := range errs {
		if err != nil && !errors.Is(err, ErrConflict) {
			t.Fatalf("losing writer: %v", err)
		}
	}

	// A change that fails rolls back what it wrote.
	current, _ := s.GetReviewLoop(ctx, loop.ID)
	_, err = s.UpdateReviewLoop(ctx, loop.ID, current.Rev, func(tx *ReviewLoopTx) error {
		if err := tx.UpsertItem(ReviewLoopItem{ID: "y", Kind: "comment", Pass: 1}); err != nil {
			return err
		}
		return errors.New("no")
	})
	if err == nil {
		t.Fatal("failed change committed")
	}
	if items, _ := s.ListReviewLoopItems(ctx, loop.ID); len(items) != 0 {
		t.Fatalf("failed change left items: %+v", items)
	}
}

func TestALayerAddedLaterIsClaimedOrRefusedNamingItsHolder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	holder := mustOpen(t, s, runLoop(t, s, "run-h", "o/r", 9))
	loop := mustOpen(t, s, runLoop(t, s, "run-1", "o/r", 7))
	var refused error
	got, err := s.UpdateReviewLoop(ctx, loop.ID, loop.Rev, func(tx *ReviewLoopTx) error {
		if err := tx.AddPR(ReviewLoopPR{Number: 8, Branch: "b8"}); err != nil {
			return err
		}
		if refused = tx.AddPR(ReviewLoopPR{Number: 9, Branch: "b9"}); refused != nil {
			tx.Loop.Phase = "stuck"
			tx.Loop.Reason = refused.Error()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var conflict *ReviewLoopConflictError
	if !errors.As(refused, &conflict) || conflict.LoopID != holder.ID || !errors.Is(refused, ErrReviewLoopActive) {
		t.Fatalf("refusal = %v", refused)
	}
	if got.Phase != "stuck" {
		t.Fatalf("loop = %+v", got)
	}
	prs, _ := s.ListReviewLoopPRs(ctx, loop.ID)
	if len(prs) != 2 || prs[0].Number != 7 || prs[1].Number != 8 {
		t.Fatalf("layers = %+v", prs)
	}
	if active, _ := s.ActiveReviewLoop(ctx, "o/r", 8); active == nil || active.ID != loop.ID {
		t.Fatalf("#8 held by %+v", active)
	}
}

func TestConcurrentSlotTakesNeverExceedMaxConcurrent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const loops, max = 8, 3
	open := make([]*ReviewLoop, loops)
	for i := range open {
		open[i] = mustOpen(t, s, runLoop(t, s, fmt.Sprintf("run-%d", i), "o/r", 100+i))
	}
	var wg sync.WaitGroup
	errs := make([]error, loops)
	for i, l := range open {
		wg.Add(1)
		go func(i int, l *ReviewLoop) {
			defer wg.Done()
			_, errs[i] = s.UpdateReviewLoop(ctx, l.ID, l.Rev, func(tx *ReviewLoopTx) error {
				tx.Loop.Pass = 1
				return tx.StartPass(ReviewLoopPass{Pass: 1, Pins: ReviewLoopPins{{Number: 100 + i, HeadSHA: "abc"}}}, max)
			})
		}(i, l)
	}
	wg.Wait()
	took := 0
	var full ReviewLoop
	for i, err := range errs {
		switch {
		case err == nil:
			took++
		case errors.Is(err, ErrReviewSlotsFull):
			full = *open[i]
		default:
			t.Fatalf("slot take: %v", err)
		}
	}
	if took != max {
		t.Fatalf("%d slots taken, want %d", took, max)
	}
	// A refused take leaves the loop as it was.
	if after, _ := s.GetReviewLoop(ctx, full.ID); after.Pass != 0 || after.Rev != full.Rev {
		t.Fatalf("refused loop moved: %+v", after)
	}
	// Closing a loop with a pass in flight frees its slot.
	for i, err := range errs {
		if err == nil {
			if _, _, err := s.CloseReviewLoop(ctx, open[i].ID, "cancelled", loopAt); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if _, err := s.UpdateReviewLoop(ctx, full.ID, full.Rev, func(tx *ReviewLoopTx) error {
		return tx.StartPass(ReviewLoopPass{Pass: 1}, max)
	}); err != nil {
		t.Fatalf("slot after a close: %v", err)
	}
}

func TestCloseReleasesClaimsAndDiscardsThePassInFlight(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	loop := mustOpen(t, s, runLoop(t, s, "run-1", "o/r", 1, 2))
	loop, err := s.UpdateReviewLoop(ctx, loop.ID, loop.Rev, func(tx *ReviewLoopTx) error {
		if err := tx.StartPass(ReviewLoopPass{Pass: 1, Pins: ReviewLoopPins{{Number: 1, HeadSHA: "a1"}, {Number: 2, HeadSHA: "b1"}}}, 1); err != nil {
			return err
		}
		return tx.SavePass(ReviewLoopPass{Pass: 1, State: ReviewLoopPassSubmitted, Verdict: "changes",
			Pins: ReviewLoopPins{{Number: 1, HeadSHA: "a1"}, {Number: 2, HeadSHA: "b1"}}})
	})
	if err != nil {
		t.Fatal(err)
	}
	closed, changed, err := s.CloseReviewLoop(ctx, loop.ID, "cancelled", loopAt)
	if err != nil || !changed {
		t.Fatalf("close: changed=%v err=%v", changed, err)
	}
	if closed.State != ReviewLoopClosedState || closed.Outcome != "cancelled" || closed.ClosedAt != loopAt {
		t.Fatalf("closed loop = %+v", closed)
	}
	pass, _ := s.GetReviewLoopPass(ctx, loop.ID, 1)
	if pass.State != ReviewLoopPassDiscarded || len(pass.Pins) != 2 || pass.Pins[1].HeadSHA != "b1" {
		t.Fatalf("pass = %+v", pass)
	}
	for _, pr := range []int{1, 2} {
		if active, _ := s.ActiveReviewLoop(ctx, "o/r", pr); active != nil {
			t.Fatalf("#%d still held by %s", pr, active.ID)
		}
	}
	// A transition still holding the open loop's rev is refused.
	if _, err := s.UpdateReviewLoop(ctx, loop.ID, loop.Rev, func(*ReviewLoopTx) error { return nil }); !errors.Is(err, ErrReviewLoopClosed) {
		t.Fatalf("write after close: %v", err)
	}
	// Closing again is a no-op.
	again, changed, err := s.CloseReviewLoop(ctx, loop.ID, "stopped", "2026-11-25T00:00:00Z")
	if err != nil || changed || again.Outcome != "cancelled" || again.Rev != closed.Rev {
		t.Fatalf("second close: %+v changed=%v err=%v", again, changed, err)
	}
	// The pull requests are free for a new loop.
	mustOpen(t, s, runLoop(t, s, "run-2", "o/r", 1, 2))
}

func TestCheckReviewLoopOwner(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	loop := mustOpen(t, s, runLoop(t, s, "run-1", "o/r", 1))
	owner := ReviewLoopOwner{Kind: ReviewLoopOwnerRun, RunID: "run-1"}
	if _, err := s.CheckReviewLoopOwner(ctx, loop.ID, owner); err != nil {
		t.Fatalf("owner: %v", err)
	}
	if _, err := s.CheckReviewLoopOwner(ctx, loop.ID, ReviewLoopOwner{Kind: ReviewLoopOwnerRun, RunID: "run-2"}); !errors.Is(err, ErrReviewLoopNotOwned) {
		t.Fatalf("another run: %v", err)
	}
	if _, err := s.CheckReviewLoopOwner(ctx, "rl-000000000000", owner); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown loop: %v", err)
	}
	if _, _, err := s.CloseReviewLoop(ctx, loop.ID, "stopped", loopAt); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CheckReviewLoopOwner(ctx, loop.ID, owner); !errors.Is(err, ErrReviewLoopClosed) {
		t.Fatalf("closed loop: %v", err)
	}
}

// The steward's watch writes what GitHub reports of each layer, without
// moving the loop's rev (a transition in flight is not refused for it), and
// writes nothing to a closed loop or to a layer that is not the loop's.
func TestSetReviewLoopPRStatusWritesTheLayersWithoutMovingTheRev(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	loop := mustOpen(t, s, runLoop(t, s, "run-1", "o/r", 1, 2))
	if err := s.SetReviewLoopPRStatus(ctx, loop.ID, []ReviewLoopPRStatus{{Number: 1, State: "merged", Mergeable: "UNKNOWN"}, {Number: 2, State: "open", Draft: true, Mergeable: "CONFLICTING"}, {Number: 9, State: "open"}}); err != nil {
		t.Fatal(err)
	}
	layers, _ := s.ListReviewLoopPRs(ctx, loop.ID)
	if layers[0].State != "merged" || layers[0].Mergeable != "UNKNOWN" || layers[1].State != "open" || !layers[1].Draft || layers[1].Mergeable != "CONFLICTING" {
		t.Fatalf("layers = %+v", layers)
	}
	if got, _ := s.GetReviewLoop(ctx, loop.ID); got.Rev != loop.Rev {
		t.Fatalf("rev moved from %d to %d", loop.Rev, got.Rev)
	}
	if _, _, err := s.CloseReviewLoop(ctx, loop.ID, "closed", loopAt); err != nil {
		t.Fatal(err)
	}
	if err := s.SetReviewLoopPRStatus(ctx, loop.ID, []ReviewLoopPRStatus{{Number: 2, State: "closed", Mergeable: "MERGEABLE"}}); err != nil {
		t.Fatal(err)
	}
	if layers, _ := s.ListReviewLoopPRs(ctx, loop.ID); layers[1].State != "open" {
		t.Fatalf("a closed loop's layer was written: %+v", layers[1])
	}
}

// The steward's idle-end notice reads the run loops that closed with an
// outcome since a time (spec LEG-559 AC 35).
func TestListReviewLoopsClosedSince(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	idle := mustOpen(t, s, runLoop(t, s, "run-1", "o/r", 1))
	old := mustOpen(t, s, runLoop(t, s, "run-2", "o/r", 2))
	merged := mustOpen(t, s, runLoop(t, s, "run-3", "o/r", 3))
	mustOpen(t, s, runLoop(t, s, "run-4", "o/r", 4)) // still open
	for _, c := range []struct{ id, outcome, at string }{
		{idle.ID, "idle", "2026-11-25T10:00:00Z"}, {old.ID, "idle", "2026-11-10T10:00:00Z"}, {merged.ID, "merged", "2026-11-25T11:00:00Z"},
	} {
		if _, _, err := s.CloseReviewLoop(ctx, c.id, c.outcome, c.at); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListReviewLoopsClosedSince(ctx, "idle", "2026-11-20T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != idle.ID || got[0].RunID != "run-1" {
		t.Fatalf("closed idle since: %+v", got)
	}
}
