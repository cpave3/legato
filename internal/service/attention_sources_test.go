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

type fakeReviewAttentionSource struct {
	items []ReviewQueueItem
	err   error
}

func (f *fakeReviewAttentionSource) Queue(context.Context) ([]ReviewQueueItem, error) {
	return f.items, f.err
}

func TestReviewAttentionSnapshotProjectsReadyQueue(t *testing.T) {
	source := &fakeReviewAttentionSource{items: []ReviewQueueItem{{
		TourID: "rt-1", TaskID: "task-1", Title: "Attention dashboard", Summary: "Review the grouped page",
		Unreviewed: 3, UpdatedAt: "2026-08-14T01:00:00Z", ActivityAt: "2026-08-14T02:00:00Z", PassID: "rp-2", HeadSHA: "abc123",
	}}}
	items, err := NewReviewAttentionAdapter(source).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].SignalKind != AttentionSignalReviewReady || items[0].Navigation != (AttentionNavigation{Kind: "review", Target: "rt-1"}) {
		t.Fatalf("items = %+v", items)
	}
	if items[0].SourceRevision != "rp-2:abc123:2026-08-14T02:00:00Z:2026-08-14T01:00:00Z:3" {
		t.Fatalf("revision = %q", items[0].SourceRevision)
	}
}

type fakePRAttentionSource struct {
	tasks []store.Task
	err   error
}

func (f *fakePRAttentionSource) ListPRTrackedTasks(context.Context) ([]store.Task, error) {
	return f.tasks, f.err
}

func trackedTask(t *testing.T, id string, meta store.PRMeta) store.Task {
	t.Helper()
	raw, err := store.MarshalPRMeta(&meta)
	if err != nil {
		t.Fatal(err)
	}
	return store.Task{ID: id, Title: "Tracked task", PRMeta: raw}
}

func TestPRAttentionSnapshotProjectsReliableBlockers(t *testing.T) {
	source := &fakePRAttentionSource{tasks: []store.Task{
		trackedTask(t, "both", store.PRMeta{PRNumber: 42, PRURL: "https://example.test/pr/42", State: "OPEN", ReviewDecision: "CHANGES_REQUESTED", CheckStatus: "fail"}),
		trackedTask(t, "review-only", store.PRMeta{PRNumber: 43, State: "OPEN", ReviewDecision: "CHANGES_REQUESTED", CheckStatus: "pass"}),
		trackedTask(t, "ci-only", store.PRMeta{PRNumber: 44, State: "OPEN", ReviewDecision: "APPROVED", CheckStatus: "fail"}),
		trackedTask(t, "approved", store.PRMeta{PRNumber: 45, State: "OPEN", ReviewDecision: "APPROVED", CheckStatus: "pass"}),
		trackedTask(t, "draft", store.PRMeta{PRNumber: 46, State: "OPEN", IsDraft: true, ReviewDecision: "CHANGES_REQUESTED"}),
		trackedTask(t, "pending", store.PRMeta{PRNumber: 47, State: "OPEN", ReviewDecision: "REVIEW_REQUIRED", CheckStatus: "pending"}),
		trackedTask(t, "closed", store.PRMeta{PRNumber: 48, State: "CLOSED", ReviewDecision: "CHANGES_REQUESTED"}),
		trackedTask(t, "merged", store.PRMeta{PRNumber: 49, State: "MERGED", CheckStatus: "fail"}),
		trackedTask(t, "unlinked", store.PRMeta{State: "OPEN", CheckStatus: "fail"}),
	}}
	items, err := NewPRAttentionAdapter(source).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %+v", items)
	}
	reasons := map[string]string{}
	for _, item := range items {
		reasons[item.TaskID] = item.Reason
	}
	if reasons["both"] != "Changes requested · CI failing" || reasons["review-only"] != "Changes requested" || reasons["ci-only"] != "CI failing" {
		t.Fatalf("reasons = %+v", reasons)
	}
}

