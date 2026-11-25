package slackbot

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"nhooyr.io/websocket"
)

func postedBodies(t *testing.T, f *fakeSlack, method string) []map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, b := range f.bodies[method] {
		var m map[string]any
		if err := json.Unmarshal([]byte(b), &m); err != nil {
			t.Fatalf("body %q: %v", b, err)
		}
		out = append(out, m)
	}
	return out
}

// sameJSON says whether got, decoded JSON, is the JSON want.
func sameJSON(t *testing.T, got any, want string) bool {
	t.Helper()
	var w any
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("want %q: %v", want, err)
	}
	return reflect.DeepEqual(got, w)
}

func TestPostMessageWithButtonsSendsASectionAndAnActionsBlock(t *testing.T) {
	f := newFake(t)
	f.handle("chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "channel": "C1", "ts": "9.9"})
	})
	_, err := f.client().PostMessage(t.Context(), "C1", "Findings", "1.0",
		Button{Label: "🛠 Fix it", Value: "fix:T1", Style: "primary"},
		Button{Label: "Ticket", Value: "ticket:T1"})
	if err != nil {
		t.Fatal(err)
	}
	bodies := postedBodies(t, f, "chat.postMessage")
	if len(bodies) != 1 {
		t.Fatalf("bodies = %v", bodies)
	}
	b := bodies[0]
	if b["text"] != "Findings" || b["thread_ts"] != "1.0" {
		t.Errorf("text/thread = %v", b)
	}
	want := `[{"type":"section","text":{"type":"mrkdwn","text":"Findings"}},` +
		`{"type":"actions","block_id":"buttons","elements":[` +
		`{"type":"button","action_id":"button_0","text":{"type":"plain_text","text":"🛠 Fix it","emoji":true},"value":"fix:T1","style":"primary"},` +
		`{"type":"button","action_id":"button_1","text":{"type":"plain_text","text":"Ticket","emoji":true},"value":"ticket:T1"}]}]`
	if !sameJSON(t, b["blocks"], want) {
		t.Errorf("blocks =\n%v\nwant\n%s", b["blocks"], want)
	}
}

func TestPostMessageWithoutButtonsSendsNoBlocks(t *testing.T) {
	f := newFake(t)
	f.handle("chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "channel": "C1", "ts": "9.9"})
	})
	if _, err := f.client().PostMessage(t.Context(), "C1", "plain", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := postedBodies(t, f, "chat.postMessage")[0]["blocks"]; ok {
		t.Error("a message without buttons has blocks")
	}
}

func TestALongTextWithButtonsIsSplitIntoSections(t *testing.T) {
	f := newFake(t)
	f.handle("chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "channel": "C1", "ts": "9.9"})
	})
	// Lines of 1000 runes: three fit a section, so seven make three sections.
	line := strings.Repeat("é", 999)
	lines := make([]string, 7)
	for i := range lines {
		lines[i] = line
	}
	text := strings.Join(lines, "\n")
	// And one line longer than a section is cut at a rune boundary.
	text += "\n" + strings.Repeat("ü", 3500)
	if _, err := f.client().PostMessage(t.Context(), "C1", text, "", Button{Label: "Go", Value: "go"}); err != nil {
		t.Fatal(err)
	}
	blocks := postedBodies(t, f, "chat.postMessage")[0]["blocks"].([]any)
	var joined []string
	for _, b := range blocks[:len(blocks)-1] {
		s := b.(map[string]any)["text"].(map[string]any)["text"].(string)
		if n := utf8.RuneCountInString(s); n > 3000 || n == 0 {
			t.Errorf("section of %d runes", n)
		}
		joined = append(joined, s)
	}
	if len(joined) != 5 {
		t.Errorf("%d sections, want 5", len(joined))
	}
	if strings.ReplaceAll(strings.Join(joined, ""), "\n", "") != strings.ReplaceAll(text, "\n", "") {
		t.Error("the sections do not hold the text")
	}
}

func interactive(id string, payload map[string]any) map[string]any {
	return map[string]any{"type": "interactive", "envelope_id": id, "payload": payload}
}

func blockActions() map[string]any {
	return map[string]any{
		"type":       "block_actions",
		"trigger_id": "trig-1",
		"user":       map[string]any{"id": "U7", "username": "cameron"},
		"channel":    map[string]any{"id": "C1", "name": "ops"},
		"container":  map[string]any{"type": "message", "message_ts": "5.5", "channel_id": "C1"},
		"message": map[string]any{
			"ts": "5.5", "thread_ts": "5.0", "text": "Findings",
			"blocks": []any{
				map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": "Findings"}},
				map[string]any{"type": "actions", "block_id": "buttons", "elements": []any{}},
			},
		},
		"actions": []any{map[string]any{
			"type": "button", "action_id": "button_0", "block_id": "buttons",
			"text": map[string]any{"type": "plain_text", "text": "🛠 Fix it"}, "value": "fix:T1", "action_ts": "6.6",
		}},
	}
}

