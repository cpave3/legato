package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// countingCaller is a review caller that only counts how often the engine
// asks for its loops; it has none.
type countingCaller struct {
	ReviewCaller
	asked int
}

func (c *countingCaller) Loops(context.Context) ([]ReviewLoop, error) {
	c.asked++
	return nil, nil
}

// An old Legato still running after a newer one migrated the database
// must not move reviews on: its review tick does nothing on a schema newer
// than the one it knows (spec LEG-559 M7, AC 45).
func TestTheReviewTickDoesNothingOnANewerSchema(t *testing.T) {
	ctx := context.Background()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc := NewReviewPassService(db, ReviewPassOptions{Config: func() ReviewPassConfig { return ReviewPassConfig{Enabled: true} }})
	caller := &countingCaller{}
	svc.AddCaller(caller)

	svc.Tick(ctx)
	if caller.asked != 1 {
		t.Fatalf("on the schema it knows, the tick asked for loops %d times, want 1", caller.asked)
	}

	// A newer binary migrated: the ledger names a migration this one does
	// not have.
	status, err := db.SchemaStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB().ExecContext(ctx, `INSERT INTO schema_migrations(version, name) VALUES (?, ?)`, status.Supported+1, "999_from_a_newer_legato.sql"); err != nil {
		t.Fatal(err)
	}

	svc.Tick(ctx)
	svc.Tick(ctx)
	if caller.asked != 1 {
		t.Fatalf("on a newer schema, the tick asked for loops %d more times, want none", caller.asked-1)
	}
}