func TestPRAttentionEventReconciliationDismissesResurfacesAndResolves(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "attention-pr-events.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.CreateTask(context.Background(), store.Task{ID: "task-1", Title: "Tracked task", Status: "Todo"}); err != nil {
		t.Fatal(err)
	}
	setMeta := func(meta store.PRMeta) {
		raw, marshalErr := store.MarshalPRMeta(&meta)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if updateErr := db.UpdatePRMeta(context.Background(), "task-1", raw); updateErr != nil {
			t.Fatal(updateErr)
		}
	}
	setMeta(store.PRMeta{PRNumber: 42, State: "OPEN", CheckStatus: "fail"})

	bus := events.New()
	attention := NewAttentionService(db, bus)
	adapter := NewPRAttentionAdapter(db)
	if err := attention.RegisterProducerWithEvents(adapter, events.EventPRStatusUpdated); err != nil {
		t.Fatal(err)
	}
	stop := attention.StartEventReconciliation(context.Background())
	t.Cleanup(stop)
	bus.Publish(events.Event{Type: events.EventPRStatusUpdated})
	waitForAttentionItems(t, attention, 1)
	items, _ := attention.List(context.Background(), AttentionFilter{})
	if err := attention.Dismiss(context.Background(), items[0].ID, items[0].SourceRevision); err != nil {
		t.Fatal(err)
	}
	waitForAttentionItems(t, attention, 0)

	setMeta(store.PRMeta{PRNumber: 42, State: "OPEN", ReviewDecision: "CHANGES_REQUESTED", CheckStatus: "fail"})
	bus.Publish(events.Event{Type: events.EventPRStatusUpdated})
	waitForAttentionItems(t, attention, 1)
	items, _ = attention.List(context.Background(), AttentionFilter{})
	if items[0].Reason != "Changes requested · CI failing" {
		t.Fatalf("resurfaced item = %+v", items[0])
	}

	setMeta(store.PRMeta{PRNumber: 42, State: "OPEN", ReviewDecision: "APPROVED", CheckStatus: "pass"})
	bus.Publish(events.Event{Type: events.EventPRStatusUpdated})
	waitForAttentionItems(t, attention, 0)
}

func waitForAttentionItems(t *testing.T, attention *AttentionService, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		items, err := attention.List(context.Background(), AttentionFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("attention item count did not become %d", want)
}

func TestPRAttentionSnapshotFailsClosedOnInvalidPersistedMetadata(t *testing.T) {
	bad := "not-json"
	_, err := NewPRAttentionAdapter(&fakePRAttentionSource{tasks: []store.Task{{ID: "task-1", PRMeta: &bad}}}).Snapshot(context.Background())
	if err == nil {
		t.Fatal("invalid persisted metadata did not fail the snapshot")
	}
}

type fakeAgentAttentionSource struct {
	agents []AgentSession
	err    error
}

func (f *fakeAgentAttentionSource) ListAgents(context.Context) ([]AgentSession, error) {
	return f.agents, f.err
}

func TestAgentAttentionSnapshotProjectsOnlyStandaloneIntervention(t *testing.T) {
	source := &fakeAgentAttentionSource{agents: []AgentSession{
		{ExecutionID: "waiting", AttributionTaskID: "task-waiting", Title: "Waiting", ExecutionKind: store.ExecutionInteractive, Status: store.ExecutionRunning, Activity: store.ActivityWaiting},
		{ExecutionID: "error", AttributionTaskID: "task-error", Title: "Errored", ExecutionKind: store.ExecutionInteractive, Status: store.ExecutionRunning, Activity: store.ActivityError},
		{ExecutionID: "working", AttributionTaskID: "task-working", ExecutionKind: store.ExecutionInteractive, Status: store.ExecutionRunning, Activity: store.ActivityWorking},
		{ExecutionID: "workflow", AttributionTaskID: "task-workflow", ExecutionKind: store.ExecutionWorkflowAgent, WorkflowRunID: "run-1", Status: store.ExecutionRunning, Activity: store.ActivityWaiting},
		{ExecutionID: "swarm", AttributionTaskID: "task-swarm", ExecutionKind: store.ExecutionSwarmWorker, ParentTaskID: "parent", Status: store.ExecutionRunning, Activity: store.ActivityError},
		{ExecutionID: "ended", AttributionTaskID: "task-ended", ExecutionKind: store.ExecutionInteractive, Status: store.ExecutionSucceeded, Activity: store.ActivityError},
	}}
	items, err := NewAgentAttentionAdapter(source).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].SignalKind != AttentionSignalAgentWaiting || items[1].SignalKind != AttentionSignalAgentError {
		t.Fatalf("items = %+v", items)
	}
}

func TestAttentionSourceSnapshotErrorsPropagate(t *testing.T) {
	want := errors.New("source unavailable")
	if _, err := NewReviewAttentionAdapter(&fakeReviewAttentionSource{err: want}).Snapshot(context.Background()); !errors.Is(err, want) {
		t.Fatalf("review error = %v", err)
	}
	if _, err := NewPRAttentionAdapter(&fakePRAttentionSource{err: want}).Snapshot(context.Background()); !errors.Is(err, want) {
		t.Fatalf("PR error = %v", err)
	}
	if _, err := NewAgentAttentionAdapter(&fakeAgentAttentionSource{err: want}).Snapshot(context.Background()); !errors.Is(err, want) {
		t.Fatalf("agent error = %v", err)
	}
}
