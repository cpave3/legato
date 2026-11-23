package burnlite

import (
	"context"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CloseTask is the one write: a workflow closes a ticket whose pull request
// merged (LEG-540).
func TestCloseTaskClosesWithAReasonAndASummary(t *testing.T) {
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"close_task": func(map[string]any) *mcp.CallToolResult {
			return structured(map[string]any{"task": map[string]any{"key": "EST-7", "state": "closed"}})
		},
	}}
	c := New(fake.server(t).URL, &tokens{current: "t1"}, nil)
	if err := c.CloseTask(context.Background(), "EST-7", "completed", "Merged in acme/widget#12"); err != nil {
		t.Fatal(err)
	}
	args := fake.args[0]
	if fake.calls[0] != "close_task" || args["taskId"] != "EST-7" || args["reason"] != "completed" || args["summary"] != "Merged in acme/widget#12" {
		t.Fatalf("%v %v", fake.calls, args)
	}
}

// A token without write:tasks is refused as insufficient_scope: the
// connection must be made again.
func TestCloseTaskWithAReadOnlyTokenSaysSo(t *testing.T) {
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"close_task": func(map[string]any) *mcp.CallToolResult {
			return failedTool("insufficient_scope", "requires write:tasks")
		},
	}}
	c := New(fake.server(t).URL, &tokens{current: "t1"}, nil)
	err := c.CloseTask(context.Background(), "EST-7", "completed", "")
	if !errors.Is(err, ErrInsufficientScope) {
		t.Fatalf("err = %v", err)
	}
}
