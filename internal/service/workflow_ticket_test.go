package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/burnlite"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeJira is Jira's create, comment and search endpoints.
type fakeJira struct {
	mu       sync.Mutex
	srv      *httptest.Server
	creates  []map[string]any
	comments []string
	// refuse answers a create with HTTP 400 and this body.
	refuse string
	// drop closes the connection after a create, as a network failure
	// after Jira acted would.
	drop bool
	// found is what search answers: issues as {key, fields: {summary}}.
	found []map[string]any
	jql   []string
}

func newFakeJira(t *testing.T) *fakeJira {
	t.Helper()
	f := &fakeJira{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue":
			if f.refuse != "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(f.refuse))
				return
			}
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			f.creates = append(f.creates, body)
			n := len(f.creates)
			if f.drop {
				conn, _, _ := w.(http.Hijacker).Hijack()
				_ = conn.Close()
				return
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"id":"1000%d","key":"AG-%d"}`, n, 11+n)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comment"):
			f.comments = append(f.comments, r.URL.Path)
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"id":"2000%d"}`, len(f.comments))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/search/jql":
			f.jql = append(f.jql, r.URL.Query().Get("jql"))
			body, _ := json.Marshal(map[string]any{"issues": f.found, "isLast": true})
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeJira) writer() JiraTicketWriter {
	return JiraTicketWriter{BaseURL: f.srv.URL, Email: "bot@example.com", APIToken: "jira-secret-token"}
}

// fakeBurnliteWrites is a connected Burnlite's create_task, create_note and
// list_tasks.
type fakeBurnliteWrites struct {
	mu      sync.Mutex
	srv     *httptest.Server
	state   string
	calls   []string
	args    []map[string]any
	refuse  string // a tool error code for create_task and create_note
	listing []map[string]any
	// created is when get_task says each Task was created; a Task not
	// here has no createdAt.
	created map[string]string
}

func newFakeBurnliteWrites(t *testing.T) *fakeBurnliteWrites {
	t.Helper()
	f := &fakeBurnliteWrites{state: store.IntegrationConnected}
	srv := mcp.NewServer(&mcp.Implementation{Name: "burnlite", Version: "test"}, nil)
	result := func(v any) *mcp.CallToolResult {
		b, _ := json.Marshal(v)
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		return &mcp.CallToolResult{StructuredContent: out, Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}
	}
	for _, name := range []string{"create_task", "create_note", "list_tasks", "get_task"} {
		srv.AddTool(&mcp.Tool{Name: name, InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args map[string]any
			_ = json.Unmarshal(req.Params.Arguments, &args)
			f.mu.Lock()
			defer f.mu.Unlock()
			f.calls = append(f.calls, name)
			f.args = append(f.args, args)
			if f.refuse != "" && name != "list_tasks" {
				b, _ := json.Marshal(map[string]string{"code": f.refuse, "message": "refused: " + f.refuse})
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil
			}
			switch name {
			case "create_task":
				return result(map[string]any{"task": map[string]any{"id": "uuid-9", "key": "LEG-9", "url": "/LEG-9"}}), nil
			case "create_note":
				return result(map[string]any{"note": map[string]any{"id": "note-1"}}), nil
			case "get_task":
				key := fmt.Sprint(args["taskId"])
				task := map[string]any{"id": "uuid-" + key, "key": key, "url": "/" + key}
				if at, ok := f.created[key]; ok {
					task["createdAt"] = at
				}
				return result(map[string]any{"task": task}), nil
			}
			return result(map[string]any{"tasks": f.listing}), nil
		})
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	f.srv = httptest.NewServer(handler)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeBurnliteWrites) Current(context.Context) (BurnliteLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return BurnliteLink{State: f.state, URL: f.srv.URL}, nil
}
func (f *fakeBurnliteWrites) Tokens() burnlite.TokenSource { return staticBurnliteToken{} }
func (f *fakeBurnliteWrites) Client(context.Context) (*http.Client, error) {
	return f.srv.Client(), nil
}

func (f *fakeBurnliteWrites) writer() BurnliteTicketWriter {
	return BurnliteTicketWriter{Connection: f}
}

func (f *fakeBurnliteWrites) called(name string) (n int, last map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range f.calls {
		if c == name {
			n++
			last = f.args[i]
		}
	}
	return n, last
}

type staticBurnliteToken struct{}

func (staticBurnliteToken) Token(context.Context, bool) (string, error) { return "burnlite-token", nil }

// ticketRun is a store with run wr-t, whose steps raise and comment on
// tickets.
func ticketRun(t *testing.T) (*store.Store, context.Context) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	run := store.WorkflowRun{ID: "wr-t", WorkflowName: "slack-bug", WorkflowTitle: "Slack bug", DefinitionSource: "test", DefinitionDigest: "sha",
		DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "running", CurrentStepID: "raise"}
	steps := []store.WorkflowStep{{RunID: run.ID, StepID: "raise", StepOrder: 0, Title: "Raise", ExecutorType: TicketCreateStepType, DefinitionJSON: `{}`, Status: "pending"}}
	if err := db.CreateWorkflowRun(ctx, run, steps); err != nil {
		t.Fatal(err)
	}
	return db, ctx
}

