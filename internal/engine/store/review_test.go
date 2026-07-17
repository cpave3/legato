package store

import (
	"context"
	"errors"
	"testing"
)

func TestDeleteReviewTourRemovesAllReviewArtifacts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, Task{ID: "t1", Title: "Keep me", Status: "Doing", CreatedAt: "2024-01-01", UpdatedAt: "2024-01-01"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureReviewTour(ctx, "t1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertReviewStep(ctx, ReviewStep{ID: "rs-1", TaskID: "t1", TourID: "rt-t1", Kind: "commit", CommitSHA: "abc", Seq: 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertReviewHunkNote(ctx, ReviewHunkNote{ID: "rhn-1", TaskID: "t1", TourID: "rt-t1", StepID: "rs-1", FilePath: "a.go", HunkAnchor: "h1", Body: "note"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertReviewMessage(ctx, ReviewMessage{TaskID: "t1", TourID: "rt-t1", StepID: "rs-1", Kind: "question", Author: "user", Body: "why"}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertReviewChapter(ctx, ReviewStep{ID: "rc-1", TaskID: "t1", TourID: "rt-t1", Kind: "chapter", Files: "[]", Title: "Chapter", Seq: 1}, []ReviewChapterHunk{{ID: "rch-1", TaskID: "t1", TourID: "rt-t1", StepID: "rc-1", FilePath: "a.go", HunkAnchor: "h1"}}); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteReviewTour(ctx, "rt-t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetReviewTour(ctx, "rt-t1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("tour err = %v", err)
	}
	if steps, _ := s.ListReviewSteps(ctx, "rt-t1"); len(steps) != 0 {
		t.Fatalf("steps = %+v", steps)
	}
	if notes, _ := s.ListReviewHunkNotes(ctx, "rt-t1"); len(notes) != 0 {
		t.Fatalf("notes = %+v", notes)
	}
	if msgs, _ := s.ListReviewMessages(ctx, "rt-t1"); len(msgs) != 0 {
		t.Fatalf("messages = %+v", msgs)
	}
	if hunks, _ := s.ListReviewChapterHunks(ctx, "rc-1"); len(hunks) != 0 {
		t.Fatalf("chapter hunks = %+v", hunks)
	}
	if _, err := s.GetTask(ctx, "t1"); err != nil {
		t.Fatalf("task was deleted: %v", err)
	}
}

func TestInsertReviewStepDedupesOnCommitSHA(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	inserted, err := s.InsertReviewStep(ctx, ReviewStep{ID: "rs-1", TaskID: "t1", TourID: "rt-t1", Kind: "commit", CommitSHA: "abc", Seq: 0})
	if err != nil || !inserted {
		t.Fatalf("first insert: inserted=%v err=%v", inserted, err)
	}
	inserted, err = s.InsertReviewStep(ctx, ReviewStep{ID: "rs-2", TaskID: "t1", TourID: "rt-t1", Kind: "commit", CommitSHA: "abc", Seq: 1})
	if err != nil {
		t.Fatal(err)
	}
	if inserted {
		t.Fatal("duplicate commit SHA must not insert a second step")
	}
	// Same SHA on a different task is a distinct step.
	inserted, err = s.InsertReviewStep(ctx, ReviewStep{ID: "rs-3", TaskID: "t2", TourID: "rt-t2", Kind: "commit", CommitSHA: "abc", Seq: 0})
	if err != nil || !inserted {
		t.Fatalf("other-task insert: inserted=%v err=%v", inserted, err)
	}
}

func TestListReviewStepsHonorsOrderHintThenSeq(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	two := 1
	mustInsertStep(t, s, ReviewStep{ID: "rs-a", TaskID: "t1", TourID: "rt-t1", Kind: "commit", CommitSHA: "a", Seq: 0})
	mustInsertStep(t, s, ReviewStep{ID: "rs-b", TaskID: "t1", TourID: "rt-t1", Kind: "commit", CommitSHA: "b", Seq: 1})
	mustInsertStep(t, s, ReviewStep{ID: "rs-c", TaskID: "t1", TourID: "rt-t1", Kind: "commit", CommitSHA: "c", Seq: 2, OrderHint: &two})

	steps, err := s.ListReviewSteps(ctx, "rt-t1")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{steps[0].ID, steps[1].ID, steps[2].ID}
	// rs-c has an explicit order hint so it reads first; the rest follow seq.
	want := []string{"rs-c", "rs-a", "rs-b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func mustInsertStep(t *testing.T, s *Store, step ReviewStep) {
	t.Helper()
	if _, err := s.InsertReviewStep(context.Background(), step); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateReviewStepAndPrefixLookup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustInsertStep(t, s, ReviewStep{ID: "rs-abc12345", TaskID: "t1", Kind: "commit", CommitSHA: "a", Seq: 0})

	if _, err := s.UpdateReviewStep(ctx, "rs-abc12345", func(st *ReviewStep) {
		st.Narration = "why I did this"
		st.Risk = "high"
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetReviewStepByPrefix(ctx, "t1", "rs-abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.Narration != "why I did this" || got.Risk != "high" {
		t.Fatalf("step = %+v", got)
	}

	if _, err := s.GetReviewStepByPrefix(ctx, "t1", "rs-zzz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing prefix err = %v, want ErrNotFound", err)
	}

	// Ambiguous prefixes must be rejected, not silently resolved.
	mustInsertStep(t, s, ReviewStep{ID: "rs-abc99999", TaskID: "t1", Kind: "commit", CommitSHA: "b", Seq: 1})
	if _, err := s.GetReviewStepByPrefix(ctx, "t1", "rs-abc"); err == nil {
		t.Fatal("ambiguous prefix must error")
	}
}

func TestMarkReviewStepsOrphaned(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustInsertStep(t, s, ReviewStep{ID: "rs-live", TaskID: "t1", Kind: "commit", CommitSHA: "aaa", Seq: 0})
	mustInsertStep(t, s, ReviewStep{ID: "rs-gone", TaskID: "t1", Kind: "commit", CommitSHA: "bbb", Seq: 1})
	mustInsertStep(t, s, ReviewStep{ID: "rs-dirty", TaskID: "t1", Kind: "dirty", Seq: 999999})

	if err := s.MarkReviewStepsOrphaned(ctx, "t1", []string{"aaa"}); err != nil {
		t.Fatal(err)
	}

	gone, _ := s.GetReviewStep(ctx, "rs-gone")
	if gone.OrphanedAt == nil {
		t.Fatal("rs-gone should be orphaned")
	}
	live, _ := s.GetReviewStep(ctx, "rs-live")
	if live.OrphanedAt != nil {
		t.Fatal("rs-live should not be orphaned")
	}
	dirty, _ := s.GetReviewStep(ctx, "rs-dirty")
	if dirty.OrphanedAt != nil {
		t.Fatal("non-commit steps are never orphaned")
	}

	// A SHA that reappears (e.g. branch reset back) heals its step.
	if err := s.MarkReviewStepsOrphaned(ctx, "t1", []string{"aaa", "bbb"}); err != nil {
		t.Fatal(err)
	}
	healed, _ := s.GetReviewStep(ctx, "rs-gone")
	if healed.OrphanedAt != nil {
		t.Fatal("rs-gone should be healed when its SHA is live again")
	}
}

func TestReviewHunkNoteLineRangeMigratesAndPersists(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, Task{ID: "t1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureReviewTour(ctx, "t1", ""); err != nil {
		t.Fatal(err)
	}
	start, end := 2, 4
	note := ReviewHunkNote{ID: "rhn-lines", TaskID: "t1", TourID: "rt-t1", StepID: "step", FilePath: "a.go", HunkAnchor: "hunk", LineStart: &start, LineEnd: &end, LineAnchor: "lines", Body: "range"}
	if err := s.InsertReviewHunkNote(ctx, note); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetReviewHunkNoteByPrefix(ctx, "rt-t1", "rhn-l")
	if err != nil {
		t.Fatal(err)
	}
	if got.LineStart == nil || got.LineEnd == nil || *got.LineStart != 2 || *got.LineEnd != 4 || got.LineAnchor != "lines" || got.UpdatedAt == "" {
		t.Fatalf("note = %+v", got)
	}
}

func TestReviewHunkNotesPersistAndAllowMultiplePerHunk(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	notes := []ReviewHunkNote{
		{ID: "rhn-1", TaskID: "t1", TourID: "rt-t1", StepID: "rs-1", FilePath: "main.go", HunkAnchor: "anchor", Body: "first"},
		{ID: "rhn-2", TaskID: "t1", TourID: "rt-t1", StepID: "rs-1", FilePath: "main.go", HunkAnchor: "anchor", Body: "second"},
	}
	for _, note := range notes {
		if err := s.InsertReviewHunkNote(ctx, note); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.ListReviewHunkNotes(ctx, "rt-t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Body != "first" || got[1].Body != "second" {
		t.Fatalf("notes = %+v", got)
	}
	if got[0].CreatedAt == "" {
		t.Fatal("CreatedAt was not populated")
	}
}

func TestReviewTranscriptAppendAndList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	delivered := true
	q, err := s.InsertReviewMessage(ctx, ReviewMessage{TaskID: "t1", TourID: "rt-t1", StepID: "rs-1", Kind: "question", Author: "user", Body: "why this?"}, delivered)
	if err != nil {
		t.Fatal(err)
	}
	if q.DeliveredAt == nil {
		t.Fatal("delivered question must have delivered_at set")
	}
	undelivered, err := s.InsertReviewMessage(ctx, ReviewMessage{TaskID: "t1", TourID: "rt-t1", StepID: "rs-1", Kind: "question", Author: "user", Body: "agent offline"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if undelivered.DeliveredAt != nil {
		t.Fatal("undelivered question must have NULL delivered_at")
	}
	if _, err := s.InsertReviewMessage(ctx, ReviewMessage{TaskID: "t1", TourID: "rt-t1", StepID: "rs-1", Kind: "answer", Author: "agent", Body: "because"}, true); err != nil {
		t.Fatal(err)
	}

	msgs, err := s.ListReviewMessages(ctx, "rt-t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("len(msgs) = %d, want 3", len(msgs))
	}
	if msgs[0].Body != "why this?" || msgs[2].Kind != "answer" {
		t.Fatalf("order/content wrong: %+v", msgs)
	}
}

func TestUnreviewedReviewCounts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustInsertStep(t, s, ReviewStep{ID: "rs-1", TaskID: "t1", TourID: "rt-t1", Kind: "commit", CommitSHA: "a", Seq: 0})
	mustInsertStep(t, s, ReviewStep{ID: "rs-2", TaskID: "t1", TourID: "rt-t1", Kind: "commit", CommitSHA: "b", Seq: 1})
	mustInsertStep(t, s, ReviewStep{ID: "rs-3", TaskID: "t2", TourID: "rt-t2", Kind: "commit", CommitSHA: "a", Seq: 0})

	if _, err := s.UpdateReviewStep(ctx, "rs-2", func(st *ReviewStep) {
		now := "2026-07-15 00:00:00"
		st.ReviewedAt = &now
	}); err != nil {
		t.Fatal(err)
	}

	counts, err := s.UnreviewedReviewCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts["t1"] != 1 || counts["t2"] != 1 {
		t.Fatalf("counts = %v", counts)
	}
}

func TestEnsureReviewTourCreatesAndIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tour, err := s.EnsureReviewTour(ctx, "task-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if tour.Status != "capturing" {
		t.Fatalf("Status = %q, want capturing", tour.Status)
	}

	if _, err := s.UpdateReviewTour(ctx, "rt-task-1", func(rt *ReviewTour) {
		rt.Status = "ready"
		rt.Summary = "did the thing"
	}); err != nil {
		t.Fatal(err)
	}

	again, err := s.EnsureReviewTour(ctx, "task-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != "ready" || again.Summary != "did the thing" {
		t.Fatalf("Ensure clobbered existing tour: %+v", again)
	}
}
