package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// A task in brief: what an agent or the Reeve checks often, without the
// description it already knows.
func TestATaskInBriefLeavesOutTheDescription(t *testing.T) {
	long := strings.Repeat("a long description. ", 2000)
	svc := &mockBoardService{
		columns: []service.Column{},
		cards:   map[string][]service.Card{},
		cardDetail: &service.CardDetail{
			ID: "task-1", Title: "Docker in the sandbox", Status: "Review",
			DescriptionMD: long, LocalDescriptionMD: long,
			UpdatedAt: time.Date(2026, 11, 12, 4, 0, 0, 0, time.UTC),
			Notes: []store.TaskNote{
				{ID: "tn-1", Body: "started", Source: "agent", CreatedAt: "2026-11-12 03:00:00"},
				{ID: "tn-2", Body: "PR opened and green", Source: "agent", CreatedAt: "2026-11-12 04:00:00"},
			},
			PullRequests: []service.PRMetaView{{Repo: "cpave3/legato", Branch: "legato/x", PRNumber: 60, State: "OPEN", CheckStatus: "pass"}},
		},
	}
	srv := New(svc, nil, nil, ":0")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/tasks/task-1?view=summary", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d %s", w.Code, w.Body)
	}
	if w.Body.Len() > 2000 || strings.Contains(w.Body.String(), "a long description") {
		t.Fatalf("the brief holds the description (%d bytes)", w.Body.Len())
	}
	var got TaskSummaryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "task-1" || got.Title != "Docker in the sandbox" || got.Column != "Review" || got.Notes != 2 {
		t.Fatalf("brief = %+v", got)
	}
	if got.LatestNote == nil || got.LatestNote.Body != "PR opened and green" {
		t.Fatalf("latest note = %+v", got.LatestNote)
	}
	if len(got.PullRequests) != 1 || got.PullRequests[0].PRNumber != 60 || got.PullRequests[0].State != "OPEN" || got.PullRequests[0].CheckStatus != "pass" {
		t.Fatalf("pull requests = %+v", got.PullRequests)
	}
}

// manyAgents lists several agents.
type manyAgents struct {
	promptAgentService
	list []service.AgentSession
}

func (m *manyAgents) ListAgents(context.Context) ([]service.AgentSession, error) { return m.list, nil }

func agentsServer() *Server {
	ws := 2
	board := &mockBoardService{
		workspaces: []service.Workspace{{ID: 2, Name: "Legato"}},
		cardDetail: &service.CardDetail{ID: "T-1", Title: "one", WorkspaceID: &ws},
	}
	started := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	agents := &manyAgents{list: []service.AgentSession{
		{TaskID: "T-1", Title: "one", Status: "running", Activity: "working", Sandboxed: true, StartedAt: started, Command: "claude", TmuxSession: "legato-T-1"},
		{TaskID: "T-2", Title: "two", Status: "running", StartedAt: started},
		{TaskID: "T-3", Title: "three", Status: "dead", StartedAt: started},
	}}
	return New(board, agents, nil, ":0")
}

func getAgents(t *testing.T, srv *Server, query string) []map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/agents"+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/agents%s = %d %s", query, w.Code, w.Body)
	}
	var out []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func taskIDs(agents []map[string]any) string {
	var ids []string
	for _, a := range agents {
		ids = append(ids, a["task_id"].(string))
	}
	return strings.Join(ids, ",")
}

func TestTheAgentsCanBeFilteredByTaskAndWorkspace(t *testing.T) {
	srv := agentsServer()
	if got := taskIDs(getAgents(t, srv, "")); got != "T-1,T-2,T-3" {
		t.Fatalf("all = %s", got)
	}
	if got := taskIDs(getAgents(t, srv, "?task=T-3,T-1")); got != "T-1,T-3" {
		t.Fatalf("by task = %s", got)
	}
	if got := taskIDs(getAgents(t, srv, "?status=running")); got != "T-1,T-2" {
		t.Fatalf("running = %s", got)
	}
	for _, ws := range []string{"2", "legato"} {
		if got := taskIDs(getAgents(t, srv, "?workspace="+ws)); got != "T-1" {
			t.Fatalf("workspace %s = %s", ws, got)
		}
	}
}

// The compact form has what a status check needs and nothing more.
func TestTheAgentsInCompactForm(t *testing.T) {
	got := getAgents(t, agentsServer(), "?view=compact&task=T-1")
	if len(got) != 1 {
		t.Fatalf("agents = %v", got)
	}
	want := map[string]any{"task_id": "T-1", "title": "one", "status": "running", "activity": "working", "sandboxed": true, "started_at": "2026-11-12T03:00:00Z"}
	for k, v := range want {
		if got[0][k] != v {
			t.Errorf("%s = %v, want %v", k, got[0][k], v)
		}
	}
	for k := range got[0] {
		if _, ok := want[k]; !ok && k != "slot" && k != "card_task_id" && k != "reeve" && k != "background_jobs" {
			t.Errorf("the compact form has %s", k)
		}
	}
}

// An agent's group stays in the compact form, so the Reeve can tell the
// operator's groups apart (LEG-134).
func TestTheCompactFormKeepsTheGroup(t *testing.T) {
	srv := agentsServer()
	srv.agents.(*manyAgents).list[0].Group = "Reviews"
	got := getAgents(t, srv, "?view=compact")
	if got[0]["group"] != "Reviews" {
		t.Fatalf("group = %v, want Reviews", got[0]["group"])
	}
	if _, ok := got[1]["group"]; ok {
		t.Fatalf("an agent in no group has a group: %v", got[1])
	}
}

// What an agent in the background still runs, so the Reeve can tell a
// build it waits on from an agent that stalled (LEG-144).
func TestTheAgentsSayWhatStillRuns(t *testing.T) {
	srv := agentsServer()
	started := time.Date(2026, 11, 12, 3, 5, 0, 0, time.UTC)
	agent := &srv.agents.(*manyAgents).list[1]
	agent.Activity = store.ActivityBackground
	agent.BackgroundJobs = []store.BackgroundJob{
		{Kind: store.JobBash, ID: "b1", Label: "npm run build", StartedAt: started},
		{Kind: store.JobSubagent, ID: "a1", Label: "Explore: find the callers", StartedAt: started},
	}

	for _, query := range []string{"?task=T-2", "?view=compact&task=T-2"} {
		got := getAgents(t, srv, query)
		jobs, _ := got[0]["background_jobs"].([]any)
		if len(jobs) != 2 {
			t.Fatalf("%s: background_jobs = %v", query, got[0]["background_jobs"])
		}
		first := jobs[0].(map[string]any)
		if first["kind"] != "bash" || first["label"] != "npm run build" || first["started_at"] != "2026-11-12T03:05:00Z" {
			t.Fatalf("%s: job = %v", query, first)
		}
	}
	if _, ok := getAgents(t, srv, "?view=compact&task=T-1")[0]["background_jobs"]; ok {
		t.Fatal("an agent with nothing running lists background jobs")
	}
}
