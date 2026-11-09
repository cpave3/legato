package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type nopReeveAgents struct{}

func (nopReeveAgents) SpawnAgent(context.Context, string, int, int, ...service.AgentSpawnOptions) error {
	return nil
}
func (nopReeveAgents) KillAgent(context.Context, string) error { return nil }

func reeveServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	srv.SetReeveService(service.NewReeveService(service.ReeveServiceOptions{Store: db, Agents: nopReeveAgents{}, Folder: filepath.Join(dir, "reeve")}))
	return srv, db
}

func callAs(srv *Server, token, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestAReeveSessionIsStartedEndedResumedAndDeleted(t *testing.T) {
	srv, _ := reeveServer(t)
	rec := callAs(srv, "secret", http.MethodPost, "/api/reeve/sessions", `{"model": "sonnet"}`)
	var started store.ReeveSession
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &started) != nil || started.Model != "sonnet" {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, "secret", http.MethodPost, "/api/reeve/sessions", `{}`); rec.Code != http.StatusConflict {
		t.Fatalf("a second start: %d", rec.Code)
	}
	if rec := callAs(srv, "secret", http.MethodPost, "/api/reeve/sessions/"+started.TaskID+"/end", ""); rec.Code != http.StatusOK {
		t.Fatalf("end: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, "secret", http.MethodPost, "/api/reeve/sessions/"+started.TaskID+"/resume", ""); rec.Code != http.StatusOK {
		t.Fatalf("resume: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, "secret", http.MethodDelete, "/api/reeve/sessions/"+started.TaskID, ""); rec.Code != http.StatusConflict {
		t.Fatalf("delete a running session: %d", rec.Code)
	}
	callAs(srv, "secret", http.MethodPost, "/api/reeve/sessions/"+started.TaskID+"/end", "")
	if rec := callAs(srv, "secret", http.MethodDelete, "/api/reeve/sessions/"+started.TaskID, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	rec = callAs(srv, "secret", http.MethodGet, "/api/reeve", "")
	if !strings.Contains(rec.Body.String(), `"history":[]`) {
		t.Fatalf("overview: %s", rec.Body)
	}
}

// The Reeve's token reads; it never changes anything directly.
func TestTheReevesTokenPassesOnlyReadRoutes(t *testing.T) {
	srv, db := reeveServer(t)
	rec := callAs(srv, "secret", http.MethodPost, "/api/reeve/sessions", `{}`)
	var started store.ReeveSession
	json.Unmarshal(rec.Body.Bytes(), &started)
	row, _ := db.GetReeveSession(context.Background(), started.TaskID)
	token := row.APIToken

	if rec := callAs(srv, token, http.MethodGet, "/api/reeve", ""); rec.Code != http.StatusOK {
		t.Fatalf("a read: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, token, http.MethodPost, "/api/restart", ""); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "needs_approval") {
		t.Fatalf("a risky call: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, token, http.MethodPost, "/api/approvals/ap-1/decision", `{}`); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "never") {
		t.Fatalf("a never call: %d %s", rec.Code, rec.Body)
	}
	callAs(srv, "secret", http.MethodPost, "/api/reeve/sessions/"+started.TaskID+"/end", "")
	if rec := callAs(srv, token, http.MethodGet, "/api/reeve", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("an ended session's token: %d", rec.Code)
	}
}

type listedSets []store.ReeveChangeSet

func (l listedSets) ChangeSetsOf(context.Context, string) ([]store.ReeveChangeSet, error) {
	return l, nil
}

func TestAChangeSetShowsInTheConversationAsALineWithItsState(t *testing.T) {
	srv, _ := reeveServer(t)
	srv.SetReeveChangeSets(listedSets{
		{ID: "cs-1", Reason: "rename", State: "pending", Calls: `[{},{}]`, Results: `[]`},
		{ID: "cs-2", Reason: "tidy", State: "failed", Calls: `[{},{},{}]`, Results: `[{"state":"ran"},{"state":"failed"},{"state":"skipped"}]`},
	})
	rec := callAs(srv, "secret", http.MethodGet, "/api/reeve/sessions/T/change-sets", "")
	var got struct {
		ChangeSets []ChangeSetLine `json:"change_sets"`
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.ChangeSets) != 2 || got.ChangeSets[0].Calls != 2 || got.ChangeSets[1].Ran != 1 || got.ChangeSets[1].State != "failed" {
		t.Fatalf("lines = %+v", got.ChangeSets)
	}
}
