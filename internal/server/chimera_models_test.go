package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestChimeraModelSettingsPreservesExplicitValuesAndErrors(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusBadRequest, http.StatusConflict} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			mockChimera := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if _, exists := body["model"]; exists {
					t.Error("settings-only update must not reset model")
				}
				var settings map[string]any
				if err := json.Unmarshal(body["settings"], &settings); err != nil {
					t.Error(err)
				}
				if settings["parallelToolCalls"] != false || settings["reasoningEffort"] != "max" {
					t.Errorf("settings lost: %v", settings)
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"model setting rejected"}`))
			}))
			defer mockChimera.Close()
			server := New(nil, &chimeraModelsAgentFake{endpoint: true, session: &store.AgentSession{ChimeraEndpoint: strPtr(mockChimera.URL), ChimeraSessionID: strPtr("s1")}}, nil, ":0")
			request := httptest.NewRequest(http.MethodPost, "/api/agents/TASK-1/chimera/model", strings.NewReader(`{"settings":{"parallelToolCalls":false,"reasoningEffort":"max"}}`))
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != status || !strings.Contains(response.Body.String(), "model setting rejected") {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

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
			{"ref": "manticore/gpt", "contextWindow": 256000, "vision": true, "capabilities": map[string]any{"inputModalities": []string{"text", "image"}}, "isDefault": false},
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
	if models[1]["vision"] != true || models[1]["capabilities"] == nil {
		t.Fatalf("lost discovered Codex vision metadata: %v", models[1])
	}
}

func strPtr(s string) *string { return &s }
