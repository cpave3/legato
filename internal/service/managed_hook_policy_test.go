package service

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/store"
)

// hookPolicyFixture is an agent service in Legato's sandbox (the Reeve and
// a read-only checkout run only there) whose managed hooks come from a
// real ManagedHookService, with Claude Code as a tool that takes hooks and
// Codex and Chimera as tools that do not (Phase 1).
type hookPolicyFixture struct {
	svc   AgentService
	store *store.Store
	tmux  *mockTmux
	hooks *ManagedHookService
	dir   string // the managed-hooks folder
	root  string // the capability folders
}

func newHookPolicyFixture(t *testing.T) hookPolicyFixture {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "policy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	dir := filepath.Join(t.TempDir(), "managed-hooks")
	hookSvc := NewManagedHookService(dir, s)
	dataDir := t.TempDir()
	root := filepath.Join(dataDir, "capabilities")
	mt := newMockTmux()
	claude := hooks.NewClaudeCodeAdapter("legato")
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		Adapter: claude,
		Adapters: map[string]AIToolAdapter{
			"claude-code": claude, "codex": hooks.NewCodexAdapter("legato"), "chimera": hooks.NewChimeraAdapter("legato"),
		},
		Sandbox: &SandboxSettings{Backend: &fakeBackend{}, Base: SandboxPlanInput{
			Home: t.TempDir(), DataDir: dataDir, RuntimeDir: filepath.Join(t.TempDir(), "legato"), DefaultMounts: true,
		}},
		ManagedHooks:         hookSvc,
		CapabilityFolders:    CapabilityFolders{Root: root},
		DisablePeerAwareness: true, BriefKickoffDelay: time.Millisecond,
	})
	svc.(*agentService).nativeBindTimeout = time.Millisecond
	svc.(*agentService).SetBrokerEnv(testBrokerEnv(t))
	return hookPolicyFixture{svc: svc, store: s, tmux: mt, hooks: hookSvc, dir: dir, root: root}
}

// hook writes a managed hook that runs ./stop.sh at stop, whose manifest
// has extra lines added.
func (f hookPolicyFixture) hook(t *testing.T, name string, extra ...string) {
	t.Helper()
	f.hookOn(t, name, "  stop: {command: ./stop.sh}", extra...)
}

