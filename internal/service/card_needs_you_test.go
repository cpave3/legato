package service

import (
	"reflect"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func needsApproval(id, task, source, title string) Approval {
	return Approval{Approval: store.Approval{ID: id, TaskID: task, Source: source, Title: title, Status: store.ApprovalPending}}
}

func TestNeedsYouByTask(t *testing.T) {
	attention := []AttentionItem{
		{SignalKind: AttentionSignalOperatorQuestion, TaskID: "t1"},
		{SignalKind: AttentionSignalOperatorAction, TaskID: "t1"},
		{SignalKind: AttentionSignalOperatorQuestion, TaskID: "t1"},
		{SignalKind: AttentionSignalOperatorDone, TaskID: "t2"},
		{SignalKind: AttentionSignalOperatorOther, TaskID: "t2"},
		{SignalKind: AttentionSignalPlanDecision, TaskID: "t3", Navigation: AttentionNavigation{Kind: "plan", Target: "plan-9"}},
		// Not the card's: no task, or a signal the strip does not say.
		{SignalKind: AttentionSignalOperatorQuestion},
		{SignalKind: AttentionSignalPRMergeBlocked, TaskID: "t5"},
		{SignalKind: AttentionSignalApproval, TaskID: "t5"},
	}
	approvals := []Approval{
		needsApproval("ap-1", "t4", ApprovalSourceAgentPermission, "Run a command"),
		needsApproval("ap-2", "t4", ApprovalSourceAgentQuestion, "Which database?"),
		needsApproval("ap-3", "t6", ApprovalSourceAgentQuestion, "Which database?"),
		needsApproval("ap-4", "t7", ApprovalSourceAgentPermission, "Use Edit"),
		needsApproval("ap-5", "t8", "credential", "Use the GitHub token"),
		needsApproval("ap-6", "", ApprovalSourceAgentPermission, "Run a command"),
	}
	got := NeedsYouByTask(attention, approvals)
	want := map[string]*CardNeedsYou{
		"t1": {Items: map[string]int{"question": 2, "action": 1}},
		"t2": {Items: map[string]int{"done": 1, "other": 1}},
		"t3": {Plans: &NeedsYouPlans{Count: 1, PlanID: "plan-9"}},
		"t4": {Approvals: &NeedsYouApprovals{Count: 2, ID: "ap-1", Label: "permission to run a command"}},
		"t6": {Approvals: &NeedsYouApprovals{Count: 1, ID: "ap-3", Label: "Which database?"}},
		"t7": {Approvals: &NeedsYouApprovals{Count: 1, ID: "ap-4", Label: "permission to use Edit"}},
		"t8": {Approvals: &NeedsYouApprovals{Count: 1, ID: "ap-5", Label: "use the GitHub token"}},
	}
	if !reflect.DeepEqual(got, want) {
		for k, v := range got {
			t.Logf("%s: %+v %+v %+v", k, v.Items, v.Approvals, v.Plans)
		}
		t.Fatalf("needs you differs")
	}
	if len(NeedsYouByTask(nil, nil)) != 0 {
		t.Fatal("nothing open gives no entries")
	}
}