func ticketStep(attempt string, with map[string]any) ResolvedWorkflowStep {
	return ResolvedWorkflowStep{RunID: "wr-t", AttemptID: attempt, StepID: "raise", With: with}
}

func jiraBug() map[string]any {
	return map[string]any{"provider": "jira", "project": "AG", "summary": "Login fails in prod",
		"description": "Seen today:\n\n- step one\n\n```sh\ncurl -s /login\n```", "labels": []any{"prod"}, "priority": "High",
		"fields": map[string]any{"customfield_10020": 7}}
}

func TestTicketCreateRaisesAJiraIssue(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	result, err := NewTicketCreateExecutor(db, jira.writer()).Start(ctx, ticketStep("wa-1", jiraBug()))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"key": "AG-12", "id": "10001", "url": jira.srv.URL + "/browse/AG-12", "provider": "jira", "created": true}
	if result.Status != "succeeded" || fmt.Sprint(result.Outputs) != fmt.Sprint(want) {
		t.Fatalf("result = %+v", result)
	}
	fields := jira.creates[0]["fields"].(map[string]any)
	if fields["issuetype"].(map[string]any)["name"] != "Bug" || fields["summary"] != "Login fails in prod" || fields["customfield_10020"] != float64(7) {
		t.Fatalf("fields = %v", fields)
	}
	desc := fields["description"].(map[string]any)
	if desc["type"] != "doc" || !strings.Contains(fmt.Sprint(desc), "codeBlock") || !strings.Contains(fmt.Sprint(desc), "bulletList") {
		t.Fatalf("description = %v", desc)
	}
}

// A retry is a new attempt of the same step: it returns the first ticket
// and creates none.
func TestARetriedTicketCreateReturnsTheFirstKey(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	step := NewTicketCreateExecutor(db, jira.writer())
	first, err := step.Start(ctx, ticketStep("wa-1", jiraBug()))
	if err != nil {
		t.Fatal(err)
	}
	again, err := step.Start(ctx, ticketStep("wa-2", jiraBug()))
	if err != nil {
		t.Fatal(err)
	}
	if len(jira.creates) != 1 {
		t.Fatalf("Jira got %d creates; want 1", len(jira.creates))
	}
	if again.Outputs["key"] != first.Outputs["key"] || again.Outputs["url"] != first.Outputs["url"] || again.Outputs["created"] != false {
		t.Fatalf("first %v, again %v", first.Outputs, again.Outputs)
	}
}

// Jira may have created the issue when the answer is lost. The retry looks
// for it before creating another.
func TestATicketCreateWhoseAnswerWasLostIsFoundOnRetry(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	jira.drop = true
	step := NewTicketCreateExecutor(db, jira.writer())
	if _, err := step.Start(ctx, ticketStep("wa-1", jiraBug())); err == nil || !strings.Contains(err.Error(), "may have been created") {
		t.Fatalf("err = %v", err)
	}
	jira.mu.Lock()
	jira.drop = false
	jira.found = []map[string]any{
		{"id": "10001", "key": "AG-12", "fields": map[string]any{"summary": "Login fails in prod"}},
		{"id": "10002", "key": "AG-13", "fields": map[string]any{"summary": "Login fails in prod (again)"}},
	}
	jira.mu.Unlock()
	result, err := step.Start(ctx, ticketStep("wa-2", jiraBug()))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outputs["key"] != "AG-12" || result.Outputs["id"] != "10001" || result.Outputs["created"] != false || len(jira.creates) != 1 {
		t.Fatalf("result %v, creates %d", result.Outputs, len(jira.creates))
	}
	if q := jira.jql[0]; !strings.Contains(q, `project = "AG"`) || !strings.Contains(q, "creator = currentUser()") {
		t.Fatalf("jql = %q", q)
	}
}

