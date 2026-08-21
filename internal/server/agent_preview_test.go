package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestAgentPreviewReturnsPlainBoundedTail(t *testing.T) {
	prefix := strings.Repeat("old", agentPreviewMaxRunes)
	agents := &promptAgentService{captureOutput: prefix + "\x1b[31mrecent output\x1b[0m"}
	server := New(nil, agents, nil, ":0")
	request := httptest.NewRequest(http.MethodGet, "/api/agents/worker-1/preview", nil)
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		TaskID  string `json:"task_id"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.TaskID != "worker-1" || agents.capturedTaskID != "worker-1" {
		t.Fatalf("task ids: body=%q captured=%q", body.TaskID, agents.capturedTaskID)
	}
	if strings.Contains(body.Content, "\x1b") || !strings.HasSuffix(body.Content, "recent output") {
		t.Fatalf("content=%q", body.Content)
	}
	if got := len([]rune(body.Content)); got != agentPreviewMaxRunes {
		t.Fatalf("preview runes=%d want=%d", got, agentPreviewMaxRunes)
	}
	if len(server.streams.streams) != 0 {
		t.Fatalf("preview created streams: %#v", server.streams.streams)
	}
}

func TestAgentPreviewRejectsNativeAgent(t *testing.T) {
	agents := &promptAgentService{chimeraEndpoint: "http://chimera", chimeraSessionID: "session-1"}
	server := New(nil, agents, nil, ":0")
	request := httptest.NewRequest(http.MethodGet, "/api/agents/worker-1/preview", nil)
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if agents.capturedTaskID != "" {
		t.Fatalf("native preview captured task %q", agents.capturedTaskID)
	}
}

func TestAgentPreviewReturnsNotFoundForStoppedAgent(t *testing.T) {
	agents := &promptAgentService{captureErr: errors.Join(errors.New("session missing"), store.ErrNotFound)}
	server := New(nil, agents, nil, ":0")
	request := httptest.NewRequest(http.MethodGet, "/api/agents/worker-1/preview", nil)
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