// hookOn writes a managed hook with the given events: block, with extra
// lines added to its manifest, and a script ./stop.sh.
func (f hookPolicyFixture) hookOn(t *testing.T, name, events string, extra ...string) {
	t.Helper()
	manifest := "name: " + name + "\ndescription: " + name + ".\n" + strings.Join(extra, "\n") + "\nevents:\n" + events + "\n"
	writeHookManifest(t, f.dir, name, manifest)
	if err := os.WriteFile(filepath.Join(f.dir, name, "stop.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func (f hookPolicyFixture) spawn(t *testing.T, taskID string, opt AgentSpawnOptions) {
	t.Helper()
	if _, err := f.store.GetTask(context.Background(), taskID); err != nil {
		createTask(t, f.store, taskID)
	}
	if opt.WorkingDir == "" {
		opt.WorkingDir = t.TempDir()
	}
	if err := f.svc.SpawnAgent(context.Background(), taskID, 0, 0, opt); err != nil {
		t.Fatal(err)
	}
}

// offs is what an agent's spawn recorded: each hook's off reasons, none
// for a hook that is on.
func (f hookPolicyFixture) offs(t *testing.T, ctx context.Context, taskID string) map[string][]string {
	t.Helper()
	got, err := f.svc.(ManagedHooksReader).AgentManagedHooks(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Recorded {
		t.Fatalf("%s: nothing recorded", taskID)
	}
	offs := map[string][]string{}
	for _, hook := range got.Hooks {
		if hook.On != (len(hook.Off) == 0) {
			t.Fatalf("%s: %+v is on and off at once", taskID, hook)
		}
		offs[hook.Name] = append([]string{}, hook.Off...)
	}
	return offs
}

func wantOffs(t *testing.T, who string, got map[string][]string, want map[string][]string) {
	t.Helper()
	for name, reasons := range want {
		if reasons == nil {
			reasons = []string{}
		}
		if g, ok := got[name]; !ok || !slices.Equal(g, reasons) {
			t.Errorf("%s: %s off = %v (recorded %v), want %v", who, name, g, ok, reasons)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%s: recorded %v, want exactly %v", who, got, want)
	}
}

func (f hookPolicyFixture) notes(t *testing.T, taskID string) []store.TaskNote {
	t.Helper()
	notes, err := f.store.ListTaskNotes(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	return notes
}

// given names the hooks in a session's hooks bundle.
func (f hookPolicyFixture) given(t *testing.T, session string) []string {
	t.Helper()
	env := f.tmux.envVarsFor(session)
	entries, _ := os.ReadDir(filepath.Join(f.root, env["LEGATO_EXECUTION_ID"], "claude-code", "legato-hooks", "copies"))
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestEveryLayerOfTheHookPolicyIsRecordedAndAnyOffWins(t *testing.T) {
	ctx := context.Background()
	f := newHookPolicyFixture(t)
	f.hook(t, "gate")
	f.hook(t, "quiet")
	f.hook(t, "codex-only", "agent_tools: [codex]")
	f.hook(t, "twice-off", "agent_tools: [chimera]")
	f.hook(t, "unchecked")
	writeHookManifest(t, f.dir, "broken", "name: other\ndescription: Typo.\nevents:\n  stop: {command: ./a}\n")
	for _, name := range []string{"quiet", "twice-off"} {
		if err := f.hooks.SetEnabled(ctx, name, false); err != nil {
			t.Fatal(err)
		}
	}

	f.spawn(t, "task-a", AgentSpawnOptions{HooksOff: []string{"unchecked", "no-such-hook"}})

	wantOffs(t, "task-a", f.offs(t, ctx, "task-a"), map[string][]string{
		"gate":       nil,
		"quiet":      {"off:global"},
		"codex-only": {"off:unsupported-tool"},
		"twice-off":  {"off:global", "off:unsupported-tool"},
		"unchecked":  {"off:spawn"},
		"broken":     {"off:invalid"},
	})
	if got := f.given(t, "legato-task-a"); !slices.Equal(got, []string{"gate"}) {
		t.Fatalf("given %v, want only the hook that is on", got)
	}
	recorded, err := f.svc.(ManagedHooksReader).AgentManagedHooks(ctx, "task-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, hook := range recorded.Hooks {
		if want := hook.Name == "unchecked"; slices.Equal(hook.ChosenOff, []string{"spawn"}) != want {
			t.Fatalf("%s chosen off %v", hook.Name, hook.ChosenOff)
		}
	}
	notes := f.notes(t, "task-a")
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "no-such-hook") || notes[0].Source != "legato" {
		t.Fatalf("notes = %+v, want one saying no-such-hook was ignored", notes)
	}

	// A change on disk or to the global state reaches the next spawn
	// without a restart, and leaves the running agent's records alone.
	if err := f.hooks.SetEnabled(ctx, "gate", false); err != nil {
		t.Fatal(err)
	}
	f.hook(t, "added")
	f.spawn(t, "task-b", AgentSpawnOptions{})
	next := f.offs(t, ctx, "task-b")
	if !slices.Equal(next["gate"], []string{"off:global"}) || len(next["added"]) != 0 || len(next["unchecked"]) != 0 {
		t.Fatalf("the next spawn recorded %v", next)
	}
	if _, ok := next["added"]; !ok {
		t.Fatal("a hook added on disk did not reach the next spawn")
	}
	if first := f.offs(t, ctx, "task-a"); len(first["gate"]) != 0 || len(first) != 6 {
		t.Fatalf("the running agent's records changed: %v", first)
	}
}

func TestReadOnlyReviewerReeveFindOutAndWorkflowAgentsAreOffByThemselves(t *testing.T) {
	ctx := context.Background()
	f := newHookPolicyFixture(t)
	f.hook(t, "gate")
	f.hook(t, "looker", "read_only: true")

	// A review loop: the author keeps its hooks, the reviewer slot does not.
	f.spawn(t, "task-r", AgentSpawnOptions{})
	f.spawn(t, "task-r", AgentSpawnOptions{Slot: SlotReviewer, ReadOnlyCheckout: true})
	wantOffs(t, "author", f.offs(t, ctx, "task-r"), map[string][]string{"gate": nil, "looker": nil})
	wantOffs(t, "reviewer", f.offs(t, WithAgentSlot(ctx, SlotReviewer), "task-r"), map[string][]string{
		"gate": {"off:read-only", "off:reviewer"}, "looker": nil,
	})

	f.spawn(t, "task-ro", AgentSpawnOptions{ReadOnlyCheckout: true})
	wantOffs(t, "read-only", f.offs(t, ctx, "task-ro"), map[string][]string{"gate": {"off:read-only"}, "looker": nil})

	reeve, err := f.store.CreateReeveTask(ctx, "Reeve")
	if err != nil {
		t.Fatal(err)
	}
	f.spawn(t, reeve, AgentSpawnOptions{Ephemeral: true})
	wantOffs(t, "the Reeve", f.offs(t, ctx, reeve), map[string][]string{"gate": {"off:reeve"}, "looker": {"off:reeve"}})

	f.spawn(t, "task-f", AgentSpawnOptions{HandoffPurpose: HandoffFindOut})
	wantOffs(t, "find-out", f.offs(t, ctx, "task-f"), map[string][]string{"gate": {"off:find-out"}, "looker": {"off:find-out"}})

	f.spawn(t, "task-w", AgentSpawnOptions{ExecutionKind: store.ExecutionWorkflowAgent, WorkflowRunID: "run-1", WorkflowAttemptID: "attempt-1"})
	wantOffs(t, "workflow", f.offs(t, ctx, "task-w"), map[string][]string{"gate": {"off:workflow"}, "looker": {"off:workflow"}})
	if err := f.svc.KillAgent(ctx, "task-w"); err != nil {
		t.Fatal(err)
	}
	f.spawn(t, "task-w", AgentSpawnOptions{})
	wantOffs(t, "the same task outside the workflow", f.offs(t, ctx, "task-w"), map[string][]string{"gate": nil, "looker": nil})
}

func TestAFindOutHandoffsAgentStaysOffWhenStartedAgain(t *testing.T) {
	ctx := context.Background()
	f := newHookPolicyFixture(t)
	f.hook(t, "gate")
	if err := f.store.CreateHandoffCard(ctx, store.Task{ID: "task-h", Title: "Find out", Status: "Backlog"}, nil, store.Handoff{
		MainInstanceID: "main", HandoffID: "h-1", TaskID: "task-h", Purpose: HandoffFindOut, Kind: HandoffNewBranch,
		State: store.HandoffInProgress, CreatedAt: "2026-11-29T00:00:00Z", UpdatedAt: "2026-11-29T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	// A restart or resume does not say the purpose again.
	f.spawn(t, "task-h", AgentSpawnOptions{})
	wantOffs(t, "restarted find-out", f.offs(t, ctx, "task-h"), map[string][]string{"gate": {"off:find-out"}})
}

func TestAToolThatCannotRunHooksStartsWithNoneAndOneSkipNote(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"codex", "chimera"} {
		t.Run(kind, func(t *testing.T) {
			f := newHookPolicyFixture(t)
			f.spawn(t, "task-none", AgentSpawnOptions{AgentKind: kind})
			if notes := f.notes(t, "task-none"); len(notes) != 0 {
				t.Fatalf("with no managed-hooks folder, notes = %+v", notes)
			}

			f.hook(t, "gate")
			f.hook(t, "claude-only", "agent_tools: [claude-code]")
			f.spawn(t, "task-u", AgentSpawnOptions{AgentKind: kind})
			if !f.tmux.sessions["legato-task-u"] {
				t.Fatal("the agent did not start")
			}
			wantOffs(t, kind, f.offs(t, ctx, "task-u"), map[string][]string{
				"gate": {"off:unsupported-tool"}, "claude-only": {"off:unsupported-tool"},
			})
			env := f.tmux.envVarsFor("legato-task-u")
			if pathExists(filepath.Join(f.root, env["LEGATO_EXECUTION_ID"])) {
				t.Fatal("a tool that cannot run hooks was given a bundle")
			}
			notes := f.notes(t, "task-u")
			if len(notes) != 1 || notes[0].Source != "legato" || !strings.Contains(notes[0].Body, kind) || !strings.Contains(notes[0].Body, "gate") || strings.Contains(notes[0].Body, "claude-only") {
				t.Fatalf("notes = %+v, want one skip note naming %s and gate", notes, kind)
			}
		})
	}
}

func TestAnAgentFromBeforeManagedHooksHasNoneRecorded(t *testing.T) {
	ctx := context.Background()
	f := newHookPolicyFixture(t)
	f.spawn(t, "task-old", AgentSpawnOptions{})
	got, err := f.svc.(ManagedHooksReader).AgentManagedHooks(ctx, "task-old")
	if err != nil {
		t.Fatal(err)
	}
	if got.Recorded || got.ExecutionID == "" || !reflect.DeepEqual(got.Hooks, []AgentManagedHook{}) {
		t.Fatalf("got %+v, want none recorded", got)
	}
}

func TestAStoppedAgentsManagedHooksCanStillBeRead(t *testing.T) {
	ctx := context.Background()
	f := newHookPolicyFixture(t)
	f.hook(t, "gate")
	f.spawn(t, "task-s", AgentSpawnOptions{})
	f.spawn(t, "task-s", AgentSpawnOptions{Slot: SlotReviewer, ReadOnlyCheckout: true})
	if err := f.svc.(SlotAgents).KillAgentInSlot(ctx, "task-s", SlotReviewer); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.KillAgent(ctx, "task-s"); err != nil {
		t.Fatal(err)
	}
	wantOffs(t, "stopped author", f.offs(t, ctx, "task-s"), map[string][]string{"gate": nil})
	wantOffs(t, "stopped reviewer", f.offs(t, WithAgentSlot(ctx, SlotReviewer), "task-s"), map[string][]string{
		"gate": {"off:read-only", "off:reviewer"},
	})
	wantOffs(t, "stopped reviewer by address", f.offs(t, ctx, "task-s_"+SlotReviewer), map[string][]string{
		"gate": {"off:read-only", "off:reviewer"},
	})
}

func TestAStoppedAgentWithNoHooksIsNotMistakenForAnEarlierOne(t *testing.T) {
	ctx := context.Background()
	f := newHookPolicyFixture(t)
	f.hook(t, "gate")
	f.spawn(t, "task-e", AgentSpawnOptions{})
	if err := f.svc.KillAgent(ctx, "task-e"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(f.dir, "gate")); err != nil {
		t.Fatal(err)
	}
	f.spawn(t, "task-e", AgentSpawnOptions{})
	running, err := f.svc.(ManagedHooksReader).AgentManagedHooks(ctx, "task-e")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.KillAgent(ctx, "task-e"); err != nil {
		t.Fatal(err)
	}
	stopped, err := f.svc.(ManagedHooksReader).AgentManagedHooks(ctx, "task-e")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.ExecutionID != running.ExecutionID || stopped.Recorded || len(stopped.Hooks) != 0 {
		t.Fatalf("after stopping, got %+v, want the newest execution %s with none recorded", stopped, running.ExecutionID)
	}
}