func TestATicketCreateWhoseAnswerWasLostIsCreatedOnRetryWhenNotFound(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	jira.drop = true
	step := NewTicketCreateExecutor(db, jira.writer())
	_, _ = step.Start(ctx, ticketStep("wa-1", jiraBug()))
	jira.mu.Lock()
	jira.drop = false
	jira.mu.Unlock()
	result, err := step.Start(ctx, ticketStep("wa-2", jiraBug()))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outputs["key"] != "AG-13" || result.Outputs["created"] != true || len(jira.creates) != 2 {
		t.Fatalf("result %v, creates %d", result.Outputs, len(jira.creates))
	}
}

func TestATicketCreateFindingSeveralCandidatesNamesThem(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	jira.drop = true
	step := NewTicketCreateExecutor(db, jira.writer())
	_, _ = step.Start(ctx, ticketStep("wa-1", jiraBug()))
	jira.mu.Lock()
	jira.drop = false
	jira.found = []map[string]any{
		{"id": "1", "key": "AG-12", "fields": map[string]any{"summary": "Login fails in prod"}},
		{"id": "2", "key": "AG-14", "fields": map[string]any{"summary": "Login fails in prod"}},
	}
	jira.mu.Unlock()
	_, err := step.Start(ctx, ticketStep("wa-2", jiraBug()))
	if err == nil || !strings.Contains(err.Error(), "AG-12") || !strings.Contains(err.Error(), "AG-14") || len(jira.creates) != 1 {
		t.Fatalf("err = %v, creates %d", err, len(jira.creates))
	}
}

// Jira's refusal made nothing: the step fails with what Jira said, and a
// retry creates.
func TestAJiraRefusalFailsTheStepWithTheFieldAndARetryCreates(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	jira.refuse = `{"errorMessages":[],"errors":{"issuetype":"Specify a valid issue type"}}`
	step := NewTicketCreateExecutor(db, jira.writer())
	_, err := step.Start(ctx, ticketStep("wa-1", jiraBug()))
	if err == nil || !strings.Contains(err.Error(), "issuetype: Specify a valid issue type") || !strings.Contains(err.Error(), "AG") {
		t.Fatalf("err = %v", err)
	}
	jira.mu.Lock()
	jira.refuse = ""
	jira.mu.Unlock()
	result, err := step.Start(ctx, ticketStep("wa-2", jiraBug()))
	if err != nil || result.Outputs["created"] != true || len(jira.jql) != 0 {
		t.Fatalf("retry: %v, %v, searched %v", result.Outputs, err, jira.jql)
	}
}

func TestTicketStepsSayWhichSettingsAreMissing(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := JiraTicketWriter{BaseURL: "https://acme.atlassian.net", Email: "bot@example.com", APIToken: ""}
	disconnected := newFakeBurnliteWrites(t)
	disconnected.state = store.IntegrationDisconnected
	step := NewTicketCreateExecutor(db, jira, disconnected.writer())

	_, err := step.Start(ctx, ticketStep("wa-1", jiraBug()))
	if err == nil || !strings.Contains(err.Error(), "jira.api_token") || strings.Contains(err.Error(), "jira.base_url") {
		t.Fatalf("err = %v", err)
	}
	with := map[string]any{"provider": "burnlite", "project": "LEG", "summary": "S", "size": "Small", "uncertainty": "Low"}
	if _, err := step.Start(ctx, ticketStep("wa-1", with)); err == nil || !strings.Contains(err.Error(), "not connected to Burnlite") {
		t.Fatalf("err = %v", err)
	}
	comment := NewTicketCommentExecutor(db, jira, disconnected.writer())
	if _, err := comment.Start(ctx, ticketStep("wa-1", map[string]any{"provider": "jira", "key": "AG-1", "body": "x"})); err == nil || !strings.Contains(err.Error(), "jira.api_token") {
		t.Fatalf("err = %v", err)
	}
}

