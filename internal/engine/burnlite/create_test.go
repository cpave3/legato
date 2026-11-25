package burnlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CreateTask files a Task for a workflow's ticket.create step (LEG-640).
func TestCreateTaskSendsTheDraftAndReadsTheKey(t *testing.T) {
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"create_task": func(map[string]any) *mcp.CallToolResult {
			return structured(map[string]any{"task": map[string]any{"id": "uuid-9", "key": "LEG-9", "url": "/LEG-9", "taskName": "Fix login"}})
		},
	}}
	c := New(fake.server(t).URL, &tokens{current: "t1"}, nil)
	got, err := c.CreateTask(context.Background(), TaskDraft{
		Project: "LEG", Name: "Fix login", Description: "It **fails**", Labels: []string{"bug"},
		Kind: "work", Size: "Medium", Uncertainty: "Low", Milestone: "LEG-3", Holder: "legato",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != (CreatedTask{ID: "uuid-9", Key: "LEG-9", URL: "/LEG-9"}) {
		t.Fatalf("created = %+v", got)
	}
	want := map[string]any{"projectId": "LEG", "taskName": "Fix login", "description": "It **fails**", "labels": []any{"bug"},
		"kind": "work", "size": "Medium", "uncertaintyLevel": "Low", "milestoneId": "LEG-3", "holder": "legato"}
	if fake.calls[0] != "create_task" || !reflect.DeepEqual(fake.args[0], want) {
		t.Fatalf("%v %v", fake.calls, fake.args[0])
	}
}

// What is not given is not sent: Burnlite refuses a size on a Meta task.
func TestCreateTaskLeavesOutWhatIsNotGiven(t *testing.T) {
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"create_task": func(map[string]any) *mcp.CallToolResult {
			return structured(map[string]any{"task": map[string]any{"id": "u", "key": "LEG-10", "url": "/LEG-10"}})
		},
	}}
	c := New(fake.server(t).URL, &tokens{current: "t1"}, nil)
	if _, err := c.CreateTask(context.Background(), TaskDraft{Project: "LEG", Name: "Decide", Kind: "meta"}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"projectId": "LEG", "taskName": "Decide", "kind": "meta"}
	if !reflect.DeepEqual(fake.args[0], want) {
		t.Fatalf("args = %v", fake.args[0])
	}
}

func TestCreateTaskWithAReadOnlyTokenSaysSo(t *testing.T) {
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"create_task": func(map[string]any) *mcp.CallToolResult {
			return failedTool("insufficient_scope", "requires write:tasks")
		},
	}}
	c := New(fake.server(t).URL, &tokens{current: "t1"}, nil)
	if _, err := c.CreateTask(context.Background(), TaskDraft{Project: "LEG", Name: "x", Kind: "meta"}); !errors.Is(err, ErrInsufficientScope) {
		t.Fatalf("err = %v", err)
	}
}

// AddNote is ticket.comment's write.
func TestAddNoteAddsANoteToATask(t *testing.T) {
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"create_note": func(map[string]any) *mcp.CallToolResult {
			return structured(map[string]any{"note": map[string]any{"id": "note-1", "content": "Seen again"}})
		},
	}}
	c := New(fake.server(t).URL, &tokens{current: "t1"}, nil)
	id, err := c.AddNote(context.Background(), "LEG-9", "Seen again", "legato")
	if err != nil {
		t.Fatal(err)
	}
	if id != "note-1" {
		t.Fatalf("id = %q", id)
	}
	want := map[string]any{"taskId": "LEG-9", "content": "Seen again", "holder": "legato"}
	if fake.calls[0] != "create_note" || !reflect.DeepEqual(fake.args[0], want) {
		t.Fatalf("%v %v", fake.calls, fake.args[0])
	}
}

func TestAddNoteToAMissingTaskIsNotFound(t *testing.T) {
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"create_note": func(map[string]any) *mcp.CallToolResult { return failedTool("not_found", "no Task LEG-999") },
	}}
	c := New(fake.server(t).URL, &tokens{current: "t1"}, nil)
	if _, err := c.AddNote(context.Background(), "LEG-999", "x", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// When a Task was created tells a Task a lost create made from one that
// was only changed since (LEG-640).
func TestGetTaskReadsWhenItWasCreated(t *testing.T) {
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"get_task": func(map[string]any) *mcp.CallToolResult {
			return structured(map[string]any{"task": map[string]any{"id": "uuid-9", "key": "LEG-9", "taskName": "Fix login", "createdAt": "2026-11-25T07:52:12.810Z"}})
		},
	}}
	c := New(fake.server(t).URL, &tokens{current: "t1"}, nil)
	task, err := c.GetTask(context.Background(), "LEG-9")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 11, 25, 7, 52, 12, 810_000_000, time.UTC); !task.CreatedAt.Equal(want) || task.ID != "uuid-9" {
		t.Fatalf("task = %+v", task)
	}
}
