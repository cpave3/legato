package burnlite

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeBurnlite is a Burnlite MCP endpoint with canned tool results. It
// accepts only the bearer token in token.
type fakeBurnlite struct {
	mu      sync.Mutex
	token   string
	results map[string]func(args map[string]any) *mcp.CallToolResult
	calls   []string
	args    []map[string]any
	auth    []string
}

func (f *fakeBurnlite) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "burnlite", Version: "test"}, nil)
	for _, name := range []string{"list_projects", "list_tasks", "get_task"} {
		srv.AddTool(&mcp.Tool{Name: name, InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args map[string]any
			_ = json.Unmarshal(req.Params.Arguments, &args)
			f.mu.Lock()
			f.calls = append(f.calls, name)
			f.args = append(f.args, args)
			result := f.results[name]
			f.mu.Unlock()
			return result(args), nil
		})
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		token := f.token
		f.mu.Unlock()
		if r.URL.Path != "/api/llm/mcp" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func structured(v any) *mcp.CallToolResult {
	b, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return &mcp.CallToolResult{StructuredContent: out, Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}
}

func failedTool(code, message string) *mcp.CallToolResult {
	b, _ := json.Marshal(map[string]string{"code": code, "message": message})
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

// tokens hands out the token it holds; force swaps in the next one.
type tokens struct {
	mu      sync.Mutex
	current string
	next    string
	forced  int
	err     error
}

func (s *tokens) Token(_ context.Context, force bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", s.err
	}
	if force {
		s.forced++
		if s.next != "" {
			s.current = s.next
		}
	}
	return s.current, nil
}

const listRow = `{"id":"u-7","number":7,"key":"EST-7","url":"/EST-7","taskName":"Wire the thing","kind":"work",
"milestoneId":"m-3","milestoneKey":"EST-3","milestoneName":"Sync","state":"open","closeReason":null,"resolutionSummary":null,
"completedAt":null,"labels":["api","sync"],"blocked":true,"openBlockers":["EST-5"],
"claim":{"holder":"agent-1","credential":"oauth:x","claimedAt":"2026-11-05T01:00:00.000Z"},"frontier":false,
"updatedAt":"2026-11-05T01:02:03.456Z"}`

func TestListTasksReadsEachRow(t *testing.T) {
	var row map[string]any
	_ = json.Unmarshal([]byte(listRow), &row)
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"list_tasks": func(map[string]any) *mcp.CallToolResult { return structured(map[string]any{"tasks": []any{row}}) },
	}}
	ts := fake.server(t)
	c := New(ts.URL, &tokens{current: "t1"}, nil)

	rows, err := c.ListTasks(context.Background(), "EST", "any")
	if err != nil {
		t.Fatal(err)
	}
	if fake.args[0]["projectId"] != "EST" || fake.args[0]["state"] != "any" {
		t.Errorf("args = %v", fake.args[0])
	}
	want := TaskRow{Key: "EST-7", URL: "/EST-7", Name: "Wire the thing", Kind: "work", MilestoneKey: "EST-3", MilestoneName: "Sync",
		State: "open", Labels: []string{"api", "sync"}, Blocked: true, OpenBlockers: []string{"EST-5"}, ClaimHolder: "agent-1",
		UpdatedAt: time.Date(2026, 11, 5, 1, 2, 3, 456_000_000, time.UTC)}
	if len(rows) != 1 || !rowsEqual(rows[0], want) {
		t.Fatalf("rows = %+v", rows)
	}
}

func rowsEqual(a, b TaskRow) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb) && a.UpdatedAt.Equal(b.UpdatedAt)
}

