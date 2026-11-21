package cli_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func placeStage(t *testing.T, s *store.Store, taskID string) store.WorkStage {
	t.Helper()
	got, err := s.GetWorkStage(context.Background(), service.TaskPlace(taskID, ""))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		return store.WorkStage{}
	}
	return *got
}

// An agent reports its stage with report_stage: the stage is the agent's,
// with its note as the reason. A stage that is not one is the call's error.
func TestReportStageRecordsTheAgentsStage(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID})

	result := callMCP(t, session, "report_stage", map[string]any{"stage": "reviewing", "note": "PR #12 open"})
	if result.IsError {
		t.Fatalf("report_stage: %s", mcpText(result))
	}
	got := placeStage(t, s, "task1")
	if got.Stage != store.WorkStageReviewing || got.Source != store.WorkStageSourceAgent || got.Reason != "PR #12 open" || got.ExecutionID != executionID {
		t.Fatalf("stage = %+v", got)
	}

	result = callMCP(t, session, "report_stage", map[string]any{"stage": "done"})
	if !result.IsError || !strings.Contains(mcpText(result), "invalid stage") {
		t.Fatalf("an unknown stage: error %v, %q", result.IsError, mcpText(result))
	}
	if placeStage(t, s, "task1").Stage != store.WorkStageReviewing {
		t.Fatal("an unknown stage changed the stage")
	}
}

// A for_operator item of kind done reports Finished, with its title.
func TestADoneItemReportsFinished(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID})

	callMCP(t, session, "for_operator", map[string]any{"kind": "question", "title": "Postgres or SQLite?"})
	if got := placeStage(t, s, "task1"); got.Stage != "" {
		t.Fatalf("a question set stage %q", got.Stage)
	}
	if result := callMCP(t, session, "for_operator", map[string]any{"kind": "done", "title": "The answer is in the note"}); result.IsError {
		t.Fatalf("for_operator: %s", mcpText(result))
	}
	if got := placeStage(t, s, "task1"); got.Stage != store.WorkStageFinished || got.Source != store.WorkStageSourceAgent || got.Reason != "The answer is in the note" {
		t.Fatalf("after a done item: %+v", got)
	}
}

// legato agent stage reports for the caller's own session, over the broker
// or straight to the store.
func TestAgentStageReportsOverTheBrokerAndDirectly(t *testing.T) {
	s := newTestStore(t)
	execID, token := brokerSession(t, s, "abc123")
	h := cli.NewBrokerHandler(s, cli.BrokerDeps{})

	if _, err := call(h, token, "agent.stage", map[string]any{"stage": "implementing", "note": "fixing the parser"}); err != nil {
		t.Fatal(err)
	}
	if got := placeStage(t, s, "abc123"); got.Stage != store.WorkStageImplementing || got.Source != store.WorkStageSourceAgent || got.Reason != "fixing the parser" {
		t.Fatalf("over the broker: %+v", got)
	}
	if _, err := call(h, token, "agent.stage", map[string]any{"stage": "nearly"}); code(err) != "rejected" {
		t.Fatalf("an unknown stage over the broker: %v", err)
	}

	stage, err := cli.AgentStage(context.Background(), s, "abc123", execID, "Finished", "merged")
	if err != nil || stage != store.WorkStageFinished {
		t.Fatalf("directly: %q, %v", stage, err)
	}
	if got := placeStage(t, s, "abc123"); got.Stage != store.WorkStageFinished || got.Reason != "merged" {
		t.Fatalf("directly: %+v", got)
	}
}
