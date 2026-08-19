package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestAgentSessionCreatedLinksSessionToTask(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.CreateTask(t.Context(), store.Task{ID: "TASK-1", Title: "Task", Status: "Backlog"}); err != nil {
		t.Fatal(err)
	}

	if err := AgentSessionCreated(s, "TASK-1", "chimera-123"); err != nil {
		t.Fatal(err)
	}
	task, err := s.GetTask(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.ChimeraSessionID == nil || *task.ChimeraSessionID != "chimera-123" {
		t.Fatalf("ChimeraSessionID = %v", task.ChimeraSessionID)
	}
}

func TestAgentSessionCreatedFromHookLinksTaskNamedParent(t *testing.T) {
	s := newChimeraSessionTestStore(t)

	registered, err := AgentSessionCreatedFromHook(s, "TASK-1", "random-parent-id", strings.NewReader(`{"event":"SessionCreated","session_id":"random-parent-id","session_name":"TASK-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !registered {
		t.Fatal("matching parent session was not registered")
	}
	assertTaskChimeraSessionID(t, s, "TASK-1", "random-parent-id")
}

func TestAgentSessionCreatedFromHookIgnoresSpawnedChild(t *testing.T) {
	s := newChimeraSessionTestStore(t)
	if err := AgentSessionCreated(s, "TASK-1", "durable-parent"); err != nil {
		t.Fatal(err)
	}

	registered, err := AgentSessionCreatedFromHook(s, "TASK-1", "temporary-child", strings.NewReader(`{"event":"SessionCreated","session_id":"temporary-child"}`))
	if err != nil {
		t.Fatal(err)
	}
	if registered {
		t.Fatal("spawned child session was registered")
	}
	assertTaskChimeraSessionID(t, s, "TASK-1", "durable-parent")
}

func TestAgentSessionCreatedFromHookRejectsInvalidPayloadWithoutMutation(t *testing.T) {
	tests := map[string]string{
		"malformed JSON":      `{`,
		"wrong event":         `{"event":"Stop","session_id":"parent","session_name":"TASK-1"}`,
		"session ID mismatch": `{"event":"SessionCreated","session_id":"other","session_name":"TASK-1"}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			s := newChimeraSessionTestStore(t)
			if err := AgentSessionCreated(s, "TASK-1", "durable-parent"); err != nil {
				t.Fatal(err)
			}
			if _, err := AgentSessionCreatedFromHook(s, "TASK-1", "parent", strings.NewReader(payload)); err == nil {
				t.Fatal("expected invalid payload error")
			}
			assertTaskChimeraSessionID(t, s, "TASK-1", "durable-parent")
		})
	}
}

func newChimeraSessionTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.CreateTask(t.Context(), store.Task{ID: "TASK-1", Title: "Task", Status: "Backlog"}); err != nil {
		t.Fatal(err)
	}
	return s
}

func assertTaskChimeraSessionID(t *testing.T, s *store.Store, taskID, want string) {
	t.Helper()
	task, err := s.GetTask(t.Context(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.ChimeraSessionID == nil || *task.ChimeraSessionID != want {
		t.Fatalf("ChimeraSessionID = %v, want %q", task.ChimeraSessionID, want)
	}
}

func TestAgentSessionCreatedRejectsMissingIdentity(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := AgentSessionCreated(s, "", "session"); err == nil {
		t.Fatal("expected missing task ID error")
	}
	if err := AgentSessionCreated(s, "task", ""); err == nil {
		t.Fatal("expected missing session ID error")
	}
}