func TestTicketStepsChooseTheOnlyProviderSetUp(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	unsetJira := JiraTicketWriter{}
	connected := newFakeBurnliteWrites(t)
	disconnected := newFakeBurnliteWrites(t)
	disconnected.state = "none"
	bare := map[string]any{"key": "AG-1", "body": "Seen again"}

	result, err := NewTicketCommentExecutor(db, jira.writer(), disconnected.writer()).Start(ctx, ticketStep("wa-1", bare))
	if err != nil || len(jira.comments) != 1 || !strings.HasPrefix(result.Outputs["url"].(string), jira.srv.URL) {
		t.Fatalf("only Jira: %v, %v", result.Outputs, err)
	}
	db2, _ := ticketRun(t)
	result, err = NewTicketCommentExecutor(db2, unsetJira, connected.writer()).Start(ctx, ticketStep("wa-1", map[string]any{"key": "LEG-1", "body": "Seen again"}))
	if n, _ := connected.called("create_note"); err != nil || n != 1 {
		t.Fatalf("only Burnlite: %v, %v", result.Outputs, err)
	}
	db3, _ := ticketRun(t)
	if _, err := NewTicketCommentExecutor(db3, jira.writer(), connected.writer()).Start(ctx, ticketStep("wa-1", bare)); err == nil || !strings.Contains(err.Error(), "set provider to jira or burnlite") {
		t.Fatalf("both: %v", err)
	}
	if _, err := NewTicketCommentExecutor(db3, unsetJira, disconnected.writer()).Start(ctx, ticketStep("wa-1", bare)); err == nil || !strings.Contains(err.Error(), "no ticket provider is set up") {
		t.Fatalf("neither: %v", err)
	}
	if _, err := NewTicketCommentExecutor(db3, jira.writer()).Start(ctx, ticketStep("wa-1", map[string]any{"provider": "github", "key": "AG-1", "body": "x"})); err == nil || !strings.Contains(err.Error(), "jira or burnlite") {
		t.Fatalf("unknown provider: %v", err)
	}
}

func TestTicketCreateFilesABurnliteTask(t *testing.T) {
	db, ctx := ticketRun(t)
	bl := newFakeBurnliteWrites(t)
	with := map[string]any{"provider": "burnlite", "project": "LEG", "summary": "Fix login", "description": "It **fails**",
		"labels": []any{"bug"}, "size": "Medium", "uncertainty": "Low", "milestone": "LEG-3"}
	result, err := NewTicketCreateExecutor(db, bl.writer()).Start(ctx, ticketStep("wa-1", with))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outputs["key"] != "LEG-9" || result.Outputs["id"] != "uuid-9" || result.Outputs["url"] != bl.srv.URL+"/LEG-9" || result.Outputs["provider"] != "burnlite" {
		t.Fatalf("outputs = %v", result.Outputs)
	}
	_, args := bl.called("create_task")
	want := map[string]any{"projectId": "LEG", "taskName": "Fix login", "description": "It **fails**", "labels": []any{"bug"},
		"kind": "work", "size": "Medium", "uncertaintyLevel": "Low", "milestoneId": "LEG-3", "holder": "legato"}
	if fmt.Sprint(args) != fmt.Sprint(want) {
		t.Fatalf("args = %v", args)
	}
}

func TestTicketCreateRefusesWhatTheProviderCannotTake(t *testing.T) {
	db, ctx := ticketRun(t)
	bl := newFakeBurnliteWrites(t)
	jira := newFakeJira(t)
	step := NewTicketCreateExecutor(db, jira.writer(), bl.writer())
	base := func(extra map[string]any) map[string]any {
		with := map[string]any{"provider": "burnlite", "project": "LEG", "summary": "S", "size": "Small", "uncertainty": "Low"}
		for k, v := range extra {
			with[k] = v
		}
		return with
	}
	for _, tc := range []struct {
		name string
		with map[string]any
		want string
	}{
		{"a Work task with no size", map[string]any{"provider": "burnlite", "project": "LEG", "summary": "S", "uncertainty": "Low"}, "size"},
		{"a Meta task with a size", base(map[string]any{"kind": "meta"}), "size"},
		{"a size Burnlite lacks", base(map[string]any{"size": "Huge"}), "Huge"},
		{"priority on Burnlite", base(map[string]any{"priority": "High"}), "priority"},
		{"type on Burnlite", base(map[string]any{"type": "Bug"}), "type"},
		{"fields on Burnlite", base(map[string]any{"fields": map[string]any{"x": 1}}), "fields"},
		{"a field Legato sets", map[string]any{"provider": "jira", "project": "AG", "summary": "S", "fields": map[string]any{"summary": "other"}}, "summary"},
		{"a label with a space", map[string]any{"provider": "jira", "project": "AG", "summary": "S", "labels": []any{"two words"}}, "two words"},
		{"size on Jira", map[string]any{"provider": "jira", "project": "AG", "summary": "S", "size": "Small"}, "size"},
		{"no summary", map[string]any{"provider": "jira", "project": "AG"}, "summary"},
		{"no project", map[string]any{"provider": "jira", "summary": "S"}, "project"},
		{"a summary too long", map[string]any{"provider": "jira", "project": "AG", "summary": strings.Repeat("x", 256)}, "255"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := step.Start(ctx, ticketStep("wa-1", tc.with))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v; want it to name %q", err, tc.want)
			}
		})
	}
	if n, _ := bl.called("create_task"); n != 0 || len(jira.creates) != 0 {
		t.Fatalf("created %d Burnlite and %d Jira tickets", n, len(jira.creates))
	}
}

