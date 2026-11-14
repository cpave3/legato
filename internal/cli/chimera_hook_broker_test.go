package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cpave3/legato/internal/cli"
)

// These synthetic CHIM69 shapes go through the same parser the link uses,
// then the authenticated broker handler and its real SQLite store.
func TestChimeraHook_BrokerAndStoreAgree(t *testing.T) {
	direct := newHookAgent(t)
	s := newTestStore(t)
	seedColumns(t, s)
	_, token := brokerSession(t, s, "abc123")
	h := cli.NewBrokerHandler(s, cli.BrokerDeps{})
	for _, tc := range []struct {
		payload, activity string
		jobs              int
	}{
		{`{"type":"turn.start"}`, "working", 0},
		{`{"type":"permission.request","requestId":"p1","pendingRequests":1}`, "waiting", 0},
		{`{"type":"question.request","requestId":"q1","pendingRequests":2}`, "waiting", 0},
		{`{"type":"message.append"}`, "waiting", 0},
		{`{"type":"permission.resolved","requestId":"p1","pendingRequests":1}`, "waiting", 0},
		{`{"type":"question.resolved","requestId":"q1"}`, "waiting", 0},
		{`{"type":"question.resolved","requestId":"q1","pendingRequests":0}`, "working", 0},
		{`{"type":"turn.complete","reason":"stop"}`, "", 0},
		{`{"type":"turn.start"}`, "working", 0},
		{`{"type":"background.start","jobId":"b1","command":"go test"}`, "working", 1},
		{`{"type":"agent.spawn","agentId":"a1","description":"review","background":true}`, "working", 2},
		{`{"type":"agent.spawn","agentId":"fg","background":false}`, "working", 2},
		{`{"type":"turn.complete","reason":"stop"}`, "background", 2},
		{`{"type":"turn.complete","reason":"error","isSubagent":true}`, "background", 2},
		{`{"type":"background.exit","jobId":"b1","command":"go test"}`, "background", 1},
		{`{"type":"agent.finish","agentId":"a1","description":"review","background":true}`, "", 0},
		{`{"type":"background.start","jobId":"b2","command":"wait"}`, "", 1},
		{`{"type":"turn.complete","reason":"stop"}`, "background", 1},
		{`{"type":"turn.complete","reason":"error"}`, "error", 1},
		{`{"type":"turn.complete","reason":"waiting_for_input"}`, "waiting", 1},
		{`{"type":"turn.complete","reason":"interrupted"}`, "", 1},
		{`{"type":"turn.complete","reason":"timeout"}`, "", 1},
		{`{"type":"turn.complete","reason":"cancelled"}`, "", 1},
		{`{"type":"turn.complete","reason":"future-reason"}`, "", 1},
		{`{"type":"turn.complete","reason":"stop","running":[]}`, "", 0},
	} {
		got := sendChimera(t, direct, tc.payload)
		hook, err := cli.ParseChimeraHook([]byte(tc.payload))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := call(h, token, "agent.hook", map[string]any{"kind": "chimera", "hook": hook}); err != nil {
			t.Fatal(err)
		}
		session, err := s.GetAgentSessionByTaskID(context.Background(), "abc123")
		if err != nil {
			t.Fatal(err)
		}
		if got.activity != tc.activity || len(got.jobs) != tc.jobs || session.Activity != tc.activity || len(session.BackgroundJobs) != tc.jobs || session.HookSource != "mod" {
			t.Fatalf("%s: direct=%+v broker=%+v, want %q with %d jobs", tc.payload, got, session, tc.activity, tc.jobs)
		}
		for i, job := range got.jobs {
			brokerJob := session.BackgroundJobs[i]
			if job.ID != brokerJob.ID || job.Kind != brokerJob.Kind || job.Label != brokerJob.Label || job.StartedAt.IsZero() || brokerJob.StartedAt.IsZero() {
				t.Fatalf("job mismatch: %+v vs %+v", job, brokerJob)
			}
		}
	}
	if _, err := call(h, token, "agent.state", map[string]any{"activity": "working", "session_id": "stale-child"}); err != nil {
		t.Fatal(err)
	}
	after, err := s.GetAgentSessionByTaskID(context.Background(), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if after.Activity != "" || len(after.BackgroundJobs) != 0 {
		t.Fatalf("stale script changed state: %+v", after)
	}
}

func TestChimeraHook_BrokerIgnoresOtherHookWritersAfterModClaim(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	_, token := brokerSession(t, s, "abc123")
	notifier := &recordingNotifier{}
	h := cli.NewBrokerHandler(s, cli.BrokerDeps{OS: notifier})
	for _, payload := range []string{
		`{"type":"background.start","jobId":"job","command":"sleep 60"}`,
		`{"type":"permission.request","requestId":"p"}`,
	} {
		hook, err := cli.ParseChimeraHook([]byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := call(h, token, "agent.hook", map[string]any{"kind": "chimera", "hook": hook}); err != nil {
			t.Fatal(err)
		}
	}
	beforeNotices := len(notifier.calls)
	for _, source := range []string{"claude-code", ""} {
		if _, err := call(h, token, "agent.hook", map[string]any{"kind": source, "hook": cli.ClaudeHook{Event: cli.ClaudeSessionEnd}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := call(h, token, "agent.state", map[string]any{"activity": ""}); err != nil {
		t.Fatal(err)
	}
	after, err := s.GetAgentSessionByTaskID(context.Background(), "abc123")
	if err != nil || after.Activity != "waiting" || after.HookSource != "mod" || len(after.BackgroundJobs) != 1 {
		t.Fatalf("other writer replaced mod state/jobs: %+v %v", after, err)
	}
	if len(notifier.calls) != beforeNotices {
		t.Fatalf("ignored hook emitted a notice: %+v", notifier.calls[beforeNotices:])
	}
}

func TestChimeraHook_BrokerRefusesAnotherTaskOrExecution(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	_, token := brokerSession(t, s, "abc123")
	otherExecution, _ := brokerSession(t, s, "other1")
	h := cli.NewBrokerHandler(s, cli.BrokerDeps{})
	for _, identity := range []map[string]any{{"task_id": "other1"}, {"execution_id": otherExecution}} {
		identity["kind"] = "chimera"
		identity["hook"] = cli.ChimeraHook{Event: "turn.start"}
		if _, err := call(h, token, "agent.hook", identity); code(err) != "forbidden" {
			t.Fatalf("spoofed identity: %v", err)
		}
	}
	for _, id := range []string{"abc123", "other1"} {
		session, err := s.GetAgentSessionByTaskID(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if session.Activity != "" || session.HookSource != "" {
			t.Fatalf("refused hook changed %s: %+v", id, session)
		}
	}
}

func TestChimeraHook_JobTextIsCleanedAndBoundedThroughStoreAndBroker(t *testing.T) {
	for _, brokered := range []bool{false, true} {
		t.Run(fmt.Sprint("broker=", brokered), func(t *testing.T) {
			s := newTestStore(t)
			seedColumns(t, s)
			execution, token := brokerSession(t, s, "abc123")
			h := cli.NewBrokerHandler(s, cli.BrokerDeps{})
			for i := 0; i < 40; i++ {
				raw, err := json.Marshal(map[string]any{"type": "background.start", "jobId": fmt.Sprintf("job-%d-", i) + strings.Repeat("x", 100), "command": "go\n\t test\u001b " + strings.Repeat("界", 250)})
				if err != nil {
					t.Fatal(err)
				}
				hook, err := cli.ParseChimeraHook(raw)
				if err != nil {
					t.Fatal(err)
				}
				if brokered {
					_, err = call(h, token, "agent.hook", map[string]any{"kind": "chimera", "hook": hook})
				} else {
					err = cli.ApplyChimeraHook(s, cli.ChimeraHookReport{TaskID: "abc123", ExecutionID: execution, Hook: hook}, nil, nil)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			session, err := s.GetAgentSessionByTaskID(context.Background(), "abc123")
			if err != nil {
				t.Fatal(err)
			}
			if len(session.BackgroundJobs) != 32 {
				t.Fatalf("jobs=%d, want 32", len(session.BackgroundJobs))
			}
			for _, job := range session.BackgroundJobs {
				if utf8.RuneCountInString(job.Label) != 200 || utf8.RuneCountInString(job.ID) != 64 || !strings.HasPrefix(job.Label, "go test ") || !strings.HasSuffix(job.Label, "…") || strings.ContainsAny(job.Label, "\n\t\x1b") {
					t.Fatalf("unclean job: %+v", job)
				}
			}
		})
	}
}
