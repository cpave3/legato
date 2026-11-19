package store

import (
	"context"
	"errors"
	"testing"
)

func seedScopeTask(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := s.CreateTask(context.Background(), Task{ID: id, Title: id, Status: "Doing", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
}

func scopeBatchFixture(origin string, ids ...string) (ScopeBatch, []ScopePin) {
	batch := ScopeBatch{ID: "sb-1", Origin: origin, CreatedAt: "2026-01-01T00:00:00Z"}
	var pins []ScopePin
	for i, id := range ids {
		pins = append(pins, ScopePin{ID: id, BatchID: batch.ID, Origin: origin, Seq: i, URL: origin + "/a", Path: "/a", Comment: "c " + id, State: ScopeSent, CreatedAt: batch.CreatedAt, UpdatedAt: batch.CreatedAt})
	}
	return batch, pins
}

func TestScopeBatchesKeepTheirPinsInOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	batch, pins := scopeBatchFixture("http://localhost:5173", "sp-1", "sp-2")
	if err := s.InsertScopeBatch(ctx, batch, pins); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListScopePins(ctx, ScopePinFilter{Origins: []string{"http://localhost:5173"}})
	if err != nil || len(got) != 2 || got[0].ID != "sp-1" || got[1].ID != "sp-2" {
		t.Fatalf("pins = %+v, %v", got, err)
	}
	if other, _ := s.ListScopePins(ctx, ScopePinFilter{Origins: []string{"http://other"}}); len(other) != 0 {
		t.Fatalf("another origin's pins = %+v", other)
	}
	if none, _ := s.ListScopePins(ctx, ScopePinFilter{Origins: []string{}}); len(none) != 0 {
		t.Fatalf("no origins named, pins = %+v", none)
	}
}

func TestScopePinStateChangesAreConditional(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	batch, pins := scopeBatchFixture("http://localhost:5173", "sp-1", "sp-2")
	if err := s.InsertScopeBatch(ctx, batch, pins); err != nil {
		t.Fatal(err)
	}

	if err := s.ReopenScopePin(ctx, "sp-1", "again", "t"); !errors.Is(err, ErrConflict) {
		t.Fatalf("reopening a sent pin = %v, want ErrConflict", err)
	}
	if err := s.MarkScopePinsDelivered(ctx, []string{"sp-1"}, "t1"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSentScopePin(ctx, "sp-1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("withdrawing a delivered pin = %v, want ErrConflict", err)
	}
	if err := s.ResolveScopePin(ctx, "sp-1", ScopeFixed, "done", "task1", "t2"); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveScopePin(ctx, "sp-1", ScopeWontFix, "again", "task1", "t3"); !errors.Is(err, ErrConflict) {
		t.Fatalf("resolving a resolved pin = %v, want ErrConflict", err)
	}
	pin, err := s.GetScopePin(ctx, "sp-1")
	if err != nil || pin.State != ScopeFixed || pin.Note != "done" || pin.ResolvedByTask != "task1" {
		t.Fatalf("pin = %+v, %v", pin, err)
	}
	if err := s.ReopenScopePin(ctx, "sp-1", "still broken", "t4"); err != nil {
		t.Fatal(err)
	}
	pin, _ = s.GetScopePin(ctx, "sp-1")
	if pin.State != ScopeSent || pin.Comment != "still broken" || pin.Note != "" || pin.DeliveredAt != nil || pin.ResolvedAt != nil {
		t.Fatalf("reopened pin = %+v", pin)
	}

	if err := s.DeleteSentScopePin(ctx, "sp-2"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSentScopePin(ctx, "sp-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetScopePin(ctx, "sp-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("withdrawn pin = %v, want ErrNotFound", err)
	}
	if err := s.DeleteSentScopePin(ctx, "sp-9"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("withdrawing an unknown pin = %v, want ErrNotFound", err)
	}
	var batches int
	if err := s.db.Get(&batches, `SELECT COUNT(*) FROM scope_batches`); err != nil || batches != 0 {
		t.Fatalf("an empty batch was kept: %d, %v", batches, err)
	}
}

func TestAScopeSubscriptionTakesTheOriginFromTheLastSubscriber(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedScopeTask(t, s, "task1")
	seedScopeTask(t, s, "task2")
	origin := "http://localhost:5173"

	previous, err := s.SetScopeSubscription(ctx, ScopeSubscription{Origin: origin, TaskID: "task1", Slot: SlotMain, CreatedAt: "t1"})
	if err != nil || previous != nil {
		t.Fatalf("first subscribe: previous = %+v, %v", previous, err)
	}
	previous, err = s.SetScopeSubscription(ctx, ScopeSubscription{Origin: origin, TaskID: "task2", CreatedAt: "t2"})
	if err != nil || previous == nil || previous.TaskID != "task1" {
		t.Fatalf("takeover: previous = %+v, %v", previous, err)
	}
	sub, err := s.GetScopeSubscription(ctx, origin)
	if err != nil || sub.TaskID != "task2" || sub.Slot != SlotMain {
		t.Fatalf("subscription = %+v, %v", sub, err)
	}
	if ok, _ := s.DeleteScopeSubscription(ctx, origin, "task1", SlotMain); ok {
		t.Fatal("the old subscriber removed the new one's subscription")
	}

	if err := s.DeleteTask(ctx, "task2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetScopeSubscription(ctx, origin); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a deleted task's subscription = %v, want ErrNotFound", err)
	}
}

func TestEndingASlotsScopeSubscriptions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedScopeTask(t, s, "task1")
	for _, origin := range []string{"http://a", "http://b"} {
		if _, err := s.SetScopeSubscription(ctx, ScopeSubscription{Origin: origin, TaskID: "task1", CreatedAt: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	if subs, _ := s.ScopeSubscriptionsOfSlot(ctx, "task1", ""); len(subs) != 2 {
		t.Fatalf("slot subscriptions = %+v", subs)
	}
	origins, err := s.DeleteScopeSubscriptionsOfSlot(ctx, "task1", SlotMain)
	if err != nil || len(origins) != 2 || origins[0] != "http://a" {
		t.Fatalf("ended origins = %v, %v", origins, err)
	}
	if subs, _ := s.ListScopeSubscriptions(ctx); len(subs) != 0 {
		t.Fatalf("subscriptions left = %+v", subs)
	}
}

func TestScopeSlotStatusSaysWhetherTheSlotHasALiveOrInterruptedAgent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedScopeTask(t, s, "task1")
	if status, err := s.ScopeSlotStatus(ctx, "task1", ""); err != nil || status != "" {
		t.Fatalf("no agent: %q, %v", status, err)
	}
	if err := s.InsertAgentSession(ctx, AgentSession{TaskID: "task1", TmuxSession: "legato-task1", Status: AgentSessionInterrupted}); err != nil {
		t.Fatal(err)
	}
	if status, _ := s.ScopeSlotStatus(ctx, "task1", SlotMain); status != AgentSessionInterrupted {
		t.Fatalf("interrupted agent: %q", status)
	}
	if err := s.InsertAgentSession(ctx, AgentSession{TaskID: "task1", TmuxSession: "legato-task1-new", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if status, _ := s.ScopeSlotStatus(ctx, "task1", SlotMain); status != "running" {
		t.Fatalf("running agent: %q", status)
	}
}
