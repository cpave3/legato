package cli_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// operatorAttention is the Attention service a running Legato has, with the
// operator items producer registered.
func operatorAttention(t *testing.T, s *store.Store) *service.AttentionService {
	t.Helper()
	attention := service.NewAttentionService(s, nil)
	if err := attention.RegisterProducer(service.NewOperatorItemsAttentionAdapter(s)); err != nil {
		t.Fatal(err)
	}
	return attention
}

// operatorItems reconciles the producer, as the event an item's change
// publishes makes a running Legato do, and lists what Attention shows.
func operatorItems(t *testing.T, attention *service.AttentionService) []service.AttentionItem {
	t.Helper()
	ctx := context.Background()
	if err := attention.Reconcile(ctx, service.OperatorItemsAttentionProducer); err != nil {
		t.Fatal(err)
	}
	items, err := attention.List(ctx, service.AttentionFilter{Producer: service.OperatorItemsAttentionProducer})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

var operatorItemID = regexp.MustCompile(`\bop-[0-9a-f]{10}\b`)

func postedID(t *testing.T, text string) string {
	t.Helper()
	id := operatorItemID.FindString(text)
	if id == "" {
		t.Fatalf("for_operator did not return an item ID: %q", text)
	}
	return id
}

func TestForOperatorPutsEachKindOfItemInAttention(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	attention := operatorAttention(t, s)
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID})

	cases := []struct {
		kind, signal string
		args         map[string]any
	}{
		{"action", service.AttentionSignalOperatorAction, map[string]any{"why": "Blocks the 7 LEG-365 build tickets", "how": map[string]any{"kind": "slash", "text": "/to-tickets"}}},
		{"question", service.AttentionSignalOperatorQuestion, map[string]any{"options": []string{"Postgres", "SQLite"}}},
		{"done", service.AttentionSignalOperatorDone, map[string]any{"how": map[string]any{"kind": "link", "text": "https://github.com/cpave3/legato/pull/1"}}},
		{"other", service.AttentionSignalOperatorOther, nil},
	}
	ids := map[string]string{}
	for _, c := range cases {
		args := map[string]any{"kind": c.kind, "title": "Item of kind " + c.kind}
		for k, v := range c.args {
			args[k] = v
		}
		result := callMCP(t, session, "for_operator", args)
		if result.IsError {
			t.Fatalf("for_operator %s failed: %s", c.kind, mcpText(result))
		}
		text := mcpText(result)
		if !strings.Contains(text, "It is in the operator's rail. If you can't go on without it, end your turn.") {
			t.Fatalf("for_operator result = %q", text)
		}
		ids[c.kind] = postedID(t, text)
	}

	items := operatorItems(t, attention)
	if len(items) != len(cases) {
		t.Fatalf("attention lists %d operator items, want %d: %#v", len(items), len(cases), items)
	}
	byKey := map[string]service.AttentionItem{}
	for _, item := range items {
		byKey[item.SourceKey] = item
	}
	for _, c := range cases {
		item, ok := byKey[ids[c.kind]]
		if !ok {
			t.Fatalf("no attention item for the %s item %s", c.kind, ids[c.kind])
		}
		if item.SignalKind != c.signal || item.Title != "Item of kind "+c.kind || item.TaskID != "task1" || item.Urgency != "normal" {
			t.Fatalf("%s item = %#v", c.kind, item)
		}
		if item.Navigation.Kind != "agent" || item.Navigation.Target != "task1" {
			t.Fatalf("%s item navigates to %#v, want the agent of task1", c.kind, item.Navigation)
		}
		if len(item.Actions) == 0 || item.Actions[0].ID != "open" || item.Actions[0].Kind != "navigate" || item.Actions[0].Intent != "primary" {
			t.Fatalf("%s item actions = %#v, want Open first and primary", c.kind, item.Actions)
		}
	}
	if why := byKey[ids["action"]].Reason; why != "Blocks the 7 LEG-365 build tickets" {
		t.Fatalf("the action item's reason = %q, want its why", why)
	}
}

