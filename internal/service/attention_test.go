package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
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
		SignalKind: AttentionSignalPlanDecision,
		Producer:   "example", SourceKey: "question-1", SourceRevision: "4",
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

func TestAttentionEventReconciliationFansOutToEveryRegisteredProducer(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "attention-fanout.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	bus := events.New()
	svc := NewAttentionService(db, bus)
	first := &staticAttentionProducer{name: "first", items: []AttentionInput{{
		SignalKind: AttentionSignalPlanDecision, SourceKey: "one", SourceRevision: "1",
		Title: "One", Reason: "First", Category: "test", Urgency: "normal",
		Navigation: AttentionNavigation{Kind: "task", Target: "one"},
	}}}
	second := &staticAttentionProducer{name: "second", items: []AttentionInput{{
		SignalKind: AttentionSignalPlanDecision, SourceKey: "two", SourceRevision: "1",
		Title: "Two", Reason: "Second", Category: "test", Urgency: "normal",
		Navigation: AttentionNavigation{Kind: "task", Target: "two"},
	}}}
	if err := svc.RegisterProducerWithEvents(first, events.EventAgentDied); err != nil {
		t.Fatal(err)
	}
	if err := svc.RegisterProducerWithEvents(second, events.EventAgentDied); err != nil {
		t.Fatal(err)
	}
	stop := svc.StartEventReconciliation(context.Background())
	t.Cleanup(stop)
	bus.Publish(events.Event{Type: events.EventAgentDied})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		items, listErr := svc.List(context.Background(), AttentionFilter{})
		if listErr != nil {
			t.Fatal(listErr)
		}
		if len(items) == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("shared event did not reconcile both producers")
}

func TestAttentionReconcilePublishesOnlyWhenItemChanges(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "attention-events.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	bus := events.New()
	changed := bus.Subscribe(events.EventAttentionChanged)
	t.Cleanup(func() { bus.Unsubscribe(changed) })
	svc := NewAttentionService(db, bus)
	producer := &staticAttentionProducer{name: "example", items: []AttentionInput{{
		SignalKind: AttentionSignalPlanDecision,
		SourceKey:  "one", SourceRevision: "1", Title: "One", Reason: "Needs attention",
		Category: "approval", Urgency: "normal",
		Navigation: AttentionNavigation{Kind: "example", Target: "one"},
	}}}
	if err := svc.RegisterProducer(producer); err != nil {
		t.Fatal(err)
	}

	if err := svc.Reconcile(context.Background(), "example"); err != nil {
		t.Fatal(err)
	}
	assertAttentionEvent(t, changed)

	if err := svc.Reconcile(context.Background(), "example"); err != nil {
		t.Fatal(err)
	}
	assertNoAttentionEvent(t, changed)

	producer.items[0].SourceRevision = "2"
	producer.items[0].Reason = "Still needs attention"
	if err := svc.Reconcile(context.Background(), "example"); err != nil {
		t.Fatal(err)
	}
	assertAttentionEvent(t, changed)
}

func assertAttentionEvent(t *testing.T, ch <-chan events.Event) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("attention change event was not published")
	}
}

func assertNoAttentionEvent(t *testing.T, ch <-chan events.Event) {
	t.Helper()
	select {
	case event := <-ch:
		t.Fatalf("unexpected attention change event: %+v", event)
	case <-time.After(25 * time.Millisecond):
	}
}

func TestAttentionReconcilePublishesWhenMissingItemsResolve(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "attention-resolve-event.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	bus := events.New()
	changed := bus.Subscribe(events.EventAttentionChanged)
	svc := NewAttentionService(db, bus)
	producer := &staticAttentionProducer{name: "example", items: []AttentionInput{{
		SignalKind: AttentionSignalPlanDecision, SourceKey: "one", SourceRevision: "1",
		Title: "One", Reason: "Needs attention", Category: "approval", Urgency: "normal",
		Navigation: AttentionNavigation{Kind: "task", Target: "one"},
	}}}
	if err := svc.RegisterProducer(producer); err != nil {
		t.Fatal(err)
	}
	if err := svc.Reconcile(context.Background(), producer.name); err != nil {
		t.Fatal(err)
	}
	assertAttentionEvent(t, changed)
	producer.items = nil
	if err := svc.Reconcile(context.Background(), producer.name); err != nil {
		t.Fatal(err)
	}
	event := <-changed
	payload, ok := event.Payload.(events.AttentionChangedPayload)
	if !ok || payload.Producer != "example" || payload.Kind != "resolved" {
		t.Fatalf("resolution payload = %#v", event.Payload)
	}
}

func TestAttentionReconcileResolvesMissingOnlyAfterSuccessfulSnapshot(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "attention-reconcile.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	svc := NewAttentionService(db, nil)
	producer := &staticAttentionProducer{name: "example", items: []AttentionInput{{
		SignalKind: AttentionSignalPlanDecision,
		SourceKey:  "one", SourceRevision: "1", Title: "One", Reason: "Needs attention",
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

func TestChangesRequestedPlanStaysOutOfAttention(t *testing.T) {
	db, planSvc, bundle := newPlanFixture(t)
	ctx := context.Background()
	plan, err := planSvc.Submit(ctx, "task-1", "search", bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := planSvc.RequestChanges(ctx, plan.Plan.ID); err != nil {
		t.Fatal(err)
	}
	queue, err := planSvc.Queue(ctx)
	if err != nil || len(queue) != 1 || queue[0].Status != "changes_requested" {
		t.Fatalf("plan queue = %+v, err = %v", queue, err)
	}

	attention := NewAttentionService(db, nil)
	adapter := NewPlanAttentionAdapter(planSvc)
	if err := attention.RegisterProducer(adapter); err != nil {
		t.Fatal(err)
	}
	if err := attention.Reconcile(ctx, adapter.Producer()); err != nil {
		t.Fatal(err)
	}
	items, err := attention.List(ctx, AttentionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("changes-requested plan leaked into Attention: %+v", items)
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
		SignalKind: AttentionSignalPlanDecision,
		Producer:   "example", SourceKey: "approval-1", SourceRevision: "1",
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
