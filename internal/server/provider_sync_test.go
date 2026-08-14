package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

type fakeProviderSync struct {
	result service.ProviderSyncResult
	err    error
	calls  int
}

func (s *fakeProviderSync) Sync(context.Context) (service.ProviderSyncResult, error) {
	s.calls++
	return s.result, s.err
}

func TestBoardSyncHandlerReturnsProviderOutcomes(t *testing.T) {
	board := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	sync := &fakeProviderSync{result: service.ProviderSyncResult{
		Jira:   service.ProviderSyncSourceResult{Status: service.ProviderSyncCompleted, TasksSynced: 3},
		GitHub: service.ProviderSyncSourceResult{Status: service.ProviderSyncFailed, Error: "rate limited"},
	}}
	srv := New(board, nil, nil, ":0")
	srv.SetProviderSyncService(sync)
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/board/sync", nil))
	if recorder.Code != http.StatusOK || sync.calls != 1 {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, sync.calls, recorder.Body.String())
	}
	var result service.ProviderSyncResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Jira.TasksSynced != 3 || result.GitHub.Error != "rate limited" {
		t.Fatalf("result = %#v", result)
	}
}

func TestBoardSyncHandlerRejectsOverlapAndUnsupportedMethod(t *testing.T) {
	board := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(board, nil, nil, ":0")
	srv.SetProviderSyncService(&fakeProviderSync{err: service.ErrProviderSyncInProgress})

	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/board/sync", nil))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/board/sync", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method status = %d", recorder.Code)
	}
}

func TestBoardSyncHandlerRequiresConfiguredCoordinator(t *testing.T) {
	board := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(board, nil, nil, ":0")
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/board/sync", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestBoardSyncHandlerReturnsUnexpectedCoordinatorError(t *testing.T) {
	board := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(board, nil, nil, ":0")
	srv.SetProviderSyncService(&fakeProviderSync{err: errors.New("unexpected")})
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/board/sync", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", recorder.Code)
	}
}
