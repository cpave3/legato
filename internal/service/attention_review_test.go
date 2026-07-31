package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestReviewAttentionCanBeCompletedThroughInbox(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "a.go", "package a\n")
	head := gitCommitAll(t, f.repo, "add a")
	_, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{Title: "Package A", Includes: []ChapterInclude{{FilePath: "a.go", Hunk: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Ready(ctx, f.tourID, "Initial review"); err != nil {
		t.Fatal(err)
	}
	_ = head

	attention := NewAttentionService(f.store, nil)
	adapter := NewReviewAttentionAdapter(f.svc)
	if err := attention.RegisterProducer(adapter); err != nil {
		t.Fatal(err)
	}
	if err := attention.RegisterHandler(adapter.Producer(), adapter); err != nil {
		t.Fatal(err)
	}
	if err := attention.Reconcile(ctx, adapter.Producer()); err != nil {
		t.Fatal(err)
	}

	items, err := attention.List(ctx, AttentionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Navigation.Kind != "review" || items[0].Navigation.Target != f.tourID {
		t.Fatalf("items = %+v", items)
	}

	if err := attention.Execute(ctx, items[0].ID, AttentionActionRequest{
		ActionID: "complete", ExpectedRevision: items[0].SourceRevision, Confirmed: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := attention.Reconcile(ctx, adapter.Producer()); err != nil {
		t.Fatal(err)
	}
	items, _ = attention.List(ctx, AttentionFilter{})
	if len(items) != 0 {
		t.Fatalf("completed review remains in inbox: %+v", items)
	}
}

func TestCapturingReviewResolvesExistingAttentionItem(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	if err := f.svc.Ready(ctx, f.tourID, "ready"); err != nil {
		t.Fatal(err)
	}

	attention := NewAttentionService(f.store, nil)
	adapter := NewReviewAttentionAdapter(f.svc)
	if err := attention.RegisterProducer(adapter); err != nil {
		t.Fatal(err)
	}
	if err := attention.Reconcile(ctx, adapter.Producer()); err != nil {
		t.Fatal(err)
	}
	items, _ := attention.List(ctx, AttentionFilter{})
	if len(items) != 1 {
		t.Fatalf("ready items = %+v", items)
	}

	if err := f.svc.RestartWithFeedback(ctx, f.tourID, "continue working"); err != nil {
		t.Fatal(err)
	}
	if err := attention.Reconcile(ctx, adapter.Producer()); err != nil {
		t.Fatal(err)
	}
	items, _ = attention.List(ctx, AttentionFilter{})
	if len(items) != 0 {
		t.Fatalf("capturing review remains in attention API/Stream Deck snapshot: %+v", items)
	}
}

func TestWorkflowAttentionSnapshotReportsManualStep(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "attention-workflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Simulate a run with a waiting_manual step by inserting minimal records.
	// The adapter reads ListRuns (which sets NeedsAttention) and Show (which
	// returns step status). We use a fake source to avoid full workflow setup.
	fake := &fakeWorkflowAttentionSource{
		runs: []store.WorkflowRun{
			{ID: "wr-1", WorkflowTitle: "Deploy", TaskID: "task-1", Status: "waiting",
				CurrentStepID: "approve", NeedsAttention: true, AttentionReason: "manual action required: Approve"},
		},
		view: &store.WorkflowRunView{
			Run: store.WorkflowRun{ID: "wr-1", Status: "waiting", CurrentStepID: "approve"},
			Steps: []store.WorkflowStepRun{
				{RunID: "wr-1", StepID: "approve", Status: "waiting_manual", Title: "Approve"},
			},
		},
	}

	adapter := NewWorkflowAttentionAdapter(fake)
	items, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].SourceKey != "wr-1:approve" || items[0].Reason != "Manual action required: Approve" {
		t.Fatalf("items = %+v", items)
	}
	if len(items[0].Actions) != 2 || items[0].Actions[1].ID != "trigger" {
		t.Fatalf("actions = %+v", items[0].Actions)
	}
}
