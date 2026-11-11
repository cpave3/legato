package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// crash makes every tmux session of the fixture vanish, as when the
// machine goes down with Legato.
func (f restartFixture) crash() {
	f.tmux.mu.Lock()
	defer f.tmux.mu.Unlock()
	for name := range f.tmux.sessions {
		delete(f.tmux.sessions, name)
	}
}

func (f restartFixture) interrupted() InterruptedAgents {
	return f.svc.(InterruptedAgents)
}

func TestStartupSweepKeepsAgentsWhoseSessionIsGoneAsInterrupted(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "task-i")
	cwd := t.TempDir()
	if err := f.svc.SpawnAgent(ctx, "task-i", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd}); err != nil {
		t.Fatal(err)
	}
	f.reportConversation(t, "task-i", cwd)
	f.crash()

	count, err := f.interrupted().SweepInterruptedAgents(ctx)
	if err != nil || count != 1 {
		t.Fatalf("sweep = %d, %v; want 1 interrupted", count, err)
	}
	// What reconciles later in the run does not turn it into an ended agent.
	if err := f.svc.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}

	list, err := f.interrupted().ListInterruptedAgents(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("interrupted = %#v, %v; want one", list, err)
	}
	got := list[0]
	if got.TaskID != "task-i" || got.AgentKind != "claude-code" || got.WorkingDir != cwd || !got.Resumable || got.Reason != "" {
		t.Fatalf("interrupted agent = %#v", got)
	}
	if running, _ := f.svc.ListAgents(ctx); len(running) != 0 {
		t.Fatalf("running agents = %#v, want none", running)
	}
}

// interruptedAgent spawns a Claude Code agent on taskID in cwd, records its
// conversation when reported is true, and crashes the machine under it.
func (f restartFixture) interruptedAgent(t *testing.T, taskID, cwd string, reported bool, opts ...AgentSpawnOptions) {
	t.Helper()
	ctx := context.Background()
	createTask(t, f.store, taskID)
	opt := AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd}
	if len(opts) > 0 {
		opt = opts[0]
	}
	if err := f.svc.SpawnAgent(ctx, taskID, 0, 0, opt); err != nil {
		t.Fatal(err)
	}
	if reported {
		f.reportConversation(t, taskID, cwd)
	}
	f.crash()
	if _, err := f.interrupted().SweepInterruptedAgents(ctx); err != nil {
		t.Fatal(err)
	}
}

func (f restartFixture) linesEventually(t *testing.T, taskID string, n int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if lines := f.tmux.sentLinesFor("legato-" + taskID); len(lines) >= n {
			return lines
		}
		time.Sleep(10 * time.Millisecond)
	}
	return f.tmux.sentLinesFor("legato-" + taskID)
}

type fixedResources map[string]Resource

func (r fixedResources) Resolve(_ context.Context, names []string) ([]Resource, error) {
	var out []Resource
	for _, name := range names {
		if found, ok := r[name]; ok {
			out = append(out, found)
		}
	}
	return out, nil
}

func TestResumingAnInterruptedAgentResumesItsConversationAndSaysItWasInterrupted(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	cwd := t.TempDir()
	f.interruptedAgent(t, "task-i", cwd, true)
	f.clearSent()

	if err := f.interrupted().ResumeInterruptedAgent(ctx, "task-i", false); err != nil {
		t.Fatal(err)
	}

	lines := f.linesEventually(t, "task-i", 2)
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "claude --resume conversation-1") {
		t.Fatalf("lines = %q, want the resumed conversation first", lines)
	}
	if !strings.Contains(strings.Join(lines[1:], "\n"), "interrupted") {
		t.Fatalf("lines = %q, want a note that the session was interrupted", lines)
	}
	// The note is Legato's, not the person's: its tag is what makes the
	// conversation draw it as a message from Legato.
	if !strings.HasPrefix(lines[1], "[legato interrupted] ") {
		t.Fatalf("note = %q, want it to open with Legato's tag", lines[1])
	}
	session, err := f.store.GetAgentSessionByTaskID(ctx, "task-i")
	if err != nil {
		t.Fatalf("running session: %v", err)
	}
	if execution, err := f.store.GetExecution(ctx, session.ExecutionID); err != nil || execution.WorkingDir != cwd {
		t.Fatalf("execution = %#v, %v; want working dir %s", execution, err, cwd)
	}
	if list, _ := f.interrupted().ListInterruptedAgents(ctx); len(list) != 0 {
		t.Fatalf("interrupted after resume = %#v, want none", list)
	}
}

