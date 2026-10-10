package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nhooyr.io/websocket"

	"github.com/cpave3/legato/internal/engine/codexbridge"
)

// bridgedCodexAgent is a running Codex agent bound to a real bridge, whose
// app server answers every call but whose terminal has opened no thread yet.
func bridgedCodexAgent(t *testing.T, taskID, launchedIn string) (AgentService, string) {
	t.Helper()
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		for {
			_, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			var call struct {
				ID json.RawMessage `json:"id"`
			}
			if json.Unmarshal(data, &call) == nil && len(call.ID) > 0 {
				reply, _ := json.Marshal(map[string]any{"id": call.ID, "result": map[string]any{}})
				_ = conn.Write(context.Background(), websocket.MessageText, reply)
			}
		}
	}))
	t.Cleanup(appServer.Close)

	bridge := codexbridge.New(codexbridge.Options{
		TaskID: taskID, Token: "secret", CWD: launchedIn,
		AppServerURL: "ws" + strings.TrimPrefix(appServer.URL, "http"),
	})
	if err := bridge.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		bridge.Close()
		server.Close()
	})
	endpoint := server.URL + "/secret"

	svc, s, _ := newDiscoveryTestService(t)
	runningAgent(t, s, taskID, "codex")
	if err := s.UpdateChimeraDiscovery(context.Background(), taskID, taskID, endpoint); err != nil {
		t.Fatal(err)
	}
	return svc, endpoint
}

func TestAMessageToACodexAgentWhoseTerminalIsStillStartingIsHeldNotLost(t *testing.T) {
	svc, endpoint := bridgedCodexAgent(t, "TASK-42", t.TempDir())

	// Codex's terminal is up but has not opened a thread: it is starting, or
	// showing a prompt of its own. Legato still accepts the message.
	if err := svc.SendMessage(context.Background(), "TASK-42", "read your brief and begin"); err != nil {
		t.Fatalf("the message was refused: %v", err)
	}

	resp, err := http.Get(endpoint + "/v1/sessions/TASK-42")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var snapshot struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	// The person sees it in the conversation while it waits.
	if len(snapshot.Messages) != 2 || snapshot.Messages[1].Role != "user" || snapshot.Messages[1].Content != "read your brief and begin" {
		t.Fatalf("messages = %+v", snapshot.Messages)
	}
}
