package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type chimeraSnapshotAgentFake struct {
	service.AgentService
	session       *store.AgentSession
	invalidateErr error
	cleared       bool
	invalidations []chimeraBindingInvalidation
}

type chimeraBindingInvalidation struct {
	agentSessionID int
	sessionID      string
	endpoint       string
}

func (f *chimeraSnapshotAgentFake) GetAgentSession(context.Context, string) (*store.AgentSession, error) {
	return f.session, nil
}

func (f *chimeraSnapshotAgentFake) InvalidateChimeraBinding(_ context.Context, agentSessionID int, sessionID, endpoint string) (bool, error) {
	f.invalidations = append(f.invalidations, chimeraBindingInvalidation{agentSessionID, sessionID, endpoint})
	return f.cleared, f.invalidateErr
}

func TestChimeraSnapshotPreservesPendingPermissions(t *testing.T) {
	chimeraServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sessions/chimera-live":
			fmt.Fprint(w, `{"id":"chimera-live","status":"waiting_for_permission","pendingPermissions":[{"requestId":"req-1","tool":"bash","target":"host","command":"rm -rf cache","reason":"Runs on host","cwd":"/tmp","policyAnalysis":{"version":1,"outcome":"approval_required","stages":[],"findings":[]}}]}`)
		case "/v1/sessions/chimera-live/context":
			fmt.Fprint(w, `{"contextWindow":200000,"lastPromptTokens":1200}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(chimeraServer.Close)

	sessionID := "chimera-live"
	endpoint := chimeraServer.URL
	agents := &chimeraSnapshotAgentFake{
		session: &store.AgentSession{ID: 17, TaskID: "TASK-42", ChimeraSessionID: &sessionID, ChimeraEndpoint: &endpoint},
	}
	server := New(nil, agents, nil, ":0")
	request := httptest.NewRequest(http.MethodGet, "/api/agents/TASK-42/chimera/snapshot", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	permissions, ok := body["pendingPermissions"].([]any)
	if !ok || len(permissions) != 1 {
		t.Fatalf("pendingPermissions=%#v", body["pendingPermissions"])
	}
	permission := permissions[0].(map[string]any)
	if permission["requestId"] != "req-1" || permission["command"] != "rm -rf cache" {
		t.Fatalf("permission=%#v", permission)
	}
	if body["contextWindow"] != float64(200000) || body["usedContextTokens"] != float64(1200) {
		t.Fatalf("context fields=%#v", body)
	}
}

func TestChimeraSnapshotPreservesExplicitEmptyPendingPermissions(t *testing.T) {
	chimeraServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sessions/chimera-live":
			fmt.Fprint(w, `{"id":"chimera-live","pendingPermissions":[]}`)
		case "/v1/sessions/chimera-live/context":
			fmt.Fprint(w, `{"contextWindow":200000,"lastPromptTokens":null}`)
		}
	}))
	t.Cleanup(chimeraServer.Close)

	sessionID := "chimera-live"
	endpoint := chimeraServer.URL
	agents := &chimeraSnapshotAgentFake{
		session: &store.AgentSession{ID: 17, TaskID: "TASK-42", ChimeraSessionID: &sessionID, ChimeraEndpoint: &endpoint},
	}
	server := New(nil, agents, nil, ":0")
	request := httptest.NewRequest(http.MethodGet, "/api/agents/TASK-42/chimera/snapshot", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	var body map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if string(body["pendingPermissions"]) != "[]" {
		t.Fatalf("pendingPermissions=%s", body["pendingPermissions"])
	}
}

func TestInitialChimeraSnapshotFailureInvalidatesBindingAndBroadcastsAgentsChanged(t *testing.T) {
	chimeraServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing session", http.StatusNotFound)
	}))
	t.Cleanup(chimeraServer.Close)

	sessionID := "chimera-old"
	endpoint := chimeraServer.URL
	agents := &chimeraSnapshotAgentFake{
		session: &store.AgentSession{ID: 17, TaskID: "TASK-42", ChimeraSessionID: &sessionID, ChimeraEndpoint: &endpoint},
		cleared: true,
	}
	server := New(nil, agents, nil, ":0")
	messages := make(chan WSMessage, 1)
	client := &wsClient{sendFn: func(message WSMessage) { messages <- message }}
	server.hub.add(client)
	defer server.hub.remove(client)

	request := httptest.NewRequest(http.MethodGet, "/api/agents/TASK-42/chimera/snapshot?initial=true", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(agents.invalidations) != 1 {
		t.Fatalf("invalidations=%v", agents.invalidations)
	}
	if got := agents.invalidations[0]; got.agentSessionID != 17 || got.sessionID != sessionID || got.endpoint != endpoint {
		t.Fatalf("invalidation=%#v", got)
	}
	timeout := time.After(time.Second)
	for {
		select {
		case message := <-messages:
			if message.Type == MsgAgentsChanged {
				return
			}
		case <-timeout:
			t.Fatal("agents_changed was not broadcast")
		}
	}
}

func TestChimeraSnapshotFailureDoesNotInvalidateLaterRefresh(t *testing.T) {
	chimeraServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(chimeraServer.Close)

	sessionID := "chimera-old"
	endpoint := chimeraServer.URL
	agents := &chimeraSnapshotAgentFake{
		session: &store.AgentSession{ID: 17, TaskID: "TASK-42", ChimeraSessionID: &sessionID, ChimeraEndpoint: &endpoint},
		cleared: true,
	}
	server := New(nil, agents, nil, ":0")

	request := httptest.NewRequest(http.MethodGet, "/api/agents/TASK-42/chimera/snapshot", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(agents.invalidations) != 0 {
		t.Fatalf("later refresh invalidated binding: %v", agents.invalidations)
	}
}

func TestInitialChimeraSnapshotContextFailureDoesNotInvalidateBinding(t *testing.T) {
	chimeraServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sessions/chimera-old":
			fmt.Fprint(w, `{"id":"chimera-old"}`)
		case "/v1/sessions/chimera-old/context":
			http.Error(w, "context unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(chimeraServer.Close)

	sessionID := "chimera-old"
	endpoint := chimeraServer.URL
	agents := &chimeraSnapshotAgentFake{
		session: &store.AgentSession{ID: 17, TaskID: "TASK-42", ChimeraSessionID: &sessionID, ChimeraEndpoint: &endpoint},
		cleared: true,
	}
	server := New(nil, agents, nil, ":0")

	request := httptest.NewRequest(http.MethodGet, "/api/agents/TASK-42/chimera/snapshot?initial=true", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(agents.invalidations) != 0 {
		t.Fatalf("context failure invalidated binding: %v", agents.invalidations)
	}
}

func TestInitialChimeraSnapshotInvalidationFailureIsReported(t *testing.T) {
	chimeraServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing session", http.StatusNotFound)
	}))
	t.Cleanup(chimeraServer.Close)

	sessionID := "chimera-old"
	endpoint := chimeraServer.URL
	agents := &chimeraSnapshotAgentFake{
		session:       &store.AgentSession{ID: 17, TaskID: "TASK-42", ChimeraSessionID: &sessionID, ChimeraEndpoint: &endpoint},
		invalidateErr: errors.New("database unavailable"),
	}
	server := New(nil, agents, nil, ":0")

	request := httptest.NewRequest(http.MethodGet, "/api/agents/TASK-42/chimera/snapshot?initial=true", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if body := response.Body.String(); body == "" || !containsAll(body, "missing session", "database unavailable") {
		t.Fatalf("body=%q", body)
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