func TestForOperatorWithTheSameTitleUpdatesTheItemAndUndoesADismissal(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	attention := operatorAttention(t, s)
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID})
	ctx := context.Background()

	first := postedID(t, mcpText(callMCP(t, session, "for_operator", map[string]any{"kind": "action", "title": "Run /to-spec", "why": "Blocks the spec"})))
	items := operatorItems(t, attention)
	if len(items) != 1 {
		t.Fatalf("items = %#v", items)
	}
	if err := attention.Dismiss(ctx, items[0].ID, items[0].SourceRevision); err != nil {
		t.Fatal(err)
	}
	if items := operatorItems(t, attention); len(items) != 0 {
		t.Fatalf("a dismissed item is still listed: %#v", items)
	}

	result := callMCP(t, session, "for_operator", map[string]any{"kind": "question", "title": "Run /to-spec", "why": "Blocks the spec and the tickets", "options": []string{"Now", "Later"}})
	if result.IsError {
		t.Fatalf("for_operator again failed: %s", mcpText(result))
	}
	if again := postedID(t, mcpText(result)); again != first {
		t.Fatalf("posting the same title made item %s, want %s updated", again, first)
	}
	items = operatorItems(t, attention)
	if len(items) != 1 || items[0].SourceKey != first || items[0].SignalKind != service.AttentionSignalOperatorQuestion || items[0].Reason != "Blocks the spec and the tickets" {
		t.Fatalf("after the update, items = %#v", items)
	}
}

func TestClearForOperatorClosesOneItemOrAllOfTheCallersOwn(t *testing.T) {
	s := newTestStore(t)
	mine := seedMCPAgent(t, s, "task1")
	theirs := seedMCPAgent(t, s, "task2")
	attention := operatorAttention(t, s)
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: mine})
	other := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task2", ExecutionID: theirs})

	a := postedID(t, mcpText(callMCP(t, session, "for_operator", map[string]any{"kind": "action", "title": "Merge the PR"})))
	b := postedID(t, mcpText(callMCP(t, session, "for_operator", map[string]any{"kind": "done", "title": "The rail is built"})))
	c := postedID(t, mcpText(callMCP(t, session, "for_operator", map[string]any{"kind": "other", "title": "Heads up"})))
	theirItem := postedID(t, mcpText(callMCP(t, other, "for_operator", map[string]any{"kind": "action", "title": "Restart Legato"})))

	// Another agent's item is not the caller's to clear.
	if result := callMCP(t, session, "clear_for_operator", map[string]any{"id": theirItem}); !result.IsError {
		t.Fatalf("clearing another agent's item succeeded: %s", mcpText(result))
	}
	if result := callMCP(t, session, "clear_for_operator", map[string]any{"id": a}); result.IsError {
		t.Fatalf("clear_for_operator %s failed: %s", a, mcpText(result))
	}
	keys := func() map[string]bool {
		out := map[string]bool{}
		for _, item := range operatorItems(t, attention) {
			out[item.SourceKey] = true
		}
		return out
	}
	if got := keys(); got[a] || !got[b] || !got[c] || !got[theirItem] {
		t.Fatalf("after clearing %s, items = %v", a, got)
	}

	result := callMCP(t, session, "clear_for_operator", map[string]any{})
	if result.IsError {
		t.Fatalf("clear_for_operator with no id failed: %s", mcpText(result))
	}
	if !strings.Contains(mcpText(result), "2") {
		t.Fatalf("clear_for_operator did not say how many it cleared: %q", mcpText(result))
	}
	if got := keys(); len(got) != 1 || !got[theirItem] {
		t.Fatalf("after clearing all of task1's items, items = %v", got)
	}
	item, err := s.GetOperatorItem(context.Background(), b)
	if err != nil || item.Status != store.OperatorItemClosed || item.CloseReason != store.OperatorItemCleared || item.ClosedAt == nil {
		t.Fatalf("cleared item = %#v, %v", item, err)
	}
}

func TestForOperatorRefusesInputItCannotShow(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID})

	for name, args := range map[string]map[string]any{
		"an unknown kind":           {"kind": "fyi", "title": "Something"},
		"no title":                  {"kind": "action", "title": "  "},
		"a title too long":          {"kind": "action", "title": strings.Repeat("x", 121)},
		"a title of two lines":      {"kind": "action", "title": "One\nTwo"},
		"a why too long":            {"kind": "action", "title": "Something", "why": strings.Repeat("x", 201)},
		"options on a non-question": {"kind": "action", "title": "Something", "options": []string{"Yes", "No"}},
		"more than four options":    {"kind": "question", "title": "Which?", "options": []string{"a", "b", "c", "d", "e"}},
		"an unknown how":            {"kind": "action", "title": "Something", "how": map[string]any{"kind": "script", "text": "rm -rf /"}},
		"a how with no text":        {"kind": "action", "title": "Something", "how": map[string]any{"kind": "command", "text": ""}},
		"a link that is no address": {"kind": "done", "title": "Something", "how": map[string]any{"kind": "link", "text": "javascript:alert(1)"}},
	} {
		if result := callMCP(t, session, "for_operator", args); !result.IsError {
			t.Errorf("for_operator with %s succeeded: %s", name, mcpText(result))
		}
	}
	items, err := s.ListOpenOperatorItems(context.Background())
	if err != nil || len(items) != 0 {
		t.Fatalf("refused calls left items: %#v, %v", items, err)
	}
}

