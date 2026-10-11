package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// conversationAgentService is an agent service whose native agents can be
// asked what they said and did.
type conversationAgentService struct {
	*promptAgentService
	tail        *service.ConversationTail
	err         error
	askedTaskID string
	askedLimit  int
}

func (c *conversationAgentService) ConversationTail(_ context.Context, taskID string, limit int) (*service.ConversationTail, error) {
	c.askedTaskID, c.askedLimit = taskID, limit
	return c.tail, c.err
}

func getConversation(agents service.AgentService, target string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	New(nil, agents, nil, ":0").Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	return response
}

func TestAgentConversationReturnsWhatANativeAgentSaidAndDid(t *testing.T) {
	agents := &conversationAgentService{promptAgentService: &promptAgentService{}, tail: &service.ConversationTail{
		Status: "waiting_for_permission",
		Entries: []service.ConversationEntry{
			{Kind: service.EntryUser, Text: "clean up"},
			{Kind: service.EntryAssistant, Text: "Removing the build output."},
			{Kind: service.EntryTool, Tool: "bash", Text: "Clean and rebuild", Command: "rm -rf dist && pnpm build", State: service.ToolRunning},
		},
		Permission:    &service.ConversationPermission{Tool: "bash", Command: "rm -rf dist && pnpm build", Reason: "outside the sandbox"},
		Question:      &service.ConversationQuestion{Text: "Which color?", Options: []string{"Red", "Blue"}},
		ContextUsed:   138000,
		ContextWindow: 200000,
	}}

	response := getConversation(agents, "/api/agents/worker-1/conversation")

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"task_id": "worker-1",
		"status":  "waiting_for_permission",
		"entries": []any{
			map[string]any{"kind": "user", "text": "clean up"},
			map[string]any{"kind": "assistant", "text": "Removing the build output."},
			map[string]any{"kind": "tool", "text": "Clean and rebuild", "tool": "bash", "command": "rm -rf dist && pnpm build", "state": "running"},
		},
		"permission":     map[string]any{"tool": "bash", "command": "rm -rf dist && pnpm build", "reason": "outside the sandbox"},
		"question":       map[string]any{"text": "Which color?", "options": []any{"Red", "Blue"}},
		"context_used":   float64(138000),
		"context_window": float64(200000),
	}
	got, _ := json.Marshal(body)
	wanted, _ := json.Marshal(want)
	if string(got) != string(wanted) {
		t.Fatalf("body:\n got %s\nwant %s", got, wanted)
	}
	if agents.askedTaskID != "worker-1" || agents.askedLimit != defaultConversationEntries {
		t.Fatalf("asked for %d entries of %q", agents.askedLimit, agents.askedTaskID)
	}
}

func TestAgentConversationOfASilentAgentIsAnEmptyList(t *testing.T) {
	agents := &conversationAgentService{promptAgentService: &promptAgentService{}, tail: &service.ConversationTail{Status: "idle"}}

	response := getConversation(agents, "/api/agents/worker-1/conversation")

	var body struct {
		Entries    []any `json:"entries"`
		Permission any   `json:"permission"`
		Question   any   `json:"question"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Entries == nil || len(body.Entries) != 0 || body.Permission != nil || body.Question != nil {
		t.Fatalf("body=%s; want an empty list and no request", response.Body.String())
	}
}

func TestAgentConversationReadsAsManyEntriesAsAsked(t *testing.T) {
	for target, want := range map[string]int{
		"/api/agents/worker-1/conversation?limit=5":    5,
		"/api/agents/worker-1/conversation?limit=9999": maxConversationEntries,
		"/api/agents/worker-1/conversation?limit=0":    defaultConversationEntries,
		"/api/agents/worker-1/conversation?limit=many": defaultConversationEntries,
	} {
		agents := &conversationAgentService{promptAgentService: &promptAgentService{}, tail: &service.ConversationTail{}}
		getConversation(agents, target)
		if agents.askedLimit != want {
			t.Errorf("%s asked for %d entries, want %d", target, agents.askedLimit, want)
		}
	}
}

func TestAgentConversationSaysWhyThereIsNoneToRead(t *testing.T) {
	for name, test := range map[string]struct {
		agents service.AgentService
		status int
	}{
		"an agent driven through its terminal": {
			&conversationAgentService{promptAgentService: &promptAgentService{}, err: service.ErrNoNativeEndpoint}, http.StatusConflict,
		},
		"an agent that is not running": {
			&conversationAgentService{promptAgentService: &promptAgentService{}, err: errors.Join(errors.New("session missing"), store.ErrNotFound)}, http.StatusNotFound,
		},
		"an endpoint that does not answer": {
			&conversationAgentService{promptAgentService: &promptAgentService{}, err: errors.New("connection refused")}, http.StatusBadGateway,
		},
		"an agent service that reads no conversations": {
			&promptAgentService{}, http.StatusNotImplemented,
		},
	} {
		if response := getConversation(test.agents, "/api/agents/worker-1/conversation"); response.Code != test.status {
			t.Errorf("%s: status=%d want=%d body=%s", name, response.Code, test.status, response.Body.String())
		}
	}
}

func TestAgentConversationIsReadOnly(t *testing.T) {
	agents := &conversationAgentService{promptAgentService: &promptAgentService{}, tail: &service.ConversationTail{}}
	response := httptest.NewRecorder()

	New(nil, agents, nil, ":0").Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/agents/worker-1/conversation", nil))

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d want=%d", response.Code, http.StatusMethodNotAllowed)
	}
	if agents.askedTaskID != "" {
		t.Fatal("a refused request should not reach the agent")
	}
}
