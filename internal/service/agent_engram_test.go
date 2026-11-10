package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/engram"
	"github.com/cpave3/legato/internal/engine/store"
)

type engramSpawnFixture struct {
	store *store.Store
	tmux  *mockTmux
	cli   *fakeEngramCLI
	svc   AgentService
}

func newEngramSpawnFixture(t *testing.T, kind string) *engramSpawnFixture {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: &fakeAdapter{name: kind}})
	cli := &fakeEngramCLI{where: testWhere, ids: []engram.Identity{{Name: "mneme"}, {Name: "argus"}}, boot: engram.BootResult{Packet: "# mneme\nIssued identity: keeper of notes\n"}}
	engrams, _ := bootService(cli)
	svc.(interface{ SetEngrams(AgentEngrams) }).SetEngrams(engrams)
	createTask(t, s, "task-1")
	return &engramSpawnFixture{store: s, tmux: mt, cli: cli, svc: svc}
}

func (f *engramSpawnFixture) prompt(t *testing.T) string {
	t.Helper()
	path := f.tmux.envVarsFor("legato-task-1")["LEGATO_ROLE_PROMPT_FILE"]
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (f *engramSpawnFixture) execution(t *testing.T) string {
	t.Helper()
	sess, err := f.store.GetAgentSessionByTaskID(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	return sess.ExecutionID
}

func TestSpawnAgent_AsAnEngram(t *testing.T) {
	f := newEngramSpawnFixture(t, "claude-code")
	var report SpawnReport
	ctx := WithSpawnReport(context.Background(), &report)
	if err := f.svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{Engram: "mneme", Model: "opus"}); err != nil {
		t.Fatal(err)
	}
	exec := f.execution(t)
	env := f.tmux.envVarsFor("legato-task-1")
	want := map[string]string{
		"ENGRAM_IDENTITY": "mneme", "ENGRAM_HOME": testWhere.Store.Root, "ENGRAM_SESSION": exec,
		"ENGRAM_HARNESS": "claude-code", "ENGRAM_MODEL": "opus", "LEGATO_TASK_ID": "task-1", "LEGATO_EXECUTION_ID": exec,
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("session env %s = %q, want %q", k, env[k], v)
		}
	}
	prompt := f.prompt(t)
	if !strings.HasPrefix(prompt, "You are running as the engram mneme.") {
		t.Fatalf("the engram does not come first in the launch prompt:\n%s", prompt)
	}
	if !strings.Contains(prompt, "<engram-boot name=\"mneme\">\n# mneme\nIssued identity: keeper of notes\n</engram-boot>") {
		t.Fatalf("launch prompt lacks the packet:\n%s", prompt)
	}
	name, session, err := f.store.ExecutionEngram(context.Background(), exec)
	if err != nil || name != "mneme" || session != exec {
		t.Fatalf("execution engram = %q %q %v", name, session, err)
	}
	if report.Engram != "mneme" || report.EngramWarning != "" {
		t.Fatalf("report = %+v", report)
	}
}

func TestSpawnAgent_WithoutAnEngramSetsNone(t *testing.T) {
	f := newEngramSpawnFixture(t, "claude-code")
	if err := f.svc.SpawnAgent(context.Background(), "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	if f.cli.boots != 0 {
		t.Fatal("a normal session booted an engram")
	}
	for k := range f.tmux.envVarsFor("legato-task-1") {
		if strings.HasPrefix(k, "ENGRAM_") {
			t.Errorf("a normal session has %s", k)
		}
	}
}

func TestSpawnAgent_AFailedBootStartsTheAgentWithoutItsEngram(t *testing.T) {
	f := newEngramSpawnFixture(t, "codex")
	f.cli.bootErr = &engram.Error{Code: 1, Message: "disk is full"}
	var report SpawnReport
	ctx := WithSpawnReport(context.Background(), &report)
	if err := f.svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{Engram: "mneme"}); err != nil {
		t.Fatalf("a failed boot stopped the spawn: %v", err)
	}
	for k := range f.tmux.envVarsFor("legato-task-1") {
		if strings.HasPrefix(k, "ENGRAM_") {
			t.Errorf("an agent whose engram did not boot has %s", k)
		}
	}
	if strings.Contains(f.prompt(t), "engram-boot") {
		t.Fatal("the launch prompt has a packet")
	}
	if name, _, _ := f.store.ExecutionEngram(context.Background(), f.execution(t)); name != "" {
		t.Fatalf("execution records engram %q", name)
	}
	if !strings.Contains(report.EngramWarning, "disk is full") || report.Engram != "" {
		t.Fatalf("report = %+v", report)
	}
	failures, err := f.store.ListEngramBootFailures(context.Background())
	if err != nil || len(failures) != 1 || failures[0].Engram != "mneme" || !strings.Contains(failures[0].Reason, "disk is full") || failures[0].TaskID != "task-1" {
		t.Fatalf("recorded failures = %+v %v", failures, err)
	}
}

