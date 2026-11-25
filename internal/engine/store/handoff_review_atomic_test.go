package store

import (
	"context"
	"testing"
)

// crashOnLoopWrite makes every write of a review loop fail, as a crash
// between a pass's write and its loop's would; the returned func lifts it.
func crashOnLoopWrite(t *testing.T, s *Store) func() {
	t.Helper()
	for _, q := range []string{
		`CREATE TRIGGER crash_loop_insert BEFORE INSERT ON review_loops BEGIN SELECT RAISE(ABORT, 'crash between writes'); END`,
		`CREATE TRIGGER crash_loop_update BEFORE UPDATE ON review_loops BEGIN SELECT RAISE(ABORT, 'crash between writes'); END`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return func() {
		for _, q := range []string{`DROP TRIGGER crash_loop_insert`, `DROP TRIGGER crash_loop_update`} {
			if _, err := s.db.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// loopAtPassOne is a loop whose pass 1 is posted with one open blocking
// finding, ready for pass 2.
func loopAtPassOne(t *testing.T) (*Store, HandoffReview) {
	t.Helper()
	s := twoReviewLoops(t)
	ctx := context.Background()
	loop := HandoffReview{TaskID: "widget-card", Settings: "{}", Phase: "ready", Pass: 1, UpdatedAt: "2024-01-01T00:00:00Z"}
	pass := HandoffReviewPass{TaskID: "widget-card", Pass: 1, State: "posted", Verdict: "changes", StartedAt: "2024-01-01T00:00:00Z"}
	if err := s.InsertHandoffReviewPass(ctx, pass, []HandoffReviewPR{{TaskID: "widget-card", Pass: 1, Repo: "acme/widget", Number: 7, Branch: "b", HeadSHA: "aaa"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceHandoffReviewFindings(ctx, "widget-card", 1, []HandoffReviewFinding{{ID: "f-1", TaskID: "widget-card", Pass: 1, PR: 7, Severity: "blocking", Title: "t", Status: "open"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveHandoffReview(ctx, loop); err != nil {
		t.Fatal(err)
	}
	return s, loop
}

func mustLoop(t *testing.T, s *Store) HandoffReview {
	t.Helper()
	loop, err := s.GetHandoffReview(context.Background(), "widget-card")
	if err != nil || loop == nil {
		t.Fatalf("loop = %+v, %v", loop, err)
	}
	return *loop
}

// Each change to a pass and its loop is one transaction: a crash injected
// after the pass's write, at its loop's, leaves neither written; with no
// crash, both are.
func TestAPassAndItsLoopAreWrittenTogetherOrNotAtAll(t *testing.T) {
	ctx := context.Background()
	pass2 := HandoffReviewPass{TaskID: "widget-card", Pass: 2, State: "reviewing", StartedAt: "2024-01-02T00:00:00Z"}
	pinned := []HandoffReviewPR{{TaskID: "widget-card", Pass: 2, Repo: "acme/widget", Number: 7, Branch: "b", HeadSHA: "bbb"}}

	t.Run("starting a pass", func(t *testing.T) {
		s, loop := loopAtPassOne(t)
		next := loop
		next.Pass, next.Phase = 2, "reviewing"
		lift := crashOnLoopWrite(t, s)
		if err := s.StartHandoffReviewPass(ctx, pass2, pinned, next, 1); err == nil {
			t.Fatal("the crash was not seen")
		}
		if _, err := s.GetHandoffReviewPass(ctx, "widget-card", 2); err != ErrNotFound {
			t.Fatalf("pass 2 was written without its loop: %v", err)
		}
		if got := mustLoop(t, s); got.Pass != 1 || got.Phase != "ready" {
			t.Fatalf("loop = %+v", got)
		}
		lift()
		if err := s.StartHandoffReviewPass(ctx, pass2, pinned, next, 1); err != nil {
			t.Fatalf("starting pass 2 after the crash: %v", err)
		}
		if got := mustLoop(t, s); got.Pass != 2 || got.Phase != "reviewing" {
			t.Fatalf("loop = %+v", got)
		}
		if prs, _ := s.ListHandoffReviewPRs(ctx, "widget-card", 2); len(prs) != 1 {
			t.Fatalf("pass 2's pull requests = %+v", prs)
		}
	})

	t.Run("ending a pass", func(t *testing.T) {
		s, loop := loopAtPassOne(t)
		next := loop
		next.Pass, next.Phase = 2, "reviewing"
		if err := s.StartHandoffReviewPass(ctx, pass2, pinned, next, 1); err != nil {
			t.Fatal(err)
		}
		unable := pass2
		unable.State, unable.Reason = "unable", "the reviewer ran past its time"
		stuck := next
		stuck.Phase, stuck.Reason = "stuck", unable.Reason
		lift := crashOnLoopWrite(t, s)
		if err := s.UpdateHandoffReviewPassWithLoop(ctx, unable, stuck); err == nil {
			t.Fatal("the crash was not seen")
		}
		if got, _ := s.GetHandoffReviewPass(ctx, "widget-card", 2); got.State != "reviewing" {
			t.Fatalf("pass 2 = %+v; want it as it was", got)
		}
		lift()
		if err := s.UpdateHandoffReviewPassWithLoop(ctx, unable, stuck); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.GetHandoffReviewPass(ctx, "widget-card", 2); got.State != "unable" || mustLoop(t, s).Phase != "stuck" {
			t.Fatalf("pass 2 = %+v, loop = %+v", got, mustLoop(t, s))
		}
	})

	t.Run("posting a pass", func(t *testing.T) {
		s, loop := loopAtPassOne(t)
		next := loop
		next.Pass, next.Phase = 2, "reviewing"
		if err := s.StartHandoffReviewPass(ctx, pass2, pinned, next, 1); err != nil {
			t.Fatal(err)
		}
		posted := pass2
		posted.State, posted.Verdict = "posted", "changes"
		waiting := next
		waiting.Phase = "waiting_for_author"
		items := []HandoffReviewItem{{ID: "f-2", TaskID: "widget-card", Kind: "blocking", Pass: 2, PR: 7, Version: 1, Title: "t", CreatedAt: "2024-01-02T00:00:00Z"}}
		lift := crashOnLoopWrite(t, s)
		if err := s.FinishHandoffReviewPass(ctx, posted, items, waiting); err == nil {
			t.Fatal("the crash was not seen")
		}
		if got, _ := s.GetHandoffReviewPass(ctx, "widget-card", 2); got.State != "reviewing" {
			t.Fatalf("pass 2 = %+v; want it as it was", got)
		}
		if got, _ := s.ListHandoffReviewItems(ctx, "widget-card"); len(got) != 0 {
			t.Fatalf("items = %+v; want none raised", got)
		}
		lift()
		if err := s.FinishHandoffReviewPass(ctx, posted, items, waiting); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.ListHandoffReviewItems(ctx, "widget-card"); len(got) != 1 || mustLoop(t, s).Phase != "waiting_for_author" {
			t.Fatalf("items = %+v, loop = %+v", got, mustLoop(t, s))
		}
	})

	t.Run("discarding a pass", func(t *testing.T) {
		s, loop := loopAtPassOne(t)
		next := loop
		next.Pass, next.Phase = 2, "reviewing"
		if err := s.StartHandoffReviewPass(ctx, pass2, pinned, next, 1); err != nil {
			t.Fatal(err)
		}
		if err := s.SubmitHandoffReviewPass(ctx, HandoffReviewPass{TaskID: "widget-card", Pass: 2, State: "submitted", Verdict: "clean"}, nil,
			[]HandoffReviewRuling{{Finding: "f-1", Status: "resolved", Ruling: "fixed"}}); err != nil {
			t.Fatal(err)
		}
		discarded := pass2
		discarded.State, discarded.Reason = "discarded", "#7 moved"
		back := next
		back.Pass, back.Phase = 1, "ready"
		lift := crashOnLoopWrite(t, s)
		if err := s.DiscardHandoffReviewPass(ctx, discarded, back); err == nil {
			t.Fatal("the crash was not seen")
		}
		if got, _ := s.GetHandoffReviewPass(ctx, "widget-card", 2); got.State != "submitted" {
			t.Fatalf("pass 2 = %+v; want it as it was", got)
		}
		if f, _ := s.ListHandoffReviewFindings(ctx, "widget-card"); len(f) != 1 || f[0].Status != "resolved" {
			t.Fatalf("findings = %+v; want pass 2's ruling to stand", f)
		}
		lift()
		if err := s.DiscardHandoffReviewPass(ctx, discarded, back); err != nil {
			t.Fatal(err)
		}
		if f, _ := s.ListHandoffReviewFindings(ctx, "widget-card"); len(f) != 1 || f[0].Status != "open" || mustLoop(t, s).Pass != 1 {
			t.Fatalf("findings = %+v, loop = %+v", f, mustLoop(t, s))
		}
	})

	t.Run("holding a loop for Retry", func(t *testing.T) {
		s, loop := loopAtPassOne(t)
		next := loop
		next.Pass, next.Phase = 2, "reviewing"
		if err := s.StartHandoffReviewPass(ctx, pass2, pinned, next, 1); err != nil {
			t.Fatal(err)
		}
		lift := crashOnLoopWrite(t, s)
		if _, err := s.HoldHandoffReviewForRetry(ctx, "widget-card", "the step failed", "2024-01-03T00:00:00Z"); err == nil {
			t.Fatal("the crash was not seen")
		}
		if got, _ := s.GetHandoffReviewPass(ctx, "widget-card", 2); got.State != "reviewing" {
			t.Fatalf("pass 2 = %+v; want it as it was", got)
		}
		lift()
		if _, err := s.HoldHandoffReviewForRetry(ctx, "widget-card", "the step failed", "2024-01-03T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
		got, _ := s.GetHandoffReviewPass(ctx, "widget-card", 2)
		if got.State != "discarded" || mustLoop(t, s).Pass != 1 || mustLoop(t, s).Phase != "ready" {
			t.Fatalf("pass 2 = %+v, loop = %+v", got, mustLoop(t, s))
		}
	})
}

// A submission's findings, rulings and the pass's state are one
// transaction: a submission that fails part-way leaves none of them.
func TestASubmissionIsWrittenWholeOrNotAtAll(t *testing.T) {
	ctx := context.Background()
	s, loop := loopAtPassOne(t)
	next := loop
	next.Pass, next.Phase = 2, "reviewing"
	if err := s.StartHandoffReviewPass(ctx, HandoffReviewPass{TaskID: "widget-card", Pass: 2, State: "reviewing", StartedAt: "2024-01-02T00:00:00Z"}, nil, next, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER crash_pass_update BEFORE UPDATE ON review_loop_passes BEGIN SELECT RAISE(ABORT, 'crash between writes'); END`); err != nil {
		t.Fatal(err)
	}
	err := s.SubmitHandoffReviewPass(ctx, HandoffReviewPass{TaskID: "widget-card", Pass: 2, State: "submitted", Verdict: "changes"},
		[]HandoffReviewFinding{{ID: "f-2", TaskID: "widget-card", Pass: 2, PR: 7, Severity: "blocking", Title: "new", Status: "open"}},
		[]HandoffReviewRuling{{Finding: "f-1", Status: "resolved", Ruling: "fixed"}})
	if err == nil {
		t.Fatal("the crash was not seen")
	}
	if f, _ := s.ListHandoffReviewFindings(ctx, "widget-card"); len(f) != 1 || f[0].ID != "f-1" || f[0].Status != "open" {
		t.Fatalf("findings = %+v; want only pass 1's, unruled", f)
	}
}

// Holding a loop for Retry keeps what a Retry resumes: a loop waiting for
// its author keeps its phase and pass; one a cancel ended after a pass that
// asked for changes waits for its author again.
func TestHoldingALoopForRetryKeepsWhereItStood(t *testing.T) {
	ctx := context.Background()
	s, loop := loopAtPassOne(t)
	loop.Phase = "waiting_for_author"
	if err := s.SaveHandoffReview(ctx, loop); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.HoldHandoffReviewForRetry(ctx, "widget-card", "failed", "2024-01-03T00:00:00Z"); err != nil || changed {
		t.Fatalf("changed = %v, %v; want a waiting loop left as it is", changed, err)
	}
	if got := mustLoop(t, s); got.Phase != "waiting_for_author" || got.Pass != 1 {
		t.Fatalf("loop = %+v", got)
	}
	loop.Phase, loop.Reason = "ended", "cancelled"
	if err := s.SaveHandoffReview(ctx, loop); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.HoldHandoffReviewForRetry(ctx, "widget-card", "failed", "2024-01-03T00:00:00Z"); err != nil || !changed {
		t.Fatalf("changed = %v, %v", changed, err)
	}
	if got := mustLoop(t, s); got.Phase != "waiting_for_author" || got.Pass != 1 || got.Reason != "" {
		t.Fatalf("loop = %+v", got)
	}
	if f, _ := s.ListHandoffReviewFindings(ctx, "widget-card"); len(f) != 1 {
		t.Fatalf("findings = %+v", f)
	}
}
