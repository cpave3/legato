package cli

import (
	"path/filepath"
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
