package service

import (
	"context"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// Every signal kind Attention knows belongs to one of the rail's groups,
// and a kind with no group of its own falls into Legato's.
func TestEveryKnownSignalHasARailGroup(t *testing.T) {
	groups := map[string]bool{}
	for _, group := range config.RailGroups {
		groups[group] = true
	}
	for signal := range knownAttentionSignals {
		if !groups[RailGroupOf(signal)] {
			t.Errorf("%s is in rail group %q, which is not one", signal, RailGroupOf(signal))
		}
	}
	for signal, want := range map[string]string{
		AttentionSignalOperatorAction: "actions", AttentionSignalOperatorQuestion: "questions",
		AttentionSignalOperatorDone: "done", AttentionSignalOperatorOther: "other",
		AttentionSignalApproval: "approvals", AttentionSignalReevePeer: "legato",
		AttentionSignalAgentWaiting: "agents", AttentionSignalAgentError: "agents",
		AttentionSignalWorkflowFailed: "workflows", AttentionSignalWorkflowTriggerLoopStop: "workflows", AttentionSignalWorkflowReviewStuck: "workflows",
		AttentionSignalWorkflowReviewAuthor: "workflows", AttentionSignalWorkflowReviewSteward: "workflows",
		AttentionSignalPlanDecision: "plans",
		AttentionSignalReviewReady:  "review", AttentionSignalHandoffDone: "review",
		AttentionSignalPRMergeBlocked: "merge", AttentionSignalArgusCase: "merge", AttentionSignalArgusNeedsYou: "merge",
		AttentionSignalUpdateAvailable: "legato", AttentionSignalAgentsInterrupted: "legato", AttentionSignalHandoffFailed: "legato",
		"something_new": "legato",
	} {
		if got := RailGroupOf(signal); got != want {
			t.Errorf("RailGroupOf(%s) = %q, want %q", signal, got, want)
		}
	}
}

func TestTheRailViewFollowsEachSetting(t *testing.T) {
	items := []AttentionItem{
		{ID: "a", SignalKind: AttentionSignalOperatorAction},
		{ID: "r", SignalKind: AttentionSignalReviewReady},
		{ID: "m", SignalKind: AttentionSignalPRMergeBlocked},
		{ID: "u", SignalKind: AttentionSignalUpdateAvailable},
	}
	ids := func(rail config.RailConfig) string {
		out := ""
		for _, item := range RailView(items, rail, RailAsks{}) {
			out += item.ID + ":" + item.RailGroup + " "
		}
		return out
	}
	if got := ids(config.RailConfig{}); got != "a:actions u:legato " {
		t.Fatalf("by default the rail shows %q", got)
	}
	on, off := true, false
	if got := ids(config.RailConfig{Review: &on, Merge: &on, Actions: &off}); got != "r:review m:merge u:legato " {
		t.Fatalf("with review and merge on and actions off, the rail shows %q", got)
	}
	if items[0].RailGroup != "" {
		t.Fatal("the rail view changed the items it was given")
	}
}

// One wait is one rail item: while an agent's execution has a pending
// approval or an open operator item, its Waiting item is not in the rail,
// and while it has a pending question approval, neither are its operator
// questions. An agent waiting with nothing else still shows Waiting.
func TestTheRailShowsOneItemForOneWait(t *testing.T) {
	ctx := context.Background()
	db := newServiceTestStore(t)
	// e-question asks with AskUserQuestion and posted the same question
	// with for_operator, and a command to run too.
	if _, _, err := db.InsertApproval(ctx, store.Approval{ID: "ap-q", Source: ApprovalSourceAgentQuestion, SourceKey: "q", ExecutionID: "e-question", TaskID: "T1", Title: "Which index?", Status: store.ApprovalPending}); err != nil {
		t.Fatal(err)
	}
	question, err := db.PostOperatorItem(ctx, store.OperatorItem{ID: "oi-q", TaskID: "T1", ExecutionID: "e-question", Kind: OperatorItemQuestion, Title: "Which index?"}, []string{"SQLite", "Bleve"})
	if err != nil {
		t.Fatal(err)
	}
	action, err := db.PostOperatorItem(ctx, store.OperatorItem{ID: "oi-a", TaskID: "T1", ExecutionID: "e-question", Kind: OperatorItemAction, Title: "Run the migration"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// e-permission waits on a permission request; e-posted on an operator
	// item; e-plain on nothing Legato knows of.
	if _, _, err := db.InsertApproval(ctx, store.Approval{ID: "ap-p", Source: ApprovalSourceAgentPermission, SourceKey: "p", ExecutionID: "e-permission", TaskID: "T2", Title: "Run rm", Status: store.ApprovalPending}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PostOperatorItem(ctx, store.OperatorItem{ID: "oi-p", TaskID: "T3", ExecutionID: "e-posted", Kind: OperatorItemAction, Title: "Approve the PR"}, nil); err != nil {
		t.Fatal(err)
	}
	// A question answered earlier no longer hides anything.
	if _, _, err := db.InsertApproval(ctx, store.Approval{ID: "ap-old", Source: ApprovalSourceAgentQuestion, SourceKey: "old", ExecutionID: "e-plain", TaskID: "T4", Title: "Old", Status: store.ApprovalPending}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CloseApproval(ctx, "ap-old", store.ApprovalClose{Status: store.ApprovalAnswered}); err != nil {
		t.Fatal(err)
	}

	attention := NewAttentionService(db, nil)
	asks, err := attention.RailAsks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	waiting := func(execution string) AttentionItem {
		return AttentionItem{ID: "w-" + execution, Producer: "agents", SignalKind: AttentionSignalAgentWaiting, SourceKey: execution}
	}
	items := []AttentionItem{
		{ID: "approval-q", Producer: "approvals", SignalKind: AttentionSignalApproval, SourceKey: "ap-q"},
		waiting("e-question"),
		{ID: "op-q", Producer: OperatorItemsAttentionProducer, SignalKind: AttentionSignalOperatorQuestion, SourceKey: question.ID},
		{ID: "op-a", Producer: OperatorItemsAttentionProducer, SignalKind: AttentionSignalOperatorAction, SourceKey: action.ID},
		{ID: "error-q", Producer: "agents", SignalKind: AttentionSignalAgentError, SourceKey: "e-question"},
		waiting("e-permission"),
		waiting("e-posted"),
		waiting("e-plain"),
	}
	got := ""
	for _, item := range RailView(items, config.RailConfig{}, asks) {
		got += item.ID + " "
	}
	if want := "approval-q op-a error-q w-e-plain "; got != want {
		t.Fatalf("the rail shows %q, want %q", got, want)
	}
	// Without what the agents ask, the rail filters by group alone.
	if got := len(RailView(items, config.RailConfig{}, RailAsks{})); got != len(items) {
		t.Fatalf("with no asks the rail shows %d of %d", got, len(items))
	}
}
