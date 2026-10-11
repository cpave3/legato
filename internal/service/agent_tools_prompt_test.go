package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// toolsPromptAdapter stands in for an agent tool whose sessions get Legato's
// tools but whose model is not told about them by the tool itself.
type toolsPromptAdapter struct {
	*fakeAdapter
	tools string
}

func (a toolsPromptAdapter) ToolsPrompt() string { return a.tools }

func spawnAndReadPrompt(t *testing.T, adapter AIToolAdapter, taskID string, opts ...AgentSpawnOptions) (string, bool) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: adapter})
	createTask(t, s, taskID)
	if err := svc.SpawnAgent(context.Background(), taskID, 0, 0, opts...); err != nil {
		t.Fatal(err)
	}
	path := mt.envVarsFor("legato-" + taskID)["LEGATO_ROLE_PROMPT_FILE"]
	if path == "" {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data), true
}

// A plain session of such a tool had no launch prompt at all. It gets one,
// so its agent knows the tools are there without being told by the person.
func TestSpawnAgent_ToolsPromptReachesAPlainSession(t *testing.T) {
	adapter := toolsPromptAdapter{fakeAdapter: &fakeAdapter{name: "codex-like"}, tools: "## Legato tools\n\nUse show_card without being asked."}

	prompt, ok := spawnAndReadPrompt(t, adapter, "task-1")

	if !ok {
		t.Fatal("no launch prompt was written, so the agent never learns of its tools")
	}
	if prompt != "## Legato tools\n\nUse show_card without being asked." {
		t.Fatalf("launch prompt = %q", prompt)
	}
}

func TestSpawnAgent_ToolsPromptFollowsTheRolePromptOfASwarmParticipant(t *testing.T) {
	adapter := toolsPromptAdapter{
		fakeAdapter: &fakeAdapter{name: "codex-like", rolePrompts: map[string]string{"conductor": "you are the conductor"}},
		tools:       "## Legato tools\n\nUse show_card without being asked.",
	}

	prompt, ok := spawnAndReadPrompt(t, adapter, "parent-1", AgentSpawnOptions{Role: "conductor", ParentTaskID: "parent-1", Brief: "kick off"})

	if !ok {
		t.Fatal("no launch prompt was written")
	}
	role, tools := strings.Index(prompt, "you are the conductor"), strings.Index(prompt, "## Legato tools")
	if role != 0 || tools <= role {
		t.Fatalf("launch prompt = %q, want the role prompt first and the tools guidance after it", prompt)
	}
}

func TestSpawnAgent_NoToolsPromptLeavesTheLaunchPromptAlone(t *testing.T) {
	// An adapter that has the capability but nothing to say (its sessions get
	// no tools) must not turn a prompt-less launch into one with a prompt.
	if prompt, ok := spawnAndReadPrompt(t, toolsPromptAdapter{fakeAdapter: &fakeAdapter{name: "codex-like"}}, "task-1"); ok {
		t.Fatalf("a launch prompt was written with nothing to say: %q", prompt)
	}
	// An adapter without the capability is launched as before.
	if prompt, ok := spawnAndReadPrompt(t, &fakeAdapter{name: "plain"}, "task-2"); ok {
		t.Fatalf("a launch prompt was written for an adapter with none: %q", prompt)
	}
}
