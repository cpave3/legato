package service

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// writeFakeCheckout makes dir a checkout on branch at commit, as git's
// files, so no git is needed to read it.
func writeFakeCheckout(t *testing.T, dir, branch, commit string) {
	t.Helper()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/"+branch+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "refs", "heads", branch), []byte(commit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readJSONFile(t *testing.T, path string, into any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// readHookEnv reads a hook.env: KEY=VALUE lines.
func readHookEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	vars := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok {
			t.Fatalf("%s: line %q is not KEY=VALUE", path, scanner.Text())
		}
		vars[key] = value
	}
	return vars
}

// claudeEvents names the Claude Code events a hooks bundle registers, and
// the runner events each entry passes.
func claudeEvents(t *testing.T, bundle string) map[string][]string {
	t.Helper()
	var file struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	readJSONFile(t, filepath.Join(bundle, "hooks", "hooks.json"), &file)
	out := map[string][]string{}
	for event, groups := range file.Hooks {
		for _, group := range groups {
			for _, hook := range group.Hooks {
				_, after, _ := strings.Cut(hook.Command, "--event ")
				name, _, _ := strings.Cut(after, " ")
				out[event] = append(out[event], name)
			}
		}
		slices.Sort(out[event])
	}
	return out
}

func (f hookPolicyFixture) bundle(t *testing.T, session string) (bundle, executionID string, env map[string]string) {
	t.Helper()
	env = f.tmux.envVarsFor(session)
	executionID = env["LEGATO_EXECUTION_ID"]
	return filepath.Join(f.root, executionID, "claude-code", "legato-hooks"), executionID, env
}

func TestAClaudeCodeAgentWithAHookLoadsItsExecutionsHooksPlugin(t *testing.T) {
	ctx := context.Background()
	f := newHookPolicyFixture(t)
	f.hookOn(t, "gate", "  stop: {command: ./stop.sh, timeout: 300}")
	work := t.TempDir()
	writeFakeCheckout(t, work, "feature", "1111111111111111111111111111111111111111")
	createTask(t, f.store, "task-a")
	if err := f.store.SetCodeContext(ctx, "task-a", &store.CodeContext{Repo: "Acme/Widget"}); err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC().Add(-time.Second)
	f.spawn(t, "task-a", AgentSpawnOptions{WorkingDir: work})

	bundle, executionID, env := f.bundle(t, "legato-task-a")
	if !slices.Contains(filepath.SplitList(env["CLAUDE_CODE_PLUGIN_DIRS"]), bundle) {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q, want the execution's legato-hooks plugin %s", env["CLAUDE_CODE_PLUGIN_DIRS"], bundle)
	}
	if got := claudeEvents(t, bundle); !reflect.DeepEqual(got, map[string][]string{
		"SessionStart": {"canary"}, "UserPromptSubmit": {"turn_reset"}, "Stop": {"stop"},
	}) {
		t.Fatalf("hooks.json registers %v, want the stop entry, the canary and the prompt entry", got)
	}
	wantOffs(t, "task-a", f.offs(t, ctx, "task-a"), map[string][]string{"gate": nil})
	if notes := f.notes(t, "task-a"); len(notes) != 0 {
		t.Fatalf("notes = %+v, want none", notes)
	}

	copyDir := filepath.Join(bundle, "copies", "gate")
	if info, err := os.Stat(filepath.Join(copyDir, "stop.sh")); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("the hook's copy: %v, %v; want an executable stop.sh", info, err)
	}
	seedPath := filepath.Join(bundle, "seed.json")
	stateDir := "/tmp/legato-hooks/" + executionID + "/gate"
	envPath := filepath.Join(bundle, "env", "gate.env")
	if got := readHookEnv(t, envPath); !reflect.DeepEqual(got, map[string]string{
		"LEGATO_HOOK_NAME": "gate", "LEGATO_HOOK_DIR": copyDir, "LEGATO_HOOK_STATE_DIR": stateDir, "LEGATO_HOOK_SEED": seedPath,
	}) {
		t.Fatalf("hook.env = %v", got)
	}

	var seed hookSeed
	readJSONFile(t, seedPath, &seed)
	seedTime, err := time.Parse(time.RFC3339, seed.Time)
	if err != nil || seedTime.Before(before) || seedTime.After(time.Now().Add(time.Second)) {
		t.Fatalf("seed time %q: %v", seed.Time, err)
	}
	seed.Time = ""
	if want := (hookSeed{Version: 1, Head: "1111111111111111111111111111111111111111", Branch: "feature", Repo: "github.com/acme/widget",
		TaskID: "task-a", ExecutionID: executionID, WorkDir: work}); seed != want {
		t.Fatalf("seed = %+v, want %+v", seed, want)
	}

	var hookCtx hookContext
	readJSONFile(t, filepath.Join(bundle, "context.json"), &hookCtx)
	if want := (hookContext{Version: 1, Tool: "claude-code", TaskID: "task-a", ExecutionID: executionID, Slot: store.SlotMain,
		Repo: "github.com/acme/widget", WorkDir: work, Seed: seedPath}); hookCtx != want {
		t.Fatalf("context = %+v, want %+v", hookCtx, want)
	}

	var index hookIndex
	readJSONFile(t, filepath.Join(bundle, "index.json"), &index)
	if want := (hookIndex{Version: 1, Hooks: []hookIndexEntry{{
		Name: "gate", Dir: copyDir, Env: envPath, StateDir: stateDir,
		Events: map[string]hookIndexHandler{"stop": {Command: "./stop.sh", Timeout: 300}},
	}}}); !reflect.DeepEqual(index, want) {
		t.Fatalf("index = %+v, want %+v", index, want)
	}
}

