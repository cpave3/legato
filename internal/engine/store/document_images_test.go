package store

import (
	"context"
	"testing"
)

func TestPlanRevisionsKeepTheirOwnImages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, Task{ID: "t1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	plan := Plan{ID: "pl-t1", TaskID: "t1", Title: "Plan", Status: "proposed", LatestRevision: 1}
	if err := s.InsertPlanRevision(ctx, plan,
		PlanRevision{ID: "pr-1", PlanID: "pl-t1", Revision: 1, Markdown: "one", ManifestJSON: "{}", Images: `[{"name":"assets/a.png","file":"aa.png"}]`}, nil); err != nil {
		t.Fatal(err)
	}
	plan.LatestRevision = 2
	if err := s.InsertPlanRevision(ctx, plan,
		PlanRevision{ID: "pr-2", PlanID: "pl-t1", Revision: 2, Markdown: "two", ManifestJSON: "{}"}, nil); err != nil {
		t.Fatal(err)
	}

	first, err := s.GetPlanRevision(ctx, "pl-t1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Images != `[{"name":"assets/a.png","file":"aa.png"}]` {
		t.Fatalf("first revision's images = %q", first.Images)
	}
	second, err := s.GetPlanRevision(ctx, "pl-t1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if second.Images != "" {
		t.Fatalf("second revision's images = %q, want none", second.Images)
	}
}

func TestReviewPassPlanCarriesTheCapturedRevisionsImages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, Task{ID: "t1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertPlanRevision(ctx,
		Plan{ID: "pl-t1", TaskID: "t1", Title: "Plan", Status: "proposed", LatestRevision: 1},
		PlanRevision{ID: "pr-1", PlanID: "pl-t1", Revision: 1, Markdown: "one", ManifestJSON: "{}", Images: `[{"name":"assets/a.png","file":"aa.png"}]`}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ApprovePlan(ctx, "pl-t1", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureReviewTour(ctx, "t1", ""); err != nil {
		t.Fatal(err)
	}
	passes, err := s.ListReviewPasses(ctx, "rt-t1")
	if err != nil || len(passes) != 1 {
		t.Fatalf("passes = %+v, err = %v", passes, err)
	}
	captured, err := s.GetReviewPassPlan(ctx, passes[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if captured.Images != `[{"name":"assets/a.png","file":"aa.png"}]` {
		t.Fatalf("captured plan's images = %q", captured.Images)
	}
}

func TestReviewChaptersKeepTheirImages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, Task{ID: "t1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureReviewTour(ctx, "t1", ""); err != nil {
		t.Fatal(err)
	}
	chapter := ReviewStep{ID: "rc-1", TaskID: "t1", TourID: "rt-t1", Kind: "chapter", Files: "[]", Title: "Chapter", Narration: "![a](a.png)", Images: `[{"name":"a.png","file":"aa.png"}]`}
	if err := s.InsertReviewChapter(ctx, chapter, nil); err != nil {
		t.Fatal(err)
	}
	stored, err := s.GetReviewStep(ctx, "rc-1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Images != chapter.Images {
		t.Fatalf("chapter's images = %q", stored.Images)
	}

	stored.Narration = "![b](b.png)"
	stored.Images = `[{"name":"b.png","file":"bb.png"}]`
	if err := s.UpdateReviewChapter(ctx, *stored, nil, false); err != nil {
		t.Fatal(err)
	}
	edited, err := s.GetReviewStep(ctx, "rc-1")
	if err != nil {
		t.Fatal(err)
	}
	if edited.Images != `[{"name":"b.png","file":"bb.png"}]` {
		t.Fatalf("edited chapter's images = %q", edited.Images)
	}
}

func TestReviewAnswersKeepTheirImages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, Task{ID: "t1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureReviewTour(ctx, "t1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertReviewMessage(ctx, ReviewMessage{TaskID: "t1", TourID: "rt-t1", StepID: "rs-1", Kind: "question", Author: "user", Body: "how does it look?"}, true); err != nil {
		t.Fatal(err)
	}
	answer, err := s.ReplaceLatestReviewAnswer(ctx, ReviewMessage{TaskID: "t1", TourID: "rt-t1", StepID: "rs-1", Author: "agent", Body: "![now](now.png)", Images: `[{"name":"now.png","file":"aa.png"}]`})
	if err != nil {
		t.Fatal(err)
	}
	if answer.Images != `[{"name":"now.png","file":"aa.png"}]` {
		t.Fatalf("answer's images = %q", answer.Images)
	}
	// An answer given again replaces the first, images included.
	again, err := s.ReplaceLatestReviewAnswer(ctx, ReviewMessage{TaskID: "t1", TourID: "rt-t1", StepID: "rs-1", Author: "agent", Body: "no picture"})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != answer.ID || again.Images != "" {
		t.Fatalf("replaced answer = %+v", again)
	}
}

func TestPlanAnswersKeepTheirImages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, Task{ID: "t1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertPlanRevision(ctx,
		Plan{ID: "pl-t1", TaskID: "t1", Title: "Plan", Status: "proposed", LatestRevision: 1},
		PlanRevision{ID: "pr-1", PlanID: "pl-t1", Revision: 1, Markdown: "one", ManifestJSON: "{}"}, nil); err != nil {
		t.Fatal(err)
	}
	message, err := s.InsertPlanMessage(ctx, PlanMessage{PlanID: "pl-t1", RevisionID: "pr-1", ThreadID: "th-1", Kind: "answer", Author: "agent", Body: "![now](now.png)", Images: `[{"name":"now.png","file":"aa.png"}]`}, true)
	if err != nil {
		t.Fatal(err)
	}
	if message.Images != `[{"name":"now.png","file":"aa.png"}]` {
		t.Fatalf("message's images = %q", message.Images)
	}
}
