package store

import (
	"context"
	"errors"
	"testing"
)

func pushed(key, revision string) IntegrationAttention {
	return IntegrationAttention{Integration: "argus", Key: key, Revision: revision, Signal: "argus_case", Title: "CI failing", Reason: "flaky", Urgency: "normal", Target: `{}`, Link: "https://argus.lan/cases/1"}
}

func TestAPushedItemIsKeptAndUpdated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	got, err := s.UpsertIntegrationAttention(ctx, pushed("case-1", "r1"))
	if err != nil || got.State != IntegrationAttentionOpen || got.Revision != "r1" {
		t.Fatalf("first = %+v, %v", got, err)
	}
	next := pushed("case-1", "r2")
	next.Title = "CI failing again"
	got, err = s.UpsertIntegrationAttention(ctx, next)
	if err != nil || got.Revision != "r2" || got.Title != "CI failing again" {
		t.Fatalf("update = %+v, %v", got, err)
	}
	open, err := s.ListOpenIntegrationAttention(ctx, "argus")
	if err != nil || len(open) != 1 {
		t.Fatalf("open = %+v, %v", open, err)
	}
}

// A retried upsert that lands after the resolve carries the resolve's
// revision, and must not bring the case back.
func TestAnUpsertAtTheResolvedRevisionChangesNothing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.UpsertIntegrationAttention(ctx, pushed("case-1", "r7")); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveIntegrationAttention(ctx, "argus", "case-1", "r7", "closed"); err != nil {
		t.Fatal(err)
	}
	got, err := s.UpsertIntegrationAttention(ctx, pushed("case-1", "r7"))
	if err != nil || got.State != IntegrationAttentionResolved {
		t.Fatalf("late retry = %+v, %v", got, err)
	}
	got, err = s.UpsertIntegrationAttention(ctx, pushed("case-1", "r8"))
	if err != nil || got.State != IntegrationAttentionOpen || got.Revision != "r8" {
		t.Fatalf("reopened = %+v, %v", got, err)
	}
}

func TestResolvingAnUnknownKeyIsNotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.ResolveIntegrationAttention(context.Background(), "argus", "case-x", "r1", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestSyncResolvesOnlyTheOpenItemsLeftOut(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, key := range []string{"case-a", "case-b", "case-c"} {
		if _, err := s.UpsertIntegrationAttention(ctx, pushed(key, "r1")); err != nil {
			t.Fatal(err)
		}
	}
	other := pushed("case-b", "r1")
	other.Integration = "other"
	if _, err := s.UpsertIntegrationAttention(ctx, other); err != nil {
		t.Fatal(err)
	}
	n, err := s.ResolveIntegrationAttentionExcept(ctx, "argus", []string{"case-a", "case-z"}, "sync")
	if err != nil || n != 2 {
		t.Fatalf("resolved = %d, %v", n, err)
	}
	open, _ := s.ListOpenIntegrationAttention(ctx, "argus")
	if len(open) != 1 || open[0].Key != "case-a" {
		t.Fatalf("open = %+v", open)
	}
	if open, _ := s.ListOpenIntegrationAttention(ctx, "other"); len(open) != 1 {
		t.Fatalf("another integration's item was resolved: %+v", open)
	}
	// The sync recorded the revision, so a re-upsert of it stays resolved.
	if got, _ := s.UpsertIntegrationAttention(ctx, pushed("case-b", "r1")); got.State != IntegrationAttentionResolved {
		t.Fatalf("case-b = %+v", got)
	}
}
