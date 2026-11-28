package service

import (
	"context"
	"slices"
	"testing"
)

// The offs an operator chose at spawn survive every relaunch of the agent
// (spec LEG-687 "Per-execution records and carrying offs through
// relaunches", story 47), while the global, manifest and auto-off layers
// are worked out again (story 48).

// relaunchFixture is the hook policy fixture seen as a restart fixture, so
// the restart, crash and restore helpers work on it.
func newRelaunchFixture(t *testing.T) (hookPolicyFixture, restartFixture) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	f := newHookPolicyFixture(t)
	t.Cleanup(f.svc.(*agentService).waitLaunchDeliveries)
	f.hook(t, "gate")
	f.hook(t, "picked")
	return f, restartFixture{svc: f.svc, store: f.store, tmux: f.tmux}
}

// runningWithPickedOff starts a Claude Code agent on taskID with picked
// turned off at spawn, as the spawn dialog sends it, and records its
// conversation.
func runningWithPickedOff(t *testing.T, f hookPolicyFixture, r restartFixture, taskID, cwd string) {
	t.Helper()
	f.spawn(t, taskID, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd, HooksOff: []string{"picked"}})
	r.reportConversation(t, taskID, cwd)
}

// wantPickedKept checks the newest execution of taskID has picked off at
// spawn, chosen by spawn, and gate on.
func wantPickedKept(t *testing.T, f hookPolicyFixture, taskID, before string) {
	t.Helper()
	got, err := f.svc.(ManagedHooksReader).AgentManagedHooks(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExecutionID == before {
		t.Fatalf("%s: still the execution %s; nothing was started again", taskID, before)
	}
	for _, hook := range got.Hooks {
		switch hook.Name {
		case "picked":
			if !slices.Equal(hook.Off, []string{"off:spawn"}) || !slices.Equal(hook.ChosenOff, []string{ChosenOffSpawn}) {
				t.Errorf("%s: picked = %+v, want off:spawn chosen by spawn", taskID, hook)
			}
		case "gate":
			if !hook.On {
				t.Errorf("%s: gate = %+v, want on", taskID, hook)
			}
		}
	}
	if len(got.Hooks) != 2 {
		t.Fatalf("%s: recorded %+v, want gate and picked", taskID, got.Hooks)
	}
}

func latestExecution(t *testing.T, f hookPolicyFixture, taskID string) string {
	t.Helper()
	got, err := f.svc.(ManagedHooksReader).AgentManagedHooks(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	return got.ExecutionID
}

func TestARestartKeepsTheHooksTurnedOffAtSpawn(t *testing.T) {
	ctx := context.Background()
	f, r := newRelaunchFixture(t)
	cwd := t.TempDir()
	runningWithPickedOff(t, f, r, "task-r", cwd)
	before := latestExecution(t, f, "task-r")

	if err := f.svc.(AgentRestarter).RestartAgent(ctx, "task-r", nil); err != nil {
		t.Fatal(err)
	}
	wantPickedKept(t, f, "task-r", before)
}

func TestASandboxSwitchKeepsTheHooksTurnedOffAtSpawn(t *testing.T) {
	ctx := context.Background()
	f, r := newRelaunchFixture(t)
	cwd := t.TempDir()
	runningWithPickedOff(t, f, r, "task-s", cwd)
	before := latestExecution(t, f, "task-s")

	sandboxed, err := f.svc.(SandboxSwitcher).RestartAgentSwitchingSandbox(ctx, "task-s", nil)
	if err != nil || sandboxed {
		t.Fatalf("switch = %v, %v; want it out of the sandbox", sandboxed, err)
	}
	wantPickedKept(t, f, "task-s", before)
}

func TestAnInterruptedResumeAndAFreshStartKeepTheHooksTurnedOffAtSpawn(t *testing.T) {
	for _, tc := range []struct {
		name            string
		fresh, reported bool
	}{
		{"resume", false, true},
		{"start fresh with no conversation", true, false},
		{"start fresh instead of resuming", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f, r := newRelaunchFixture(t)
			cwd := t.TempDir()
			f.spawn(t, "task-i", AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd, HooksOff: []string{"picked"}})
			if tc.reported {
				r.reportConversation(t, "task-i", cwd)
			}
			before := latestExecution(t, f, "task-i")
			r.crash()
			if _, err := r.interrupted().SweepInterruptedAgents(ctx); err != nil {
				t.Fatal(err)
			}

			if err := r.interrupted().ResumeInterruptedAgent(ctx, "task-i", tc.fresh); err != nil {
				t.Fatal(err)
			}
			wantPickedKept(t, f, "task-i", before)
		})
	}
}

func TestRestoringOrStartingAConversationAfreshKeepsTheHooksTurnedOffAtSpawn(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		name := "restore"
		if fresh {
			name = "start fresh"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f, r := newRelaunchFixture(t)
			cwd := t.TempDir()
			runningWithPickedOff(t, f, r, "task-h", cwd)
			before := latestExecution(t, f, "task-h")
			if err := f.svc.KillAgent(ctx, "task-h"); err != nil {
				t.Fatal(err)
			}

			id := r.conversationID(t, "task-h", "conversation-1")
			var err error
			if fresh {
				_, err = r.history().StartFresh(ctx, id, RestoreIntoTask, cwd)
			} else {
				err = restoreErr(r.history(), ctx, id, RestoreIntoTask)
			}
			if err != nil {
				t.Fatal(err)
			}
			wantPickedKept(t, f, "task-h", before)
		})
	}
}

func TestARestartWorksOutTheGlobalOffsAgain(t *testing.T) {
	ctx := context.Background()
	f, r := newRelaunchFixture(t)
	f.hook(t, "lifted")
	if err := f.hooks.SetEnabled(ctx, "lifted", false); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	runningWithPickedOff(t, f, r, "task-g", cwd)
	wantOffs(t, "at spawn", f.offs(t, ctx, "task-g"), map[string][]string{
		"gate": nil, "picked": {"off:spawn"}, "lifted": {"off:global"},
	})

	// Meanwhile the operator turns gate off for every agent, and lifted
	// back on.
	if err := f.hooks.SetEnabled(ctx, "gate", false); err != nil {
		t.Fatal(err)
	}
	if err := f.hooks.SetEnabled(ctx, "lifted", true); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.(AgentRestarter).RestartAgent(ctx, "task-g", nil); err != nil {
		t.Fatal(err)
	}
	wantOffs(t, "after the restart", f.offs(t, ctx, "task-g"), map[string][]string{
		"gate": {"off:global"}, "picked": {"off:spawn"}, "lifted": nil,
	})
}

func TestARestartCarriesNoOffTheAgentWasNotSpawnedWith(t *testing.T) {
	ctx := context.Background()
	f, r := newRelaunchFixture(t)
	cwd := t.TempDir()
	f.spawn(t, "task-n", AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd})
	r.reportConversation(t, "task-n", cwd)
	if err := f.svc.(AgentRestarter).RestartAgent(ctx, "task-n", nil); err != nil {
		t.Fatal(err)
	}
	wantOffs(t, "restarted", f.offs(t, ctx, "task-n"), map[string][]string{"gate": nil, "picked": nil})
}
