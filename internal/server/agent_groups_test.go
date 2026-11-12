package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// groupsServer is a server over a real agent service with one running agent
// for each task ID.
func groupsServer(t *testing.T, taskIDs ...string) (*Server, *store.Store) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "groups.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	for _, id := range taskIDs {
		if err := db.UpsertTask(ctx, store.Task{ID: id, Title: "Task " + id, Status: "Doing", Priority: "medium"}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: id, AgentKind: "claude-code", TmuxSession: "legato-" + id, Status: "running"}); err != nil {
			t.Fatal(err)
		}
	}
	agents := service.NewAgentService(db, &mockTmuxManager{}, t.TempDir(), service.AgentServiceOptions{DisablePeerAwareness: true})
	srv := New(&mockBoardService{}, agents, nil, ":0")
	srv.notifyTaskChanged = func(string) {}
	return srv, db
}

func groupsRequest(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func listedGroups(t *testing.T, srv *Server) map[string]string {
	t.Helper()
	rec := groupsRequest(t, srv, http.MethodGet, "/api/agents", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/agents = %d: %s", rec.Code, rec.Body.String())
	}
	var agents []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &agents); err != nil {
		t.Fatal(err)
	}
	groups := map[string]string{}
	for _, agent := range agents {
		group, _ := agent["group"].(string)
		groups[agent["task_id"].(string)] = group
	}
	return groups
}

func TestAgentGroupRouteSetsAndClearsTheGroupTheAgentListShows(t *testing.T) {
	srv, _ := groupsServer(t, "T-1", "T-2")
	notified := []string{}
	srv.notifyTaskChanged = func(id string) { notified = append(notified, id) }
	sent := make(chan WSMessage, 8)
	client := &wsClient{sendFn: func(msg WSMessage) { sent <- msg }}
	srv.hub.add(client)
	defer srv.hub.remove(client)

	rec := groupsRequest(t, srv, http.MethodPut, "/api/agents/T-1/group", `{"group":"Autopilot"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT group = %d: %s", rec.Code, rec.Body.String())
	}
	if got := listedGroups(t, srv); got["T-1"] != "Autopilot" || got["T-2"] != "" {
		t.Fatalf("groups = %v, want T-1 in Autopilot and T-2 in none", got)
	}
	if !reflect.DeepEqual(notified, []string{"T-1"}) {
		t.Fatalf("other Legato processes told of %v, want [T-1]", notified)
	}
	if msg := <-sent; msg.Type != MsgAgentsChanged {
		t.Fatalf("web clients sent %q, want %q", msg.Type, MsgAgentsChanged)
	}

	rec = groupsRequest(t, srv, http.MethodPut, "/api/agents/T-1/group", `{"group":null}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT null group = %d: %s", rec.Code, rec.Body.String())
	}
	if got := listedGroups(t, srv); got["T-1"] != "" {
		t.Fatalf("T-1 group = %q after clearing, want none", got["T-1"])
	}
}

func TestAgentGroupRouteRefusesAnUnknownTask(t *testing.T) {
	srv, _ := groupsServer(t)
	rec := groupsRequest(t, srv, http.MethodPut, "/api/agents/NOPE/group", `{"group":"x"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestAgentGroupsListsTheDefaultsThenTheGroupsInUse(t *testing.T) {
	srv, _ := groupsServer(t, "T-1", "T-2")
	srv.SetGroupDefaults([]string{"Tickets", "Tooling"})
	groupsRequest(t, srv, http.MethodPut, "/api/agents/T-1/group", `{"group":"Autopilot"}`)
	groupsRequest(t, srv, http.MethodPut, "/api/agents/T-2/group", `{"group":"Tooling"}`)

	rec := groupsRequest(t, srv, http.MethodGet, "/api/agent-groups", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/agent-groups = %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Defaults []string `json:"defaults"`
		Groups   []string `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Tickets", "Tooling"}; !reflect.DeepEqual(got.Defaults, want) {
		t.Fatalf("defaults = %v, want %v", got.Defaults, want)
	}
	if want := []string{"Tickets", "Tooling", "Autopilot"}; !reflect.DeepEqual(got.Groups, want) {
		t.Fatalf("groups = %v, want %v", got.Groups, want)
	}
}

func TestAgentGroupsRenameAndDelete(t *testing.T) {
	srv, _ := groupsServer(t, "T-1", "T-2")
	groupsRequest(t, srv, http.MethodPut, "/api/agents/T-1/group", `{"group":"Side projects"}`)
	groupsRequest(t, srv, http.MethodPut, "/api/agents/T-2/group", `{"group":"Side projects"}`)

	rec := groupsRequest(t, srv, http.MethodPut, "/api/agent-groups/Side%20projects", `{"name":"Personal"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename = %d: %s", rec.Code, rec.Body.String())
	}
	if got := listedGroups(t, srv); got["T-1"] != "Personal" || got["T-2"] != "Personal" {
		t.Fatalf("groups after rename = %v, want both Personal", got)
	}

	rec = groupsRequest(t, srv, http.MethodPut, "/api/agent-groups/Personal", `{"name":"  "}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("rename to blank = %d, want 400", rec.Code)
	}

	rec = groupsRequest(t, srv, http.MethodDelete, "/api/agent-groups/Personal", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d: %s", rec.Code, rec.Body.String())
	}
	if got := listedGroups(t, srv); got["T-1"] != "" || got["T-2"] != "" {
		t.Fatalf("groups after delete = %v, want none", got)
	}
}
