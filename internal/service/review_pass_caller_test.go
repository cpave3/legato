package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// An instance with review passes on and no handoffs (no paired main) has the
// engine, which does nothing until a caller has a loop.
func TestTheReviewPassEngineDoesNothingWithoutACallersLoop(t *testing.T) {
	ctx := context.Background()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// No agents, GitHub or checkout: touching any of them would panic.
	svc := NewReviewPassService(db, ReviewPassOptions{Config: func() ReviewPassConfig { return ReviewPassConfig{Enabled: true} }})
	svc.Tick(ctx)
	NewHandoffReviewCaller(svc, HandoffReviewCallerOptions{})
	svc.Tick(ctx)
	if loops, err := db.ListHandoffReviews(ctx); err != nil || len(loops) != 0 {
		t.Fatalf("loops = %+v, %v", loops, err)
	}
}
