package claudebridge

import (
	"net/http"
	"testing"
)

func TestCompactAsksTheModForTheSessionsOwnCompaction(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))

	code, _ := h.post("/v1/sessions/task-1/compact", map[string]any{"instructions": "  keep the plan "})

	if code != http.StatusAccepted {
		t.Fatalf("POST compact = %d", code)
	}
	if command := h.nextCommand(); command["type"] != "compact" || command["instructions"] != "keep the plan" {
		t.Fatalf("command = %v", command)
	}
}

func TestCompactWithNoBodyHasNoInstructions(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))

	if code, _ := h.post("/v1/sessions/task-1/compact", nil); code != http.StatusAccepted {
		t.Fatalf("POST compact = %d", code)
	}
	if command := h.nextCommand(); command["type"] != "compact" || command["instructions"] != nil {
		t.Fatalf("command = %v", command)
	}
}

// Claude Code compacts only between turns, and one compaction at a time.
func TestCompactIsRefusedDuringATurnOrACompaction(t *testing.T) {
	for name, event := range map[string]map[string]any{
		"turn":       {"type": "turn_start", "turnId": "turn-1"},
		"compaction": {"type": "compaction_started"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.mod(hello(nil), event)

			if code, _ := h.post("/v1/sessions/task-1/compact", nil); code != http.StatusConflict {
				t.Fatalf("POST compact = %d, want 409", code)
			}
		})
	}
}

func TestACompactionIsReportedAndTheContextFollowsIt(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(map[string]any{"context": map[string]any{"tokens": 49805, "window": 1000000}}))
	events := h.subscribe("")

	h.mod(map[string]any{"type": "compaction_started"})
	if event := nextEvent(t, events); event["type"] != "compaction_started" || event["reason"] != "manual" {
		t.Fatalf("event = %v", event)
	}

	h.mod(map[string]any{
		"type": "compaction_finished", "tokensBefore": 49805, "tokensAfter": 6100,
		"context": map[string]any{"tokens": 6100, "window": 1000000},
	})
	finished := nextEvent(t, events)
	if finished["type"] != "compaction_finished" || finished["tokensBefore"] != float64(49805) || finished["tokensAfter"] != float64(6100) {
		t.Fatalf("event = %v", finished)
	}
	// The meter drops with it: the watcher reads usage_updated.
	if usage := nextEvent(t, events); usage["type"] != "usage_updated" || usage["usedContextTokens"] != float64(6100) {
		t.Fatalf("event = %v", usage)
	}

	// Finished, the session can be compacted again.
	if code, _ := h.post("/v1/sessions/task-1/compact", nil); code != http.StatusAccepted {
		t.Fatalf("POST compact after a compaction = %d", code)
	}
}

func TestAFailedCompactionIsReported(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")

	h.mod(map[string]any{"type": "compaction_started"}, map[string]any{"type": "compaction_failed", "error": "Not enough messages to compact."})

	nextEvent(t, events)
	if event := nextEvent(t, events); event["type"] != "compaction_failed" || event["error"] != "Not enough messages to compact." {
		t.Fatalf("event = %v", event)
	}
	if code, _ := h.post("/v1/sessions/task-1/compact", nil); code != http.StatusAccepted {
		t.Fatalf("POST compact after a failure = %d", code)
	}
}

// A mod that reloaded mid-compaction never reports its end: the session it
// describes again is not compacting.
func TestASessionDescribedAgainIsNotCompacting(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), map[string]any{"type": "compaction_started"})

	h.mod(hello(nil))

	if code, _ := h.post("/v1/sessions/task-1/compact", nil); code != http.StatusAccepted {
		t.Fatalf("POST compact = %d", code)
	}
}