func TestSpawnAgent_AnEngramThatIsRequiredFailsTheSpawnWhenItIsUnknown(t *testing.T) {
	f := newEngramSpawnFixture(t, "codex")
	err := f.svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Engram: "ghost", EngramRequired: true})
	if !errors.Is(err, engram.ErrUnknownIdentity) {
		t.Fatalf("spawn error = %v, want unknown engram", err)
	}
	if len(f.tmux.envVars) != 0 {
		t.Fatal("an agent started")
	}
}

func TestSpawnAgent_EndsTheEngramSessionWhenTheSpawnFailsAfterItsBoot(t *testing.T) {
	f := newEngramSpawnFixture(t, "claude-code")
	f.tmux.spawnErr = errors.New("tmux is gone")
	if err := f.svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Engram: "mneme"}); err == nil {
		t.Fatal("spawn succeeded")
	}
	if len(f.cli.ended) != 1 {
		t.Fatalf("ended %d engram sessions, want 1", len(f.cli.ended))
	}
}

func TestSpawnAgent_SwarmAgentsAreNotStartedAsEngrams(t *testing.T) {
	f := newEngramSpawnFixture(t, "claude-code")
	err := f.svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Engram: "mneme", Role: "conductor"})
	if err == nil {
		t.Fatal("a conductor started as an engram")
	}
}

func TestSpawnAgent_AResumedConversationKeepsItsEngramSession(t *testing.T) {
	f := newEngramSpawnFixture(t, "chimera")
	if err := f.svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Engram: "mneme", EngramSession: "ex-first", SessionPolicy: "resume", ResumeSessionID: "chimera-1"}); err != nil {
		t.Fatal(err)
	}
	env := f.tmux.envVarsFor("legato-task-1")
	if env["ENGRAM_SESSION"] != "ex-first" || env["ENGRAM_IDENTITY"] != "mneme" {
		t.Fatalf("resumed env = %v", env)
	}
	if f.cli.boots != 0 || strings.Contains(f.prompt(t), "engram-boot") {
		t.Fatal("a resumed conversation was booted again")
	}
	if name, session, _ := f.store.ExecutionEngram(context.Background(), f.execution(t)); name != "mneme" || session != "ex-first" {
		t.Fatalf("execution engram = %q %q", name, session)
	}
}

func TestSpawnAgent_AResumeFindsTheTasksLastSessionOfThatEngram(t *testing.T) {
	f := newEngramSpawnFixture(t, "chimera")
	ctx := context.Background()
	if err := f.svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{Engram: "mneme"}); err != nil {
		t.Fatal(err)
	}
	first := f.execution(t)
	if err := f.svc.KillAgent(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{Engram: "mneme", SessionPolicy: "resume", ResumeSessionID: "chimera-1"}); err != nil {
		t.Fatal(err)
	}
	if got := f.tmux.envVarsFor("legato-task-1")["ENGRAM_SESSION"]; got != first {
		t.Fatalf("ENGRAM_SESSION = %q, want the first execution's %q", got, first)
	}
}

func TestListAgentsSaysWhichEngramAnAgentRunsAs(t *testing.T) {
	f := newEngramSpawnFixture(t, "claude-code")
	if err := f.svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Engram: "mneme"}); err != nil {
		t.Fatal(err)
	}
	agents, err := f.svc.ListAgents(context.Background())
	if err != nil || len(agents) != 1 || agents[0].Engram != "mneme" {
		t.Fatalf("agents = %+v %v", agents, err)
	}
}

// touchingEngrams records the bindings a spawn touches.
type touchingEngrams struct {
	AgentEngrams
	touched []string
}

func (t *touchingEngrams) Touch(_ context.Context, dir, name string) error {
	t.touched = append(t.touched, name)
	return nil
}

func TestAWorkflowAgentNeverTouchesBindings(t *testing.T) {
	f := newEngramSpawnFixture(t, "claude-code")
	engrams, _ := bootService(f.cli)
	touching := &touchingEngrams{AgentEngrams: engrams}
	f.svc.(interface{ SetEngrams(AgentEngrams) }).SetEngrams(touching)
	if err := f.svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Engram: "mneme", WorkflowRunID: "wr-1", ExecutionKind: store.ExecutionWorkflowAgent}); err != nil {
		t.Fatal(err)
	}
	if len(touching.touched) != 0 {
		t.Fatalf("a workflow agent touched %v", touching.touched)
	}
	if name, _, _ := f.store.ExecutionEngram(context.Background(), f.execution(t)); name != "mneme" {
		t.Fatalf("the workflow agent did not start as mneme: %q", name)
	}
}

