package codexbridge

import (
	"reflect"
	"testing"
)

func reasoningItem(id string, summary []string, content []string) map[string]any {
	return map[string]any{"type": "reasoning", "id": id, "summary": summary, "content": content}
}

// thoughts returns the reasoning of each message of a snapshot that has any.
func thoughts(snap snapshot) []string {
	var out []string
	for _, message := range snap.Messages {
		parts, _ := message.Content.([]any)
		for _, part := range parts {
			if part, _ := part.(map[string]any); part["type"] == "reasoning" {
				out = append(out, part["text"].(string))
			}
		}
	}
	return out
}

func TestReasoningSummaryIsShownAsItArrivesAndKeptWithTheConversation(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")
	delta := func(method string, params map[string]any) {
		params["threadId"], params["turnId"], params["itemId"] = threadID, "turn-1", "rs-1"
		h.codex.notify(method, params)
	}

	h.item("started", reasoningItem("rs-1", []string{}, []string{}))
	delta("item/reasoning/summaryPartAdded", map[string]any{"summaryIndex": 0})
	delta("item/reasoning/summaryTextDelta", map[string]any{"summaryIndex": 0, "delta": "**Planning the change**"})
	// A summary comes in parts; the next one begins a paragraph of its own.
	delta("item/reasoning/summaryPartAdded", map[string]any{"summaryIndex": 1})
	delta("item/reasoning/summaryTextDelta", map[string]any{"summaryIndex": 1, "delta": "The flag belongs in list.ts."})

	var streamed string
	for i := 0; i < 3; i++ {
		event := nextEvent(t, events)
		if event["type"] != "reasoning_text_delta" || event["id"] != "rs-1" {
			t.Fatalf("event = %v", event)
		}
		streamed += event["delta"].(string)
	}
	if want := "**Planning the change**\n\nThe flag belongs in list.ts."; streamed != want {
		t.Fatalf("streamed %q, want %q", streamed, want)
	}
	if got := thoughts(h.snapshot()); len(got) != 0 {
		t.Fatalf("a thought still arriving is not in the snapshot yet: %v", got)
	}

	h.item("completed", reasoningItem("rs-1", []string{"**Planning the change**", "The flag belongs in list.ts."}, []string{}))
	done := nextEvent(t, events)
	if done["type"] != "reasoning_text_done" || done["id"] != "rs-1" || done["text"] != streamed {
		t.Fatalf("event = %v", done)
	}
	if got := thoughts(h.snapshot()); !reflect.DeepEqual(got, []string{streamed}) {
		t.Fatalf("snapshot thoughts = %q", got)
	}
}

func TestAThoughtStandsBeforeTheCallItLedTo(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")

	h.item("completed", reasoningItem("rs-1", []string{"Look at the files first."}, nil))
	h.item("completed", commandItem("exec-1", "ls", "completed", "main.go\n", 0))
	h.item("completed", agentMessage("msg-1", "One Go file."))

	snap := h.snapshot()
	// The empty entry that holds the launch prompt's place, the step that
	// thought and ran the command, and the reply.
	if len(snap.Messages) != 3 {
		t.Fatalf("messages = %+v", snap.Messages)
	}
	step, _ := snap.Messages[1].Content.([]any)
	kinds := []string{}
	for _, part := range step {
		kinds = append(kinds, part.(map[string]any)["type"].(string))
	}
	if want := []string{"reasoning", "tool-call"}; !reflect.DeepEqual(kinds, want) {
		t.Fatalf("step = %v, want %v", kinds, want)
	}
	if len(snap.ToolCalls) != 1 {
		t.Fatalf("tool calls = %+v", snap.ToolCalls)
	}
}

func TestRawReasoningIsShownWhenTheModelGivesNoSummary(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")

	h.item("started", reasoningItem("rs-1", nil, nil))
	h.codex.notify("item/reasoning/textDelta", map[string]any{
		"threadId": threadID, "turnId": "turn-1", "itemId": "rs-1", "contentIndex": 0, "delta": "The user wants a flag.",
	})
	if event := nextEvent(t, events); event["type"] != "reasoning_text_delta" || event["delta"] != "The user wants a flag." {
		t.Fatalf("event = %v", event)
	}

	h.item("completed", reasoningItem("rs-1", []string{}, []string{"The user wants a flag."}))
	if event := nextEvent(t, events); event["type"] != "reasoning_text_done" || event["text"] != "The user wants a flag." {
		t.Fatalf("event = %v", event)
	}
}

func TestReasoningWithNothingToReadIsNotShown(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")

	// The model reasoned, and Codex was told to keep no summary of it.
	h.item("started", reasoningItem("rs-1", []string{}, []string{}))
	h.item("completed", reasoningItem("rs-1", []string{}, []string{}))
	h.item("completed", agentMessage("msg-1", "Done."))

	if event := nextEvent(t, events); event["type"] != "assistant_text_done" {
		t.Fatalf("event = %v, want only the reply", event)
	}
	if got := thoughts(h.snapshot()); len(got) != 0 {
		t.Fatalf("snapshot thoughts = %q", got)
	}
}

func TestAResumedThreadShowsWhatItThoughtEarlier(t *testing.T) {
	h := newHarness(t)
	events := h.subscribe("")
	h.codex.on("thread/resume", func(map[string]any) (any, *rpcError) {
		return map[string]any{"thread": terminalThread(map[string]any{"turns": []any{
			map[string]any{"id": "turn-0", "status": "completed", "items": []any{
				userMessage("item-u0", "what is in this repo?"),
				reasoningItem("rs-0", []string{"Look at the files first."}, nil),
				agentMessage("item-a0", "One Go file."),
			}},
		}})}, nil
	})

	h.codex.notify("thread/started", map[string]any{"thread": terminalThread(nil)})

	if event := eventOf(t, events, "reasoning_text_done"); event["text"] != "Look at the files first." {
		t.Fatalf("event = %v", event)
	}
	if got := thoughts(h.snapshot()); !reflect.DeepEqual(got, []string{"Look at the files first."}) {
		t.Fatalf("snapshot thoughts = %q", got)
	}
}
