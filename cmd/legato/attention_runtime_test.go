package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestNativeSecurityRuntimeRegistrationProjectsDurableAndLiveRefusals(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, err := store.New(filepath.Join(t.TempDir(), "attention.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.RecordNativeSecurityIncident(ctx, "task", "execution", "old-generation", "permission"); err != nil {
		t.Fatal(err)
	}
	bus := events.New()
	attention := service.NewAttentionService(db, bus)
	registerNativeSecurityAttention(attention, db)
	if err := attention.ReconcileAll(ctx); err != nil {
		t.Fatal(err)
	}
	if count, err := attention.Count(ctx); err != nil || count != 1 {
		t.Fatalf("startup count = %d, %v", count, err)
	}
	stop := attention.StartEventReconciliation(ctx)
	defer stop()
	changed := bus.Subscribe(events.EventAttentionChanged)
	defer bus.Unsubscribe(changed)
	if err := db.RecordNativeSecurityIncident(ctx, "task", "execution", "new-generation", "question"); err != nil {
		t.Fatal(err)
	}
	bus.Publish(events.Event{Type: events.EventApprovalChanged})
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("live refusal did not invalidate Attention")
	}
	if count, err := attention.Count(ctx); err != nil || count != 2 {
		t.Fatalf("live count = %d, %v", count, err)
	}
}
