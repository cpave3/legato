package store

import (
	"context"
	"testing"
)

func TestSubscriptionUsageKeepsTheNewestReadingPerProvider(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	rows, err := s.ListSubscriptionUsage(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("empty store: %v %v", rows, err)
	}
	if err := s.PutSubscriptionUsage(ctx, "codex", `{"state":"ok"}`); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSubscriptionUsage(ctx, "claude", `{"state":"stale"}`); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSubscriptionUsage(ctx, "claude", `{"state":"ok"}`); err != nil {
		t.Fatal(err)
	}
	rows, err = s.ListSubscriptionUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Provider != "claude" || rows[0].Reading != `{"state":"ok"}` || rows[1].Provider != "codex" {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].UpdatedAt == "" {
		t.Fatal("no updated_at")
	}
}
