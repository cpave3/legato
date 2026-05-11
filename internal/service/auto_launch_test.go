package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// fakeAdapter implements AIToolAdapter + LaunchCommandAdapter + RolePromptingAdapter
// for testing the auto-launch flow.
type fakeAdapter struct {
	name        string
	rolePrompts map[string]string
	launchFn    func(env map[string]string, brief, tier string) string
}

func (f *fakeAdapter) Name() string                                 { return f.name }
func (f *fakeAdapter) InstallHooks(string) error                    { return nil }
func (f *fakeAdapter) UninstallHooks(string) error                  { return nil }
func (f *fakeAdapter) EnvVars(taskID, socket string) map[string]string {
	return map[string]string{"LEGATO_TASK_ID": taskID}
}
func (f *fakeAdapter) RoleSystemPrompt(role string) string {
	if f.rolePrompts == nil {
		return ""
	}
	return f.rolePrompts[role]
}
func (f *fakeAdapter) LaunchCommand(env map[string]string, brief, tier string) string {
	if f.launchFn != nil {
		return f.launchFn(env, brief, tier)
	}
	if _, ok := env["LEGATO_ROLE_PROMPT_FILE"]; !ok {
		return ""
	}
	return `fake-tool --prompt "$(cat $LEGATO_ROLE_PROMPT_FILE)"`
}

func TestSpawnAgent_AutoLaunchHappyPath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	workDir := t.TempDir()
	mt := newMockTmux()
	adapter := &fakeAdapter{
		name:        "fake",
		rolePrompts: map[string]string{"conductor": "you are the conductor"},
	}
	svc := NewAgentService(s, mt, workDir, AgentServiceOptions{Adapter: adapter})
	ctx := context.Background()
	createTask(t, s, "parent-1")

	if err := svc.SpawnAgent(ctx, "parent-1", 0, 0, AgentSpawnOptions{
		Role:         "conductor",
		ParentTaskID: "parent-1",
		Brief:        "kick off the swarm",
	}); err != nil {
		t.Fatal(err)
	}

	sessionName := "legato-parent-1"
	env := mt.envVarsFor(sessionName)

	// Role prompt and brief should be on disk, paths surfaced via env.
	rolePath := env["LEGATO_ROLE_PROMPT_FILE"]
	briefPath := env["LEGATO_BRIEF_FILE"]
	if rolePath == "" {
		t.Fatal("LEGATO_ROLE_PROMPT_FILE not set")
	}
	if briefPath == "" {
		t.Fatal("LEGATO_BRIEF_FILE not set")
	}
	if data, err := os.ReadFile(rolePath); err != nil || string(data) != "you are the conductor" {
		t.Errorf("role prompt file = %q (err %v)", string(data), err)
	}
	if data, err := os.ReadFile(briefPath); err != nil || string(data) != "kick off the swarm" {
		t.Errorf("brief file = %q (err %v)", string(data), err)
	}

	lines := mt.sentLinesFor(sessionName)
	if len(lines) < 1 {
		t.Fatalf("sentLines empty, want at least one launch line")
	}
	if !strings.Contains(lines[0], `fake-tool --prompt "$(cat $LEGATO_ROLE_PROMPT_FILE)"`) {
		t.Errorf("launch line = %q", lines[0])
	}

	// Brief kickoff message should arrive asynchronously after the launch.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(mt.sentLinesFor(sessionName)) >= 2 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if len(mt.sentLinesFor(sessionName)) < 2 {
		t.Fatalf("expected brief kickoff send-keys, got only %v", mt.sentLinesFor(sessionName))
	}
	if mt.sentLinesFor(sessionName)[1] != briefKickoffMessage {
		t.Errorf("kickoff line = %q, want %q", mt.sentLinesFor(sessionName)[1], briefKickoffMessage)
	}

	if env["LEGATO_AGENT_ROLE"] != "conductor" {
		t.Errorf("LEGATO_AGENT_ROLE = %q", env["LEGATO_AGENT_ROLE"])
	}
	if env["LEGATO_PARENT_TASK_ID"] != "parent-1" {
		t.Errorf("LEGATO_PARENT_TASK_ID = %q", env["LEGATO_PARENT_TASK_ID"])
	}
}

func TestSpawnAgent_NoAdapterFallsBackToShell(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "task-1")

	if err := svc.SpawnAgent(ctx, "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	if len(mt.sentLinesFor("legato-task-1")) != 0 {
		t.Errorf("expected no auto-launch when no adapter, got %v", mt.sentLinesFor("legato-task-1"))
	}
}

func TestSpawnAgent_AdapterWithoutLaunchCommandSkipsAutoLaunch(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	// Adapter that implements EnvVars but NOT LaunchCommandAdapter.
	type minimalAdapter struct{ *fakeAdapter }
	// Wrap fakeAdapter via embedding inversion: a brand-new struct that
	// only implements AIToolAdapter, not the launcher interface.
	plain := struct {
		AIToolAdapter
	}{
		AIToolAdapter: simpleAdapter{name: "plain"},
	}
	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: plain})
	ctx := context.Background()
	createTask(t, s, "task-1")

	if err := svc.SpawnAgent(ctx, "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	if len(mt.sentLinesFor("legato-task-1")) != 0 {
		t.Errorf("expected no auto-launch when adapter doesn't implement LaunchCommand, got %v", mt.sentLinesFor("legato-task-1"))
	}
}