func TestAPreToolHookIsNotRegisteredAndIsNotedAsNotDeliveredYet(t *testing.T) {
	f := newHookPolicyFixture(t)
	f.hookOn(t, "guard", "  pre_tool: {command: ./stop.sh}")
	f.hookOn(t, "both", "  post_tool: {command: ./stop.sh}\n  stop: {command: ./stop.sh}")
	f.hookOn(t, "plain", "  stop: {command: ./stop.sh}")
	f.spawn(t, "task-p", AgentSpawnOptions{})

	bundle, _, _ := f.bundle(t, "legato-task-p")
	if got := claudeEvents(t, bundle); !reflect.DeepEqual(got, map[string][]string{
		"SessionStart": {"canary"}, "UserPromptSubmit": {"turn_reset"}, "Stop": {"stop"},
	}) {
		t.Fatalf("hooks.json registers %v, want no tool events", got)
	}
	var index hookIndex
	readJSONFile(t, filepath.Join(bundle, "index.json"), &index)
	events := map[string][]string{}
	for _, hook := range index.Hooks {
		events[hook.Name] = slices.Sorted(func(yield func(string) bool) {
			for e := range hook.Events {
				if !yield(e) {
					return
				}
			}
		})
	}
	if !reflect.DeepEqual(events, map[string][]string{"both": {"stop"}, "guard": nil, "plain": {"stop"}}) {
		t.Fatalf("the index's events = %v, want only those delivered", events)
	}
	notes := f.notes(t, "task-p")
	if len(notes) != 1 || notes[0].Source != "legato" || !strings.Contains(notes[0].Body, "not delivered yet") ||
		!strings.Contains(notes[0].Body, "guard") || !strings.Contains(notes[0].Body, "both") || strings.Contains(notes[0].Body, "plain") {
		t.Fatalf("notes = %+v, want one launch note naming guard and both", notes)
	}
}

