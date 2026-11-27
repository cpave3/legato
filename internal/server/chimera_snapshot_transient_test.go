package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// A first panel load that meets a Chimera that is alive but failing for the
// moment must keep the binding: only a session that is gone clears it.
func TestInitialChimeraSnapshotTransientFailureKeepsBinding(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"unavailable": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "busy", http.StatusServiceUnavailable)
		},
		"server error": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		},
		"auth refused": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusUnauthorized)
		},
		"truncated body": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte(`{"id":"x"`))
		},
		"malformed body": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`not json`))
		},
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			upstream := httptest.NewServer(handler)
			t.Cleanup(upstream.Close)
			sessionID, endpoint := "chimera-live", upstream.URL+"/endpoint-secret-canary"
			agents := &chimeraSnapshotAgentFake{
				session: &store.AgentSession{ID: 17, TaskID: "TASK-42", ChimeraSessionID: &sessionID, ChimeraEndpoint: &endpoint},
				cleared: true,
			}
			response := httptest.NewRecorder()
			New(nil, agents, nil, ":0").Handler().ServeHTTP(response,
				httptest.NewRequest(http.MethodGet, "/api/agents/TASK-42/chimera/snapshot?initial=true", nil))
			if response.Code != http.StatusBadGateway {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if len(agents.invalidations) != 0 {
				t.Fatalf("a transient failure cleared the binding: %v", agents.invalidations)
			}
			body := response.Body.String()
			if strings.Contains(body, "endpoint-secret-canary") {
				t.Fatalf("response disclosed the endpoint: %s", body)
			}
			if !strings.Contains(body, "chimera: unavailable (") {
				t.Fatalf("response does not name the failure class: %s", body)
			}
		})
	}
}

// A Chimera that no longer listens is gone: its binding is cleared, so the
// agent is shown through its terminal.
func TestInitialChimeraSnapshotRefusedConnectionInvalidatesBinding(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	upstream.Close()
	sessionID, endpoint := "chimera-old", upstream.URL
	agents := &chimeraSnapshotAgentFake{
		session: &store.AgentSession{ID: 17, TaskID: "TASK-42", ChimeraSessionID: &sessionID, ChimeraEndpoint: &endpoint},
		cleared: true,
	}
	response := httptest.NewRecorder()
	New(nil, agents, nil, ":0").Handler().ServeHTTP(response,
		httptest.NewRequest(http.MethodGet, "/api/agents/TASK-42/chimera/snapshot?initial=true", nil))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(agents.invalidations) != 1 {
		t.Fatalf("invalidations=%v", agents.invalidations)
	}
}