func TestResumingAnInterruptedAgentGivesItItsResourcesAgain(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	cwd := t.TempDir()
	docs := t.TempDir()
	f.svc.(*agentService).SetResourceResolver(fixedResources{"docs": {Name: "docs", Paths: []ResourcePath{{Path: docs, Access: "read"}}}})
	f.interruptedAgent(t, "task-i", cwd, true, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd,
		Resources: []Resource{{Name: "docs", Paths: []ResourcePath{{Path: docs, Access: "read"}}}}})

	if list, _ := f.interrupted().ListInterruptedAgents(ctx); len(list) != 1 || len(list[0].Resources) != 1 || list[0].Resources[0] != "docs" {
		t.Fatalf("interrupted = %#v, want it to list its resource", list)
	}
	if err := f.interrupted().ResumeInterruptedAgent(ctx, "task-i", false); err != nil {
		t.Fatal(err)
	}
	names, err := f.svc.(*agentService).AttachedResources(ctx, "task-i")
	if err != nil || len(names) != 1 || names[0] != "docs" {
		t.Fatalf("resources after resume = %v, %v; want docs", names, err)
	}
}

func TestAnInterruptedAgentWithNoConversationCanOnlyStartANewOne(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	cwd := t.TempDir()
	f.interruptedAgent(t, "task-i", cwd, false)

	list, _ := f.interrupted().ListInterruptedAgents(ctx)
	if len(list) != 1 || list[0].Resumable || !list[0].CanStartNew || !strings.Contains(list[0].Reason, "conversation") {
		t.Fatalf("interrupted = %#v, want not resumable, a new one offered, and why", list)
	}
	if err := f.interrupted().ResumeInterruptedAgent(ctx, "task-i", false); !errors.Is(err, ErrAgentNotResumable) {
		t.Fatalf("resume = %v, want ErrAgentNotResumable", err)
	}
	f.clearSent()
	if err := f.interrupted().ResumeInterruptedAgent(ctx, "task-i", true); err != nil {
		t.Fatal(err)
	}
	lines := f.linesEventually(t, "task-i", 1)
	if len(lines) == 0 || strings.Contains(lines[0], "--resume") {
		t.Fatalf("lines = %q, want a new conversation", lines)
	}
	if _, err := f.store.GetAgentSessionByTaskID(ctx, "task-i"); err != nil {
		t.Fatalf("running session: %v", err)
	}
}

func TestAnInterruptedAgentWhoseFolderIsGoneIsNotStarted(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	cwd := t.TempDir()
	f.interruptedAgent(t, "task-i", cwd, true)
	if err := os.RemoveAll(cwd); err != nil {
		t.Fatal(err)
	}

	list, _ := f.interrupted().ListInterruptedAgents(ctx)
	if len(list) != 1 || list[0].Resumable || list[0].CanStartNew || !strings.Contains(list[0].Reason, "gone") {
		t.Fatalf("interrupted = %#v, want refused because its folder is gone", list)
	}
	for _, fresh := range []bool{false, true} {
		if err := f.interrupted().ResumeInterruptedAgent(ctx, "task-i", fresh); !errors.Is(err, ErrAgentNotResumable) {
			t.Fatalf("resume (fresh %v) = %v, want ErrAgentNotResumable", fresh, err)
		}
	}
}

