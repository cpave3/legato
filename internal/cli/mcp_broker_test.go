package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/mcpserver"
)

func TestTheHostRunsAnAgentsToolsForItsOwnSession(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	_, token := brokerSession(t, s, "abc123")
	brokerSession(t, s, "other1")
	var builtFor []string
	ops := cli.MCPBrokerOps(func(caller cli.BrokerCaller) cli.MCPOptions {
		builtFor = append(builtFor, caller.TaskID)
		return cli.MCPOptions{Store: s, TaskID: caller.TaskID, ExecutionID: caller.ExecutionID}
	})
	h := cli.NewBrokerHandler(s, cli.BrokerDeps{Ops: ops})

	listed, err := call(h, token, "mcp.list", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(listed)
	var list struct {
		Instructions string           `json:"instructions"`
		Tools        []mcpserver.Spec `json:"tools"`
	}
	_ = json.Unmarshal(raw, &list)
	names := map[string]bool{}
	for _, tool := range list.Tools {
		names[tool.Name] = true
	}
	if !names["task_context"] || !names["show_card"] || list.Instructions == "" || len(list.Tools[0].InputSchema) == 0 {
		t.Fatalf("listed = %s", raw)
	}

	got, err := call(h, token, "mcp.call", map[string]any{"name": "task_context", "arguments": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(got)
	if !strings.Contains(string(raw), "abc123") || strings.Contains(string(raw), "other1") {
		t.Fatalf("task_context = %s", raw)
	}
	if builtFor[len(builtFor)-1] != "abc123" {
		t.Fatalf("built for %v", builtFor)
	}

	// A tool's own error reaches the model as the call's result.
	got, err = call(h, token, "mcp.call", map[string]any{"name": "add_task_note", "arguments": map[string]any{"body": ""}})
	if err != nil {
		t.Fatalf("a tool error failed the request: %v", err)
	}
	raw, _ = json.Marshal(got)
	if !strings.Contains(string(raw), `"is_error":true`) {
		t.Fatalf("add_task_note with no body = %s", raw)
	}
	if _, err := call(h, token, "mcp.call", map[string]any{"name": "no_such_tool"}); code(err) != "unknown_tool" {
		t.Fatalf("unknown tool: %v", err)
	}
}
