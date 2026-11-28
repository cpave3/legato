package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/skills"
	"github.com/cpave3/legato/internal/engine/store"
)

// recordingCapabilities is a CapabilitySource that gives the same
// capabilities to every agent and remembers what it was asked.
type recordingCapabilities struct {
	given    AgentCapabilities
	requests []CapabilityRequest
}

func (r *recordingCapabilities) Capabilities(_ context.Context, req CapabilityRequest) (AgentCapabilities, error) {
	r.requests = append(r.requests, req)
	return r.given, nil
}

// newSandboxedCapabilityService is an agent service in Legato's sandbox
// (the Reeve and a read-only checkout run only there) with source as its
// capability source and claude as its only tool.
func newSandboxedCapabilityService(t *testing.T, source CapabilitySource, claude AIToolAdapter) (AgentService, *store.Store, *mockTmux, string) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "kinds.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	dataDir := t.TempDir()
	root := filepath.Join(dataDir, "capabilities")
	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		Adapter:  claude,
		Adapters: map[string]AIToolAdapter{"claude-code": claude},
		Sandbox: &SandboxSettings{Backend: &fakeBackend{}, Base: SandboxPlanInput{
			Home: t.TempDir(), DataDir: dataDir, RuntimeDir: filepath.Join(t.TempDir(), "legato"), DefaultMounts: true,
		}},
		Capabilities:         source,
		CapabilityFolders:    CapabilityFolders{Root: root},
		DisablePeerAwareness: true, BriefKickoffDelay: time.Millisecond,
	})
	svc.(*agentService).nativeBindTimeout = time.Millisecond
	svc.(*agentService).SetBrokerEnv(testBrokerEnv(t))
	return svc, s, mt, root
}

