package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

type recordingAttentionHandler struct {
	calls []AttentionActionRequest
	err   error
}

func (h *recordingAttentionHandler) Execute(_ context.Context, request AttentionActionRequest) error {
	h.calls = append(h.calls, request)
	return h.err
}

func TestAttentionActionDelegatesToOwningProducer(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "attention-actions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	svc := NewAttentionService(db, nil)
	handler := &recordingAttentionHandler{}
	if err := svc.RegisterHandler("example", handler); err != nil {
		t.Fatal(err)
	}
	item, err := svc.Upsert(context.Background(), AttentionInput{
		Producer: "example", SourceKey: "question-1", SourceRevision: "4",
		Title: "Question", Reason: "Reply needed", Category: "question", Urgency: "normal",
		Navigation: AttentionNavigation{Kind: "example", Target: "question-1"},
		Actions:    []AttentionAction{{ID: "reply", Label: "Reply", Kind: "execute", RequiresText: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = svc.Execute(context.Background(), item.ID, AttentionActionRequest{
		ActionID: "reply", ExpectedRevision: "4", Text: "Proceed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(handler.calls) != 1 || handler.calls[0].SourceKey != "question-1" || handler.calls[0].Text != "Proceed" {
		t.Fatalf("calls = %+v", handler.calls)
	}
	if err := svc.Execute(context.Background(), item.ID, AttentionActionRequest{ActionID: "reply", ExpectedRevision: "3", Text: "stale"}); err != ErrAttentionConflict {
		t.Fatalf("stale error = %v, want ErrAttentionConflict", err)
	}
	if err := svc.Execute(context.Background(), item.ID, AttentionActionRequest{ActionID: "missing", ExpectedRevision: "4"}); err == nil {
		t.Fatal("unknown action succeeded")
	}
}

type staticAttentionProducer struct {
	name  string
	items []AttentionInput
	err   error
}

func (p *staticAttentionProducer) Producer() string { return p.name }
func (p *staticAttentionProducer) Snapshot(context.Context) ([]AttentionInput, error) {
	return p.items, p.err
}

func TestAttentionReconcileResolvesMissingOnlyAfterSuccessfulSnapshot(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "attention-reconcile.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	svc := NewAttentionService(db, nil)
	producer := &staticAttentionProducer{name: "example", items: []AttentionInput{{
		SourceKey: "one", SourceRevision: "1", Title: "One", Reason: "Needs attention",
		Category: "approval", Urgency: "normal",
		Navigation: AttentionNavigation{Kind: "example", Target: "one"},
	}}}
	if err := svc.RegisterProducer(producer); err != nil {
		t.Fatal(err)
	}
	if err := svc.Reconcile(context.Background(), "example"); err != nil {
		t.Fatal(err)
	}
	items, _ := svc.List(context.Background(), AttentionFilter{})
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}

	producer.err = errors.New("source unavailable")
	producer.items = nil
	if err := svc.Reconcile(context.Background(), "example"); err == nil {
		t.Fatal("failed snapshot reconciled successfully")
	}
	items, _ = svc.List(context.Background(), AttentionFilter{})
	if len(items) != 1 {
		t.Fatalf("failed snapshot erased item: %+v", items)
	}

	producer.err = nil
	if err := svc.Reconcile(context.Background(), "example"); err != nil {
		t.Fatal(err)
	}
	items, _ = svc.List(context.Background(), AttentionFilter{})
	if len(items) != 0 {
		t.Fatalf("missing source was not resolved: %+v", items)
	}
}

func TestPlanAttentionCanBeApprovedThroughInbox(t *testing.T) {
	db, planSvc, bundle := newPlanFixture(t)
	ctx := context.Background()
	plan, err := planSvc.Submit(ctx, "task-1", "search", bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := planSvc.Respond(ctx, plan.Plan.ID, "backend", PlanResponseInput{Values: []string{"sqlite"}}); err != nil {
		t.Fatal(err)
	}

	attention := NewAttentionService(db, nil)
	adapter := NewPlanAttentionAdapter(planSvc)
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
	if len(items) != 1 || items[0].Navigation.Kind != "plan" || items[0].Navigation.Target != plan.Plan.ID {
		t.Fatalf("items = %+v", items)
	}

	if err := attention.Execute(ctx, items[0].ID, AttentionActionRequest{
		ActionID: "approve", ExpectedRevision: items[0].SourceRevision, Confirmed: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := attention.Reconcile(ctx, adapter.Producer()); err != nil {
		t.Fatal(err)
	}
	items, _ = attention.List(ctx, AttentionFilter{})
	if len(items) != 0 {
		t.Fatalf("approved plan remains in inbox: %+v", items)
	}
}

func TestAttentionItemLifecycle(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "attention.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	svc := NewAttentionService(db, nil)
	ctx := context.Background()
	input := AttentionInput{
		Producer: "example", SourceKey: "approval-1", SourceRevision: "1",
		TaskID: "task-1", Title: "Approval needed", Reason: "A decision is required",
		Category: "approval", Urgency: "normal",
		Navigation: AttentionNavigation{Kind: "example", Target: "approval-1"},
		Actions:    []AttentionAction{{ID: "open", Label: "Open", Kind: "navigate"}},
	}

	first, err := svc.Upsert(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Reason = "Updated reason"
	second, err := svc.Upsert(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("duplicate source created a new item: %q != %q", second.ID, first.ID)
	}
	items, err := svc.List(ctx, AttentionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Reason != "Updated reason" {
		t.Fatalf("items = %+v", items)
	}

	if err := svc.Dismiss(ctx, first.ID, "1"); err != nil {
		t.Fatal(err)
	}
	items, _ = svc.List(ctx, AttentionFilter{})
	if len(items) != 0 {
		t.Fatalf("dismissed item remains active: %+v", items)
	}
	if _, err := svc.Upsert(ctx, input); err != nil {
		t.Fatal(err)
	}
	items, _ = svc.List(ctx, AttentionFilter{})
	if len(items) != 0 {
		t.Fatalf("same revision resurfaced dismissed item: %+v", items)
	}

	input.SourceRevision = "2"
	if _, err := svc.Upsert(ctx, input); err != nil {
		t.Fatal(err)
	}
	items, _ = svc.List(ctx, AttentionFilter{})
	if len(items) != 1 || items[0].SourceRevision != "2" {
		t.Fatalf("new revision did not resurface item: %+v", items)
	}

	if err := svc.Resolve(ctx, "example", "approval-1"); err != nil {
		t.Fatal(err)
	}
	items, _ = svc.List(ctx, AttentionFilter{})
	if len(items) != 0 {
		t.Fatalf("resolved item remains active: %+v", items)
	}
}
