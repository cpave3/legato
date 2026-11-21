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
	got := NeedsYouByTask(NeedsYouSources{Attention: attention, Approvals: approvals})
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
	if len(NeedsYouByTask(NeedsYouSources{})) != 0 {
		t.Fatal("nothing open gives no entries")
	}
}

func TestNeedsYouSaysWhichAgentWaits(t *testing.T) {
	agents := []AgentSession{
		// t1: the main agent runs while its reviewer asks a question.
		{TaskID: "t1", Status: "running", Activity: "working", AgentKind: "claude-code", ExecutionID: "ex-main"},
		{TaskID: "t1_reviewer", CardTaskID: "t1", Slot: SlotReviewer, Status: "running", Activity: "waiting", ExecutionID: "ex-rev"},
		// t2: one of two agents waits.
		{TaskID: "t2", Status: "running", Activity: "waiting", AttributionTaskID: "t2", ExecutionID: "ex-a"},
		{TaskID: "w-1", Status: "running", Activity: "working", AttributionTaskID: "t2", AgentKind: "codex", ExecutionID: "ex-b"},
		// t3: a waiting reviewer with nothing to answer, an idle main.
		{TaskID: "t3", Status: "running", Activity: "", ExecutionID: "ex-c"},
		{TaskID: "t3_reviewer", CardTaskID: "t3", Slot: SlotReviewer, Status: "running", Activity: "waiting", ExecutionID: "ex-d"},
		// Agents that agree say nothing.
		{TaskID: "t5", Status: "running", Activity: "working", ExecutionID: "ex-e"},
		{TaskID: "w-2", Status: "running", Activity: "working", AttributionTaskID: "t5", ExecutionID: "ex-f"},
	}
	approvals := []Approval{
		{Approval: store.Approval{ID: "ap-r", TaskID: "t1", ExecutionID: "ex-rev", Source: ApprovalSourceAgentQuestion, Title: "Block the merge?", Status: store.ApprovalPending}},
	}
	interrupted := []InterruptedAgent{
		{TaskID: "t4", AgentKind: "claude-code", Resumable: true},
		{TaskID: "t6", AgentKind: "codex", CanStartNew: true},
	}
	got := NeedsYouByTask(NeedsYouSources{Agents: agents, Approvals: approvals, Interrupted: interrupted})
	if n := got["t1"]; n == nil || n.Approvals != nil || len(n.Slots) != 1 || n.Slots[0] != (NeedsYouSlot{Slot: SlotReviewer, Label: "question", ApprovalID: "ap-r"}) {
		t.Fatalf("t1 = %+v", n)
	}
	if n := got["t2"]; n == nil || n.WaitingAgents != 1 || n.Agents != 2 || n.WorkingAgents != 1 {
		t.Fatalf("t2 = %+v", n)
	}
	if n := got["t3"]; n == nil || len(n.Slots) != 1 || n.Slots[0] != (NeedsYouSlot{Slot: SlotReviewer}) {
		t.Fatalf("t3 = %+v", n)
	}
	if n := got["t4"]; n == nil || n.Interrupted == nil || n.Interrupted.AgentKind != "claude-code" || !n.Interrupted.Resumable {
		t.Fatalf("t4 = %+v", n)
	}
	if n := got["t6"]; n != nil {
		t.Fatalf("an interrupted agent that cannot be resumed says nothing: %+v", n.Interrupted)
	}
	if n := got["t5"]; n != nil {
		t.Fatalf("t5 = %+v", n)
	}
}
