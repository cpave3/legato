package store

import (
	"context"
	"errors"
	"testing"
)

func TestADelegationIsFoundByItsKeyAndMovesThroughItsStates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	d := Delegation{ID: "dl-1", Integration: "argus", IdempotencyKey: "argus-1", RequestHash: "h", CaseID: "ci:a/b#1", Mode: "fix",
		Target: `{"task":"t1"}`, Brief: "fix it", TaskID: "t1", Route: "message", State: DelegationPending}
	if err := s.InsertDelegation(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertDelegation(ctx, Delegation{ID: "dl-2", Integration: "argus", IdempotencyKey: "argus-1", RequestHash: "h", CaseID: "c", Mode: "fix", Target: "{}", Brief: "b", Route: "start", State: DelegationPending}); !errors.Is(err, ErrConflict) {
		t.Fatalf("same key err = %v", err)
	}
	got, err := s.GetDelegationByKey(ctx, "argus", "argus-1")
	if err != nil || got.ID != "dl-1" || got.Brief != "fix it" {
		t.Fatalf("by key = %+v, %v", got, err)
	}
	if _, err := s.GetDelegationByKey(ctx, "other", "argus-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other integration err = %v", err)
	}
	if err := s.SetDelegationDelivered(ctx, "dl-1", "resume", "ex-9", true); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetDelegation(ctx, "dl-1")
	if got.State != DelegationDelivered || got.Route != "resume" || got.ExecutionID != "ex-9" || !got.Enforced || got.DeliveredAt == nil {
		t.Fatalf("delivered = %+v", got)
	}
	if err := s.SetDelegationFailed(ctx, "dl-1", "spawn_failed", "no tmux"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetDelegation(ctx, "dl-1")
	if got.State != DelegationFailed || got.ErrorCode != "spawn_failed" || got.ErrorMessage != "no tmux" {
		t.Fatalf("failed = %+v", got)
	}
	open, _ := s.ListDelegationsByState(ctx, DelegationPending)
	if len(open) != 0 {
		t.Fatalf("pending = %+v", open)
	}
	byTask, _ := s.ListDelegationsForTask(ctx, "t1")
	if len(byTask) != 1 {
		t.Fatalf("by task = %+v", byTask)
	}
}

func TestADelegationMessageIsIdempotentPerDelegation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_ = s.InsertDelegation(ctx, Delegation{ID: "dl-1", Integration: "argus", IdempotencyKey: "k", RequestHash: "h", CaseID: "c", Mode: "fix", Target: "{}", Brief: "b", Route: "message", State: DelegationDelivered})
	m := DelegationMessage{ID: "dm-1", DelegationID: "dl-1", IdempotencyKey: "m1", RequestHash: "h", Text: "yes", State: DelegationPending}
	if err := s.InsertDelegationMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertDelegationMessage(ctx, DelegationMessage{ID: "dm-2", DelegationID: "dl-1", IdempotencyKey: "m1", RequestHash: "h", Text: "yes", State: DelegationPending}); !errors.Is(err, ErrConflict) {
		t.Fatalf("same key err = %v", err)
	}
	if err := s.SetDelegationMessageState(ctx, "dm-1", DelegationDelivered, "", ""); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetDelegationMessageByKey(ctx, "dl-1", "m1")
	if err != nil || got.State != DelegationDelivered || got.DeliveredAt == nil {
		t.Fatalf("message = %+v, %v", got, err)
	}
}