func TestSpawnAgent_AdapterWithoutRolePromptSkipsLaunch(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mt := newMockTmux()
	adapter := &fakeAdapter{name: "fake"} // no rolePrompts → returns "" → empty launch
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: adapter})
	ctx := context.Background()
	createTask(t, s, "task-1")

	if err := svc.SpawnAgent(ctx, "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	if len(mt.sentLinesFor("legato-task-1")) != 0 {
		t.Errorf("expected no auto-launch when role prompt empty, got %v", mt.sentLinesFor("legato-task-1"))
	}
}

// simpleAdapter is a minimal AIToolAdapter implementation that does not
// implement RolePromptingAdapter or LaunchCommandAdapter.
type simpleAdapter struct{ name string }

func (s simpleAdapter) Name() string                                 { return s.name }
func (s simpleAdapter) InstallHooks(string) error                    { return nil }
func (s simpleAdapter) UninstallHooks(string) error                  { return nil }
func (s simpleAdapter) EnvVars(taskID, socket string) map[string]string {
	return map[string]string{"LEGATO_TASK_ID": taskID}
}

// recordingPublisher captures EventAgentDied calls for assertions.
type recordingPublisher struct {
	calls []recordedDied
}

type recordedDied struct {
	taskID, parentTaskID, subtaskID, role string
}

func (r *recordingPublisher) PublishAgentDied(taskID, parentTaskID, subtaskID, role string) {
	r.calls = append(r.calls, recordedDied{taskID, parentTaskID, subtaskID, role})
}

// selfKickoffAdapter implements LaunchSelfKickoff returning true — i.e. the
// launch command already is the agent's first user turn (chimera-style).
type selfKickoffAdapter struct{ *fakeAdapter }

func (selfKickoffAdapter) LaunchIsSelfKickoff() bool { return true }

func TestSpawnAgent_LaunchSelfKickoffSuppressesBriefMessage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mt := newMockTmux()
	adapter := selfKickoffAdapter{&fakeAdapter{
		name:        "chimera-like",
		rolePrompts: map[string]string{"conductor": "you are the conductor"},
	}}
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: adapter})
	ctx := context.Background()
	createTask(t, s, "parent-1")

	if err := svc.SpawnAgent(ctx, "parent-1", 0, 0, AgentSpawnOptions{
		Role:         "conductor",
		ParentTaskID: "parent-1",
		Brief:        "kick off the swarm",
	}); err != nil {
		t.Fatal(err)
	}

	sessionName := "legato-parent-1"

	// Wait long enough for the kickoff debounce to fire if it were going to.
	time.Sleep(500 * time.Millisecond)

	for _, line := range mt.sentLinesFor(sessionName) {
		if line == briefKickoffMessage {
			t.Errorf("LaunchSelfKickoff=true should suppress the brief kickoff send-keys; saw %q", line)
		}
	}
}

// preambleAdapter implements RolePromptPreambleAdapter — the preamble must be
// prepended to the role-prompt file written to disk.
type preambleAdapter struct {
	*fakeAdapter
	preamble string
}

func (p preambleAdapter) RolePromptPreamble() string { return p.preamble }

func TestSpawnAgent_RolePromptPreambleIsPrepended(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mt := newMockTmux()
	adapter := preambleAdapter{
		fakeAdapter: &fakeAdapter{
			name:        "chimera-like",
			rolePrompts: map[string]string{"worker": "ROLE BODY"},
		},
		preamble: "SANDBOX-NOTE",
	}
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: adapter})
	ctx := context.Background()
	createTask(t, s, "task-1")

	if err := svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{
		Role:         "worker",
		ParentTaskID: "parent-x",
		SubtaskID:    "st-x",
		Brief:        "do the thing",
	}); err != nil {
		t.Fatal(err)
	}

	rolePath := mt.envVarsFor("legato-task-1")["LEGATO_ROLE_PROMPT_FILE"]
	if rolePath == "" {
		t.Fatal("LEGATO_ROLE_PROMPT_FILE not set")
	}
	data, err := os.ReadFile(rolePath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "SANDBOX-NOTE") {
		t.Errorf("role prompt missing preamble: %q", body)
	}
	if !strings.Contains(body, "ROLE BODY") {
		t.Errorf("role prompt missing role body: %q", body)
	}
	preIdx := strings.Index(body, "SANDBOX-NOTE")
	bodyIdx := strings.Index(body, "ROLE BODY")
	if preIdx >= bodyIdx {
		t.Errorf("preamble should come before role body; preamble at %d, body at %d", preIdx, bodyIdx)
	}
}

func TestKillAgent_PublishesEventAgentDied(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mt := newMockTmux()
	pub := &recordingPublisher{}
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{EventBus: pub})
	ctx := context.Background()
	createTask(t, s, "task-1")

	if err := svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{
		Role:         "backend",
		ParentTaskID: "parent-x",
		SubtaskID:    "st-1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.KillAgent(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}

	if len(pub.calls) != 1 {
		t.Fatalf("PublishAgentDied calls = %d, want 1", len(pub.calls))
	}
	got := pub.calls[0]
	if got.taskID != "task-1" || got.parentTaskID != "parent-x" || got.subtaskID != "st-1" || got.role != "backend" {
		t.Errorf("payload = %+v", got)
	}
}
