package cli_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
)

// hookAgent is a running Claude Code agent whose hooks the tests replay.
type hookAgent struct {
	t         *testing.T
	s         *store.Store
	taskID    string
	execution string
	notifier  *recordingNotifier
}

func newHookAgent(t *testing.T) *hookAgent {
	t.Helper()
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")
	if err := s.InsertAgentSession(context.Background(), store.AgentSession{TaskID: "abc123", TmuxSession: "legato-abc123", AgentKind: "claude-code", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	return &hookAgent{t: t, s: s, taskID: "abc123", execution: linkTestExecution(t, s, "abc123"), notifier: &recordingNotifier{}}
}

// step is the agent as Legato sees it after one hook.
type step struct {
	event    string
	activity string
	jobs     []store.BackgroundJob
}

// replay feeds a recorded sequence's payloads, one per line, to Legato as
// the hook command would, and returns what Legato saw after each.
func (a *hookAgent) replay(name string) []step {
	a.t.Helper()
	file, err := os.Open(filepath.Join("testdata", "claude-hooks", name+".jsonl"))
	if err != nil {
		a.t.Fatal(err)
	}
	defer file.Close()
	var steps []step
	lines := bufio.NewScanner(file)
	lines.Buffer(make([]byte, 1<<20), 1<<20)
	for lines.Scan() {
		steps = append(steps, a.send([]byte(lines.Text())))
	}
	if err := lines.Err(); err != nil {
		a.t.Fatal(err)
	}
	return steps
}

func (a *hookAgent) send(payload []byte) step {
	a.t.Helper()
	hook, err := cli.ParseClaudeHook(payload)
	if err != nil {
		a.t.Fatalf("parsing %s: %v", payload, err)
	}
	if err := cli.ApplyClaudeHook(a.s, cli.ClaudeHookReport{TaskID: a.taskID, ExecutionID: a.execution, Hook: hook}, a.notifier, a.notifier); err != nil {
		a.t.Fatalf("applying %s: %v", hook.Event, err)
	}
	return a.now(hook.Event)
}

func (a *hookAgent) now(event string) step {
	a.t.Helper()
	session, err := a.s.GetAgentSessionByTaskID(context.Background(), a.taskID)
	if err != nil {
		a.t.Fatal(err)
	}
	return step{event: event, activity: session.Activity, jobs: session.BackgroundJobs}
}

// states lists the activity after each step, idle written as "idle".
func states(steps []step) string {
	out := make([]string, len(steps))
	for i, s := range steps {
		activity := s.activity
		if activity == "" {
			activity = "idle"
		}
		out[i] = s.event + "=" + activity
	}
	return strings.Join(out, " ")
}

// jobsOf names each job as kind:label.
func jobsOf(s step) string {
	out := make([]string, len(s.jobs))
	for i, job := range s.jobs {
		out[i] = job.Kind + ":" + job.Label
	}
	return strings.Join(out, ", ")
}

func TestClaudeHook_BackgroundBashKeepsTheAgentInTheBackgroundUntilItWakesIt(t *testing.T) {
	agent := newHookAgent(t)

	steps := agent.replay("background-bash")

	want := "SessionStart=idle UserPromptSubmit=working PreToolUse=working PostToolUse=working Stop=background UserPromptSubmit=working Stop=idle SessionEnd=idle"
	if got := states(steps); got != want {
		t.Fatalf("states:\n got %s\nwant %s", got, want)
	}
	stopped := steps[4]
	if got := jobsOf(stopped); got != "bash:Sleep 15 seconds then echo finished, in background" {
		t.Fatalf("jobs at the end of the turn = %q", got)
	}
	if stopped.jobs[0].ID != "by65pjakw" || stopped.jobs[0].StartedAt.IsZero() {
		t.Fatalf("job = %+v, want its ID and when it was seen", stopped.jobs[0])
	}
	if woken := steps[5]; len(woken.jobs) != 0 {
		t.Fatalf("jobs once the shell woke the agent = %q, want none", jobsOf(woken))
	}
}

func TestClaudeHook_ASubagentStillRunningAtTheEndOfTheTurnIsNamed(t *testing.T) {
	agent := newHookAgent(t)

	steps := agent.replay("several-jobs-and-subagent")

	// Two shells and a subagent start; the turn ends; the subagent's own
	// tool calls come after it and leave the agent in the background; each
	// job wakes it in turn, and the last leaves it idle.
	want := "SessionStart=idle UserPromptSubmit=working " +
		"PreToolUse=working PostToolUse=working PreToolUse=working PostToolUse=working " +
		"PreToolUse=working SubagentStart=working PostToolUse=working Stop=background " +
		"PreToolUse=background UserPromptSubmit=working Stop=background " +
		"PostToolUse=background SubagentStop=background UserPromptSubmit=working Stop=background " +
		"UserPromptSubmit=working Stop=idle SessionEnd=idle"
	if got := states(steps); got != want {
		t.Fatalf("states:\n got %s\nwant %s", got, want)
	}
	if got, want := jobsOf(steps[9]), "bash:Sleep 8 seconds then echo a, bash:Sleep 25 seconds then echo b, subagent:general-purpose: count files"; got != want {
		t.Fatalf("jobs at the end of the turn:\n got %s\nwant %s", got, want)
	}
	if got, want := jobsOf(steps[11]), "bash:Sleep 25 seconds then echo b, subagent:general-purpose: count files"; got != want {
		t.Fatalf("jobs once the first shell woke it:\n got %s\nwant %s", got, want)
	}
	if got, want := jobsOf(steps[16]), "bash:Sleep 25 seconds then echo b"; got != want {
		t.Fatalf("jobs once the subagent finished:\n got %s\nwant %s", got, want)
	}
	// A job keeps the time it was first seen, from its start in the turn.
	if !steps[16].jobs[0].StartedAt.Equal(steps[9].jobs[1].StartedAt) {
		t.Fatalf("started_at moved: %v then %v", steps[9].jobs[1].StartedAt, steps[16].jobs[0].StartedAt)
	}
}

func TestClaudeHook_AMonitorIsListedAsAMonitor(t *testing.T) {
	agent := newHookAgent(t)

	steps := agent.replay("monitor")

	stopped := steps[6]
	if stopped.event != "Stop" || stopped.activity != store.ActivityBackground {
		t.Fatalf("after the turn: %+v, want background", stopped)
	}
	if got := jobsOf(stopped); got != "monitor:Report when the sleep 10 command prints READY" {
		t.Fatalf("jobs = %q", got)
	}
	if got := states(steps[7:]); got != "UserPromptSubmit=working Stop=idle SessionEnd=idle" {
		t.Fatalf("after it reported: %s", got)
	}
}

func TestClaudeHook_ASessionCronWillWakeTheAgent(t *testing.T) {
	agent := newHookAgent(t)

	steps := agent.replay("cron")

	stopped := steps[6]
	if stopped.event != "Stop" || stopped.activity != store.ActivityBackground {
		t.Fatalf("after the turn: %+v, want background", stopped)
	}
	if len(stopped.jobs) != 1 || stopped.jobs[0].Kind != store.JobCron || stopped.jobs[0].Label != "check the build" || stopped.jobs[0].Schedule != "2-57/5 * * * *" {
		t.Fatalf("jobs = %+v, want the cron", stopped.jobs)
	}
}

func TestClaudeHook_AnAPIErrorIsAnError(t *testing.T) {
	agent := newHookAgent(t)
	if err := agent.s.UpdateTaskNotifyEnabled(context.Background(), agent.taskID, true); err != nil {
		t.Fatal(err)
	}

	steps := agent.replay("api-error")

	if got := states(steps[:3]); got != "SessionStart=idle UserPromptSubmit=working StopFailure=error" {
		t.Fatalf("states: %s", got)
	}
	if len(agent.notifier.calls) == 0 {
		t.Fatal("an error said nothing")
	}
}

func TestClaudeHook_PermissionPromptsAndQuestionsWaitForTheOperator(t *testing.T) {
	for _, test := range []struct{ name, want string }{
		{"permission-allowed", "UserPromptSubmit=working PreToolUse=working PermissionRequest=waiting Notification=waiting PostToolUse=working Stop=idle SubagentStop=idle"},
		{"question", "UserPromptSubmit=working PreToolUse=working PermissionRequest=waiting Notification=waiting PostToolUse=working Stop=idle SubagentStop=idle"},
		// "No" in the terminal fires nothing more: only the bridge can tell.
		{"permission-denied", "UserPromptSubmit=working PreToolUse=working PermissionRequest=waiting Notification=waiting"},
	} {
		t.Run(test.name, func(t *testing.T) {
			agent := newHookAgent(t)
			if got := states(agent.replay(test.name)); got != test.want {
				t.Fatalf("states:\n got %s\nwant %s", got, test.want)
			}
		})
	}
}

// No hook fires for an interrupted turn; the native bridge reports it.
func TestClaudeHook_AnInterruptedTurnEndsWhenTheBridgeSaysSo(t *testing.T) {
	for _, test := range []struct{ name, before, after string }{
		{"interrupt", "working", "idle"},
		{"permission-denied", "waiting", "idle"},
	} {
		t.Run(test.name, func(t *testing.T) {
			agent := newHookAgent(t)
			steps := agent.replay(test.name)
			if got := steps[len(steps)-1].activity; got != test.before {
				t.Fatalf("before the bridge: %q, want %q", got, test.before)
			}

			after := agent.turnEnded("aborted")

			if got := orIdle(after.activity); got != test.after {
				t.Fatalf("after the bridge: %q, want %q", got, test.after)
			}
		})
	}
}

func TestClaudeHook_AnInterruptedTurnWithJobsStillRunningIsInTheBackground(t *testing.T) {
	agent := newHookAgent(t)
	agent.send(lineOf(t, "several-jobs-and-subagent", 1))  // the turn starts
	agent.send(lineOf(t, "several-jobs-and-subagent", 9))  // Stop: three jobs
	agent.send(lineOf(t, "several-jobs-and-subagent", 11)) // woken by the first
	// That turn is interrupted instead of ending.

	after := agent.turnEnded("aborted")

	if after.activity != store.ActivityBackground || len(after.jobs) != 2 {
		t.Fatalf("after the interrupt: %+v, want background with two jobs", after)
	}
}

func TestClaudeHook_TheBridgeNeverUndoesAStop(t *testing.T) {
	agent := newHookAgent(t)
	agent.send(lineOf(t, "background-bash", 1)) // a turn
	agent.send(lineOf(t, "background-bash", 4)) // its Stop: a shell runs

	after := agent.turnEnded("aborted")

	if after.activity != store.ActivityBackground {
		t.Fatalf("after a late bridge report: %q, want background", after.activity)
	}
}

func TestClaudeHook_ManualCompactionWorksThenSettles(t *testing.T) {
	agent := newHookAgent(t)

	steps := agent.replay("compact")

	if got := states(steps); got != "PreCompact=working SubagentStop=working SessionStart=working PostCompact=idle" {
		t.Fatalf("states: %s", got)
	}
}

func TestClaudeHook_ClearEndsTheTurnAndItsJobs(t *testing.T) {
	agent := newHookAgent(t)
	agent.send(lineOf(t, "background-bash", 1))
	agent.send(lineOf(t, "background-bash", 3))
	if got := agent.send(lineOf(t, "background-bash", 4)); got.activity != store.ActivityBackground {
		t.Fatalf("before /clear: %q", got.activity)
	}

	steps := agent.replay("clear")

	if got := states(steps); got != "SessionEnd=idle SessionStart=idle Notification=idle" {
		t.Fatalf("states: %s", got)
	}
	if len(steps[0].jobs) != 0 {
		t.Fatalf("jobs after /clear = %q", jobsOf(steps[0]))
	}
}

// Hook scripts `legato hooks install` wrote may still fire beside the
// plugin; the old Stop script reports idle whatever runs.
func TestClaudeHook_OldInstalledScriptsAreIgnoredOnceThePluginReports(t *testing.T) {
	agent := newHookAgent(t)
	agent.send(lineOf(t, "background-bash", 0))
	agent.send(lineOf(t, "background-bash", 1))
	agent.send(lineOf(t, "background-bash", 4))

	if err := cli.AgentState(agent.s, cli.AgentStateReport{TaskID: agent.taskID, ExecutionID: agent.execution, Activity: ""}, agent.notifier, agent.notifier); err != nil {
		t.Fatal(err)
	}

	if got := agent.now("old Stop script"); got.activity != store.ActivityBackground {
		t.Fatalf("after the old script: %q, want background", got.activity)
	}
}

func TestClaudeHook_StateAndJobsOutliveLegato(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legato.db")
	s, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")
	if err := s.InsertAgentSession(context.Background(), store.AgentSession{TaskID: "abc123", TmuxSession: "legato-abc123", AgentKind: "claude-code", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	agent := &hookAgent{t: t, s: s, taskID: "abc123", execution: linkTestExecution(t, s, "abc123"), notifier: &recordingNotifier{}}
	agent.send(lineOf(t, "background-bash", 1))
	agent.send(lineOf(t, "background-bash", 3))
	agent.send(lineOf(t, "background-bash", 4))
	s.Close()

	reopened, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	session, err := reopened.GetAgentSessionByTaskID(context.Background(), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if session.Activity != store.ActivityBackground || len(session.BackgroundJobs) != 1 || session.BackgroundJobs[0].Kind != store.JobBash {
		t.Fatalf("after a restart: %q %+v", session.Activity, session.BackgroundJobs)
	}
}

func (a *hookAgent) turnEnded(reason string) step {
	a.t.Helper()
	if err := cli.ApplyTurnEnded(a.s, a.taskID, a.execution, reason, a.notifier, a.notifier); err != nil {
		a.t.Fatal(err)
	}
	return a.now(cli.HookTurnEnded)
}

// lineOf is one payload of a recorded sequence, counting from 0.
func lineOf(t *testing.T, name string, n int) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "claude-hooks", name+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	return []byte(lines[n])
}

func orIdle(activity string) string {
	if activity == "" {
		return "idle"
	}
	return activity
}

// A sandboxed agent's hook reaches Legato through the broker, an
// unsandboxed one's may apply directly; the same sequence lands the same.
func TestClaudeHook_SandboxedAndUnsandboxedAgentsReportTheSameStates(t *testing.T) {
	for _, name := range []string{"several-jobs-and-subagent", "monitor", "permission-allowed", "api-error"} {
		t.Run(name, func(t *testing.T) {
			direct := newHookAgent(t)
			want := direct.replay(name)

			s := newTestStore(t)
			seedColumns(t, s)
			_, token := brokerSession(t, s, "abc123")
			h := cli.NewBrokerHandler(s, cli.BrokerDeps{})
			brokered := &hookAgent{t: t, s: s, taskID: "abc123"}
			raw, err := os.ReadFile(filepath.Join("testdata", "claude-hooks", name+".jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			for i, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				hook, err := cli.ParseClaudeHook([]byte(line))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := call(h, token, "agent.hook", map[string]any{"hook": hook, "working_dir": "/work"}); err != nil {
					t.Fatalf("line %d (%s): %v", i, hook.Event, err)
				}
				got := brokered.now(hook.Event)
				if got.activity != want[i].activity || jobsOf(got) != jobsOf(want[i]) {
					t.Fatalf("line %d (%s): brokered %q [%s], direct %q [%s]", i, hook.Event, got.activity, jobsOf(got), want[i].activity, jobsOf(want[i]))
				}
			}
		})
	}
}

// The Codex bridge reports turns the same way. A report from a bridge never
// marks the session as the plugin's, so Codex's own hooks still count.
func TestABridgeReportLeavesTheSessionsOwnHooksInCharge(t *testing.T) {
	for _, tc := range []struct{ reason, want string }{
		{"interrupted", "idle"},
		{"failed", "error"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			agent := newHookAgent(t)
			report := func(activity string) {
				t.Helper()
				if err := cli.AgentState(agent.s, cli.AgentStateReport{TaskID: agent.taskID, ExecutionID: agent.execution, Activity: activity}, agent.notifier, agent.notifier); err != nil {
					t.Fatal(err)
				}
			}
			report(store.ActivityWorking)

			if got := orIdle(agent.turnEnded(tc.reason).activity); got != tc.want {
				t.Fatalf("after the bridge: %q, want %q", got, tc.want)
			}

			report(store.ActivityWorking)
			if got := agent.now("UserPromptSubmit"); got.activity != store.ActivityWorking {
				t.Fatalf("the session's own hook was ignored after a bridge report: %q", got.activity)
			}
		})
	}
}

// What a session reports of its jobs is the agent's to write, and reaches
// the Reeve, the API and the operator's terminal: Legato keeps known kinds,
// one short line of text and a bounded list.
func TestClaudeHook_JobsAreKeptToWhatLegatoCanShowSafely(t *testing.T) {
	agent := newHookAgent(t)
	agent.send(lineOf(t, "background-bash", 1))
	var running []map[string]any
	for i := range 40 {
		running = append(running, map[string]any{"id": fmt.Sprintf("j%d", i), "type": "bash\n- LEG-1: forged line", "description": "line one\nline two \x1b]52;c;aGk=\x07 " + strings.Repeat("x", 400)})
	}
	payload, _ := json.Marshal(map[string]any{"hook_event_name": "Stop", "background_tasks": running})

	got := agent.send(payload)

	if len(got.jobs) != 32 {
		t.Fatalf("jobs kept = %d, want 32", len(got.jobs))
	}
	job := got.jobs[0]
	if job.Kind != "other" {
		t.Fatalf("kind = %q, want other", job.Kind)
	}
	if strings.ContainsAny(job.Label, "\n\x1b\x07") || utf8.RuneCountInString(job.Label) > 200 || !strings.HasPrefix(job.Label, "line one line two") {
		t.Fatalf("label = %q", job.Label)
	}
}

// A subagent's own permission prompt waits on the operator; once allowed,
// the agent goes back to what it was doing: working in a foreground
// subagent, in the background for one that outlived the turn. The payloads
// follow the recorded shapes (a subagent's hooks carry its agent_id).
func TestClaudeHook_ASubagentsPermissionPromptEndsWhenItIsAnswered(t *testing.T) {
	permission := func(agentID string) []byte {
		return []byte(`{"hook_event_name":"PermissionRequest","agent_id":"` + agentID + `","tool_name":"Bash","tool_input":{"command":"touch x"}}`)
	}
	allowed := func(agentID string) []byte {
		return []byte(`{"hook_event_name":"PostToolUse","agent_id":"` + agentID + `","tool_name":"Bash","tool_input":{"command":"touch x"},"tool_response":{"stdout":""}}`)
	}

	t.Run("foreground", func(t *testing.T) {
		agent := newHookAgent(t)
		agent.send(lineOf(t, "background-bash", 1)) // the turn starts
		if got := agent.send(permission("a1")); got.activity != store.ActivityWaiting {
			t.Fatalf("on the prompt: %q", got.activity)
		}
		if got := agent.send(allowed("a1")); got.activity != store.ActivityWorking {
			t.Fatalf("once allowed: %q, want working", got.activity)
		}
	})
	t.Run("background", func(t *testing.T) {
		agent := newHookAgent(t)
		agent.send(lineOf(t, "several-jobs-and-subagent", 1))
		agent.send(lineOf(t, "several-jobs-and-subagent", 9)) // Stop: two shells and the subagent
		const subagent = "ad5f8a8d3914377a8"
		if got := agent.send(permission(subagent)); got.activity != store.ActivityWaiting {
			t.Fatalf("on the prompt: %q", got.activity)
		}
		if got := agent.send(allowed(subagent)); got.activity != store.ActivityBackground || len(got.jobs) != 3 {
			t.Fatalf("once allowed: %q [%s], want background with its jobs", got.activity, jobsOf(got))
		}
	})
}

// A subagent's API error is not the session's: its turn goes on.
func TestClaudeHook_ASubagentsErrorIsNotTheSessions(t *testing.T) {
	agent := newHookAgent(t)
	agent.send(lineOf(t, "background-bash", 1))

	got := agent.send([]byte(`{"hook_event_name":"StopFailure","agent_id":"a1","error":"rate_limit"}`))

	if got.activity != store.ActivityWorking {
		t.Fatalf("after a subagent's error: %q, want working", got.activity)
	}
}

// Jobs that end together may wake the session with one prompt.
func TestClaudeHook_OnePromptCanReportSeveralJobsEnding(t *testing.T) {
	agent := newHookAgent(t)
	agent.send(lineOf(t, "several-jobs-and-subagent", 1))
	agent.send(lineOf(t, "several-jobs-and-subagent", 9))
	prompt := "<task-notification>\n<task-id>bvf2m49z6</task-id>\n<status>completed</status>\n</task-notification>\n<task-notification>\n<task-id>bmvy04csq</task-id>\n<status>completed</status>\n</task-notification>"
	payload, _ := json.Marshal(map[string]any{"hook_event_name": "UserPromptSubmit", "prompt": prompt})

	got := agent.send(payload)

	if jobsOf(got) != "subagent:general-purpose: count files" {
		t.Fatalf("jobs = %q, want only the subagent", jobsOf(got))
	}
}

// An interrupt is the operator's doing and ends the turn quietly; a turn
// that failed is still news.
func TestTurnEnded_NotifiesOnlyOfAnError(t *testing.T) {
	for _, tc := range []struct {
		reason string
		notify bool
	}{{"aborted", false}, {"failed", true}} {
		t.Run(tc.reason, func(t *testing.T) {
			agent := newHookAgent(t)
			if err := agent.s.UpdateTaskNotifyEnabled(context.Background(), agent.taskID, true); err != nil {
				t.Fatal(err)
			}
			agent.send(lineOf(t, "background-bash", 1))

			if err := cli.ApplyTurnEnded(agent.s, agent.taskID, agent.execution, tc.reason, agent.notifier, agent.notifier); err != nil {
				t.Fatal(err)
			}

			if notified := len(agent.notifier.calls) > 0; notified != tc.notify {
				t.Fatalf("notified = %v, want %v (%+v)", notified, tc.notify, agent.notifier.calls)
			}
		})
	}
}

// The cases below could not be recorded where these tests were written; their
// payloads follow the shapes of the recorded ones and Claude Code's hook
// reference.

// A resumed or reloaded agent is a new session: what ran in the old process
// ended with it, and the new one reports for itself from its first hook.
func TestClaudeHook_AResumedAgentStartsWithNothingInTheBackground(t *testing.T) {
	agent := newHookAgent(t)
	agent.send(lineOf(t, "background-bash", 1))
	agent.send(lineOf(t, "background-bash", 3))
	agent.send(lineOf(t, "background-bash", 4)) // background, a shell running
	ctx := context.Background()
	if err := agent.s.UpdateAgentSessionStatus(ctx, agent.taskID, "dead"); err != nil {
		t.Fatal(err)
	}
	if err := agent.s.TerminalizeExecution(ctx, agent.execution, store.ExecutionCancelled); err != nil {
		t.Fatal(err)
	}
	if err := agent.s.InsertAgentSession(ctx, store.AgentSession{TaskID: agent.taskID, TmuxSession: "legato-abc123-resumed", AgentKind: "claude-code", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	resumedSession, err := agent.s.GetAgentSessionByTaskID(ctx, agent.taskID)
	if err != nil {
		t.Fatal(err)
	}
	agent.execution = "exec-resumed"
	if err := agent.s.CreateExecution(ctx, store.Execution{ID: agent.execution, Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: agent.taskID, ExecutionTaskID: agent.taskID, AgentSessionID: &resumedSession.ID, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	start := agent.send([]byte(`{"hook_event_name":"SessionStart","source":"resume"}`))
	resumed := agent.send([]byte(`{"hook_event_name":"UserPromptSubmit","prompt":"[legato interrupted] Carry on."}`))

	if start.activity != "" || len(start.jobs) != 0 {
		t.Fatalf("on resume: %q [%s], want idle with nothing running", start.activity, jobsOf(start))
	}
	if resumed.activity != store.ActivityWorking {
		t.Fatalf("on the resume note: %q, want working", resumed.activity)
	}
	session, _ := agent.s.GetAgentSessionByTaskID(ctx, agent.taskID)
	if session.HookSource != store.HookSourcePlugin {
		t.Fatalf("hook source = %q, want the plugin's from its first hook", session.HookSource)
	}
}

func TestClaudeHook_ARateLimitIsAnError(t *testing.T) {
	agent := newHookAgent(t)
	agent.send(lineOf(t, "background-bash", 1))

	got := agent.send([]byte(`{"hook_event_name":"StopFailure","error":"rate_limit","last_assistant_message":"API Error: rate limited"}`))

	if got.activity != store.ActivityError {
		t.Fatalf("after a rate limit: %q, want error", got.activity)
	}
}

// Compaction Claude Code starts itself happens inside a turn, which goes on.
func TestClaudeHook_AutomaticCompactionKeepsTheTurnWorking(t *testing.T) {
	agent := newHookAgent(t)
	agent.send(lineOf(t, "background-bash", 1))

	steps := []step{
		agent.send([]byte(`{"hook_event_name":"PreCompact","trigger":"auto","custom_instructions":null}`)),
		agent.send([]byte(`{"hook_event_name":"SessionStart","source":"compact"}`)),
		agent.send([]byte(`{"hook_event_name":"PostCompact","trigger":"auto","compact_summary":"…"}`)),
	}

	if got := states(steps); got != "PreCompact=working SessionStart=working PostCompact=working" {
		t.Fatalf("states: %s", got)
	}
}

// A subagent run in the foreground is part of the turn: its tool calls keep
// it working, and its return starts nothing that runs on.
func TestClaudeHook_AForegroundSubagentIsPartOfTheTurn(t *testing.T) {
	agent := newHookAgent(t)
	agent.send(lineOf(t, "background-bash", 1))

	steps := []step{
		agent.send([]byte(`{"hook_event_name":"PostToolUse","agent_id":"a1","agent_type":"Explore","tool_name":"Grep","tool_input":{"pattern":"x"},"tool_response":{"numFiles":0}}`)),
		agent.send([]byte(`{"hook_event_name":"PostToolUse","tool_name":"Agent","tool_input":{"description":"find callers","prompt":"…","subagent_type":"Explore"},"tool_response":{"status":"completed","content":[{"type":"text","text":"none"}]}}`)),
		agent.send([]byte(`{"hook_event_name":"Stop","background_tasks":[],"session_crons":[]}`)),
	}

	if got := states(steps); got != "PostToolUse=working PostToolUse=working Stop=idle" {
		t.Fatalf("states: %s", got)
	}
	if len(steps[1].jobs) != 0 {
		t.Fatalf("a foreground subagent left jobs: %s", jobsOf(steps[1]))
	}
}