func TestTicketCreateSaysToReconnectAReadOnlyBurnlite(t *testing.T) {
	db, ctx := ticketRun(t)
	bl := newFakeBurnliteWrites(t)
	bl.refuse = "insufficient_scope"
	_, err := NewTicketCreateExecutor(db, bl.writer()).Start(ctx, ticketStep("wa-1", map[string]any{"project": "LEG", "summary": "S", "kind": "meta"}))
	if err == nil || !strings.Contains(err.Error(), "connect Legato to Burnlite again") || !errors.Is(err, burnlite.ErrInsufficientScope) {
		t.Fatalf("err = %v", err)
	}
}

// A lost Burnlite answer is found by its name among the project's Tasks
// created since the attempt began. list_tasks lists those updated since,
// so a Task of that name made long ago and changed since is not it.
func TestABurnliteCreateWhoseAnswerWasLostIsFoundOnRetry(t *testing.T) {
	db, ctx := ticketRun(t)
	bl := newFakeBurnliteWrites(t)
	with := map[string]any{"project": "LEG", "summary": "Fix login", "kind": "meta"}
	step := NewTicketCreateExecutor(db, bl.writer())
	// An earlier attempt began and never learned the outcome.
	if _, _, err := db.BeginWorkflowStepEffect(ctx, store.WorkflowStepEffect{RunID: "wr-t", Path: "raise", Effect: TicketCreateStepType,
		AttemptID: "wa-1", Provider: BurnliteIntegration, Target: "LEG", Summary: "Fix login"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	bl.listing = []map[string]any{
		{"id": "uuid-LEG-6", "key": "LEG-6", "url": "/LEG-6", "taskName": "Fix login", "state": "open"},
		{"id": "uuid-LEG-7", "key": "LEG-7", "url": "/LEG-7", "taskName": "Fix login", "state": "open"},
		{"id": "uuid-LEG-8", "key": "LEG-8", "url": "/LEG-8", "taskName": "Fix logout", "state": "open"},
	}
	bl.created = map[string]string{"LEG-6": "2026-01-02T03:04:05.000Z", "LEG-7": now, "LEG-8": now}
	result, err := step.Start(ctx, ticketStep("wa-2", with))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outputs["key"] != "LEG-7" || result.Outputs["id"] != "uuid-LEG-7" || result.Outputs["url"] != bl.srv.URL+"/LEG-7" || result.Outputs["created"] != false {
		t.Fatalf("outputs = %v", result.Outputs)
	}
	if n, _ := bl.called("create_task"); n != 0 {
		t.Fatalf("created %d", n)
	}
}

func TestABurnliteCreateWhoseAnswerWasLostCreatesWhenOnlyAnOldTaskHasTheName(t *testing.T) {
	db, ctx := ticketRun(t)
	bl := newFakeBurnliteWrites(t)
	if _, _, err := db.BeginWorkflowStepEffect(ctx, store.WorkflowStepEffect{RunID: "wr-t", Path: "raise", Effect: TicketCreateStepType,
		AttemptID: "wa-1", Provider: BurnliteIntegration, Target: "LEG", Summary: "Fix login"}); err != nil {
		t.Fatal(err)
	}
	bl.listing = []map[string]any{{"id": "uuid-LEG-6", "key": "LEG-6", "url": "/LEG-6", "taskName": "Fix login", "state": "open"}}
	bl.created = map[string]string{"LEG-6": "2026-01-02T03:04:05.000Z"}
	result, err := NewTicketCreateExecutor(db, bl.writer()).Start(ctx, ticketStep("wa-2", map[string]any{"project": "LEG", "summary": "Fix login", "kind": "meta"}))
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := bl.called("create_task"); n != 1 || result.Outputs["key"] != "LEG-9" || result.Outputs["created"] != true {
		t.Fatalf("outputs %v, creates %d", result.Outputs, n)
	}
}

// A Burnlite too old to say when a Task was created cannot tell a new one
// from an old one: the step stops rather than guess.
func TestABurnliteCreateWhoseAnswerWasLostStopsWhenBurnliteCannotSayWhenATaskWasMade(t *testing.T) {
	db, ctx := ticketRun(t)
	bl := newFakeBurnliteWrites(t)
	if _, _, err := db.BeginWorkflowStepEffect(ctx, store.WorkflowStepEffect{RunID: "wr-t", Path: "raise", Effect: TicketCreateStepType,
		AttemptID: "wa-1", Provider: BurnliteIntegration, Target: "LEG", Summary: "Fix login"}); err != nil {
		t.Fatal(err)
	}
	bl.listing = []map[string]any{{"id": "uuid-LEG-6", "key": "LEG-6", "url": "/LEG-6", "taskName": "Fix login", "state": "open"}}
	_, err := NewTicketCreateExecutor(db, bl.writer()).Start(ctx, ticketStep("wa-2", map[string]any{"project": "LEG", "summary": "Fix login", "kind": "meta"}))
	if err == nil || !strings.Contains(err.Error(), "LEG-6") {
		t.Fatalf("err = %v", err)
	}
	if n, _ := bl.called("create_task"); n != 0 {
		t.Fatalf("created %d", n)
	}
}

func TestTicketCommentAddsOnceAcrossRetries(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	step := NewTicketCommentExecutor(db, jira.writer())
	with := map[string]any{"key": " ag-12 ", "body": "Seen again in **prod** today"}
	first, err := step.Start(ctx, ticketStep("wa-1", with))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"url": jira.srv.URL + "/browse/AG-12?focusedCommentId=20001", "id": "20001", "key": "AG-12"}
	if fmt.Sprint(first.Outputs) != fmt.Sprint(want) {
		t.Fatalf("outputs = %v", first.Outputs)
	}
	again, err := step.Start(ctx, ticketStep("wa-2", with))
	if err != nil || fmt.Sprint(again.Outputs) != fmt.Sprint(want) || len(jira.comments) != 1 {
		t.Fatalf("again %v, %v, comments %v", again.Outputs, err, jira.comments)
	}
	if jira.comments[0] != "/rest/api/3/issue/AG-12/comment" {
		t.Fatalf("comment path = %q", jira.comments[0])
	}
}

func TestTicketCommentAddsABurnliteNote(t *testing.T) {
	db, ctx := ticketRun(t)
	bl := newFakeBurnliteWrites(t)
	result, err := NewTicketCommentExecutor(db, bl.writer()).Start(ctx, ticketStep("wa-1", map[string]any{"key": "LEG-9", "body": "Seen again"}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outputs["url"] != bl.srv.URL+"/LEG-9" || result.Outputs["id"] != "note-1" || result.Outputs["key"] != "LEG-9" {
		t.Fatalf("outputs = %v", result.Outputs)
	}
	if _, args := bl.called("create_note"); args["taskId"] != "LEG-9" || args["content"] != "Seen again" {
		t.Fatalf("args = %v", args)
	}
}

// A comment whose outcome was lost is not searched for: the step says to
// check, and the retry after that adds it.
func TestATicketCommentWhoseAnswerWasLostAsksToCheck(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	if _, _, err := db.BeginWorkflowStepEffect(ctx, store.WorkflowStepEffect{RunID: "wr-t", Path: "raise", Effect: TicketCommentStepType,
		AttemptID: "wa-1", Provider: "jira", Target: "AG-12"}); err != nil {
		t.Fatal(err)
	}
	step := NewTicketCommentExecutor(db, jira.writer())
	with := map[string]any{"key": "AG-12", "body": "x"}
	if _, err := step.Start(ctx, ticketStep("wa-2", with)); err == nil || !strings.Contains(err.Error(), "check AG-12") || len(jira.comments) != 0 {
		t.Fatalf("err = %v, comments %v", err, jira.comments)
	}
	if _, err := step.Start(ctx, ticketStep("wa-3", with)); err != nil || len(jira.comments) != 1 {
		t.Fatalf("err = %v, comments %v", err, jira.comments)
	}
}
