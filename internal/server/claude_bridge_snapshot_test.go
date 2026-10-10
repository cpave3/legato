package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/claudebridge"
	"github.com/cpave3/legato/internal/engine/store"
)

// A Claude Code agent's bridge is bound to the agent session exactly as a
// Chimera endpoint is, so the web panel's snapshot route serves it unchanged.
func TestNativeSnapshotServesAClaudeCodeSessionThroughItsBridge(t *testing.T) {
	bridge := claudebridge.New(claudebridge.Options{TaskID: "TASK-42", Token: "secret"})
	bridgeServer := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		bridge.Close()
		bridgeServer.Close()
	})
	endpoint := bridgeServer.URL + "/secret"
	events := `{"events":[
		{"type":"hello","sessionId":"claude-1","cwd":"/work/repo","model":"claude-opus-5-5","context":{"tokens":4200,"window":200000}},
		{"type":"row","uuid":"u1","door":"prompt","origin":{"kind":"composer"},"message":{"type":"user","role":"user","content":[{"type":"text","text":"run the tests"}]}},
		{"type":"row","uuid":"u2","door":"response","origin":{"kind":"model"},"message":{"type":"assistant","role":"assistant","content":[
			{"type":"text","text":"Running them."},
			{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"go test ./..."}}]}}
	]}`
	resp, err := http.Post(endpoint+"/mod/events", "application/json", strings.NewReader(events))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	sessionID := "TASK-42"
	agents := &chimeraSnapshotAgentFake{
		session: &store.AgentSession{ID: 17, TaskID: "TASK-42", AgentKind: "claude-code", ChimeraSessionID: &sessionID, ChimeraEndpoint: &endpoint},
	}
	server := New(nil, agents, nil, ":0")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/agents/TASK-42/chimera/snapshot?initial=true", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var snapshot struct {
		Name              string `json:"name"`
		Status            string `json:"status"`
		ContextWindow     int    `json:"contextWindow"`
		UsedContextTokens int    `json:"usedContextTokens"`
		Messages          []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
		ToolCalls []struct {
			CallID string         `json:"callId"`
			Name   string         `json:"name"`
			Args   map[string]any `json:"args"`
		} `json:"toolCalls"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if snapshot.Name != "TASK-42" || snapshot.Status != "idle" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.ContextWindow != 200000 || snapshot.UsedContextTokens != 4200 {
		t.Fatalf("context = %d/%d", snapshot.UsedContextTokens, snapshot.ContextWindow)
	}
	if len(snapshot.Messages) != 3 || snapshot.Messages[1].Role != "user" || snapshot.Messages[1].Content != "run the tests" || snapshot.Messages[2].Role != "assistant" {
		t.Fatalf("messages = %+v", snapshot.Messages)
	}
	if len(snapshot.ToolCalls) != 1 || snapshot.ToolCalls[0].Name != "bash" || snapshot.ToolCalls[0].Args["command"] != "go test ./..." {
		t.Fatalf("tool calls = %+v", snapshot.ToolCalls)
	}
	if len(agents.invalidations) != 0 {
		t.Fatalf("a live bridge binding was invalidated: %+v", agents.invalidations)
	}
}

// The panel's model and settings routes reach a Claude Code session's bridge
// through the same handlers that proxy a Chimera session.
func TestNativeModelRoutesDriveAClaudeCodeSessionThroughItsBridge(t *testing.T) {
	bridge := claudebridge.New(claudebridge.Options{TaskID: "TASK-42", Models: []string{"claude-sonnet-5-5"}})
	bridgeServer := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		bridge.Close()
		bridgeServer.Close()
	})
	resp, err := http.Post(bridgeServer.URL+"/mod/events", "application/json",
		strings.NewReader(`{"events":[{"type":"hello","cwd":"/work","model":"claude-opus-5-5"},{"type":"settings","effort":"medium"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	sessionID, endpoint := "TASK-42", bridgeServer.URL
	agents := &chimeraModelsAgentFake{endpoint: true, session: &store.AgentSession{
		ID: 17, TaskID: "TASK-42", AgentKind: "claude-code", ChimeraSessionID: &sessionID, ChimeraEndpoint: &endpoint,
	}}
	server := New(nil, agents, nil, ":0")
	call := func(method, path, body string) (int, string) {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
		return response.Code, response.Body.String()
	}

	code, body := call(http.MethodGet, "/api/agents/TASK-42/chimera/models", "")
	if code != http.StatusOK || !strings.Contains(body, `"ref":"anthropic/claude-opus-5-5"`) || !strings.Contains(body, `"ref":"anthropic/claude-sonnet-5-5"`) {
		t.Fatalf("models = %d %s", code, body)
	}

	code, body = call(http.MethodPost, "/api/agents/TASK-42/chimera/model", `{"model":"anthropic/claude-sonnet-5-5","expectedModel":"anthropic/claude-opus-5-5"}`)
	if code != http.StatusOK || !strings.Contains(body, `"to":"anthropic/claude-sonnet-5-5"`) {
		t.Fatalf("choose model = %d %s", code, body)
	}

	code, body = call(http.MethodPost, "/api/agents/TASK-42/chimera/model", `{"expectedModel":"anthropic/claude-sonnet-5-5","settings":{"reasoningEffort":"high"}}`)
	if code != http.StatusOK {
		t.Fatalf("choose effort = %d %s", code, body)
	}
	var saved struct {
		SettingsState struct {
			Model     string         `json:"model"`
			Effective map[string]any `json:"effective"`
		} `json:"settingsState"`
	}
	if err := json.Unmarshal([]byte(body), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.SettingsState.Model != "anthropic/claude-sonnet-5-5" || saved.SettingsState.Effective["reasoningEffort"] != "high" {
		t.Fatalf("settings state = %+v", saved.SettingsState)
	}

	// The panel's `/model default` resets to the session's own model.
	code, body = call(http.MethodPost, "/api/agents/TASK-42/chimera/model", `{"model":"default"}`)
	if code != http.StatusOK || !strings.Contains(body, `"to":"anthropic/claude-opus-5-5"`) {
		t.Fatalf("reset model = %d %s", code, body)
	}
}
