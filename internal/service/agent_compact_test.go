package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func compactor(t *testing.T, svc AgentService) AgentCompactor {
	t.Helper()
	c, ok := svc.(AgentCompactor)
	if !ok {
		t.Fatal("the agent service cannot compact agents")
	}
	return c
}

func TestCompactReachesABridgedClaudeAgentWithItsFocus(t *testing.T) {
	agent := newBridgedClaudeAgent(t, "TASK-42")

	if err := compactor(t, agent.svc).CompactAgent(context.Background(), "TASK-42", "keep the plan"); err != nil {
		t.Fatal(err)
	}

	// The session's own compaction, never a /compact typed as a message.
	if command := agent.nextCommand(); command["type"] != "compact" || command["instructions"] != "keep the plan" {
		t.Fatalf("command = %v", command)
	}
}

func TestCompactIsRefusedWhileAClaudeAgentWorks(t *testing.T) {
	agent := newBridgedClaudeAgent(t, "TASK-42")
	agent.mod(`{"type":"turn_start","turnId":"turn-1"}`)

	err := compactor(t, agent.svc).CompactAgent(context.Background(), "TASK-42", "")

	if !errors.Is(err, ErrAgentWorking) {
		t.Fatalf("err = %v, want ErrAgentWorking", err)
	}
}

// nativeSession is a native endpoint that records the compactions asked of
// it, as a Chimera session serves them.
type nativeSession struct {
	mu     sync.Mutex
	bodies []map[string]any
	status int
}

func (n *nativeSession) compactions() []map[string]any {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]map[string]any(nil), n.bodies...)
}

func newNativeAgent(t *testing.T, taskID, kind string, session *nativeSession) (AgentService, *store.Store) {
	t.Helper()
	svc, s, _ := newDiscoveryTestService(t)
	createTask(t, s, taskID)
	ctx := context.Background()
	if err := s.InsertAgentSession(ctx, store.AgentSession{
		TaskID: taskID, TmuxSession: "legato-" + taskID, AgentKind: kind, Status: "running",
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sessions/session-1/compact" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		session.mu.Lock()
		session.bodies = append(session.bodies, body)
		status := session.status
		session.mu.Unlock()
		if status == 0 {
			status = http.StatusAccepted
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	if err := s.UpdateChimeraDiscovery(ctx, taskID, "session-1", server.URL); err != nil {
		t.Fatal(err)
	}
	return svc, s
}

func TestCompactReachesAChimeraSession(t *testing.T) {
	session := &nativeSession{}
	svc, _ := newNativeAgent(t, "TASK-7", "chimera", session)

	if err := compactor(t, svc).CompactAgent(context.Background(), "TASK-7", ""); err != nil {
		t.Fatal(err)
	}

	if got := session.compactions(); len(got) != 1 {
		t.Fatalf("compactions = %v, want one", got)
	}
}

func TestCompactIsRefusedWhileAChimeraSessionRuns(t *testing.T) {
	session := &nativeSession{status: http.StatusConflict}
	svc, _ := newNativeAgent(t, "TASK-7", "chimera", session)

	err := compactor(t, svc).CompactAgent(context.Background(), "TASK-7", "")

	if !errors.Is(err, ErrAgentWorking) {
		t.Fatalf("err = %v, want ErrAgentWorking", err)
	}
}

// Codex and Chimera compact with their own prompt only: focus words would be
// dropped without a word, so they are refused before anything is asked.
func TestCompactWithFocusIsRefusedForToolsThatCannotTakeIt(t *testing.T) {
	for _, kind := range []string{"codex", "chimera"} {
		t.Run(kind, func(t *testing.T) {
			session := &nativeSession{}
			svc, _ := newNativeAgent(t, "TASK-7", kind, session)

			err := compactor(t, svc).CompactAgent(context.Background(), "TASK-7", "keep the plan")

			if !errors.Is(err, ErrCompactFocusUnsupported) {
				t.Fatalf("err = %v, want ErrCompactFocusUnsupported", err)
			}
			if got := session.compactions(); len(got) != 0 {
				t.Fatalf("compactions = %v, want none", got)
			}
		})
	}
}

func TestCompactOfAnAgentWithoutANativeEndpointIsUnsupported(t *testing.T) {
	svc, s, _ := newDiscoveryTestService(t)
	createTask(t, s, "TASK-9")
	if err := s.InsertAgentSession(context.Background(), store.AgentSession{
		TaskID: "TASK-9", TmuxSession: "legato-TASK-9", AgentKind: "aider", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}

	err := compactor(t, svc).CompactAgent(context.Background(), "TASK-9", "")

	if !errors.Is(err, ErrCompactUnsupported) {
		t.Fatalf("err = %v, want ErrCompactUnsupported", err)
	}
}
