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

	// issues are what GET /issue/{key} answers, and what assign and
	// transitions change (LEG-656); a key not here is a 404.
	issues map[string]*fakeIssue
	// users are what user search finds, by a part of the email.
	users []fakeUser
	// requests are every request but create, comment and search, as
	// "METHOD path".
	requests []string
	// broken answers every issue read with HTTP 500.
	broken bool
}

type fakeIssue struct {
	status   string
	assignee *fakeUser
}

type fakeUser struct{ id, name, email string }

var fakeJiraMe = fakeUser{"acc-me", "Cameron", "bot@example.com"}

// fakeJiraTransitions is the workflow: the transitions from each status.
var fakeJiraTransitions = map[string][][3]string{
	"To Do":       {{"11", "Start work", "In Progress"}, {"31", "Close", "Done"}},
	"In Progress": {{"41", "Stop work", "To Do"}, {"31", "Close", "Done"}},
	"Done":        {{"51", "Reopen", "To Do"}},
}

func (f *fakeJira) issue(key, status string, assignee *fakeUser) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.issues == nil {
		f.issues = map[string]*fakeIssue{}
	}
	f.issues[key] = &fakeIssue{status: status, assignee: assignee}
}

// writes are the requests that change an issue.
func (f *fakeJira) writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.requests {
		if !strings.HasPrefix(r, "GET ") {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeJira) count(request string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if r == request {
			n++
		}
	}
	return n
}

func (f *fakeJira) serveIssues(w http.ResponseWriter, r *http.Request, raw []byte) bool {
	user := func(u *fakeUser) any {
		if u == nil {
			return nil
		}
		return map[string]any{"accountId": u.id, "displayName": u.name, "emailAddress": u.email}
	}
	if strings.HasSuffix(r.URL.Path, "/comment") {
		return false
	}
	if r.URL.Path == "/rest/api/3/myself" || r.URL.Path == "/rest/api/3/user/search" || strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/") {
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/myself":
		body, _ := json.Marshal(user(&fakeJiraMe))
		_, _ = w.Write(body)
		return true
	case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/user/search":
		found := []any{}
		for _, u := range f.users {
			if strings.Contains(strings.ToLower(u.email), strings.ToLower(r.URL.Query().Get("query"))) {
				found = append(found, user(&u))
			}
		}
		body, _ := json.Marshal(found)
		_, _ = w.Write(body)
		return true
	case strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/"):
		rest := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
		key, action, _ := strings.Cut(rest, "/")
		if f.broken {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"errorMessages":["Internal server error"]}`))
			return true
		}
		issue, ok := f.issues[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errorMessages":["Issue does not exist or you do not have permission to see it."],"errors":{}}`))
			return true
		}
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		switch {
		case r.Method == http.MethodGet && action == "":
			out, _ := json.Marshal(map[string]any{"id": "1", "key": key, "fields": map[string]any{
				"summary": "S", "status": map[string]any{"name": issue.status}, "assignee": user(issue.assignee)}})
			_, _ = w.Write(out)
		case r.Method == http.MethodGet && action == "transitions":
			var list []any
			for _, t := range fakeJiraTransitions[issue.status] {
				list = append(list, map[string]any{"id": t[0], "name": t[1], "to": map[string]any{"name": t[2]}})
			}
			out, _ := json.Marshal(map[string]any{"transitions": list})
			_, _ = w.Write(out)
		case r.Method == http.MethodPost && action == "transitions":
			id := fmt.Sprint(body["transition"].(map[string]any)["id"])
			for _, t := range fakeJiraTransitions[issue.status] {
				if t[0] == id {
					issue.status = t[2]
					w.WriteHeader(http.StatusNoContent)
					return true
				}
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errorMessages":["Transition id '` + id + `' is not valid for this issue."]}`))
		case r.Method == http.MethodPut && action == "assignee":
			id, _ := body["accountId"].(string)
			switch id {
			case "":
				issue.assignee = nil
			case fakeJiraMe.id:
				issue.assignee = &fakeJiraMe
			default:
				issue.assignee = &fakeUser{id: id, name: "User " + id}
				for _, u := range f.users {
					if u.id == id {
						issue.assignee = &u
					}
				}
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			return false
		}
		return true
	}
	return false
}

