package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/burnlite"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeBurnliteServer is a Burnlite's MCP endpoint over HTTP, its tasks and
// milestones in memory, answering list_tasks and list_milestones with their
// filters as Burnlite does. The service reaches it through the real
// burnlite client.
type fakeBurnliteServer struct {
	mu         sync.Mutex
	tasks      []fakeBurnliteTask // in Burnlite's order
	milestones []fakeBurnliteMilestone
	calls      []string // tool:args of each call
	srv        *httptest.Server
}

type fakeBurnliteTask struct {
	Key, Name, Kind, Milestone, State, CloseReason, Claim string
	Labels, BlockedBy                                     []string
}

type fakeBurnliteMilestone struct {
	Key, Name, State string
	Labels           []string
}

func (f *fakeBurnliteServer) task(key string) *fakeBurnliteTask {
	for i := range f.tasks {
		if f.tasks[i].Key == key {
			return &f.tasks[i]
		}
	}
	return nil
}

func (f *fakeBurnliteServer) openBlockers(t fakeBurnliteTask) []string {
	open := []string{}
	for _, key := range t.BlockedBy {
		if b := f.task(key); b != nil && b.State == "open" {
			open = append(open, key)
		}
	}
	return open
}

func (f *fakeBurnliteServer) row(t fakeBurnliteTask) map[string]any {
	blockers := f.openBlockers(t)
	var milestoneName any
	var milestone any
	if t.Milestone != "" {
		milestone = t.Milestone
		for _, m := range f.milestones {
			if m.Key == t.Milestone {
				milestoneName = m.Name
			}
		}
	}
	var claim any
	if t.Claim != "" {
		claim = map[string]any{"holder": t.Claim}
	}
	labels := t.Labels
	if labels == nil {
		labels = []string{}
	}
	return map[string]any{"key": t.Key, "url": "/" + t.Key, "taskName": t.Name, "kind": t.Kind, "milestoneKey": milestone, "milestoneName": milestoneName,
		"state": t.State, "closeReason": nil, "labels": labels, "blocked": len(blockers) > 0, "openBlockers": blockers, "claim": claim,
		"frontier": t.State == "open" && t.Claim == "" && len(blockers) == 0, "updatedAt": "2026-11-25T08:00:00.000Z"}
}

func stateMatches(want, state string) bool {
	return want == "any" || want == "" && state == "open" || want == state
}

func hasLabels(have []string, want []any) bool {
	for _, w := range want {
		if !slices.ContainsFunc(have, func(h string) bool { return strings.EqualFold(h, w.(string)) }) {
			return false
		}
	}
	return true
}

func (f *fakeBurnliteServer) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "burnlite", Version: "test"}, nil)
	result := func(v any) *mcp.CallToolResult {
		b, _ := json.Marshal(v)
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		return &mcp.CallToolResult{StructuredContent: out, Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}
	}
	notFound := func(what string) *mcp.CallToolResult {
		b, _ := json.Marshal(map[string]string{"code": "not_found", "message": what + " not found"})
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
	}
	tools := map[string]func(args map[string]any) *mcp.CallToolResult{
		"list_projects": func(map[string]any) *mcp.CallToolResult {
			return result(map[string]any{"projects": []any{map[string]any{"key": "EST", "name": "Estimator"}}})
		},
		"list_tasks": func(args map[string]any) *mcp.CallToolResult {
			rows := []any{}
			for _, task := range f.tasks {
				labels, _ := args["labels"].([]any)
				switch {
				case projectOf(task.Key) != args["projectId"],
					args["milestoneId"] != nil && task.Milestone != args["milestoneId"],
					args["kind"] != nil && task.Kind != args["kind"],
					!stateMatches(stringArg(args["state"]), task.State),
					!hasLabels(task.Labels, labels):
					continue
				}
				row := f.row(task)
				if args["frontier"] == true && row["frontier"] != true {
					continue
				}
				rows = append(rows, row)
			}
			return result(map[string]any{"tasks": rows})
		},
		"list_milestones": func(args map[string]any) *mcp.CallToolResult {
			rows := []any{}
			for _, m := range f.milestones {
				labels, _ := args["labels"].([]any)
				if projectOf(m.Key) != args["projectId"] || !stateMatches(stringArg(args["state"]), m.State) || !hasLabels(m.Labels, labels) {
					continue
				}
				rows = append(rows, map[string]any{"key": m.Key, "url": "/" + m.Key, "name": m.Name, "state": m.State, "closeReason": nil,
					"labels": m.Labels, "updatedAt": "2026-11-25T08:00:00.000Z"})
			}
			return result(map[string]any{"milestones": rows})
		},
		"get_task": func(args map[string]any) *mcp.CallToolResult {
			task := f.task(stringArg(args["taskId"]))
			if task == nil {
				return notFound("Task")
			}
			blockers := []any{}
			for _, key := range task.BlockedBy {
				blockers = append(blockers, map[string]any{"key": key, "taskName": key, "state": f.task(key).State})
			}
			return result(map[string]any{"task": map[string]any{"key": task.Key, "url": "/" + task.Key, "taskName": task.Name, "kind": task.Kind,
				"state": task.State, "description": "Build " + task.Name + ".", "labels": task.Labels, "blocked": len(f.openBlockers(*task)) > 0,
				"blockers": blockers, "updatedAt": "2026-11-25T08:00:00.000Z"}})
		},
		"get_milestone": func(args map[string]any) *mcp.CallToolResult {
			for _, m := range f.milestones {
				if m.Key != stringArg(args["milestoneId"]) {
					continue
				}
				rows := []any{}
				for _, task := range f.tasks {
					if task.Milestone == m.Key {
						rows = append(rows, f.row(task))
					}
				}
				return result(map[string]any{"milestone": map[string]any{"key": m.Key, "url": "/" + m.Key, "name": m.Name, "state": m.State,
					"description": "The " + m.Name + " milestone.", "labels": m.Labels, "tasks": rows, "updatedAt": "2026-11-25T08:00:00.000Z"}})
			}
			return notFound("Milestone")
		},
	}
	for name, tool := range tools {
		srv.AddTool(&mcp.Tool{Name: name, InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args map[string]any
			_ = json.Unmarshal(req.Params.Arguments, &args)
			f.mu.Lock()
			defer f.mu.Unlock()
			encoded, _ := json.Marshal(args)
			f.calls = append(f.calls, name+":"+string(encoded))
			return tool(args), nil
		})
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/mcp" || r.Header.Get("Authorization") != "Bearer burnlite-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f.srv
}

