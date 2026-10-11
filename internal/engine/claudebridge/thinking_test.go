package claudebridge

import (
	"reflect"
	"testing"
)

func thinkingBlock(text string) map[string]any {
	return map[string]any{"type": "thinking", "thinking": text}
}

func thinkingDelta(index int, text string) map[string]any {
	return map[string]any{"type": "thinking_delta", "turnId": "turn-1", "step": 0, "index": index, "text": text}
}

// parts returns the kind of each part of every assistant message of a
// snapshot, a message to a slice.
func parts(snap snapshot) [][]string {
	var out [][]string
	for _, message := range snap.Messages {
		blocks, _ := message.Content.([]any)
		if message.Role != "assistant" || len(blocks) == 0 {
			continue
		}
		var kinds []string
		for _, block := range blocks {
			kinds = append(kinds, block.(map[string]any)["type"].(string))
		}
		out = append(out, kinds)
	}
	return out
}

// thoughts returns the reasoning text of a snapshot, in order.
func thoughts(snap snapshot) []string {
	var out []string
	for _, message := range snap.Messages {
		blocks, _ := message.Content.([]any)
		for _, block := range blocks {
			if block, _ := block.(map[string]any); block["type"] == "reasoning" {
				out = append(out, block["text"].(string))
			}
		}
	}
	return out
}

func TestThinkingStreamsThenSettlesFromTheStoredRow(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")
	h.mod(map[string]any{"type": "turn_start", "turnId": "turn-1"})
	nextEvent(t, events)

	h.mod(thinkingDelta(0, "The flag belongs "), thinkingDelta(0, "in list.ts."))
	first, second := nextEvent(t, events), nextEvent(t, events)
	if first["type"] != "reasoning_text_delta" || first["delta"] != "The flag belongs " || second["delta"] != "in list.ts." {
		t.Fatalf("deltas = %v, %v", first, second)
	}
	streamID, _ := first["id"].(string)
	if streamID == "" || second["id"] != streamID {
		t.Fatalf("deltas of one block must share a stream id: %v, %v", first["id"], second["id"])
	}
	if got := thoughts(h.snapshot()); len(got) != 0 {
		t.Fatalf("a thought still arriving is not in the snapshot yet: %q", got)
	}

	h.mod(assistantRow("row-t", thinkingBlock("The flag belongs in list.ts.")))
	done := nextEvent(t, events)
	if done["type"] != "reasoning_text_done" || done["text"] != "The flag belongs in list.ts." || done["id"] != streamID {
		t.Fatalf("done = %v, want it to settle stream %q", done, streamID)
	}
	if got := thoughts(h.snapshot()); !reflect.DeepEqual(got, []string{"The flag belongs in list.ts."}) {
		t.Fatalf("snapshot thoughts = %q", got)
	}
}

// The transcript keeps the block the model signed, which for most models has
// no text; what streamed is then all there is to show.
func TestThinkingThatOnlyStreamedIsKeptAsItStreamed(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")
	h.mod(map[string]any{"type": "turn_start", "turnId": "turn-1"})
	nextEvent(t, events)

	h.mod(thinkingDelta(0, "Checking the tests first."))
	nextEvent(t, events)
	h.mod(assistantRow("row-t", thinkingBlock("")))

	done := nextEvent(t, events)
	if done["type"] != "reasoning_text_done" || done["text"] != "Checking the tests first." {
		t.Fatalf("done = %v", done)
	}
	if got := thoughts(h.snapshot()); !reflect.DeepEqual(got, []string{"Checking the tests first."}) {
		t.Fatalf("snapshot thoughts = %q", got)
	}
}

func TestThinkingWithNothingToReadIsNotShown(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")

	h.mod(assistantRow("row-t", thinkingBlock("")))
	h.mod(assistantRow("row-a", textBlock("Done.")))

	if event := nextEvent(t, events); event["type"] != "assistant_text_done" {
		t.Fatalf("event = %v, want only the reply", event)
	}
	if got := thoughts(h.snapshot()); len(got) != 0 {
		t.Fatalf("snapshot thoughts = %q", got)
	}
}

func TestAThoughtStandsBeforeWhatItLedTo(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))

	// Claude Code stores each block of a response as a row of its own.
	h.mod(assistantRow("row-1", thinkingBlock("Read the file first.")))
	h.mod(assistantRow("row-2", textBlock("Let me look.")))
	h.mod(assistantRow("row-3", map[string]any{"type": "tool_use", "id": "toolu_1", "name": "Read", "input": map[string]any{"file_path": "/repo/list.ts"}}))
	h.mod(assistantRow("row-4", thinkingBlock("Now change it.")))
	h.mod(assistantRow("row-5", map[string]any{"type": "tool_use", "id": "toolu_2", "name": "Edit", "input": map[string]any{"file_path": "/repo/list.ts"}}))

	snap := h.snapshot()
	want := [][]string{{"reasoning", "text", "tool-call"}, {"reasoning", "tool-call"}}
	if got := parts(snap); !reflect.DeepEqual(got, want) {
		t.Fatalf("assistant messages = %v, want %v", got, want)
	}
	if len(snap.ToolCalls) != 2 {
		t.Fatalf("tool calls = %+v", snap.ToolCalls)
	}
}

func TestThinkingIsNotTakenForTheReply(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")
	h.mod(map[string]any{"type": "turn_start", "turnId": "turn-1"})
	nextEvent(t, events)

	// The thought is block 0 of the response and the reply block 1.
	h.mod(thinkingDelta(0, "Short answer."))
	h.mod(map[string]any{"type": "text_delta", "turnId": "turn-1", "step": 0, "index": 1, "text": "Yes."})
	thought, reply := nextEvent(t, events), nextEvent(t, events)

	h.mod(assistantRow("row-t", thinkingBlock("Short answer.")))
	h.mod(assistantRow("row-a", textBlock("Yes.")))
	thoughtDone, replyDone := nextEvent(t, events), nextEvent(t, events)

	if thoughtDone["type"] != "reasoning_text_done" || thoughtDone["id"] != thought["id"] {
		t.Fatalf("the stored thought should settle the thought's stream: %v", thoughtDone)
	}
	if replyDone["type"] != "assistant_text_done" || replyDone["id"] != reply["id"] {
		t.Fatalf("the stored reply should settle the reply's stream: %v", replyDone)
	}
}

func TestAThoughtCutShortDoesNotOutliveItsTurn(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")
	h.mod(map[string]any{"type": "turn_start", "turnId": "turn-1"})
	nextEvent(t, events)

	h.mod(thinkingDelta(0, "Half a thou"))
	nextEvent(t, events)
	h.mod(map[string]any{"type": "turn_complete", "turnId": "turn-1", "reason": "interrupted"})
	nextEvent(t, events)

	// The next turn's first thought is its own, not the rest of that one.
	h.mod(map[string]any{"type": "turn_start", "turnId": "turn-2"})
	nextEvent(t, events)
	h.mod(assistantRow("row-t", thinkingBlock("A new thought.")))
	if done := nextEvent(t, events); done["text"] != "A new thought." {
		t.Fatalf("done = %v", done)
	}
}
