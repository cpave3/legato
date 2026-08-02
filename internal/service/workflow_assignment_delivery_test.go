package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

type selfKickoffWorkflowAdapter struct{ *fakeAdapter }

func (a selfKickoffWorkflowAdapter) GeneralPrompt() string     { return "GENERIC-PROMPT" }
func (a selfKickoffWorkflowAdapter) LaunchIsSelfKickoff() bool { return true }

func TestSpawnAgentUsesWorkflowAssignmentAsSelfKickoffPrompt(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mt := newMockTmux()
	adapter := selfKickoffWorkflowAdapter{&fakeAdapter{name: "chimera"}}
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: adapter})
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Assignment: "WORKFLOW-ASSIGNMENT"}); err != nil {
		t.Fatal(err)
	}
	env := mt.envVarsFor("legato-task-1")
	data, err := os.ReadFile(env["LEGATO_ROLE_PROMPT_FILE"])
	if err != nil {
		t.Fatal(err)
	}
	prompt := string(data)
	if !strings.Contains(prompt, "GENERIC-PROMPT") || !strings.Contains(prompt, "WORKFLOW-ASSIGNMENT") {
		t.Fatalf("launch prompt must retain standing context and assignment: %q", data)
	}
	if lines := mt.sentLinesFor("legato-task-1"); len(lines) == 0 || !strings.Contains(lines[0], "LEGATO_ROLE_PROMPT_FILE") {
		t.Fatalf("launch lines=%v", lines)
	}
}

func TestWorkflowAgentRunnerNamesSpawnedAgent(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: selfKickoffWorkflowAdapter{&fakeAdapter{name: "chimera"}}})
	createTask(t, s, "task-1")
	runner := NewWorkflowAgentRunner(svc, s)

	if _, err := runner.SpawnWorkflowAgent(context.Background(), "task-1", "PR Review - 123", AgentSpawnOptions{Assignment: "review", ExtraEnv: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	agents, err := svc.ListAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Title != "PR Review - 123" {
		t.Fatalf("agents = %+v", agents)
	}
}