func TestTheCapabilityRequestSaysWhatTheSpawnKnowsOfTheAgent(t *testing.T) {
	ctx := context.Background()
	source := &recordingCapabilities{}
	svc, s, _, _ := newSandboxedCapabilityService(t, source, hooks.NewClaudeCodeAdapter("legato"))

	createTask(t, s, "task-w")
	if err := s.SetCodeContext(ctx, "task-w", &store.CodeContext{Repo: "Acme/Widget"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, "task-w", 0, 0, AgentSpawnOptions{
		WorkingDir: t.TempDir(), ReadOnlyCheckout: true, HandoffPurpose: HandoffFindOut,
		ExecutionKind: store.ExecutionWorkflowAgent, WorkflowRunID: "run-1", WorkflowAttemptID: "attempt-1",
		ExtraEnv: map[string]string{"LEGATO_WORKFLOW_RUN_ID": "run-1", "LEGATO_WORKFLOW_STEP_ID": "build", "LEGATO_WORKFLOW_ATTEMPT_ID": "attempt-1"},
	}); err != nil {
		t.Fatal(err)
	}
	reeveTask, err := s.CreateReeveTask(ctx, "Reeve")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, reeveTask, 0, 0, AgentSpawnOptions{WorkingDir: t.TempDir(), Ephemeral: true}); err != nil {
		t.Fatal(err)
	}
	createTask(t, s, "task-plain")
	if err := svc.SpawnAgent(ctx, "task-plain", 0, 0, AgentSpawnOptions{WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}

	if len(source.requests) != 3 {
		t.Fatalf("requests = %+v, want one per spawn", source.requests)
	}
	w, reeve, plain := source.requests[0], source.requests[1], source.requests[2]
	if w.Repository != "github.com/acme/widget" || w.WorkflowRunID != "run-1" || w.WorkflowAttemptID != "attempt-1" || w.WorkflowStepID != "build" ||
		!w.ReadOnly || w.Reeve || w.HandoffPurpose != HandoffFindOut || w.TaskID != "task-w" || w.ExecutionID == "" {
		t.Fatalf("the workflow's read-only find-out spawn asked %+v", w)
	}
	if !reeve.Reeve || reeve.TaskID != reeveTask {
		t.Fatalf("the Reeve's spawn asked %+v", reeve)
	}
	plain.TaskID, plain.ExecutionID, plain.AgentKind, plain.Slot, plain.WorkDir = "", "", "", "", ""
	if plain != (CapabilityRequest{}) {
		t.Fatalf("a plain spawn outside a repository asked %+v, want nothing beyond who it is", plain)
	}
}

// hookingClaude is Claude Code with a HooksAdapter of the test's: no real
// adapter takes managed hooks yet. Its bundle is a folder legato-hooks it
// adds to CLAUDE_CODE_PLUGIN_DIRS; with fail, it writes part of it, changes
// env, and fails.
type hookingClaude struct {
	*hooks.ClaudeCodeAdapter
	fail   bool
	events [][]string
}

func (h *hookingClaude) HooksBundle(root string, events []string, env map[string]string) (string, error) {
	h.events = append(h.events, events)
	dir := filepath.Join(root, "legato-hooks", "hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	env["CLAUDE_CODE_PLUGIN_DIRS"] += string(os.PathListSeparator) + filepath.Dir(dir)
	if h.fail {
		return "", errors.New("the disk is full")
	}
	return dir, nil
}

func writeManagedHook(t *testing.T, name string, events ...string) ManagedHook {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return ManagedHook{Name: name, Path: dir, Events: events}
}

func TestAHooksBundleThatFailsLeavesTheSkillsAndTheAgentStarts(t *testing.T) {
	ctx := context.Background()
	skill := writeLegatoSkill(t, t.TempDir(), "tdd", "Tests first.")
	source := &recordingCapabilities{given: AgentCapabilities{
		Skills: []skills.Skill{{Name: "tdd", Description: "Tests first.", Path: skill}},
		Hooks:  []ManagedHook{writeManagedHook(t, "codeeval", "stop")},
	}}
	claude := &hookingClaude{ClaudeCodeAdapter: hooks.NewClaudeCodeAdapter("legato"), fail: true}
	svc, s, mt, root := newSandboxedCapabilityService(t, source, claude)
	createTask(t, s, "task-h")
	if err := svc.SpawnAgent(ctx, "task-h", 0, 0, AgentSpawnOptions{WorkingDir: t.TempDir()}); err != nil {
		t.Fatalf("a hooks bundle that failed stopped the agent: %v", err)
	}
	if !mt.sessions["legato-task-h"] {
		t.Fatal("the agent did not start")
	}
	env := mt.envVarsFor("legato-task-h")
	tool := filepath.Join(root, env["LEGATO_EXECUTION_ID"], "claude-code")
	plugin := filepath.Join(tool, "legato-skills")
	if !pathExists(filepath.Join(plugin, "skills", "tdd", "SKILL.md")) {
		t.Fatal("the skills went with the hooks bundle that failed")
	}
	if dirs := filepath.SplitList(env["CLAUDE_CODE_PLUGIN_DIRS"]); !slices.Contains(dirs, plugin) || slices.Contains(dirs, filepath.Join(tool, "legato-hooks")) {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q, want the skills plugin and not the failed hooks bundle", env["CLAUDE_CODE_PLUGIN_DIRS"])
	}
	if pathExists(filepath.Join(tool, "legato-hooks")) {
		t.Fatal("the failed hooks bundle was left behind")
	}
}

func TestHooksAreGivenToAToolThatTakesThemEvenWithNoSkills(t *testing.T) {
	ctx := context.Background()
	source := &recordingCapabilities{given: AgentCapabilities{Hooks: []ManagedHook{
		writeManagedHook(t, "codeeval", "stop"), writeManagedHook(t, "lint", "stop", "session_start"),
	}}}
	claude := &hookingClaude{ClaudeCodeAdapter: hooks.NewClaudeCodeAdapter("legato")}
	svc, s, mt, root := newSandboxedCapabilityService(t, source, claude)
	createTask(t, s, "task-n")
	if err := svc.SpawnAgent(ctx, "task-n", 0, 0, AgentSpawnOptions{WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	env := mt.envVarsFor("legato-task-n")
	tool := filepath.Join(root, env["LEGATO_EXECUTION_ID"], "claude-code")
	bundle := filepath.Join(tool, "legato-hooks")
	if !slices.Contains(filepath.SplitList(env["CLAUDE_CODE_PLUGIN_DIRS"]), bundle) {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q, want the hooks bundle", env["CLAUDE_CODE_PLUGIN_DIRS"])
	}
	if len(claude.events) != 1 || !slices.Equal(claude.events[0], []string{"session_start", "stop"}) {
		t.Fatalf("events = %v, want each event the hooks use once", claude.events)
	}
	for _, name := range []string{"codeeval", "lint"} {
		info, err := os.Stat(filepath.Join(bundle, "hooks", name, "run"))
		if err != nil || info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("%s's run = %v, %v; want an executable copy", name, info, err)
		}
	}
	if pathExists(filepath.Join(tool, "legato-skills")) {
		t.Fatal("with no skills, the agent got a skills bundle")
	}
}

func TestAToolThatCannotTakeHooksIsGivenNone(t *testing.T) {
	ctx := context.Background()
	source := &recordingCapabilities{given: AgentCapabilities{Hooks: []ManagedHook{writeManagedHook(t, "codeeval", "stop")}}}
	svc, s, mt, root := newSandboxedCapabilityService(t, source, hooks.NewClaudeCodeAdapter("legato"))
	createTask(t, s, "task-u")
	if err := svc.SpawnAgent(ctx, "task-u", 0, 0, AgentSpawnOptions{WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	env := mt.envVarsFor("legato-task-u")
	if pathExists(filepath.Join(root, env["LEGATO_EXECUTION_ID"])) {
		t.Fatal("a tool that takes no hooks got a capability folder")
	}
	for key, value := range env {
		if strings.Contains(value, root) {
			t.Fatalf("%s = %q names the capability folders", key, value)
		}
	}
}
