package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type fakeAgentGraph struct{ graph service.AgentGraph }

func (f fakeAgentGraph) Graph(context.Context) (service.AgentGraph, error) { return f.graph, nil }

func TestTheAgentGraphGivesPlacesAndLinks(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetAgentGraphService(fakeAgentGraph{graph: service.AgentGraph{
		Agents:            map[string]service.AgentGraphPlace{"task-a": {Repo: "acme/widget", Worktree: "/w", Badges: []service.AgentBadge{{Kind: service.AgentBadgeHandoff, Label: "from erebus"}}}},
		Links:             []service.AgentLink{{Kind: service.AgentLinkPeerMessage, From: "task-a", To: "task-b", Count: 2, LastAt: "2026-11-23T09:00:00.000Z"}},
		PeerWindowSeconds: 1800,
	}})

	var body map[string]any
	status := serveJSON(t, srv, http.MethodGet, "/api/agents/graph", "", &body)
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	agents, _ := body["agents"].(map[string]any)
	place, _ := agents["task-a"].(map[string]any)
	if place["repo"] != "acme/widget" || place["worktree"] != "/w" {
		t.Errorf("task-a's place = %v, want repo acme/widget in worktree /w", place)
	}
	badges, _ := place["badges"].([]any)
	if len(badges) != 1 || badges[0].(map[string]any)["label"] != "from erebus" {
		t.Errorf("task-a's badges = %v", badges)
	}
	links, _ := body["links"].([]any)
	if len(links) != 1 {
		t.Fatalf("links = %v, want one", links)
	}
	link := links[0].(map[string]any)
	if link["kind"] != "peer_message" || link["from"] != "task-a" || link["to"] != "task-b" || link["count"] != float64(2) {
		t.Errorf("link = %v", link)
	}
	if body["peer_window_seconds"] != float64(1800) {
		t.Errorf("peer_window_seconds = %v", body["peer_window_seconds"])
	}
}

func TestTheAgentGraphGivesEachAgentsPlaceInItsWorkflowRuns(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetAgentGraphService(fakeAgentGraph{graph: service.AgentGraph{
		Agents: map[string]service.AgentGraphPlace{"LEG-1": {Workflows: []service.AgentWorkflowPlace{{RunID: "wr-1", StepID: "review", Role: service.AgentRoleAuthor}}}},
		Links:  []service.AgentLink{},
		Runs: map[string]service.AgentGraphRun{"wr-1": {ID: "wr-1", WorkflowName: "flow", WorkflowTitle: "LEG-1 one-off", Status: "running",
			CurrentStepID: "review", NeedsAttention: true, AttentionReason: "agent idle before step completion", Pass: 2, CreatedAt: "2026-11-24 04:00:00",
			Steps: []store.WorkflowStepSummary{{StepID: "implement", Title: "Implement", Status: "succeeded"}, {StepID: "review", Title: "Review", Status: "waiting"}}}},
	}})

	var body struct {
		Agents map[string]struct {
			Workflows []map[string]any `json:"workflows"`
		} `json:"agents"`
		Runs map[string]map[string]any `json:"runs"`
	}
	if status := serveJSON(t, srv, http.MethodGet, "/api/agents/graph", "", &body); status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	workflows := body.Agents["LEG-1"].Workflows
	if len(workflows) != 1 || workflows[0]["run_id"] != "wr-1" || workflows[0]["step_id"] != "review" || workflows[0]["role"] != "author" {
		t.Errorf("LEG-1's workflows = %v, want author on wr-1's review", workflows)
	}
	run := body.Runs["wr-1"]
	if run["workflow_title"] != "LEG-1 one-off" || run["status"] != "running" || run["current_step_id"] != "review" ||
		run["needs_attention"] != true || run["pass"] != float64(2) {
		t.Errorf("wr-1 = %v", run)
	}
	steps, _ := run["steps"].([]any)
	if len(steps) != 2 || steps[1].(map[string]any)["step_id"] != "review" || steps[1].(map[string]any)["status"] != "waiting" {
		t.Errorf("wr-1's steps = %v", run["steps"])
	}
}

func TestTheAgentGraphIsEmptyWithoutTheService(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	var body map[string]any
	if status := serveJSON(t, srv, http.MethodGet, "/api/agents/graph", "", &body); status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if links, ok := body["links"].([]any); !ok || len(links) != 0 {
		t.Errorf("links = %v, want an empty list", body["links"])
	}
	if agents, ok := body["agents"].(map[string]any); !ok || len(agents) != 0 {
		t.Errorf("agents = %v, want an empty object", body["agents"])
	}
	if runs, ok := body["runs"].(map[string]any); !ok || len(runs) != 0 {
		t.Errorf("runs = %v, want an empty object", body["runs"])
	}
}

func TestTheAgentGraphIsReadOnly(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/graph", "{}", nil); status != http.StatusMethodNotAllowed {
		t.Fatalf("POST status %d, want 405", status)
	}
}
