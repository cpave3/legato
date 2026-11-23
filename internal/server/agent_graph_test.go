package server

import (
	"context"
	"net/http"
	"testing"

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
}

func TestTheAgentGraphIsReadOnly(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/graph", "{}", nil); status != http.StatusMethodNotAllowed {
		t.Fatalf("POST status %d, want 405", status)
	}
}
