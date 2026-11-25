package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/discordbot"
	"github.com/cpave3/legato/internal/engine/slackbot"
)

type platformCall struct {
	method, path string
	body         map[string]any
}

// platformServer answers every call with answer and records it.
func platformServer(t *testing.T, answer func(w http.ResponseWriter, r *http.Request)) (string, *[]platformCall) {
	t.Helper()
	calls := &[]platformCall{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		*calls = append(*calls, platformCall{r.Method, r.URL.EscapedPath(), body})
		answer(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, calls
}

func slackAnswer(v map[string]any) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
}

func discordAnswer(status int, body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func testSlackConnection(base string) *slackConnection {
	return newSlackConnection("slack", &slackbot.Client{AppToken: "xapp-1", BotToken: "xoxb-1", APIBase: base})
}

func testDiscordConnection(base string) *discordConnection {
	return newDiscordConnection("discord", &discordbot.Client{Token: "tok", APIBase: base})
}

func TestSlackReactions(t *testing.T) {
	base, calls := platformServer(t, slackAnswer(map[string]any{"ok": true}))
	conn := testSlackConnection(base)
	if err := conn.React(context.Background(), "C1", "1.5", "👀", false); err != nil {
		t.Fatal(err)
	}
	if err := conn.React(context.Background(), "C1", "1.5", ":white_check_mark:", true); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 {
		t.Fatalf("calls = %+v", *calls)
	}
	add, remove := (*calls)[0], (*calls)[1]
	if add.path != "/reactions.add" || add.body["name"] != "eyes" || add.body["channel"] != "C1" || add.body["timestamp"] != "1.5" {
		t.Errorf("add = %+v", add)
	}
	if remove.path != "/reactions.remove" || remove.body["name"] != "white_check_mark" {
		t.Errorf("remove = %+v", remove)
	}
}

func TestSlackReactionAlreadyThereIsSuccess(t *testing.T) {
	base, _ := platformServer(t, slackAnswer(map[string]any{"ok": false, "error": "already_reacted"}))
	if err := testSlackConnection(base).React(context.Background(), "C1", "1.5", "eyes", false); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestSlackReactionMissingScopeSaysWhatToDo(t *testing.T) {
	for _, needed := range []string{"reactions:write", ""} {
		base, _ := platformServer(t, slackAnswer(map[string]any{"ok": false, "error": "missing_scope", "needed": needed}))
		err := testSlackConnection(base).React(context.Background(), "C1", "1.5", "eyes", false)
		if err == nil || !strings.Contains(err.Error(), "reactions:write") || !strings.Contains(err.Error(), "reinstall") {
			t.Errorf("needed %q: err = %v", needed, err)
		}
	}
}

func TestSlackReactionWithAnEmojiItCannotName(t *testing.T) {
	base, calls := platformServer(t, slackAnswer(map[string]any{"ok": true}))
	err := testSlackConnection(base).React(context.Background(), "C1", "1.5", "🦩", false)
	if err == nil || len(*calls) != 0 {
		t.Fatalf("err = %v, calls %d", err, len(*calls))
	}
}

func TestDiscordReactions(t *testing.T) {
	base, calls := platformServer(t, discordAnswer(http.StatusNoContent, ""))
	conn := testDiscordConnection(base)
	if err := conn.React(context.Background(), "c1", "m1", "eyes", false); err != nil {
		t.Fatal(err)
	}
	// Discord answers 204 again for a reaction already there.
	if err := conn.React(context.Background(), "c1", "m1", "👀", false); err != nil {
		t.Fatal(err)
	}
	if err := conn.React(context.Background(), "c1", "m1", "white_check_mark", true); err != nil {
		t.Fatal(err)
	}
	eyes := "/channels/c1/messages/m1/reactions/%F0%9F%91%80/@me"
	want := []platformCall{{method: http.MethodPut, path: eyes}, {method: http.MethodPut, path: eyes},
		{method: http.MethodDelete, path: "/channels/c1/messages/m1/reactions/%E2%9C%85/@me"}}
	if len(*calls) != len(want) {
		t.Fatalf("calls = %+v", *calls)
	}
	for i, w := range want {
		if got := (*calls)[i]; got.method != w.method || got.path != w.path {
			t.Errorf("call %d = %s %s, want %s %s", i, got.method, got.path, w.method, w.path)
		}
	}
}

func TestDiscordReactionRefusalsSayWhatToDo(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   []string
	}{
		{http.StatusForbidden, `{"message":"Missing Permissions","code":50013}`, []string{"Add Reactions", "Read Message History"}},
		{http.StatusForbidden, `{"message":"Missing Access","code":50001}`, []string{"View Channel"}},
		{http.StatusNotFound, `{"message":"Unknown Message","code":10008}`, []string{"no message m1"}},
		{http.StatusBadRequest, `{"message":"Unknown Emoji","code":10014}`, []string{"emoji"}},
	} {
		base, _ := platformServer(t, discordAnswer(tc.status, tc.body))
		err := testDiscordConnection(base).React(context.Background(), "c1", "m1", "eyes", false)
		for _, want := range tc.want {
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%s: err = %v, want %q", tc.body, err, want)
			}
		}
	}
}

func TestDiscordReactionWithAnUnknownName(t *testing.T) {
	base, calls := platformServer(t, discordAnswer(http.StatusNoContent, ""))
	err := testDiscordConnection(base).React(context.Background(), "c1", "m1", "partyparrot", false)
	if err == nil || len(*calls) != 0 {
		t.Fatalf("err = %v, calls %d", err, len(*calls))
	}
}

func TestDiscordSendExpandsKnownShortcodes(t *testing.T) {
	base, calls := platformServer(t, discordAnswer(http.StatusOK, `{"id":"new"}`))
	if _, err := testDiscordConnection(base).Send(context.Background(), "c1", "Looking :eyes: at `:eyes:`", "m9"); err != nil {
		t.Fatal(err)
	}
	got := (*calls)[0].body
	if got["content"] != "Looking 👀 at `:eyes:`" {
		t.Fatalf("content = %q", got["content"])
	}
	// Any message ID replies to that message.
	if ref, _ := got["message_reference"].(map[string]any); ref["message_id"] != "m9" {
		t.Fatalf("message_reference = %v", got["message_reference"])
	}
}

// Slack renders :shortcodes: itself, so text reaches it as written.
func TestSlackSendKeepsTextAsWritten(t *testing.T) {
	base, calls := platformServer(t, slackAnswer(map[string]any{"ok": true, "channel": "C1", "ts": "2.0"}))
	if _, err := testSlackConnection(base).Send(context.Background(), "C1", "Looking :eyes: 👀", "1.5"); err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[0].body; got["text"] != "Looking :eyes: 👀" || got["thread_ts"] != "1.5" {
		t.Fatalf("body = %v", got)
	}
}

// A custom emoji whose name is in Legato's table reaches Discord intact.
func TestDiscordSendKeepsCustomEmoji(t *testing.T) {
	base, calls := platformServer(t, discordAnswer(http.StatusOK, `{"id":"new"}`))
	text := "Ship <:rocket:123456789012345678> <a:eyes:234567890123456789> :eyes:"
	if _, err := testDiscordConnection(base).Send(context.Background(), "c1", text, ""); err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[0].body["content"]; got != "Ship <:rocket:123456789012345678> <a:eyes:234567890123456789> 👀" {
		t.Fatalf("content = %q", got)
	}
}