func TestGetTaskReadsTheTextAndTheChecklist(t *testing.T) {
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"get_task": func(args map[string]any) *mcp.CallToolResult {
			return structured(map[string]any{"task": map[string]any{
				"key": args["taskId"], "url": "/EST-7", "taskName": "Wire the thing", "kind": "work", "size": "Large",
				"uncertaintyLevel": "High", "state": "closed", "closeReason": "not_planned", "resolutionSummary": "Dropped",
				"description": "What and why", "plan": "How", "labels": []string{"api"}, "blocked": false,
				"blockers": []map[string]any{{"key": "EST-5", "taskName": "Before", "state": "closed"}},
				"claim":    nil, "updatedAt": "2026-11-05T01:02:03.456Z",
				"checklistItems": []map[string]any{{"name": "second", "done": false, "displayOrder": 2}, {"name": "first", "done": true, "displayOrder": 1}},
			}})
		},
	}}
	c := New(fake.server(t).URL, &tokens{current: "t1"}, nil)

	task, err := c.GetTask(context.Background(), "EST-7")
	if err != nil {
		t.Fatal(err)
	}
	if task.Key != "EST-7" || task.Description != "What and why" || task.Plan != "How" || task.Size != "Large" ||
		task.Uncertainty != "High" || task.State != "closed" || task.CloseReason != "not_planned" || task.ResolutionSummary != "Dropped" {
		t.Errorf("task = %+v", task)
	}
	if len(task.Checklist) != 2 || task.Checklist[0].Name != "first" || !task.Checklist[0].Done || task.Checklist[1].Name != "second" {
		t.Errorf("checklist = %+v (want displayOrder order)", task.Checklist)
	}
	if len(task.Blockers) != 1 || task.Blockers[0].Key != "EST-5" || task.Blockers[0].State != "closed" {
		t.Errorf("blockers = %+v", task.Blockers)
	}
}

func TestListProjects(t *testing.T) {
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"list_projects": func(map[string]any) *mcp.CallToolResult {
			return structured(map[string]any{"projects": []map[string]any{{"id": "p1", "key": "EST", "name": "Estimator", "status": "active"}}})
		},
	}}
	c := New(fake.server(t).URL, &tokens{current: "t1"}, nil)
	projects, err := c.ListProjects(context.Background())
	if err != nil || len(projects) != 1 || projects[0].Key != "EST" || projects[0].Name != "Estimator" {
		t.Fatalf("projects = %+v, %v", projects, err)
	}
}

func TestAToolErrorKeepsBurnlitesCode(t *testing.T) {
	fake := &fakeBurnlite{token: "t1", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"get_task": func(map[string]any) *mcp.CallToolResult { return failedTool("not_found", "Task EST-99 not found") },
	}}
	c := New(fake.server(t).URL, &tokens{current: "t1"}, nil)
	_, err := c.GetTask(context.Background(), "EST-99")
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "EST-99") {
		t.Fatalf("err = %v", err)
	}
}

func TestARefusedTokenIsRefreshedOnceThenReported(t *testing.T) {
	fake := &fakeBurnlite{token: "t2", results: map[string]func(map[string]any) *mcp.CallToolResult{
		"list_projects": func(map[string]any) *mcp.CallToolResult { return structured(map[string]any{"projects": []any{}}) },
	}}
	ts := fake.server(t)

	// The token in hand has expired; a fresh one works.
	src := &tokens{current: "t1", next: "t2"}
	if _, err := New(ts.URL, src, nil).ListProjects(context.Background()); err != nil {
		t.Fatalf("after a refresh: %v", err)
	}
	if src.forced != 1 {
		t.Errorf("forced %d refreshes", src.forced)
	}

	// No token works.
	src = &tokens{current: "bad", next: "worse"}
	_, err := New(ts.URL, src, nil).ListProjects(context.Background())
	if !errors.Is(err, ErrUnauthorized) || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestWithoutATokenNothingIsSent(t *testing.T) {
	fake := &fakeBurnlite{token: "t1"}
	ts := fake.server(t)
	_, err := New(ts.URL, &tokens{err: errors.New("not connected")}, nil).ListProjects(context.Background())
	if err == nil || len(fake.auth) != 0 {
		t.Fatalf("err = %v, requests = %d", err, len(fake.auth))
	}
}
