package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/burnlite"
	"github.com/cpave3/legato/internal/engine/store"
)

type fakeBurnliteCloser struct {
	err    error
	closed []string
}

func (f *fakeBurnliteCloser) CloseTask(_ context.Context, key, reason, summary string) error {
	if f.err != nil {
		return f.err
	}
	f.closed = append(f.closed, key+"|"+reason+"|"+summary)
	return nil
}

func burnliteCloseFixture(t *testing.T) (*store.Store, context.Context) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	provider, key := BurnliteIntegration, "LEG-7"
	if err := db.CreateTask(ctx, store.Task{ID: "LEG-7", Title: "Fix login", Status: "Doing", Provider: &provider, RemoteID: &key}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateTask(ctx, store.Task{ID: "local-1", Title: "Local", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	return db, ctx
}

func TestTheBurnliteCloseStepClosesTheTasksTicket(t *testing.T) {
	db, ctx := burnliteCloseFixture(t)
	closer := &fakeBurnliteCloser{}
	result, err := NewBurnliteCloseExecutor(db, closer).Start(ctx, ResolvedWorkflowStep{With: map[string]any{
		"task": map[string]any{"id": "LEG-7"}, "summary": "Merged in acme/widget#12",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(closer.closed) != 1 || closer.closed[0] != "LEG-7|completed|Merged in acme/widget#12" {
		t.Fatalf("closed = %v", closer.closed)
	}
	if result.Status != "succeeded" || result.Outputs["key"] != "LEG-7" || result.Outputs["closed"] != true || result.Outputs["skipped"] != false {
		t.Fatalf("result = %+v", result)
	}
}

func TestTheBurnliteCloseStepRefusesATaskNotFromBurnliteUnlessOptional(t *testing.T) {
	db, ctx := burnliteCloseFixture(t)
	closer := &fakeBurnliteCloser{}
	step := NewBurnliteCloseExecutor(db, closer)
	if _, err := step.Start(ctx, ResolvedWorkflowStep{With: map[string]any{"task": "local-1"}}); err == nil || !strings.Contains(err.Error(), "not a Burnlite ticket") {
		t.Fatalf("err = %v", err)
	}
	result, err := step.Start(ctx, ResolvedWorkflowStep{With: map[string]any{"task": "local-1", "optional": true}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outputs["skipped"] != true || result.Outputs["closed"] != false || len(closer.closed) != 0 {
		t.Fatalf("result = %+v, closed = %v", result, closer.closed)
	}
}

// A connection made before Legato wrote to Burnlite reads only: the step
// says to connect again.
func TestTheBurnliteCloseStepSaysToReconnectAReadOnlyConnection(t *testing.T) {
	db, ctx := burnliteCloseFixture(t)
	closer := &fakeBurnliteCloser{err: &burnlite.Error{Code: "insufficient_scope", Message: "requires write:tasks"}}
	_, err := NewBurnliteCloseExecutor(db, closer).Start(ctx, ResolvedWorkflowStep{With: map[string]any{"task": "LEG-7"}})
	if err == nil || !strings.Contains(err.Error(), "connect Legato to Burnlite again") {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, burnlite.ErrInsufficientScope) {
		t.Fatalf("err = %v; want the scope refusal kept", err)
	}
}