func TestASwarmAgentIsShownInterruptedButNotResumed(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	cwd := t.TempDir()
	createTask(t, f.store, "parent")
	f.interruptedAgent(t, "task-w", cwd, false, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd, Role: "backend", ParentTaskID: "parent"})

	list, _ := f.interrupted().ListInterruptedAgents(ctx)
	if len(list) != 1 || list[0].Resumable || list[0].CanStartNew || !strings.Contains(list[0].Reason, "swarm") {
		t.Fatalf("interrupted = %#v, want a swarm agent refused", list)
	}
	if err := f.interrupted().ResumeInterruptedAgent(ctx, "task-w", false); !errors.Is(err, ErrAgentNotResumable) {
		t.Fatalf("resume = %v, want ErrAgentNotResumable", err)
	}
	// Its swarm hears that it died, as it would of any lost worker.
	if len(f.bus.calls) == 0 {
		t.Fatal("no died event for the swarm")
	}
}

func TestResumeAllResumesEveryResumableAgent(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	for _, id := range []string{"task-a", "task-b"} {
		cwd := t.TempDir()
		createTask(t, f.store, id)
		if err := f.svc.SpawnAgent(ctx, id, 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd}); err != nil {
			t.Fatal(err)
		}
		f.reportConversation(t, id, cwd)
	}
	createTask(t, f.store, "task-c")
	if err := f.svc.SpawnAgent(ctx, "task-c", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	f.crash()
	if n, err := f.interrupted().SweepInterruptedAgents(ctx); err != nil || n != 3 {
		t.Fatalf("sweep = %d, %v", n, err)
	}

	results := f.interrupted().ResumeAllInterruptedAgents(ctx)

	resumed := map[string]bool{}
	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("resuming %s: %v", r.TaskID, r.Err)
		}
		resumed[r.TaskID] = true
	}
	if len(resumed) != 2 || !resumed["task-a"] || !resumed["task-b"] {
		t.Fatalf("resumed = %v, want task-a and task-b", resumed)
	}
	if list, _ := f.interrupted().ListInterruptedAgents(ctx); len(list) != 1 || list[0].TaskID != "task-c" {
		t.Fatalf("still interrupted = %#v, want task-c, which has no conversation", list)
	}
}

func TestDismissingAnInterruptedAgentEndsIt(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	f.interruptedAgent(t, "task-i", t.TempDir(), true)

	if err := f.interrupted().DismissInterruptedAgent(ctx, "task-i"); err != nil {
		t.Fatal(err)
	}
	if list, _ := f.interrupted().ListInterruptedAgents(ctx); len(list) != 0 {
		t.Fatalf("interrupted = %#v, want none", list)
	}
	if err := f.interrupted().DismissInterruptedAgent(ctx, "task-i"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second dismiss = %v, want ErrNotFound", err)
	}
}

func TestARouteResumesTheAgentsItOwns(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	f.interruptedAgent(t, "task-i", t.TempDir(), true)
	var routed []string
	f.svc.(*agentService).AddInterruptedResumeRoute(func(_ context.Context, taskID string) (bool, error) {
		routed = append(routed, taskID)
		return true, nil
	})

	if err := f.interrupted().ResumeInterruptedAgent(ctx, "task-i", false); err != nil {
		t.Fatal(err)
	}
	if len(routed) != 1 {
		t.Fatalf("routed = %v, want the task", routed)
	}
	if list, _ := f.interrupted().ListInterruptedAgents(ctx); len(list) != 0 {
		t.Fatalf("interrupted = %#v, want none once its route resumed it", list)
	}
}

// clearSent forgets what was typed into the panes so far.
func (f restartFixture) clearSent() {
	f.tmux.mu.Lock()
	defer f.tmux.mu.Unlock()
	for name := range f.tmux.sentLines {
		delete(f.tmux.sentLines, name)
	}
}

func TestTheInterruptedListenerHearsEachChange(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	heard := 0
	f.svc.(*agentService).AddInterruptedListener(func() { heard++ })
	f.interruptedAgent(t, "task-i", t.TempDir(), true)
	if heard != 1 {
		t.Fatalf("heard %d after the sweep, want 1", heard)
	}
	if err := f.interrupted().DismissInterruptedAgent(ctx, "task-i"); err != nil {
		t.Fatal(err)
	}
	if heard != 2 {
		t.Fatalf("heard %d after the dismiss, want 2", heard)
	}
}
