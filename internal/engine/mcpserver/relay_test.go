package mcpserver_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cpave3/legato/internal/engine/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAToolDescribesItselfAndCanBeCalledDirectly(t *testing.T) {
	tool := greetTool()
	spec := tool.Spec()
	if spec.Name != "greet" || spec.Title != "Greet" || !spec.ReadOnly || len(spec.InputSchema) == 0 {
		t.Fatalf("spec = %+v", spec)
	}
	result, err := tool.Call(context.Background(), json.RawMessage(`{"name":"ada","shout":true}`))
	if err != nil || result.Text != "HELLO ADA" {
		t.Fatalf("call = %+v, %v", result, err)
	}
	// Arguments that do not fit the schema are turned down here too.
	for _, raw := range []string{`{"shout":true}`, `{"name":7}`, `{"name":"ada","volume":11}`, `not json`} {
		if _, err := tool.Call(context.Background(), json.RawMessage(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestARelayedToolOffersTheSameSchemaAndForwardsItsCall(t *testing.T) {
	spec := greetTool().Spec()
	var forwarded json.RawMessage
	relay := mcpserver.NewRelayTool(spec, func(_ context.Context, raw json.RawMessage) (mcpserver.Result, error) {
		forwarded = raw
		return mcpserver.Result{Text: "relayed"}, nil
	})
	session := connect(t, mcpserver.Options{Name: "legato", Version: "1", Tools: []mcpserver.Tool{relay}})
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil || len(listed.Tools) != 1 || listed.Tools[0].Name != "greet" || listed.Tools[0].Description != spec.Description {
		t.Fatalf("listed = %+v, %v", listed, err)
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "greet", Arguments: map[string]any{"name": "ada"}})
	if err != nil || textOf(t, result) != "relayed" {
		t.Fatalf("call = %+v, %v", result, err)
	}
	var args map[string]any
	_ = json.Unmarshal(forwarded, &args)
	if args["name"] != "ada" {
		t.Fatalf("forwarded %s", forwarded)
	}
}