func TestServeAcksAClickBeforeHandingItOn(t *testing.T) {
	f := newFake(t)
	var acked atomic.Bool
	f.socket = func(ctx context.Context, c *websocket.Conn) {
		send(ctx, t, c, map[string]any{"type": "hello"})
		send(ctx, t, c, interactive("env-9", blockActions()))
		if got := readAck(ctx, t, c); got != "env-9" {
			t.Errorf("ack = %q", got)
		}
		acked.Store(true)
		_ = c.Close(websocket.StatusNormalClosure, "")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var clicks []Click
	_ = f.client().Serve(ctx, Handlers{Click: func(c Click) {
		deadline := time.Now().Add(2 * time.Second)
		for !acked.Load() && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if !acked.Load() {
			t.Error("click handled before the ack was sent")
		}
		clicks = append(clicks, c)
	}})
	if len(clicks) != 1 {
		t.Fatalf("clicks = %+v", clicks)
	}
	got := clicks[0]
	if string(got.Blocks) == "" {
		t.Error("the click has no blocks")
	}
	got.Blocks = nil
	want := Click{Channel: "C1", User: "U7", TS: "5.5", ThreadTS: "5.0", ActionID: "button_0",
		Value: "fix:T1", Label: "🛠 Fix it", InteractionID: "trig-1", Text: "Findings"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("click = %+v\nwant  %+v", got, want)
	}
}

func TestServeAcksAndIgnoresOtherInteractions(t *testing.T) {
	f := newFake(t)
	f.socket = func(ctx context.Context, c *websocket.Conn) {
		send(ctx, t, c, map[string]any{"type": "hello"})
		send(ctx, t, c, interactive("env-1", map[string]any{"type": "view_submission", "user": map[string]any{"id": "U7"}}))
		send(ctx, t, c, interactive("env-2", map[string]any{"type": "shortcut", "trigger_id": "x"}))
		noButton := blockActions()
		noButton["actions"] = []any{map[string]any{"type": "static_select", "action_id": "a"}}
		send(ctx, t, c, interactive("env-3", noButton))
		for _, want := range []string{"env-1", "env-2", "env-3"} {
			if got := readAck(ctx, t, c); got != want {
				t.Errorf("ack = %q, want %q", got, want)
			}
		}
		_ = c.Close(websocket.StatusNormalClosure, "")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var clicks int
	_ = f.client().Serve(ctx, Handlers{Click: func(Click) { clicks++ }})
	if clicks != 0 {
		t.Errorf("%d clicks from interactions that are not button clicks", clicks)
	}
}

func TestAClickWithoutATriggerIDIsKeyedByItsActionTime(t *testing.T) {
	p := blockActions()
	delete(p, "trigger_id")
	raw, _ := json.Marshal(p)
	c, ok := parseClick(raw)
	if !ok || c.InteractionID != "6.6" {
		t.Fatalf("click = %+v, %v", c, ok)
	}
}

func TestSettleReplacesTheButtonsWithALine(t *testing.T) {
	f := newFake(t)
	f.handle("chat.update", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "channel": "C1", "ts": "5.5"})
	})
	raw, _ := json.Marshal(blockActions())
	click, ok := parseClick(raw)
	if !ok {
		t.Fatal("no click")
	}
	if err := f.client().Settle(t.Context(), click, "🛠 Fix it — chosen by <@U7>"); err != nil {
		t.Fatal(err)
	}
	bodies := postedBodies(t, f, "chat.update")
	if len(bodies) != 1 {
		t.Fatalf("bodies = %v", bodies)
	}
	b := bodies[0]
	if b["channel"] != "C1" || b["ts"] != "5.5" || b["text"] != "Findings" {
		t.Errorf("body = %v", b)
	}
	want := `[{"text":{"text":"Findings","type":"mrkdwn"},"type":"section"},` +
		`{"elements":[{"text":"🛠 Fix it — chosen by <@U7>","type":"mrkdwn"}],"type":"context"}]`
	if !sameJSON(t, b["blocks"], want) {
		t.Errorf("blocks =\n%v\nwant\n%s", b["blocks"], want)
	}
	if got := f.auths["chat.update"]; got != "Bearer "+botToken {
		t.Errorf("auth = %q", got)
	}
}

func TestSettleRefusalIsAnAPIError(t *testing.T) {
	f := newFake(t)
	f.handle("chat.update", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": false, "error": "message_not_found"})
	})
	err := f.client().Settle(t.Context(), Click{Channel: "C1", TS: "5.5", Text: "x"}, "line")
	if err == nil || !strings.Contains(err.Error(), "message_not_found") {
		t.Fatalf("err = %v", err)
	}
	assertNoSecrets(t, err)
}
