package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type worktreeServiceFake struct {
	created   *store.TaskWorktree
	taskID    string
	createErr error
}

func (f *worktreeServiceFake) Create(_ context.Context, taskID, primaryDir, branch, baseBranch string) (*store.TaskWorktree, error) {
	f.taskID = taskID
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = &store.TaskWorktree{PrimaryDir: primaryDir, Path: primaryDir + "-" + branch, Branch: branch, BaseBranch: baseBranch}
	return f.created, nil
}

func TestTaskWorktreeEndpointCreatesThroughSharedWorkflow(t *testing.T) {
	board := &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}
	worktrees := &worktreeServiceFake{}
	srv := New(board, nil, nil, ":0")
	srv.SetWorktreeService(worktrees)
	var notified string
	srv.notifyTaskChanged = func(taskID string) { notified = taskID }

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/worktree", strings.NewReader(`{"primary_dir":"/projects/app","branch":"t-1","base_branch":"main"}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got TaskWorktreeResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if worktrees.taskID != "T-1" || got.Path != "/projects/app-t-1" || notified != "T-1" {
		t.Fatalf("task=%q response=%+v notified=%q", worktrees.taskID, got, notified)
	}
}

func TestTaskWorktreeEndpointRejectsUnavailableAndExistingWorktrees(t *testing.T) {
	t.Run("unavailable", func(t *testing.T) {
		srv := New(&mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}, nil, nil, ":0")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/worktree", strings.NewReader(`{}`)))
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("existing", func(t *testing.T) {
		srv := New(&mockBoardService{cardDetail: &service.CardDetail{ID: "T-1", Worktree: &store.TaskWorktree{Path: "/existing"}}}, nil, nil, ":0")
		srv.SetWorktreeService(&worktreeServiceFake{})
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/worktree", strings.NewReader(`{"primary_dir":"/repo","branch":"t-1","base_branch":"main"}`)))
		if rec.Code != http.StatusConflict {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestTaskWorktreeEndpointSurfacesWorkflowFailureWithoutNotification(t *testing.T) {
	board := &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}
	srv := New(board, nil, nil, ":0")
	srv.SetWorktreeService(&worktreeServiceFake{createErr: errors.New("yg failed")})
	notified := false
	srv.notifyTaskChanged = func(string) { notified = true }
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/worktree", strings.NewReader(`{"primary_dir":"/repo","branch":"t-1","base_branch":"main"}`)))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "yg failed") || notified {
		t.Fatalf("status=%d body=%s notified=%v", rec.Code, rec.Body.String(), notified)
	}
}
