package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBangEndpointExecutesAndAppends(t *testing.T) {
	repoDir := t.TempDir()
	var appendedContent string

	chimeraSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sessions/sess-1" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"cwd": repoDir})
		case r.URL.Path == "/v1/sessions/sess-1/messages" && r.URL.Query().Get("append") == "true":
			var body struct {
				Content string `json:"content"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			appendedContent = body.Content
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	}))
	defer chimeraSrv.Close()

	agents := &promptAgentService{}
	agents.chimeraEndpoint = chimeraSrv.URL
	agents.chimeraSessionID = "sess-1"
	s := New(nil, agents, nil, "")

	body := `{"command":"echo hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/agents/task-1/chimera/bang", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var result struct {
		Stdout   string `json:"stdout"`
		ExitCode int    `json:"exit_code"`
	}
	json.NewDecoder(rec.Body).Decode(&result)
	if result.Stdout != "hello\n" {
		t.Fatalf("stdout=%q", result.Stdout)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit_code=%d", result.ExitCode)
	}
	if !strings.Contains(appendedContent, "!echo hello (exit 0)") || !strings.Contains(appendedContent, "hello") {
		t.Fatalf("appended=%q", appendedContent)
	}
}

func TestBangEndpointTimeout(t *testing.T) {
	repoDir := t.TempDir()
	chimeraSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sessions/sess-1" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"cwd": repoDir})
		case r.URL.Path == "/v1/sessions/sess-1/messages" && r.URL.Query().Get("append") == "true":
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	}))
	defer chimeraSrv.Close()

	agents := &promptAgentService{}
	agents.chimeraEndpoint = chimeraSrv.URL
	agents.chimeraSessionID = "sess-1"
	s := New(nil, agents, nil, "")

	body := `{"command":"sleep 60"}`
	req := httptest.NewRequest(http.MethodPost, "/api/agents/task-1/chimera/bang", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var result struct {
		TimedOut bool `json:"timed_out"`
	}
	json.NewDecoder(rec.Body).Decode(&result)
	if !result.TimedOut {
		t.Fatalf("expected timeout, got %+v", result)
	}
}
