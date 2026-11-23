package codexbridge

import (
	"net/http"
	"testing"
)

func (h *harness) usage(tokens int) {
	h.t.Helper()
	h.codex.notify("thread/tokenUsage/updated", map[string]any{"threadId": threadID, "turnId": "turn-1", "tokenUsage": map[string]any{
		"last": map[string]any{"totalTokens": tokens}, "modelContextWindow": 258400,
	}})
}

func TestCompactStartsCodexsOwnCompactionOnTheTerminalsThread(t *testing.T) {
	h := newHarness(t)
	h.attach()

	if code, body := h.post("/v1/sessions/task-1/compact", map[string]any{}); code != http.StatusAccepted {
		t.Fatalf("POST compact = %d %s", code, body)
	}
	if params := h.codex.called("thread/compact/start"); params["threadId"] != threadID {
		t.Fatalf("thread/compact/start = %v", params)
	}
}

// Codex would stop the turn to compact; Legato compacts between turns only.
func TestCompactIsRefusedDuringATurn(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")

	if code, _ := h.post("/v1/sessions/task-1/compact", nil); code != http.StatusConflict {
		t.Fatalf("POST compact = %d, want 409", code)
	}
	if h.codex.count("thread/compact/start") != 0 {
		t.Fatal("a running turn was compacted")
	}
}

// Codex compacts with its own prompt only; focus words would be dropped.
func TestCompactWithFocusIsRefused(t *testing.T) {
	h := newHarness(t)
	h.attach()

	if code, _ := h.post("/v1/sessions/task-1/compact", map[string]any{"instructions": "keep the plan"}); code != http.StatusBadRequest {
		t.Fatalf("POST compact = %d, want 400", code)
	}
	if h.codex.count("thread/compact/start") != 0 {
		t.Fatal("compacted without the focus asked for")
	}
}

func TestACompactionIsReportedWithTheContextBeforeAndAfter(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	h.usage(49805)
	h.finish("turn-1", "completed")
	events := h.subscribe("")
	if code, _ := h.post("/v1/sessions/task-1/compact", nil); code != http.StatusAccepted {
		t.Fatalf("POST compact = %d", code)
	}

	// Codex runs the compaction as a turn of its own.
	h.codex.notify("turn/started", map[string]any{"threadId": threadID, "turn": map[string]any{"id": "turn-2", "status": "inProgress", "items": []any{}}})
	h.item("started", map[string]any{"type": "contextCompaction", "id": "c-1"})
	h.usage(12025)
	h.item("completed", map[string]any{"type": "contextCompaction", "id": "c-1"})

	var seen []map[string]any
	for len(seen) == 0 || seen[len(seen)-1]["type"] != "compaction_finished" {
		seen = append(seen, nextEvent(t, events))
	}
	var started map[string]any
	for _, event := range seen {
		if event["type"] == "compaction_started" {
			started = event
		}
	}
	if started == nil || started["reason"] != "manual" {
		t.Fatalf("events = %v, want compaction_started for the compaction asked for", seen)
	}
	if finished := seen[len(seen)-1]; finished["tokensBefore"] != float64(49805) || finished["tokensAfter"] != float64(12025) {
		t.Fatalf("finished = %v", finished)
	}
}

// Codex compacts on its own when its context fills; that is reported too.
func TestCodexsOwnCompactionIsReportedAsAThresholdOne(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")

	h.item("started", map[string]any{"type": "contextCompaction", "id": "c-1"})

	if event := nextEvent(t, events); event["type"] != "compaction_started" || event["reason"] != "threshold" {
		t.Fatalf("event = %v", event)
	}
}

// Joining a thread replays its history; a compaction long done is no news.
func TestPastCompactionsInAJoinedThreadAreNotReported(t *testing.T) {
	h := newHarness(t)
	h.attach()
	events := h.subscribe("")

	h.begin("turn-2", map[string]any{"id": "turn-1", "status": "completed", "items": []any{
		map[string]any{"type": "contextCompaction", "id": "c-old"},
		agentMessage("item-a1", "done"),
	}})

	for {
		event := nextEvent(t, events)
		if event["type"] == "compaction_started" || event["type"] == "compaction_finished" {
			t.Fatalf("event = %v", event)
		}
		if event["type"] == "assistant_text_done" {
			return
		}
	}
}
