package cli_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/service"
)

type fakeCaseReplies struct {
	task, execution string
	got             service.CaseReplyInput
}

func (f *fakeCaseReplies) Reply(_ context.Context, taskID, executionID string, in service.CaseReplyInput) (service.CaseReplyResult, error) {
	f.task, f.execution, f.got = taskID, executionID, in
	if in.DelegationID != "dl-1" {
		return service.CaseReplyResult{}, errors.New("delegation " + in.DelegationID + " was not sent to this task")
	}
	return service.CaseReplyResult{ID: "cr-1", State: "sent"}, nil
}

func TestMCPReplyCaseSendsTheReplyAsTheCallingSession(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	replies := &fakeCaseReplies{}
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID, CaseReplies: replies})

	result := callMCP(t, session, "reply_case", map[string]any{"delegation_id": "dl-1", "outcome": "finding", "text": "The test is flaky."})
	if result.IsError || !strings.Contains(mcpText(result), "sent") {
		t.Fatalf("result = %+v %s", result, mcpText(result))
	}
	if replies.task != "task1" || replies.execution != executionID || replies.got != (service.CaseReplyInput{DelegationID: "dl-1", Outcome: "finding", Text: "The test is flaky."}) {
		t.Fatalf("reply = %+v", replies)
	}
	result = callMCP(t, session, "reply_case", map[string]any{"delegation_id": "dl-2", "outcome": "done", "text": "x"})
	if !result.IsError || !strings.Contains(mcpText(result), "not sent to this task") {
		t.Fatalf("other delegation = %s", mcpText(result))
	}
}

func TestMCPReplyCaseIsNotOfferedWithoutAnIntegration(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID})
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == "reply_case" {
			t.Fatal("reply_case offered without an integration")
		}
	}
}
