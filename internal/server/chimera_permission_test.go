package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChimeraPermissionHandlerForwardsDenyFeedback(t *testing.T) {
	var gotPath, gotBody string
	chimeraServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer chimeraServer.Close()

	agents := &promptAgentService{chimeraEndpoint: chimeraServer.URL, chimeraSessionID: "session-1"}
	server := New(nil, agents, nil, "")
	request := httptest.NewRequest(http.MethodPost, "/api/agents/task-1/chimera/permission", http.NoBody)
	request.SetPathValue("task_id", "task-1")
	request.Body = io.NopCloser(strings.NewReader(`{"request_id":"req-1","decision":"deny","feedback":"Try a read-only command"}`))
	response := httptest.NewRecorder()

	server.chimeraPermissionHandler().ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if gotPath != "/v1/sessions/session-1/permissions/req-1" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody != `{"decision":"deny","feedback":"Try a read-only command"}` {
		t.Fatalf("body = %s", gotBody)
	}
}
