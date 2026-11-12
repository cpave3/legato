package claudebridge

import (
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"
)

// No Claude Code hook fires when a turn is interrupted, so the bridge, which
// sees every turn end, says when a main turn ended without an answer.
func TestAMainTurnThatEndsWithoutAnAnswerIsReported(t *testing.T) {
	var mu sync.Mutex
	var reasons []string
	bridge := New(Options{
		TaskID:   "task-1",
		Commands: &commandLog{lines: make(chan map[string]any, 64)},
		Now:      func() time.Time { return time.UnixMilli(1_700_000_000_000) },
		OnTurnEnded: func(reason string) {
			mu.Lock()
			defer mu.Unlock()
			reasons = append(reasons, reason)
		},
	})
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		bridge.Close()
		server.Close()
	})
	h := &harness{t: t, bridge: bridge, server: server}

	h.mod(hello(nil),
		map[string]any{"type": "turn_start", "turnId": "turn-1"},
		map[string]any{"type": "turn_complete", "turnId": "turn-1", "reason": "answer"},
		map[string]any{"type": "turn_start", "turnId": "turn-2"},
		// A subagent's turn is not the session's.
		map[string]any{"type": "turn_complete", "turnId": "turn-9", "reason": "aborted", "agentId": "agent-7"},
		map[string]any{"type": "turn_complete", "turnId": "turn-2", "reason": "aborted"},
	)

	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(reasons, []string{"aborted"}) {
		t.Fatalf("reported %q, want only the interrupted main turn", reasons)
	}
}
