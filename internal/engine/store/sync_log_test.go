package store

import (
	"context"
	"testing"
	"time"
)

func TestSyncLogInsertAndList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert entries
	if err := s.InsertSyncLog(ctx, SyncLogEntry{TaskID: "REX-1", Action: "pull", Detail: "first"}); err != nil {
		t.Fatal(err)
	}
	// Small delay so created_at differs
	time.Sleep(10 * time.Millisecond)
	if err := s.InsertSyncLog(ctx, SyncLogEntry{TaskID: "REX-1", Action: "push_status", Detail: "second"}); err != nil {
		t.Fatal(err)
	}
	// Different task
	if err := s.InsertSyncLog(ctx, SyncLogEntry{TaskID: "REX-2", Action: "pull"}); err != nil {
		t.Fatal(err)
	}

	// List for REX-1 — reverse chrono
	entries, err := s.ListSyncLogs(ctx, "REX-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Detail != "second" {
		t.Errorf("first entry Detail = %q, want %q (reverse chrono)", entries[0].Detail, "second")
	}
	if entries[0].CreatedAt == "" {
		t.Error("CreatedAt should be auto-populated")
	}
}
