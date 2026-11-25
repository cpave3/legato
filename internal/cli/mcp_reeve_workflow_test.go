package cli_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
)

const reeveDraft = `schema_version: 1
name: standup
title: Standup
on:
  schedule: {every: weekdays, at: "09:00", enabled: true}
steps:
  - {id: n, title: N, type: task.note, with: {task: "${{ workflow.task }}", body: hi}}
`

func TestTheReeveDraftsAWorkflowAndProposesItPaused(t *testing.T) {
	s := newTestStore(t)
	sets := &fakeChangeSets{}
	taskID, executionID := seedReeve(t, s)
	reeve := &cli.MCPReeve{ChangeSets: sets, WorkflowNames: func(context.Context) []string { return []string{"review-pr"} }}
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: taskID, ExecutionID: executionID, Reeve: reeve})

	got := callMCP(t, session, "draft_workflow", map[string]any{"reason": "a standup every weekday", "yaml": reeveDraft})
	if got.IsError || !strings.Contains(mcpText(got), "waits for the operator's approval") {
		t.Fatalf("result %q", mcpText(got))
	}
	if len(sets.calls) != 1 || sets.calls[0].Method != "POST" || sets.calls[0].Path != "/api/workflow-drafts" {
		t.Fatalf("proposed %+v", sets.calls)
	}
	var body struct{ YAML string }
	_ = json.Unmarshal(sets.calls[0].Body, &body)
	// A version 1 draft is proposed as version 2 (LEG-484).
	if !strings.Contains(body.YAML, "schema_version: 2") {
		t.Fatalf("the proposed draft is not version 2:\n%s", body.YAML)
	}
	if !strings.Contains(body.YAML, "enabled: false") || strings.Contains(body.YAML, "enabled: true") {
		t.Fatalf("the proposed draft is not paused:\n%s", body.YAML)
	}

	invalid := callMCP(t, session, "draft_workflow", map[string]any{"reason": "x", "yaml": strings.Replace(reeveDraft, `at: "09:00"`, `at: "9am"`, 1)})
	if !invalid.IsError || !strings.Contains(mcpText(invalid), "at must be HH:MM") {
		t.Fatalf("an invalid draft = %q", mcpText(invalid))
	}
	taken := callMCP(t, session, "draft_workflow", map[string]any{"reason": "x", "yaml": strings.Replace(reeveDraft, "name: standup", "name: review-pr", 1)})
	if !taken.IsError || !strings.Contains(mcpText(taken), "exists") {
		t.Fatalf("a taken name = %q", mcpText(taken))
	}
	if len(sets.calls) != 1 {
		t.Fatal("a refused draft was proposed")
	}
}

func TestOnlyTheReeveIsOfferedDraftWorkflow(t *testing.T) {
	s := newTestStore(t)
	reeve := &cli.MCPReeve{ChangeSets: &fakeChangeSets{}}
	taskID, executionID := seedReeve(t, s)
	if !reeveToolNames(t, connectMCP(t, cli.MCPOptions{Store: s, TaskID: taskID, ExecutionID: executionID, Reeve: reeve}))["draft_workflow"] {
		t.Fatal("the Reeve lacks draft_workflow")
	}
	other := seedMCPAgent(t, s, "work-1")
	if reeveToolNames(t, connectMCP(t, cli.MCPOptions{Store: s, TaskID: "work-1", ExecutionID: other, Reeve: reeve}))["draft_workflow"] {
		t.Fatal("an ordinary agent was offered draft_workflow")
	}
}