// toolCalls counts the calls made to one tool.
func (f *fakeBurnliteServer) toolCalls(tool string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, call := range f.calls {
		if strings.HasPrefix(call, tool+":") {
			n++
		}
	}
	return n
}

func stringArg(v any) string {
	s, _ := v.(string)
	return s
}

type burnliteToken string

func (b burnliteToken) Token(context.Context, bool) (string, error) { return string(b), nil }

// source is the Burnlite source over the fake, through the real client.
func (f *fakeBurnliteServer) source(t *testing.T, projects ...string) *burnliteSource {
	t.Helper()
	ts := f.serve(t)
	return newBurnliteSource(BurnliteSourceOptions{
		Projects: func(context.Context) []string { return projects },
		Columns:  BurnliteColumns{Ready: "Ready", Blocked: "Backlog", Claimed: "Doing", Closed: "Done"},
		Interval: time.Minute,
	}, func(context.Context) (burnliteReader, string, error) {
		return burnlite.New(ts.URL, burnliteToken("burnlite-token"), ts.Client()), ts.URL, nil
	})
}

// buildMilestoneBurnlite is example A's Burnlite: milestone EST-9, whose
// frontier is EST-10 and EST-12 (EST-11 waits on EST-10, EST-13 is closed),
// and EST-20, whose one task waits on EST-11 outside it.
func buildMilestoneBurnlite() *fakeBurnliteServer {
	return &fakeBurnliteServer{
		milestones: []fakeBurnliteMilestone{
			{Key: "EST-9", Name: "Sync", State: "open", Labels: []string{"ready-for-agent"}},
			{Key: "EST-20", Name: "Later", State: "open"},
			{Key: "EST-30", Name: "Only notes", State: "open"},
			{Key: "EST-40", Name: "Done", State: "closed"},
		},
		tasks: []fakeBurnliteTask{
			{Key: "EST-10", Name: "Wire the client", Kind: "work", Milestone: "EST-9", State: "open", Labels: []string{"api"}},
			{Key: "EST-11", Name: "Use the client", Kind: "work", Milestone: "EST-9", State: "open", BlockedBy: []string{"EST-10"}},
			{Key: "EST-12", Name: "Write it up", Kind: "work", Milestone: "EST-9", State: "open", Labels: []string{"docs"}},
			{Key: "EST-13", Name: "Spike", Kind: "work", Milestone: "EST-9", State: "closed"},
			{Key: "EST-14", Name: "Decide the shape", Kind: "meta", Milestone: "EST-9", State: "open", Claim: "someone"},
			{Key: "EST-21", Name: "Build on it", Kind: "work", Milestone: "EST-20", State: "open", BlockedBy: []string{"EST-11"}},
			{Key: "EST-31", Name: "A note", Kind: "meta", Milestone: "EST-30", State: "open"},
			{Key: "EST-41", Name: "Shipped", Kind: "work", Milestone: "EST-40", State: "closed"},
			{Key: "EST-50", Name: "Loose", Kind: "work", State: "open"},
		},
	}
}
