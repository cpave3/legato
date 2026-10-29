package cli_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// brokerSession is a running agent with a broker token, as a spawn makes.
func brokerSession(t *testing.T, s *store.Store, taskID string) (execID, token string) {
	t.Helper()
	ctx := context.Background()
	seedTask(t, s, taskID, "Doing")
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: taskID, TmuxSession: "legato-" + taskID, Command: "shell", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	execID = linkTestExecution(t, s, taskID)
	token = "lgb_token_for_" + taskID
	if err := s.SetExecutionBrokerToken(ctx, execID, service.HashBrokerToken(token)); err != nil {
		t.Fatal(err)
	}
	return execID, token
}

func call(h broker.Handler, token, op string, args any) (any, error) {
	raw, _ := json.Marshal(args)
	return h(context.Background(), broker.Request{Token: token, Op: op, Args: raw})
}

func code(err error) string {
	var be *broker.Error
	if errors.As(err, &be) {
		return be.Code
	}
	return ""
}

func TestTheBrokerActsOnlyForTheExecutionItsTokenNames(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	execID, token := brokerSession(t, s, "abc123")
	brokerSession(t, s, "other1")
	h := cli.NewBrokerHandler(s, cli.BrokerDeps{})
	ctx := context.Background()

	if _, err := call(h, "lgb_not_a_token", "agent.state", map[string]any{"activity": "working"}); code(err) != "unauthorized" {
		t.Fatalf("unknown token: %v", err)
	}
	if _, err := call(h, token, "agent.state", map[string]any{"task_id": "other1", "activity": "working"}); code(err) != "forbidden" {
		t.Fatalf("another task: %v", err)
	}
	if _, err := call(h, token, "agent.state", map[string]any{"execution_id": "exec-other1", "activity": "working"}); code(err) != "forbidden" {
		t.Fatalf("another execution: %v", err)
	}
	if _, err := call(h, token, "store.query", nil); code(err) != "unknown_op" {
		t.Fatalf("unknown op: %v", err)
	}

	if _, err := call(h, token, "agent.state", map[string]any{"activity": "working", "working_dir": "/repo"}); err != nil {
		t.Fatal(err)
	}
	session, _ := s.GetAgentSessionByTaskID(ctx, "abc123")
	if session.Activity != "working" {
		t.Fatalf("activity = %q", session.Activity)
	}
	other, _ := s.GetAgentSessionByTaskID(ctx, "other1")
	if other.Activity != "" {
		t.Fatalf("another session changed: %q", other.Activity)
	}

	if err := s.TerminalizeExecution(ctx, execID, store.ExecutionCancelled); err != nil {
		t.Fatal(err)
	}
	if _, err := call(h, token, "agent.state", map[string]any{"activity": ""}); code(err) != "unauthorized" {
		t.Fatalf("an ended execution's token: %v", err)
	}
}

func TestTheBrokerRecordsAChimeraSessionForItsOwnTask(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	_, token := brokerSession(t, s, "abc123")
	h := cli.NewBrokerHandler(s, cli.BrokerDeps{})
	if _, err := call(h, token, "agent.session_created", map[string]any{"session_id": "chm-1"}); err != nil {
		t.Fatal(err)
	}
	task, _ := s.GetTask(context.Background(), "abc123")
	if task.ChimeraSessionID == nil || *task.ChimeraSessionID != "chm-1" {
		t.Fatalf("task's chimera session = %v", task.ChimeraSessionID)
	}
}