func TestForOperatorTellsRunningLegatoInstances(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	received := make(chan ipc.Message, 4)
	server := newTestIPCServer(t, filepath.Join(ipc.SocketDir(), "legato-mcp-operator-test.sock"), func(msg ipc.Message) { received <- msg })
	defer server.Close()
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID})

	callMCP(t, session, "for_operator", map[string]any{"kind": "action", "title": "Merge the PR"})
	select {
	case msg := <-received:
		if msg.Type != "operator_items_changed" || msg.TaskID != "task1" {
			t.Fatalf("ipc message = %#v", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("running Legato instances were not told about the item")
	}
}

func TestForOperatorWorksThroughTheBroker(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	_, token := brokerSession(t, s, "abc123")
	attention := operatorAttention(t, s)
	ops := cli.MCPBrokerOps(func(caller cli.BrokerCaller) cli.MCPOptions {
		return cli.MCPOptions{Store: s, TaskID: caller.TaskID, ExecutionID: caller.ExecutionID}
	})
	h := cli.NewBrokerHandler(s, cli.BrokerDeps{Ops: ops})

	got, err := call(h, token, "mcp.call", map[string]any{"name": "for_operator", "arguments": map[string]any{"kind": "action", "title": "Test the build in a browser"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), `"is_error":true`) {
		t.Fatalf("for_operator through the broker = %s", raw)
	}
	id := postedID(t, string(raw))
	items := operatorItems(t, attention)
	if len(items) != 1 || items[0].SourceKey != id || items[0].TaskID != "abc123" {
		t.Fatalf("items = %#v", items)
	}

	got, err = call(h, token, "mcp.call", map[string]any{"name": "clear_for_operator", "arguments": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ = json.Marshal(got); strings.Contains(string(raw), `"is_error":true`) {
		t.Fatalf("clear_for_operator through the broker = %s", raw)
	}
	if items := operatorItems(t, attention); len(items) != 0 {
		t.Fatalf("after clearing, items = %#v", items)
	}
}

func TestMCPGuidanceSaysWhenToPutSomethingInTheOperatorsRail(t *testing.T) {
	for _, want := range []string{"for_operator", "clear_for_operator", "never", "progress"} {
		if !strings.Contains(cli.MCPGuidance, want) {
			t.Errorf("guidance does not mention %q", want)
		}
	}
}

// The Reeve's session has the operator tools too.
func TestTheReeveCanPostForTheOperator(t *testing.T) {
	s := newTestStore(t)
	reeveTask, reeveExecution := seedReeve(t, s)
	reeve := connectMCP(t, cli.MCPOptions{Store: s, TaskID: reeveTask, ExecutionID: reeveExecution, Reeve: everyReeveTool()})
	listed, err := reeve.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	offered := map[string]bool{}
	for _, tool := range listed.Tools {
		offered[tool.Name] = true
	}
	if !offered["for_operator"] || !offered["clear_for_operator"] {
		t.Fatalf("the Reeve is offered %v", offered)
	}
	if result := callMCP(t, reeve, "for_operator", map[string]any{"kind": "question", "title": "Restart Legato now?", "options": []string{"Yes", "Later"}}); result.IsError {
		t.Fatalf("the Reeve's for_operator failed: %s", mcpText(result))
	}
}

// A question an agent asks with its tool's own question prompt is already
// in the operator's rail: the tool's description and the guidance both say
// not to post it again with for_operator.
func TestForOperatorSaysNotToPostAQuestionTwice(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID, Agents: &fakePeers{}})
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	const want = "never post a question you are also asking with your agent tool's own question tool"
	found := false
	for _, tool := range listed.Tools {
		if tool.Name == "for_operator" {
			found = true
			if !strings.Contains(strings.ToLower(tool.Description), want) {
				t.Errorf("for_operator's description does not say %q:\n%s", want, tool.Description)
			}
		}
	}
	if !found {
		t.Fatal("for_operator is not listed")
	}
	if !strings.Contains(strings.ToLower(cli.MCPGuidance), want) {
		t.Errorf("the guidance does not say %q", want)
	}
}
