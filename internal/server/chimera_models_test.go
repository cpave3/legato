package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type chimeraModelsAgentFake struct {
	service.AgentService
	session  *store.AgentSession
	endpoint bool
}

func (f *chimeraModelsAgentFake) GetAgentSession(_ context.Context, _ string) (*store.AgentSession, error) {
	return f.session, nil
}

func (f *chimeraModelsAgentFake) HasChimeraEndpoint(_ context.Context, _ string) bool {
	return f.endpoint
}

func TestChimeraModelsHandlerRejectsNonGet(t *testing.T) {
	server := New(nil, &chimeraModelsAgentFake{
		endpoint: true,
		session: &store.AgentSession{
			ChimeraEndpoint:  strPtr("http://127.0.0.1:9999"),
			ChimeraSessionID: strPtr("s1"),
		},
	}, nil, ":0")
	request := httptest.NewRequest(http.MethodPost, "/api/agents/TASK-1/chimera/models", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestChimeraModelsHandlerReturnsNoEndpoint(t *testing.T) {
	server := New(nil, &chimeraModelsAgentFake{
		endpoint: false,
		session:  &store.AgentSession{TaskID: "TASK-1"},
	}, nil, ":0")
	request := httptest.NewRequest(http.MethodGet, "/api/agents/TASK-1/chimera/models", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestChimeraModelsHandlerProxiesModelsFromChimera(t *testing.T) {
	mockChimera := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/s1/models" || r.Method != http.MethodGet {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"ref": "manticore/glm", "contextWindow": 256000, "isDefault": true},
			{"ref": "manticore/gpt", "contextWindow": 256000, "isDefault": false},
			{"ref": "manticore/kimi", "contextWindow": 256000, "vision": true, "isDefault": false},
		})
	}))
	defer mockChimera.Close()

	server := New(nil, &chimeraModelsAgentFake{
		endpoint: true,
		session: &store.AgentSession{
			ChimeraEndpoint:  strPtr(mockChimera.URL),
			ChimeraSessionID: strPtr("s1"),
		},
	}, nil, ":0")
	request := httptest.NewRequest(http.MethodGet, "/api/agents/TASK-1/chimera/models", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var models []map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &models); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %d", len(models))
	}
	if models[0]["ref"] != "manticore/glm" {
		t.Fatalf("first model ref = %v", models[0]["ref"])
	}
	if models[0]["isDefault"] != true {
		t.Fatalf("first model should be default")
	}
	if models[2]["vision"] != true {
		t.Fatalf("third model should have vision")
	}
}

func strPtr(s string) *string { return &s }
