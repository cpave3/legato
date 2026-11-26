package burnlite

import (
	"context"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ClaimTask, ReleaseClaim and SetStage are ticket.update's writes (LEG-656).
func TestClaimTaskClaimsUnderAHolder(t *testing.T) {
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"claim_task": func(map[string]any) *mcp.CallToolResult {
			return structured(map[string]any{"claim": map[string]any{"holder": "legato"}, "alreadyHeld": false})
		},
	}}
	c := New(fake.server(t).URL, &tokens{current: "t1"}, nil)
	if err := c.ClaimTask(context.Background(), "LEG-9", "legato"); err != nil {
		t.Fatal(err)
	}
	if args := fake.args[0]; fake.calls[0] != "claim_task" || args["taskId"] != "LEG-9" || args["holder"] != "legato" {
		t.Fatalf("%v %v", fake.calls, args)
	}
}

func TestClaimTaskHeldByAnotherIsItsError(t *testing.T) {
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"claim_task": func(map[string]any) *mcp.CallToolResult {
			return failedTool("already_claimed", "LEG-9 is claimed by LEG-12")
		},
	}}
	c := New(fake.server(t).URL, &tokens{current: "t1"}, nil)
	err := c.ClaimTask(context.Background(), "LEG-9", "legato")
	var toolErr *Error
	if !errors.As(err, &toolErr) || toolErr.Code != "already_claimed" || toolErr.Message != "LEG-9 is claimed by LEG-12" {
		t.Fatalf("err = %v", err)
	}
}

func TestReleaseClaimReleasesTheHoldersClaim(t *testing.T) {
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"release_claim": func(map[string]any) *mcp.CallToolResult { return structured(map[string]any{"released": true}) },
	}}
	c := New(fake.server(t).URL, &tokens{current: "t1"}, nil)
	if err := c.ReleaseClaim(context.Background(), "LEG-9", "legato"); err != nil {
		t.Fatal(err)
	}
	if args := fake.args[0]; fake.calls[0] != "release_claim" || args["taskId"] != "LEG-9" || args["holder"] != "legato" {
		t.Fatalf("%v %v", fake.calls, args)
	}
}

func TestSetStageUpdatesTheTasksStage(t *testing.T) {
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"update_task": func(map[string]any) *mcp.CallToolResult {
			return structured(map[string]any{"task": map[string]any{"key": "LEG-9", "stage": "building"}})
		},
	}}
	c := New(fake.server(t).URL, &tokens{current: "t1"}, nil)
	if err := c.SetStage(context.Background(), "LEG-9", "building"); err != nil {
		t.Fatal(err)
	}
	if args := fake.args[0]; fake.calls[0] != "update_task" || args["taskId"] != "LEG-9" || args["stage"] != "building" || len(args) != 2 {
		t.Fatalf("%v %v", fake.calls, args)
	}
}

func TestGetTaskReadsTheStage(t *testing.T) {
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"get_task": func(map[string]any) *mcp.CallToolResult {
			return structured(map[string]any{"task": map[string]any{"key": "LEG-9", "state": "open", "stage": "ready"}})
		},
	}}
	c := New(fake.server(t).URL, &tokens{current: "t1"}, nil)
	task, err := c.GetTask(context.Background(), "LEG-9")
	if err != nil {
		t.Fatal(err)
	}
	if task.Stage != "ready" {
		t.Fatalf("stage = %q", task.Stage)
	}
}
