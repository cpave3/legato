package service

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestApprovalsAreAttentionItemsWithTheirOptionsAsActions(t *testing.T) {
	approvals, _, _ := newApprovalFixture(t)
	ctx := context.Background()
	resolver := &recordingResolver{}
	approvals.RegisterResolver("test", resolver)
	approval, _, _ := approvals.Submit(ctx, commandRequest("k1"))
	adapter := NewApprovalAttentionAdapter(approvals)

	items, err := adapter.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	item := items[0]
	if item.SignalKind != AttentionSignalApproval || item.SourceKey != approval.ID || item.TaskID != "task-1" || item.Title != "Run a command" || item.Detail != "go test ./..." {
		t.Fatalf("item = %+v", item)
	}
	if item.Navigation.Kind != "approval" || item.Navigation.Target != approval.ID {
		t.Fatalf("navigation = %+v", item.Navigation)
	}
	var actions []string
	for _, a := range item.Actions {
		actions = append(actions, a.ID+":"+a.Kind)
	}
	if len(actions) != 2 || actions[0] != "allow:once:execute" || actions[1] != "deny:execute" || item.Actions[0].Intent != "primary" {
		t.Fatalf("actions = %v", actions)
	}

	if err := adapter.Execute(ctx, AttentionActionRequest{SourceKey: approval.ID, ActionID: "allow:once"}); err != nil {
		t.Fatal(err)
	}
	decided, _ := approvals.Get(ctx, approval.ID)
	if decided.Status != store.ApprovalApproved || decided.DecidedBy != "attention" || resolver.calls[0].Option != "once" {
		t.Fatalf("decided = %+v calls=%+v", decided.Approval, resolver.calls)
	}
	if items, _ := adapter.Snapshot(ctx); len(items) != 0 {
		t.Fatalf("a decided approval is still an item: %+v", items)
	}
}

func TestAQuestionWithOneChoiceGetsAnActionPerOption(t *testing.T) {
	approvals, _, _ := newApprovalFixture(t)
	ctx := context.Background()
	resolver := &recordingResolver{}
	approvals.RegisterResolver("q", resolver)
	approval, _, _ := approvals.Submit(ctx, ApprovalInput{
		Source: "q", SourceKey: "k", TaskID: "t", Title: "Which database?",
		Details: map[string]any{"questions": []map[string]any{{"id": "q1", "question": "Which database?", "options": []map[string]string{{"label": "SQLite"}, {"label": "Postgres"}}}}},
	})
	adapter := NewApprovalAttentionAdapter(approvals)

	items, _ := adapter.Snapshot(ctx)
	var labels []string
	for _, a := range items[0].Actions {
		labels = append(labels, a.Label)
	}
	if len(labels) != 3 || labels[0] != "SQLite" || labels[1] != "Postgres" || labels[2] != "Dismiss" {
		t.Fatalf("actions = %v", labels)
	}
	if err := adapter.Execute(ctx, AttentionActionRequest{SourceKey: approval.ID, ActionID: items[0].Actions[1].ID}); err != nil {
		t.Fatal(err)
	}
	if resolver.calls[0].Decision != ApprovalAnswer || resolver.calls[0].Answers["q1"][0] != "Postgres" {
		t.Fatalf("calls = %+v", resolver.calls)
	}
}

func TestASignedApprovalOffersNoAllowInAttention(t *testing.T) {
	actions := approvalActions(Approval{Approval: store.Approval{Signed: true}, Options: []ApprovalOption{
		{ID: "once", Label: "Once", Kind: ApprovalAllow}, {ID: "deny", Label: "Deny", Kind: ApprovalDeny}}})
	for _, a := range actions {
		if strings.HasPrefix(a.ID, "allow:") {
			t.Fatalf("actions = %+v; a signed approval is allowed on the phone", actions)
		}
	}
	if len(actions) != 1 || actions[0].ID != "deny" {
		t.Fatalf("actions = %+v", actions)
	}
}
