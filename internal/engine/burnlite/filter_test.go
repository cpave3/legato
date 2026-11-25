package burnlite

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A filtered list asks Burnlite for only what the filter names (LEG-488).
func TestFindTasksSendsTheFilter(t *testing.T) {
	var row map[string]any
	_ = json.Unmarshal([]byte(listRow), &row)
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"list_tasks": func(map[string]any) *mcp.CallToolResult { return structured(map[string]any{"tasks": []any{row}}) },
	}}
	ts := fake.server(t)
	c := New(ts.URL, &tokens{current: "t1"}, nil)
	defer c.Close()

	rows, err := c.FindTasks(context.Background(), TaskFilter{Project: "EST", Milestone: "EST-3", Kind: "work", Labels: []string{"api"}, State: "open", Frontier: true})
	if err != nil {
		t.Fatal(err)
	}
	got := fake.args[0]
	if got["projectId"] != "EST" || got["milestoneId"] != "EST-3" || got["kind"] != "work" || got["state"] != "open" || got["frontier"] != true {
		t.Errorf("args = %v", got)
	}
	if labels, _ := got["labels"].([]any); len(labels) != 1 || labels[0] != "api" {
		t.Errorf("labels = %v", got["labels"])
	}
	if len(rows) != 1 || rows[0].Key != "EST-7" || rows[0].MilestoneKey != "EST-3" {
		t.Fatalf("rows = %+v", rows)
	}

	// What the filter leaves out is not sent.
	if _, err := c.FindTasks(context.Background(), TaskFilter{Project: "EST"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"milestoneId", "kind", "labels", "frontier"} {
		if _, sent := fake.args[1][name]; sent {
			t.Errorf("%s sent: %v", name, fake.args[1])
		}
	}
	if fake.args[1]["state"] != "open" {
		t.Errorf("state = %v, want open by default", fake.args[1]["state"])
	}
}

func TestFindMilestonesReadsEachRow(t *testing.T) {
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"list_milestones": func(map[string]any) *mcp.CallToolResult {
			return structured(map[string]any{"milestones": []any{
				map[string]any{"key": "EST-3", "url": "/EST-3", "name": "Sync", "state": "open", "closeReason": nil, "labels": []any{"ready-for-agent"}, "updatedAt": "2026-11-05T01:02:03.456Z"},
				map[string]any{"key": "EST-9", "url": "/EST-9", "name": "Old", "state": "closed", "closeReason": "not_planned"},
			}})
		},
	}}
	ts := fake.server(t)
	c := New(ts.URL, &tokens{current: "t1"}, nil)
	defer c.Close()

	rows, err := c.FindMilestones(context.Background(), MilestoneFilter{Project: "EST", State: "any", Labels: []string{"ready-for-agent"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := fake.args[0]; got["projectId"] != "EST" || got["state"] != "any" {
		t.Errorf("args = %v", got)
	}
	if len(rows) != 2 || rows[0].Name != "Sync" || rows[0].URL != "/EST-3" || rows[0].State != "open" || len(rows[0].Labels) != 1 ||
		rows[1].State != "closed" || rows[1].CloseReason != "not_planned" {
		t.Fatalf("rows = %+v", rows)
	}
}
