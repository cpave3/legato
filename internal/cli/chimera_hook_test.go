package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
)

// These synthetic boundary cases complement the pinned runtime recordings.
func sendChimera(t *testing.T, a *hookAgent, payload string) step {
	t.Helper()
	hook, err := cli.ParseChimeraHook([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.ApplyChimeraHook(a.s, cli.ChimeraHookReport{TaskID: a.taskID, ExecutionID: a.execution, Hook: hook}, a.notifier, a.notifier); err != nil {
		t.Fatal(err)
	}
	return a.now(hook.Event)
}

func TestChimeraHook_TurnStartsAndMessagesDoNotResumeAWaitingTurn(t *testing.T) {
	a := newHookAgent(t)
	for _, tc := range []struct{ payload, want string }{
		{`{"type":"turn.start"}`, "working"},
		{`{"type":"permission.pending","pendingRequests":1}`, "waiting"},
		{`{"type":"message.append"}`, "waiting"},
	} {
		if got := sendChimera(t, a, tc.payload); got.activity != tc.want {
			t.Fatalf("%s: %q, want %q", tc.payload, got.activity, tc.want)
		}
	}
	session, err := a.s.GetAgentSessionByTaskID(context.Background(), a.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if session.HookSource != "mod" {
		t.Fatalf("source = %q, want mod", session.HookSource)
	}
	durations, err := a.s.GetExecutionDurations(context.Background(), a.execution)
	if err != nil {
		t.Fatal(err)
	}
	_, worked := durations[store.ActivityWorking]
	_, waited := durations[store.ActivityWaiting]
	if !worked || !waited {
		t.Fatalf("durations = %+v", durations)
	}
}

func TestChimeraHook_OnlyTheLastResolutionResumesTheTurn(t *testing.T) {
	a := newHookAgent(t)
	for _, tc := range []struct{ payload, want string }{
		{`{"type":"turn.start"}`, "working"},
		{`{"type":"permission.pending","pendingRequests":1}`, "waiting"},
		{`{"type":"question.request","requestId":"q1","pendingRequests":2}`, "waiting"},
		{`{"type":"permission.resolved","requestId":"p1","pendingRequests":1}`, "waiting"},
		{`{"type":"question.resolved","requestId":"q1"}`, "waiting"},
		{`{"type":"question.resolved","requestId":"q1","pendingRequests":0}`, "working"},
	} {
		if got := sendChimera(t, a, tc.payload); got.activity != tc.want {
			t.Fatalf("%s: %q, want %q", tc.payload, got.activity, tc.want)
		}
	}
}

func TestChimeraHook_CompletionReasons(t *testing.T) {
	for _, tc := range []struct{ reason, want string }{
		{"stop", ""}, {"interrupted", ""}, {"timeout", ""}, {"cancelled", ""}, {"error", "error"}, {"waiting_for_input", "waiting"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			a := newHookAgent(t)
			sendChimera(t, a, `{"type":"turn.start"}`)
			got := sendChimera(t, a, `{"type":"turn.complete","reason":"`+tc.reason+`"}`)
			if got.activity != tc.want {
				t.Fatalf("activity = %q, want %q", got.activity, tc.want)
			}
		})
	}
}

func TestChimeraHook_RawBackgroundLifecycle(t *testing.T) {
	a := newHookAgent(t)
	for _, tc := range []struct {
		payload, activity string
		jobs              int
	}{
		{`{"type":"turn.start"}`, "working", 0},
		{`{"type":"background.start","jobId":"b1","command":"go test"}`, "working", 1},
		{`{"type":"agent.spawn","agentId":"a1","description":"review","background":true}`, "working", 2},
		{`{"type":"agent.spawn","agentId":"foreground","background":false}`, "working", 2},
		{`{"type":"turn.complete","reason":"stop"}`, "background", 2},
		{`{"type":"background.exit","jobId":"b1","command":"go test"}`, "background", 1},
		{`{"type":"agent.finish","agentId":"a1","background":true}`, "", 0},
	} {
		got := sendChimera(t, a, tc.payload)
		if got.activity != tc.activity || len(got.jobs) != tc.jobs {
			t.Fatalf("%s: %+v, want %q with %d jobs", tc.payload, got, tc.activity, tc.jobs)
		}
		if tc.jobs == 2 && (got.jobs[0].Kind != store.JobBash || got.jobs[0].Label != "go test" || got.jobs[1].Kind != store.JobSubagent || got.jobs[1].Label != "review") {
			t.Fatalf("jobs = %+v", got.jobs)
		}
	}
}

func TestChimeraHook_StaleScriptsCannotChangeModState(t *testing.T) {
	a := newHookAgent(t)
	sendChimera(t, a, `{"type":"turn.start"}`)
	if err := cli.AgentState(a.s, cli.AgentStateReport{TaskID: a.taskID, ExecutionID: a.execution, Activity: ""}, a.notifier, a.notifier); err != nil {
		t.Fatal(err)
	}
	if got := a.now("script"); got.activity != "working" {
		t.Fatalf("stale script changed mod state: %+v", got)
	}
}

func TestChimeraHook_SubagentEmittersAreIgnored(t *testing.T) {
	a := newHookAgent(t)
	sendChimera(t, a, `{"type":"turn.start"}`)
	for _, payload := range []string{
		`{"type":"turn.complete","reason":"error","isSubagent":true}`,
		`{"type":"background.start","jobId":"child-job","command":"test","isSubagent":true}`,
		`{"type":"permission.request","requestId":"child-request","isSubagent":true}`,
	} {
		got := sendChimera(t, a, payload)
		if got.activity != "working" || len(got.jobs) != 0 {
			t.Fatalf("child emitter changed parent: %+v", got)
		}
	}
}

func TestChimeraHook_NormalizedJobReportsRemainSupported(t *testing.T) {
	a := newHookAgent(t)
	got := sendChimera(t, a, `{"type":"background.start","started":{"kind":"bash","id":"b1","label":"test"}}`)
	if len(got.jobs) != 1 || got.jobs[0].ID != "b1" {
		t.Fatalf("normalized start: %+v", got)
	}
	got = sendChimera(t, a, `{"type":"turn.complete","reason":"stop","running":[{"kind":"subagent","id":"a1","label":"review"}]}`)
	if got.activity != "background" || len(got.jobs) != 1 || got.jobs[0].ID != "a1" {
		t.Fatalf("normalized running: %+v", got)
	}
	got = sendChimera(t, a, `{"type":"turn.start","woke":["a1"]}`)
	if got.activity != "working" || len(got.jobs) != 0 {
		t.Fatalf("normalized woke: %+v", got)
	}
	got = sendChimera(t, a, `{"type":"turn.complete","reason":"stop","running":[]}`)
	if got.activity != "" || len(got.jobs) != 0 {
		t.Fatalf("empty running: %+v", got)
	}
}

func TestChimeraHook_SubagentOutcomes(t *testing.T) {
	for _, background := range []bool{false, true} {
		for _, reason := range []string{"stop", "error", "interrupted"} {
			t.Run(fmt.Sprintf("background=%v/%s", background, reason), func(t *testing.T) {
				a := newHookAgent(t)
				sendChimera(t, a, `{"type":"session.start","sessionId":"parent"}`)
				sendChimera(t, a, fmt.Sprintf(`{"type":"agent.spawn","sessionId":"parent","agentId":"subject","description":"Investigate","background":%v}`, background))
				sendChimera(t, a, fmt.Sprintf(`{"type":"agent.finish","sessionId":"parent","agentId":"subject","background":%v,"reason":%q}`, background, reason))
				session, err := a.s.GetAgentSessionByTaskID(context.Background(), a.taskID)
				if err != nil {
					t.Fatal(err)
				}
				var children []struct {
					Status  string `json:"status"`
					Reason  string `json:"reason"`
					EndedAt int64  `json:"endedAt"`
				}
				if err := json.Unmarshal([]byte(session.ChimeraSubagents), &children); err != nil {
					t.Fatal(err)
				}
				want := reason
				if reason == "stop" {
					want = "finished"
				}
				if len(children) != 1 || children[0].Status != want || children[0].Reason != reason || children[0].EndedAt == 0 || len(session.BackgroundJobs) != 0 {
					t.Fatalf("session=%+v children=%+v", session, children)
				}
			})
		}
	}
}

// A Chimera subagent, foreground or background, is timed from its spawn to
// its finish; one its subagent emitted is not the session's.
func TestChimeraHook_SubagentsAreTimedFromSpawnToFinish(t *testing.T) {
	a := newHookAgent(t)
	sendChimera(t, a, `{"type":"turn.start","sessionId":"s1"}`)
	sendChimera(t, a, `{"type":"agent.spawn","sessionId":"s1","agentId":"fg","background":false,"description":"look"}`)
	sendChimera(t, a, `{"type":"agent.spawn","sessionId":"s1","agentId":"bg","background":true,"description":"count"}`)
	sendChimera(t, a, `{"type":"agent.spawn","sessionId":"child","agentId":"grandchild","isSubagent":true}`)
	if got := a.openSubagents(); fmt.Sprint(got) != "[fg bg]" {
		t.Fatalf("open = %v, want both subagents of the session", got)
	}

	sendChimera(t, a, `{"type":"agent.finish","sessionId":"s1","agentId":"fg","reason":"stop","background":false}`)
	if got := a.openSubagents(); fmt.Sprint(got) != "[bg]" {
		t.Fatalf("open after one finished = %v", got)
	}

	sendChimera(t, a, `{"type":"session.end","sessionId":"s1","reason":"exit"}`)
	if got := a.openSubagents(); len(got) != 0 {
		t.Fatalf("open after the session ended = %v, want none", got)
	}
}

func chimeraShells(t *testing.T, a *hookAgent) store.BackgroundShells {
	t.Helper()
	session, err := a.s.GetAgentSessionByTaskID(context.Background(), a.taskID)
	if err != nil {
		t.Fatal(err)
	}
	return session.BackgroundShells
}

func TestChimeraHook_BackgroundShellsAreRecordedWithTheirExitCodes(t *testing.T) {
	a := newHookAgent(t)
	sendChimera(t, a, `{"type":"turn.start"}`)
	sendChimera(t, a, `{"type":"background.start","jobId":"b1","command":"sleep 2; echo hi; sleep 30"}`)
	sendChimera(t, a, `{"type":"background.start","jobId":"b2","command":"npm run dev"}`)
	shells := chimeraShells(t, a)
	if len(shells) != 2 || shells[0].ID != "b1" || shells[0].Command != "sleep 2; echo hi; sleep 30" || shells[0].Status != store.ShellRunning ||
		shells[0].StartedAt.IsZero() || shells[1].ID != "b2" || shells[1].Status != store.ShellRunning {
		t.Fatalf("shells = %+v", shells)
	}

	sendChimera(t, a, `{"type":"background.exit","jobId":"b1","command":"sleep 2; echo hi; sleep 30","exitCode":3}`)
	shells = chimeraShells(t, a)
	if shells[0].Status != store.ShellExited || shells[0].ExitCode == nil || *shells[0].ExitCode != 3 || shells[0].EndedAt.IsZero() {
		t.Fatalf("after its exit = %+v", shells[0])
	}
	if shells[1].Status != store.ShellRunning {
		t.Fatalf("the other = %+v", shells[1])
	}
	// The job that woke nothing more is gone from the running jobs, but the
	// shell stays listed as ended.
	if got := sendChimera(t, a, `{"type":"turn.complete","reason":"stop"}`); len(got.jobs) != 1 || got.jobs[0].ID != "b2" {
		t.Fatalf("jobs = %+v", got.jobs)
	}
	if len(chimeraShells(t, a)) != 2 {
		t.Fatal("an ended shell left the list")
	}
}

func TestChimeraHook_AShellStillRunningWhenTheProcessExitsIsKilled(t *testing.T) {
	a := newHookAgent(t)
	sendChimera(t, a, `{"type":"background.start","jobId":"b1","command":"npm run dev"}`)
	sendChimera(t, a, `{"type":"session.end","reason":"switch"}`)
	if shells := chimeraShells(t, a); shells[0].Status != store.ShellRunning {
		t.Fatalf("after a switch = %+v", shells)
	}
	sendChimera(t, a, `{"type":"session.end","reason":"exit"}`)
	if shells := chimeraShells(t, a); shells[0].Status != store.ShellKilled || shells[0].ExitCode != nil {
		t.Fatalf("after the exit = %+v", shells)
	}
}

func TestChimeraHook_AnExitWithoutItsStartIsStillRecorded(t *testing.T) {
	a := newHookAgent(t)
	sendChimera(t, a, `{"type":"background.exit","jobId":"b9","command":"make","exitCode":0}`)
	shells := chimeraShells(t, a)
	if len(shells) != 1 || shells[0].Status != store.ShellExited || *shells[0].ExitCode != 0 || shells[0].Command != "make" {
		t.Fatalf("shells = %+v", shells)
	}
}

func TestChimeraHook_ShellsAreBounded(t *testing.T) {
	a := newHookAgent(t)
	for i := range 40 {
		sendChimera(t, a, fmt.Sprintf(`{"type":"background.start","jobId":"b%d","command":"true"}`, i))
		sendChimera(t, a, fmt.Sprintf(`{"type":"background.exit","jobId":"b%d","exitCode":0}`, i))
	}
	shells := chimeraShells(t, a)
	if len(shells) != 32 || shells[len(shells)-1].ID != "b39" {
		t.Fatalf("%d shells, last %+v", len(shells), shells[len(shells)-1])
	}
}