func newFakeJira(t *testing.T) *fakeJira {
	t.Helper()
	f := &fakeJira{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if f.serveIssues(w, r, raw) {
			return
		}
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
	// tasks are more of what get_task answers for a key (state, stage,
	// closeReason, claim), which claim, release, stage and close change.
	tasks map[string]map[string]any
	// refuseTool answers one tool with this error code.
	refuseTool map[string]string
}

func (f *fakeBurnliteWrites) task(key string, fields map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tasks == nil {
		f.tasks = map[string]map[string]any{}
	}
	f.tasks[key] = fields
}

// writes are the calls that change a Task.
func (f *fakeBurnliteWrites) writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if c != "get_task" && c != "list_tasks" {
			out = append(out, c)
		}
	}
	return out
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
	for _, name := range []string{"create_task", "create_note", "list_tasks", "get_task", "claim_task", "release_claim", "update_task", "close_task"} {
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
			if code := f.refuseTool[name]; code != "" {
				b, _ := json.Marshal(map[string]string{"code": code, "message": name + " refused: " + code})
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil
			}
			key := fmt.Sprint(args["taskId"])
			if _, known := f.tasks[key]; f.tasks != nil && !known && name != "create_task" && name != "create_note" && name != "list_tasks" {
				b, _ := json.Marshal(map[string]string{"code": "not_found", "message": "No Task " + key})
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil
			}
			switch name {
			case "claim_task":
				f.tasks[key]["claim"] = map[string]any{"holder": args["holder"]}
				return result(map[string]any{"claim": map[string]any{"holder": args["holder"]}}), nil
			case "release_claim":
				f.tasks[key]["claim"] = nil
				return result(map[string]any{"released": true}), nil
			case "update_task":
				f.tasks[key]["stage"] = args["stage"]
				return result(map[string]any{"task": map[string]any{"key": key}}), nil
			case "close_task":
				stage := "done"
				if args["reason"] == "not_planned" {
					stage = "dropped"
				}
				f.tasks[key]["state"], f.tasks[key]["stage"], f.tasks[key]["closeReason"] = "closed", stage, args["reason"]
				return result(map[string]any{"task": map[string]any{"key": key}}), nil
			}
			switch name {
			case "create_task":
				return result(map[string]any{"task": map[string]any{"id": "uuid-9", "key": "LEG-9", "url": "/LEG-9"}}), nil
			case "create_note":
				return result(map[string]any{"note": map[string]any{"id": "note-1"}}), nil
			case "get_task":
				task := map[string]any{"id": "uuid-" + key, "key": key, "url": "/" + key}
				if at, ok := f.created[key]; ok {
					task["createdAt"] = at
				}
				for k, v := range f.tasks[key] {
					task[k] = v
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
	want := map[string]any{"key": "AG-12", "id": "10001", "url": jira.srv.URL + "/browse/AG-12", "provider": "jira", "created": true, "warnings": []string{}}
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

// ticket.create's assignee (LEG-656) is resolved before the create, so a
// Jira issue is assigned as it is made.
func TestTicketCreateAssignsTheJiraIssueAsItIsMade(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	with := jiraBug()
	with["assignee"] = "me"
	result, err := NewTicketCreateExecutor(db, jira.writer()).Start(ctx, ticketStep("wa-1", with))
	if err != nil {
		t.Fatal(err)
	}
	fields := jira.creates[0]["fields"].(map[string]any)
	if got := fmt.Sprint(fields["assignee"]); got != "map[accountId:acc-me]" {
		t.Fatalf("fields.assignee = %v", got)
	}
	if w, ok := result.Outputs["warnings"].([]string); !ok || len(w) != 0 {
		t.Fatalf("warnings = %#v", result.Outputs["warnings"])
	}
}

func TestTicketCreateTakesFieldsAssigneeOnlyWithoutAssignee(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	step := NewTicketCreateExecutor(db, jira.writer())
	both := map[string]any{"provider": "jira", "project": "AG", "summary": "S", "assignee": "me", "fields": map[string]any{"assignee": map[string]any{"accountId": "x"}}}
	if _, err := step.Start(ctx, ticketStep("wa-1", both)); err == nil || !strings.Contains(err.Error(), "assignee") {
		t.Fatalf("err = %v", err)
	}
	raw := map[string]any{"provider": "jira", "project": "AG", "summary": "S", "fields": map[string]any{"assignee": map[string]any{"accountId": "x"}}}
	if _, err := step.Start(ctx, ticketStep("wa-2", raw)); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(jira.creates[0]["fields"].(map[string]any)["assignee"]); got != "map[accountId:x]" {
		t.Fatalf("fields.assignee = %v", got)
	}
}

// An assignee no one matches fails the step before anything is created.
func TestTicketCreateWithAnAssigneeNoOneMatchesCreatesNothing(t *testing.T) {
	db, ctx := ticketRun(t)
	jira := newFakeJira(t)
	with := jiraBug()
	with["assignee"] = "nobody@example.com"
	if _, err := NewTicketCreateExecutor(db, jira.writer()).Start(ctx, ticketStep("wa-1", with)); err == nil || !strings.Contains(err.Error(), "nobody@example.com") {
		t.Fatalf("err = %v", err)
	}
	record, err := db.GetWorkflowStepEffect(ctx, "wr-t", "raise", TicketCreateStepType)
	if err != nil || record != nil || len(jira.creates) != 0 {
		t.Fatalf("record %+v, %v, creates %d", record, err, len(jira.creates))
	}
	with["assignee"] = "none"
	if _, err := NewTicketCreateExecutor(db, jira.writer()).Start(ctx, ticketStep("wa-2", with)); err == nil || !strings.Contains(err.Error(), "none") {
		t.Fatalf("none: %v", err)
	}
}

// create_task cannot claim, so a Burnlite task is claimed after it is made.
// A claim that fails leaves the ticket made, with a warning; a retry claims
// again.
func TestTicketCreateClaimsTheBurnliteTaskAfterMakingIt(t *testing.T) {
	db, ctx := ticketRun(t)
	bl := newFakeBurnliteWrites(t)
	bl.task("LEG-9", map[string]any{"state": "open"})
	bl.refuseTool = map[string]string{"claim_task": "already_claimed"}
	with := map[string]any{"provider": "burnlite", "project": "LEG", "summary": "Fix login", "kind": "meta", "assignee": "me"}
	step := NewTicketCreateExecutor(db, bl.writer())
	result, err := step.Start(ctx, ticketStep("wa-1", with))
	if err != nil {
		t.Fatal(err)
	}
	if w, _ := result.Outputs["warnings"].([]string); result.Outputs["created"] != true || len(w) != 1 || !strings.Contains(w[0], "LEG-9") {
		t.Fatalf("outputs = %v", result.Outputs)
	}
	bl.mu.Lock()
	bl.refuseTool = nil
	bl.mu.Unlock()
	result, err = step.Start(ctx, ticketStep("wa-2", with))
	if err != nil {
		t.Fatal(err)
	}
	n, args := bl.called("claim_task")
	if w, _ := result.Outputs["warnings"].([]string); n != 2 || args["holder"] != "legato" || args["taskId"] != "LEG-9" || len(w) != 0 || result.Outputs["created"] != false {
		t.Fatalf("claims %d %v, outputs %v", n, args, result.Outputs)
	}
	if n, _ := bl.called("create_task"); n != 1 {
		t.Fatalf("created %d", n)
	}
}
