package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// historyServer serves a history holding one ended and one running
// conversation, on tasks "ended" and "live".
func historyServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	ctx := context.Background()
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cwd := t.TempDir()
	for i, task := range []string{"ended", "live"} {
		if err := db.CreateTask(ctx, store.Task{ID: task, Title: "Task " + task, Status: "Doing"}); err != nil {
			t.Fatal(err)
		}
		id, err := db.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: task, TmuxSession: "legato-" + task, AgentKind: "claude-code", Status: "running"})
		if err != nil {
			t.Fatal(err)
		}
		execution := fmt.Sprintf("ex-%d", i)
		if err := db.CreateExecution(ctx, store.Execution{ID: execution, Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: task, ExecutionTaskID: task, AgentKind: "claude-code", AgentSessionID: &id, StartedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.RecordTaskNativeSession(ctx, task, id, execution, "claude-code", "conv-"+task, cwd, ""); err != nil {
			t.Fatal(err)
		}
		if task == "ended" {
			if err := db.TerminalizeExecution(ctx, execution, store.ExecutionCancelled); err != nil {
				t.Fatal(err)
			}
		}
	}
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	srv.SetSessionHistory(service.NewSessionHistoryService(db, service.SessionHistoryOptions{}))
	return srv, db
}

func historyPage(t *testing.T, srv *Server, query string) service.SessionPage {
	t.Helper()
	rec := callAs(srv, "secret", http.MethodGet, "/api/sessions"+query, "")
	var page service.SessionPage
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &page) != nil {
		t.Fatalf("list %s: %d %s", query, rec.Code, rec.Body)
	}
	return page
}

func TestTheSessionHistoryIsListedAndFiltered(t *testing.T) {
	srv, _ := historyServer(t)
	if page := historyPage(t, srv, ""); len(page.Sessions) != 2 || page.Sessions[0].TaskID != "live" {
		t.Fatalf("list = %+v", page.Sessions)
	}
	if page := historyPage(t, srv, "?state=running"); len(page.Sessions) != 1 || page.Sessions[0].TaskID != "live" || page.Sessions[0].Restorable {
		t.Fatalf("running = %+v", page.Sessions)
	}
	if page := historyPage(t, srv, "?q=ended&limit=1"); len(page.Sessions) != 1 || page.Sessions[0].TaskID != "ended" || !page.Sessions[0].Restorable {
		t.Fatalf("search = %+v", page.Sessions)
	}
	page := historyPage(t, srv, "?limit=1")
	if page.NextCursor == "" {
		t.Fatal("a first page of one has no cursor")
	}
	if next := historyPage(t, srv, "?limit=1&cursor="+page.NextCursor); len(next.Sessions) != 1 || next.Sessions[0].TaskID != "ended" {
		t.Fatalf("second page = %+v", next.Sessions)
	}
	if page := historyPage(t, srv, "?since="+time.Now().Add(time.Hour).UTC().Format(time.RFC3339)); len(page.Sessions) != 0 {
		t.Fatalf("since the future = %+v", page.Sessions)
	}
	for _, bad := range []string{"?state=lost", "?limit=x", "?since=yesterday", "?cursor=nope"} {
		if rec := callAs(srv, "secret", http.MethodGet, "/api/sessions"+bad, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", bad, rec.Code, rec.Body)
		}
	}
}

func TestASessionIsShownWithItsRunsAndForgotten(t *testing.T) {
	srv, _ := historyServer(t)
	ids := map[string]int64{}
	for _, e := range historyPage(t, srv, "").Sessions {
		ids[e.TaskID] = e.ID
	}
	rec := callAs(srv, "secret", http.MethodGet, fmt.Sprintf("/api/sessions/%d", ids["ended"]), "")
	var detail service.SessionDetail
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &detail) != nil || len(detail.Runs) != 1 || detail.Runs[0].EndReason != "stopped" {
		t.Fatalf("detail: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, "secret", http.MethodDelete, fmt.Sprintf("/api/sessions/%d", ids["live"]), ""); rec.Code != http.StatusConflict {
		t.Fatalf("forget running: %d %s, want 409", rec.Code, rec.Body)
	}
	if rec := callAs(srv, "secret", http.MethodDelete, fmt.Sprintf("/api/sessions/%d", ids["ended"]), ""); rec.Code != http.StatusOK {
		t.Fatalf("forget: %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{fmt.Sprintf("/api/sessions/%d", ids["ended"]), "/api/sessions/nope"} {
		if rec := callAs(srv, "secret", http.MethodGet, path, ""); rec.Code != http.StatusNotFound {
			t.Fatalf("get %s: %d %s, want 404", path, rec.Code, rec.Body)
		}
	}
}

func TestWithoutAHistoryTheRoutesSaySo(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	if rec := callAs(srv, "secret", http.MethodGet, "/api/sessions", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("list: %d %s, want 503", rec.Code, rec.Body)
	}
}
