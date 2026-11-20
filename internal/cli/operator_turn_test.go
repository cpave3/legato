package cli_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func (a *hookAgent) post(title string) string {
	a.t.Helper()
	item, err := service.PostOperatorItem(context.Background(), a.s, service.OperatorItemInput{TaskID: a.taskID, ExecutionID: a.execution, Kind: service.OperatorItemAction, Title: title})
	if err != nil {
		a.t.Fatal(err)
	}
	return item.ID
}

func (a *hookAgent) itemStatus(id string) (string, string) {
	a.t.Helper()
	item, err := a.s.GetOperatorItem(context.Background(), id)
	if err != nil {
		a.t.Fatal(err)
	}
	return item.Status, item.CloseReason
}

func promptPayload(prompt string) []byte {
	raw, _ := json.Marshal(map[string]string{"hook_event_name": "UserPromptSubmit", "prompt": prompt})
	return raw
}

// A slash command typed in the agent's own terminal is the operator's turn.
func TestClaudeHook_TheOperatorsPromptClosesTheAgentsItems(t *testing.T) {
	a := newHookAgent(t)
	first, second := a.post("Run /to-tickets"), a.post("Look at the spec")

	for _, notTheOperator := range []string{
		"[legato peer t2] I changed the schema",
		"[legato review pass] Pass 2 found nothing",
		"<task-notification><task-id>b1</task-id></task-notification>",
	} {
		a.send(promptPayload(notTheOperator))
		if status, _ := a.itemStatus(first); status != store.OperatorItemOpen {
			t.Fatalf("%q closed the agent's items", notTheOperator)
		}
	}

	a.send(promptPayload("/to-tickets"))
	for _, id := range []string{first, second} {
		if status, reason := a.itemStatus(id); status != store.OperatorItemClosed || reason != store.OperatorItemOperatorTurn {
			t.Fatalf("after the operator's prompt, %s is %s (%s)", id, status, reason)
		}
	}
	if got := a.now("").activity; got != store.ActivityWorking {
		t.Fatalf("activity = %q, want working", got)
	}
}

// A sandboxed agent's hook comes through the broker; the bridge of a Codex
// session reports a user message through its host.
func TestAnOperatorTurnReachesTheHostThroughTheBroker(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	execution, token := brokerSession(t, s, "abc123")
	h := cli.NewBrokerHandler(s, cli.BrokerDeps{})
	post := func(title string) string {
		item, err := service.PostOperatorItem(context.Background(), s, service.OperatorItemInput{TaskID: "abc123", ExecutionID: execution, Kind: service.OperatorItemQuestion, Title: title})
		if err != nil {
			t.Fatal(err)
		}
		return item.ID
	}
	status := func(id string) string {
		item, err := s.GetOperatorItem(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		return item.Status
	}

	hook, _ := cli.ParseClaudeHook(promptPayload("Use Postgres"))
	first := post("Which database?")
	if _, err := call(h, token, "agent.hook", map[string]any{"hook": hook}); err != nil {
		t.Fatal(err)
	}
	if status(first) != store.OperatorItemClosed {
		t.Fatal("the operator's prompt through the broker left the item open")
	}

	second := post("Which port?")
	tagged := cli.OperatorTurnHook("[legato peer t2] use 8080")
	if _, err := call(h, token, "agent.hook", map[string]any{"hook": tagged}); err != nil {
		t.Fatal(err)
	}
	if status(second) != store.OperatorItemOpen {
		t.Fatal("a peer's message closed the item")
	}
	if _, err := call(h, token, "agent.hook", map[string]any{"hook": cli.OperatorTurnHook("8080")}); err != nil {
		t.Fatal(err)
	}
	if status(second) != store.OperatorItemClosed {
		t.Fatal("the bridge's user message left the item open")
	}
}

func TestTheStoreBridgeHostClosesItemsOnTheOperatorsMessage(t *testing.T) {
	a := newHookAgent(t)
	id := a.post("Merge the PR")
	host := cli.StoreBridgeHost(a.s)
	if _, err := host.Session(context.Background(), a.taskID, a.execution); err != nil {
		t.Fatal(err)
	}
	if err := host.OperatorTurn(context.Background(), "[legato agent] noted"); err != nil {
		t.Fatal(err)
	}
	if status, _ := a.itemStatus(id); status != store.OperatorItemOpen {
		t.Fatal("a tagged message closed the item")
	}
	before := a.now("").activity
	if err := host.OperatorTurn(context.Background(), "merged"); err != nil {
		t.Fatal(err)
	}
	if status, reason := a.itemStatus(id); status != store.OperatorItemClosed || reason != store.OperatorItemOperatorTurn {
		t.Fatalf("after the operator's message, item is %s (%s)", status, reason)
	}
	// The bridge's report says nothing of the agent's activity, which its
	// hooks own.
	if after := a.now("").activity; after != before {
		t.Fatalf("activity moved from %q to %q", before, after)
	}
}
