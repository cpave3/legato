package claudebridge

import (
	"net/http/httptest"
	"testing"
	"time"
)

func turnEndedHarness(t *testing.T, delay time.Duration) (*harness, <-chan string) {
	t.Helper()
	ended := make(chan string, 8)
	bridge := New(Options{
		TaskID:         "task-1",
		Commands:       &commandLog{lines: make(chan map[string]any, 64)},
		Now:            func() time.Time { return time.UnixMilli(1_700_000_000_000) },
		OnTurnEnded:    func(reason string) { ended <- reason },
		TurnEndedDelay: delay,
	})
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		bridge.Close()
		server.Close()
	})
	return &harness{t: t, bridge: bridge, server: server}, ended
}

// No Claude Code hook fires when a turn is interrupted, so the bridge, which
// sees every turn end, says when a main turn ended without an answer.
func TestAMainTurnThatEndsWithoutAnAnswerIsReported(t *testing.T) {
	h, ended := turnEndedHarness(t, time.Millisecond)

	h.mod(hello(nil),
		map[string]any{"type": "turn_start", "turnId": "turn-1"},
		map[string]any{"type": "turn_complete", "turnId": "turn-1", "reason": "answer"},
		map[string]any{"type": "turn_start", "turnId": "turn-2"},
		// A subagent's turn is not the session's.
		map[string]any{"type": "turn_complete", "turnId": "turn-9", "reason": "aborted", "agentId": "agent-7"},
		map[string]any{"type": "turn_complete", "turnId": "turn-2", "reason": "aborted"},
	)

	select {
	case reason := <-ended:
		if reason != "aborted" {
			t.Fatalf("reported %q, want aborted", reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the interrupted turn was never reported")
	}
	select {
	case reason := <-ended:
		t.Fatalf("also reported %q", reason)
	case <-time.After(50 * time.Millisecond):
	}
}

// A message held during the turn starts the next one at once. Its prompt
// reaches Legato on its own path, which may be faster: the end of the turn
// before it must not then mark the new turn idle.
func TestATurnThatEndsAsTheNextBeginsIsNotReported(t *testing.T) {
	h, ended := turnEndedHarness(t, 200*time.Millisecond)

	h.mod(hello(nil),
		map[string]any{"type": "turn_start", "turnId": "turn-1"},
		map[string]any{"type": "turn_complete", "turnId": "turn-1", "reason": "aborted"},
	)
	h.mod(map[string]any{"type": "turn_start", "turnId": "turn-2"})

	select {
	case reason := <-ended:
		t.Fatalf("reported %q although the next turn had started", reason)
	case <-time.After(400 * time.Millisecond):
	}
}