func TestFinishingAWorkflowAgentEndsItsEngramSession(t *testing.T) {
	f := newEngramSpawnFixture(t, "claude-code")
	ctx := context.Background()
	if err := f.svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{Engram: "mneme", WorkflowRunID: "wr-1", ExecutionKind: store.ExecutionWorkflowAgent}); err != nil {
		t.Fatal(err)
	}
	session, _ := f.store.GetAgentSessionByTaskID(ctx, "task-1")
	runner := NewWorkflowAgentRunner(f.svc, f.store)
	id := session.ID
	if err := runner.FinishWorkflowAgent(ctx, store.WorkflowStepAttempt{AgentSessionID: &id, TmuxSession: session.TmuxSession}); err != nil {
		t.Fatal(err)
	}
	if len(f.cli.ended) != 1 || envOf(f.cli.ended[0])["ENGRAM_SESSION"] != session.ExecutionID {
		t.Fatalf("ended %v", f.cli.ended)
	}
}

func TestResumingAConversationAsAnEngramItNeverRanAsBootsIt(t *testing.T) {
	f := newEngramSpawnFixture(t, "chimera")
	if err := f.svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Engram: "mneme", SessionPolicy: "resume", ResumeSessionID: "chimera-1"}); err != nil {
		t.Fatal(err)
	}
	exec := f.execution(t)
	if f.cli.boots != 1 {
		t.Fatalf("booted %d times, want a fresh boot", f.cli.boots)
	}
	if got := f.tmux.envVarsFor("legato-task-1")["ENGRAM_SESSION"]; got != exec {
		t.Fatalf("ENGRAM_SESSION = %q, want the new execution %q", got, exec)
	}
	if !strings.Contains(f.prompt(t), "<engram-boot") {
		t.Fatal("the packet is not in the launch prompt")
	}
}

// A tool whose resumed conversation takes no launch prompt gets the packet
// as its first message.
func TestAResumedNativeConversationGetsAFreshPacketAsItsFirstMessage(t *testing.T) {
	f := newRestartFixture(t)
	f.withEngrams(t)
	ctx := context.Background()
	createTask(t, f.store, "task-p")
	cwd := t.TempDir()
	if err := f.svc.SpawnAgent(ctx, "task-p", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd}); err != nil {
		t.Fatal(err)
	}
	f.reportConversation(t, "task-p", cwd)
	if err := f.svc.KillAgent(ctx, "task-p"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SpawnAgent(ctx, "task-p", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd, Engram: "mneme", SessionPolicy: "resume", ResumeSessionID: "conversation-1"}); err != nil {
		t.Fatal(err)
	}
	path := f.tmux.envVarsFor("legato-task-p")["LEGATO_PROMPT_FILE"]
	if path == "" {
		t.Fatal("no first message was written")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "<engram-boot name=\"mneme\">") {
		t.Fatalf("first message = %q", data)
	}
}

func TestASpawnThatFailsBeforeItsEngramIsRecordedEndsTheSession(t *testing.T) {
	f := newEngramSpawnFixture(t, "claude-code")
	// The learnings cannot be compiled: the spawn stops after the boot and
	// before its execution exists.
	f.svc.(*agentService).learning = failingLearnings{}
	if err := f.svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Engram: "mneme"}); err == nil {
		t.Fatal("the spawn succeeded")
	}
	if f.cli.boots != 1 || len(f.cli.ended) != 1 {
		t.Fatalf("booted %d, ended %d", f.cli.boots, len(f.cli.ended))
	}
}

type failingLearnings struct{}

func (failingLearnings) CompileLearningPrompt(context.Context, LearningPromptCompileInput) (*CompiledLearningPrompt, error) {
	return nil, errors.New("the ledger is locked")
}

func TestAResumedRestartWhoseBootFailsEndsTheSessionItKept(t *testing.T) {
	f := newEngramSpawnFixture(t, "chimera")
	// The engram is gone by the time its agent is restarted.
	f.cli.ids = nil
	if err := f.svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Engram: "mneme", EngramSession: "ex-old", SessionPolicy: "resume", ResumeSessionID: "chimera-1"}); err != nil {
		t.Fatal(err)
	}
	if len(f.cli.ended) != 1 || envOf(f.cli.ended[0])["ENGRAM_SESSION"] != "ex-old" {
		t.Fatalf("ended %v, want the kept session ex-old", f.cli.ended)
	}
}
