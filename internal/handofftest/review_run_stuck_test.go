package handofftest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// stuckRun is a run whose loop reached its one pass with a finding open.
func stuckRun(t *testing.T) (reviewRun, *store.WorkflowRunView) {
	t.Helper()
	r, view := afterPassOne(t, ReviewPassOptions{MaxPasses: 1})
	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	return r, view
}

func (r reviewRun) attention(t *testing.T) []service.AttentionInput {
	t.Helper()
	items, err := service.NewWorkflowAttentionAdapter(r.runs.Workflows).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestAStuckReviewLoopRaisesOneAttentionItemOnItsRun(t *testing.T) {
	r, view := stuckRun(t)
	items := r.attention(t)
	if len(items) != 1 || items[0].SignalKind != service.AttentionSignalWorkflowReviewStuck || items[0].Navigation.Target != view.Run.ID ||
		!strings.Contains(items[0].Reason, "limit of 1 passes") {
		t.Fatalf("items = %+v", items)
	}
	var ids []string
	for _, action := range items[0].Actions {
		ids = append(ids, action.ID)
	}
	if strings.Join(ids, ",") != "open,another_pass,end" {
		t.Fatalf("actions = %v", ids)
	}

	adapter := service.NewWorkflowAttentionAdapter(r.runs.Workflows)
	if err := adapter.Execute(context.Background(), service.AttentionActionRequest{SourceKey: items[0].SourceKey, ActionID: "another_pass"}); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if !r.reviewer(t, view.Run.TaskID) || len(r.attention(t)) != 0 {
		t.Fatalf("after another pass: reviewer %v, items %+v", r.reviewer(t, view.Run.TaskID), r.attention(t))
	}
}

func TestEndingAStuckReviewLoopCompletesTheRunAsStuck(t *testing.T) {
	r, view := stuckRun(t)
	items := r.attention(t)
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	if err := service.NewWorkflowAttentionAdapter(r.runs.Workflows).Execute(context.Background(), service.AttentionActionRequest{SourceKey: items[0].SourceKey, ActionID: "end", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	got := r.show(t, view.Run.ID)
	if got.Run.Status != "succeeded" || outputs(t, got, "review")["outcome"] != service.ReviewOutcomeStuck || got.Steps[1].Status != "succeeded" {
		t.Fatalf("run %q, outputs %+v", got.Run.Status, outputs(t, got, "review"))
	}
	if len(r.attention(t)) != 0 {
		t.Fatal("the item stayed")
	}
}
