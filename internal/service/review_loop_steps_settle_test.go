package service

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// settleWorkflows records how waiting review steps were ended, and answers
// each with err.
type settleWorkflows struct {
	err       error
	failed    []error
	completed []map[string]any
}

func (w *settleWorkflows) WaitingAttempts(context.Context, string) ([]store.WorkflowStepAttempt, error) {
	return nil, nil
}

func (w *settleWorkflows) CompleteWaiting(_ context.Context, _, _, _ string, outputs map[string]any) (*store.WorkflowRunView, error) {
	w.completed = append(w.completed, outputs)
	return nil, w.err
}

func (w *settleWorkflows) FailWaiting(context.Context, string, string, string, string) (*store.WorkflowRunView, error) {
	return nil, errors.New("not used")
}

func (w *settleWorkflows) FailWaitingError(_ context.Context, _, _, _ string, failure error) (*store.WorkflowRunView, error) {
	w.failed = append(w.failed, failure)
	return nil, w.err
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	out, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(out); log.SetFlags(flags) })
	return &buf
}

// A waiting step another process ended first (a state conflict) is no
// error to log; any other refusal is logged with the run.
func TestSettlingAWaitingStepLogsOnlyARealRefusal(t *testing.T) {
	attempt := store.WorkflowStepAttempt{ID: "a1", RunID: "run-1", Path: "review"}
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		err     error
		logged  string
		outputs map[string]any
	}{
		{name: "accepted"},
		{name: "conflict", err: store.ErrWorkflowStateConflict},
		{name: "wrapped conflict", err: errors.Join(errors.New("x"), store.ErrWorkflowStateConflict)},
		{name: "refused", err: errors.New("database is locked"), logged: "database is locked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureLog(t)
			w := &settleWorkflows{err: tc.err}
			c := &ReviewRunCaller{workflows: w}
			failure := errors.New("no loop")
			c.failWaitingLogged(ctx, attempt, failure)
			c.completeWaitingLogged(ctx, attempt, map[string]any{"ok": true}, "close")
			if len(w.failed) != 1 || w.failed[0] != failure || len(w.completed) != 1 || w.completed[0]["ok"] != true {
				t.Fatalf("failed %v, completed %v", w.failed, w.completed)
			}
			// Only this run's lines: another test's goroutine may log.
			var got string
			for _, line := range strings.SplitAfter(buf.String(), "\n") {
				if strings.Contains(line, "run-1") {
					got += line
				}
			}
			if tc.logged == "" {
				if got != "" {
					t.Fatalf("logged %q", got)
				}
				return
			}
			want := "review passes: failing run-1: " + tc.logged + "\n" +
				"review passes: completing run-1's close: " + tc.logged + "\n"
			if got != want {
				t.Fatalf("logged %q, want %q", got, want)
			}
		})
	}
}
