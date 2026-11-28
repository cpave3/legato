package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/claudebridge"
	"github.com/cpave3/legato/internal/engine/store"
)

// A Claude Code agent's bridge is bound to the agent session exactly as a
// Chimera endpoint is, so the web panel's snapshot route serves it unchanged.
func TestNativeSnapshotServesAClaudeCodeSessionThroughItsBridge(t *testing.T) {
	bridge := claudebridge.New(claudebridge.Options{DecisionKey: "test-host-key", TaskID: "TASK-42", Token: "secret"})
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

	endpoint = testClaudeControlEndpoint(t, endpoint)
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
	bridge := claudebridge.New(claudebridge.Options{DecisionKey: "test-host-key", TaskID: "TASK-42", Models: []string{"claude-sonnet-5-5"}})
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
	sessionID, endpoint := "TASK-42", testClaudeControlEndpoint(t, bridgeServer.URL)
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

func testClaudeControlEndpoint(t *testing.T, publicEndpoint string) string {
	t.Helper()
	endpoint, err := url.Parse(publicEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	endpoint.User = url.UserPassword("legato", "test-host-key")
	return endpoint.String()
}

// The web permission route must carry host authority all the way to the
// bridge, then return the decision to the mod's held request.
func TestNativePermissionRouteAnswersAClaudeCodeBridgeRequest(t *testing.T) {
	bridge := claudebridge.New(claudebridge.Options{TaskID: "TASK-42", Token: "secret", DecisionKey: "test-host-key"})
	bridgeServer := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() { bridge.Close(); bridgeServer.Close() })
	publicEndpoint := bridgeServer.URL + "/secret"
	controlEndpoint := testClaudeControlEndpoint(t, publicEndpoint)
	agents := &promptAgentService{chimeraEndpoint: controlEndpoint, chimeraSessionID: "TASK-42"}
	web := httptest.NewServer(New(nil, agents, nil, ":0").Handler())
	defer web.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	viewerRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, controlEndpoint+"/v1/sessions/TASK-42/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := http.DefaultClient.Do(viewerRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Body.Close()
	if viewer.StatusCode != http.StatusOK {
		t.Fatalf("viewer status = %d", viewer.StatusCode)
	}
	type modAnswer struct {
		Decision string `json:"decision"`
		Feedback string `json:"feedback"`
	}
	type modResult struct {
		answer modAnswer
		err    error
	}
	answered := make(chan modResult, 1)
	go func() {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, publicEndpoint+"/mod/permissions",
			strings.NewReader(`{"requestId":"permission-1","tool":"Bash","input":{"command":"git push"}}`))
		if err != nil {
			answered <- modResult{err: err}
			return
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			answered <- modResult{err: err}
			return
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			answered <- modResult{err: fmt.Errorf("mod permission status = %d", response.StatusCode)}
			return
		}
		var answer modAnswer
		err = json.NewDecoder(response.Body).Decode(&answer)
		answered <- modResult{answer, err}
	}()
	events := bufio.NewScanner(viewer.Body)
	opened := false
	for events.Scan() {
		if strings.Contains(events.Text(), `"permission_request"`) {
			opened = true
			break
		}
	}
	if !opened {
		t.Fatalf("request did not open: %v", events.Err())
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, web.URL+"/api/agents/TASK-42/chimera/permission",
		strings.NewReader(`{"request_id":"permission-1","decision":"deny","feedback":"Use a review branch"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("permission route status = %d", response.StatusCode)
	}
	select {
	case result := <-answered:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.answer.Decision != "deny" || result.answer.Feedback != "Use a review branch" {
			t.Fatalf("mod answer = %+v", result.answer)
		}
	case <-ctx.Done():
		t.Fatal("permission route never answered the mod")
	}
}