func TestAnAgentStartedAgainIsSeededWithHeadAtItsStart(t *testing.T) {
	ctx := context.Background()
	f := newHookPolicyFixture(t)
	f.hook(t, "gate")
	work := t.TempDir()
	writeFakeCheckout(t, work, "main", "1111111111111111111111111111111111111111")
	f.spawn(t, "task-r", AgentSpawnOptions{WorkingDir: work})
	firstBundle, first, _ := f.bundle(t, "legato-task-r")
	var seed hookSeed
	readJSONFile(t, filepath.Join(firstBundle, "seed.json"), &seed)
	if seed.Head != "1111111111111111111111111111111111111111" {
		t.Fatalf("first seed head %q", seed.Head)
	}

	if err := f.svc.KillAgent(ctx, "task-r"); err != nil {
		t.Fatal(err)
	}
	writeFakeCheckout(t, work, "next", "2222222222222222222222222222222222222222")
	f.spawn(t, "task-r", AgentSpawnOptions{WorkingDir: work})
	bundle, second, _ := f.bundle(t, "legato-task-r")
	if second == first {
		t.Fatal("the restart kept its execution")
	}
	readJSONFile(t, filepath.Join(bundle, "seed.json"), &seed)
	if seed.Head != "2222222222222222222222222222222222222222" || seed.Branch != "next" || seed.ExecutionID != second {
		t.Fatalf("the restart's seed = %+v, want HEAD at the restart", seed)
	}
}

func TestOutsideAGitCheckoutTheSeedHasNoHead(t *testing.T) {
	f := newHookPolicyFixture(t)
	f.hook(t, "gate")
	f.spawn(t, "task-n", AgentSpawnOptions{})
	bundle, _, _ := f.bundle(t, "legato-task-n")
	var seed hookSeed
	readJSONFile(t, filepath.Join(bundle, "seed.json"), &seed)
	if seed.Head != "" || seed.Branch != "" || seed.Version != 1 || seed.TaskID != "task-n" {
		t.Fatalf("seed = %+v", seed)
	}
}

func TestWithNoManagedHooksTheAgentStartsAsBefore(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(t *testing.T, f hookPolicyFixture)
	}{
		{"no folder", func(*testing.T, hookPolicyFixture) {}},
		{"an empty folder", func(t *testing.T, f hookPolicyFixture) {
			if err := os.MkdirAll(f.dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"only hooks that are off", func(t *testing.T, f hookPolicyFixture) {
			f.hook(t, "gate")
			if err := f.hooks.SetEnabled(context.Background(), "gate", false); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newHookPolicyFixture(t)
			c.setup(t, f)
			f.spawn(t, "task-0", AgentSpawnOptions{})
			env := f.tmux.envVarsFor("legato-task-0")
			if pathExists(filepath.Join(f.root, env["LEGATO_EXECUTION_ID"])) {
				t.Fatal("the agent got a capability folder")
			}
			for key, value := range env {
				if strings.Contains(value, f.root) || strings.HasPrefix(key, "LEGATO_HOOK") {
					t.Fatalf("%s = %q, want the environment as before", key, value)
				}
			}
			if notes := f.notes(t, "task-0"); len(notes) != 0 {
				t.Fatalf("notes = %+v", notes)
			}
		})
	}
}

// The context carries the workflow step, which no Phase 1 spawn gives
// hooks to (off:workflow), so it is checked on the writer.
func TestTheContextNamesTheWorkflowStepAndTheSlot(t *testing.T) {
	bundle := t.TempDir()
	req := CapabilityRequest{TaskID: "t", ExecutionID: "ex-1", AgentKind: "claude-code", Slot: "triage", Role: "worker", WorkDir: "/w",
		Repository: "github.com/a/b", WorkflowRunID: "run", WorkflowAttemptID: "attempt", WorkflowStepID: "step"}
	if err := writeHooksContract(bundle, nil, req, time.Date(2026, 11, 29, 1, 2, 3, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	var got hookContext
	readJSONFile(t, filepath.Join(bundle, "context.json"), &got)
	want := hookContext{Version: 1, Tool: "claude-code", TaskID: "t", ExecutionID: "ex-1", Slot: "triage", Role: "worker", Repo: "github.com/a/b",
		WorkDir: "/w", Workflow: hookWorkflow{RunID: "run", AttemptID: "attempt", StepID: "step"}, Seed: filepath.Join(bundle, "seed.json")}
	if got != want {
		t.Fatalf("context = %+v, want %+v", got, want)
	}
	var seed hookSeed
	readJSONFile(t, filepath.Join(bundle, "seed.json"), &seed)
	if seed.Time != "2026-11-29T01:02:03Z" {
		t.Fatalf("seed time %q", seed.Time)
	}
}
