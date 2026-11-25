package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/discordbot"
	"github.com/cpave3/legato/internal/engine/slackbot"
)

func chatThreadStep(with map[string]any) ResolvedWorkflowStep {
	return ResolvedWorkflowStep{RunID: "wr-1", StepID: "thread", Type: ChatThreadStepType, With: with}
}

// fakeSlackAPI answers conversations.replies with one page of messages and
// users.info from names (missing_scope when names is nil), and counts the
// calls to each.
type fakeSlackAPI struct {
	mu    sync.Mutex
	calls map[string]int
}

func newFakeSlackAPI(t *testing.T, replies []map[string]any, names map[string]string) (*fakeSlackAPI, *slackbot.Client) {
	t.Helper()
	f := &fakeSlackAPI{calls: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		method := strings.TrimPrefix(r.URL.Path, "/")
		f.mu.Lock()
		f.calls[method]++
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "conversations.replies":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "messages": replies})
		case "users.info":
			if names == nil {
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "missing_scope", "needed": "users:read"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "user": map[string]any{"profile": map[string]any{"display_name": names[form.Get("user")]}}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "unknown_method"})
		}
	}))
	t.Cleanup(srv.Close)
	return f, &slackbot.Client{BotToken: "xoxb-thread-secret", APIBase: srv.URL}
}

func (f *fakeSlackAPI) count(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[method]
}

func slackThreadBots(t *testing.T, client *slackbot.Client) *ChatBots {
	t.Helper()
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Configured: true,
		Conn: newSlackConnection("slack", client), Secrets: []string{client.BotToken}}}, nil)
	return bots
}

// The thread Cameron mentioned the bot in: his forward of an alert, a
// question, a channel join, another bot's post, then his mention.
func slackThreadWithAForwardedParent() []map[string]any {
	return []map[string]any{
		{"type": "message", "user": "U1", "text": "", "ts": "1795579920.000100", "thread_ts": "1795579920.000100",
			"attachments": []any{map[string]any{"is_share": true, "author_id": "U9", "author_name": "alice", "channel_name": "alerts",
				"from_url": "https://x.slack.com/archives/C9/p1", "text": "prod is down"}}},
		{"type": "message", "subtype": "channel_join", "user": "U3", "text": "<@U3> has joined", "ts": "1795579950.000100", "thread_ts": "1795579920.000100"},
		{"type": "message", "user": "U2", "text": "what changed?", "ts": "1795579980.000100", "thread_ts": "1795579920.000100"},
		{"type": "message", "subtype": "bot_message", "bot_id": "B1", "username": "deploybot", "text": "deployed v2", "ts": "1795580040.000100", "thread_ts": "1795579920.000100"},
		{"type": "message", "user": "U1", "text": "<@UBOT> look at this thread", "ts": "1795580100.000100", "thread_ts": "1795579920.000100"},
	}
}

func TestChatThreadReadGivesASlackThreadOldestFirstWithItsForward(t *testing.T) {
	api, client := newFakeSlackAPI(t, slackThreadWithAForwardedParent(), map[string]string{"U1": "cameron", "U2": "bob"})
	result, err := NewChatThreadReadExecutor(slackThreadBots(t, client)).Start(context.Background(),
		chatThreadStep(map[string]any{"bot": "slack", "channel": "C1", "thread_id": "1795579920.000100"}))
	if err != nil {
		t.Fatal(err)
	}
	want := "cameron · 2026-11-25 04:12 UTC\n> Forwarded from alice in #alerts:\n> prod is down\n\n" +
		"bob · 2026-11-25 04:13 UTC\nwhat changed?\n\n" +
		"cameron · 2026-11-25 04:15 UTC\n<@UBOT> look at this thread"
	if result.Status != "succeeded" || result.Outputs["transcript"] != want {
		t.Fatalf("transcript =\n%v\nwant\n%s", result.Outputs["transcript"], want)
	}
	if result.Outputs["count"] != 3 {
		t.Fatalf("count = %v", result.Outputs["count"])
	}
	messages := result.Outputs["messages"].([]map[string]any)
	if len(messages) != 3 {
		t.Fatalf("messages = %v", messages)
	}
	parent := messages[0]
	if parent["id"] != "1795579920.000100" || parent["author_id"] != "U1" || parent["author_name"] != "cameron" ||
		parent["parent"] != true || parent["forwarded"] != true || parent["forwarded_url"] != "https://x.slack.com/archives/C9/p1" ||
		parent["text"] != "> Forwarded from alice in #alerts:\n> prod is down" || parent["at"] != "2026-11-25T04:12:00.0001Z" {
		t.Fatalf("parent = %v", parent)
	}
	if messages[2]["parent"] != false || messages[2]["from_bot"] != false || messages[2]["text"] != "<@UBOT> look at this thread" {
		t.Fatalf("last = %v", messages[2])
	}
	if n := api.count("users.info"); n != 2 {
		t.Fatalf("users.info calls = %d, want one per user", n)
	}
}

func TestChatThreadReadWithoutUsersReadNamesSlackAuthorsByID(t *testing.T) {
	api, client := newFakeSlackAPI(t, slackThreadWithAForwardedParent(), nil)
	result, err := NewChatThreadReadExecutor(slackThreadBots(t, client)).Start(context.Background(),
		chatThreadStep(map[string]any{"bot": "slack", "channel": "C1", "thread_id": "1795579920.000100", "include_bots": true}))
	if err != nil {
		t.Fatal(err)
	}
	transcript := result.Outputs["transcript"].(string)
	for _, want := range []string{"<@U1> · 2026-11-25 04:12 UTC\n", "<@U2> · 2026-11-25 04:13 UTC\n", "deploybot · 2026-11-25 04:14 UTC\ndeployed v2"} {
		if !strings.Contains(transcript, want) {
			t.Errorf("transcript lacks %q:\n%s", want, transcript)
		}
	}
	if n := api.count("users.info"); n != 1 {
		t.Fatalf("users.info calls = %d, want 1: a missing scope is not asked again", n)
	}
	if result.Outputs["count"] != 4 {
		t.Fatalf("count = %v, want 4 with the bot's post", result.Outputs["count"])
	}
}

func TestChatThreadReadOfASlackMessageInNoThreadIsThatMessage(t *testing.T) {
	_, client := newFakeSlackAPI(t, []map[string]any{
		{"type": "message", "user": "U1", "text": "<@UBOT> check prod", "ts": "1795579920.000100"},
	}, map[string]string{"U1": "cameron"})
	result, err := NewChatThreadReadExecutor(slackThreadBots(t, client)).Start(context.Background(),
		chatThreadStep(map[string]any{"bot": "slack", "channel": "C1", "thread_id": "1795579920.000100"}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outputs["count"] != 1 || result.Outputs["transcript"] != "cameron · 2026-11-25 04:12 UTC\n<@UBOT> check prod" {
		t.Fatalf("outputs = %v", result.Outputs)
	}
}

// fakeDiscordAPI answers GETs from a map of path (with its query) to body,
// and Unknown Message for any other.
func fakeDiscordAPI(t *testing.T, bodies map[string]string) *discordbot.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		body, ok := bodies[key]
		if r.Method != http.MethodGet || !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Unknown Message","code":10008}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &discordbot.Client{Token: "discord-thread-secret", APIBase: srv.URL}
}

func discordThreadBots(t *testing.T, client *discordbot.Client) *ChatBots {
	t.Helper()
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "discord", Platform: "discord", Enabled: true, Configured: true,
		Conn: newDiscordConnection("discord", client), Secrets: []string{client.Token}}}, nil)
	return bots
}

func discordMessageJSON(id, channel string, typ int, author, name string, bot bool, content, at string, extra string) string {
	return fmt.Sprintf(`{"id":%q,"channel_id":%q,"type":%d,"author":{"id":%q,"global_name":%q,"bot":%t},"content":%q,"timestamp":%q%s}`,
		id, channel, typ, author, name, bot, content, at, extra)
}

func TestChatThreadReadGivesADiscordThreadWithItsForwardedStarter(t *testing.T) {
	client := fakeDiscordAPI(t, map[string]string{
		"/channels/t1": `{"id":"t1","type":11,"parent_id":"c1","guild_id":"g1"}`,
		// A thread started on a message has that message's ID.
		"/channels/c1/messages/t1": discordMessageJSON("t1", "c1", 0, "u1", "Cameron", false, "", "2026-11-25T04:12:00Z",
			`,"message_reference":{"type":1,"message_id":"m0","channel_id":"c0","guild_id":"g1"},"message_snapshots":[{"message":{"content":"prod is down"}}]`),
		"/channels/t1/messages?limit=100": "[" + strings.Join([]string{
			discordMessageJSON("m4", "t1", 0, "u1", "Cameron", false, "<@self> look at this thread", "2026-11-25T04:15:00Z", ""),
			discordMessageJSON("m3", "t1", 0, "b1", "Deploys", true, "deployed v2", "2026-11-25T04:14:00Z", ""),
			discordMessageJSON("m2", "t1", 0, "u2", "Bob", false, "what changed?", "2026-11-25T04:13:00Z", ""),
			discordMessageJSON("m1", "t1", 21, "u1", "Cameron", false, "", "2026-11-25T04:12:00Z", `,"message_reference":{"message_id":"t1","channel_id":"c1"}`),
		}, ",") + "]",
	})
	result, err := NewChatThreadReadExecutor(discordThreadBots(t, client)).Start(context.Background(),
		chatThreadStep(map[string]any{"bot": "discord", "channel": "t1", "thread_id": "t1"}))
	if err != nil {
		t.Fatal(err)
	}
	want := "Cameron · 2026-11-25 04:12 UTC\n> Forwarded message from https://discord.com/channels/g1/c0/m0:\n> prod is down\n\n" +
		"Bob · 2026-11-25 04:13 UTC\nwhat changed?\n\n" +
		"Cameron · 2026-11-25 04:15 UTC\n<@self> look at this thread"
	if result.Outputs["transcript"] != want {
		t.Fatalf("transcript =\n%v\nwant\n%s", result.Outputs["transcript"], want)
	}
	messages := result.Outputs["messages"].([]map[string]any)
	if result.Outputs["count"] != 3 || messages[0]["id"] != "t1" || messages[0]["parent"] != true || messages[0]["forwarded"] != true {
		t.Fatalf("outputs = %v", result.Outputs)
	}
}

func TestChatThreadReadGivesADiscordReplyChainRootFirst(t *testing.T) {
	client := fakeDiscordAPI(t, map[string]string{
		"/channels/c1/messages/m3": discordMessageJSON("m3", "c1", 19, "u1", "Cameron", false, "<@self> this one", "2026-11-25T04:15:00Z",
			`,"message_reference":{"type":0,"message_id":"m2","channel_id":"c1"}`),
		"/channels/c1/messages/m2": discordMessageJSON("m2", "c1", 19, "u2", "Bob", false, "it broke again", "2026-11-25T04:13:00Z",
			`,"message_reference":{"type":0,"message_id":"m1","channel_id":"c1"}`),
		"/channels/c1/messages/m1": discordMessageJSON("m1", "c1", 0, "u3", "Alice", false, "prod is down", "2026-11-25T04:12:00Z", ""),
		"/channels/c1/messages/m9": discordMessageJSON("m9", "c1", 0, "u1", "Cameron", false, "<@self> check prod", "2026-11-25T04:20:00Z", ""),
	})
	executor := NewChatThreadReadExecutor(discordThreadBots(t, client))
	result, err := executor.Start(context.Background(), chatThreadStep(map[string]any{"bot": "discord", "channel": "c1", "thread_id": "m3"}))
	if err != nil {
		t.Fatal(err)
	}
	want := "Alice · 2026-11-25 04:12 UTC\nprod is down\n\nBob · 2026-11-25 04:13 UTC\nit broke again\n\nCameron · 2026-11-25 04:15 UTC\n<@self> this one"
	if result.Outputs["transcript"] != want || result.Outputs["count"] != 3 {
		t.Fatalf("outputs = %v", result.Outputs)
	}

	// A message that replies to nothing is the whole of its thread.
	result, err = executor.Start(context.Background(), chatThreadStep(map[string]any{"bot": "discord", "channel": "c1", "thread_id": "m9"}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outputs["count"] != 1 || result.Outputs["transcript"] != "Cameron · 2026-11-25 04:20 UTC\n<@self> check prod" {
		t.Fatalf("outputs = %v", result.Outputs)
	}
}

func threadBots(t *testing.T, thread []ChatThreadMessage) (*ChatBots, *fakeChatConnection) {
	t.Helper()
	conn := &fakeChatConnection{name: "slack", platform: "slack", thread: thread}
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Configured: true, Conn: conn, DefaultChannel: "CDEF"}}, nil)
	return bots, conn
}

func longThread(replies int, replyText string) []ChatThreadMessage {
	at := time.Date(2026, 11, 25, 4, 0, 0, 0, time.UTC)
	thread := []ChatThreadMessage{{ID: "p", AuthorID: "U1", AuthorName: "cameron", At: at, Text: "the parent", Parent: true}}
	for i := range replies {
		thread = append(thread, ChatThreadMessage{ID: fmt.Sprintf("r%d", i), AuthorID: "U2", AuthorName: "bob",
			At: at.Add(time.Duration(i+1) * time.Minute), Text: fmt.Sprintf("reply %d %s", i, replyText)})
	}
	return thread
}

func TestChatThreadReadDropsTheOldestRepliesToFitTheCap(t *testing.T) {
	bots, _ := threadBots(t, longThread(100, strings.Repeat("x", 300)))
	result, err := NewChatThreadReadExecutor(bots).Start(context.Background(),
		chatThreadStep(map[string]any{"bot": "slack", "thread_id": "p", "limit": 200}))
	if err != nil {
		t.Fatal(err)
	}
	transcript := result.Outputs["transcript"].(string)
	count := result.Outputs["count"].(int)
	if len(transcript) > MaxChatThreadTranscript {
		t.Fatalf("transcript is %d bytes, over %d", len(transcript), MaxChatThreadTranscript)
	}
	if count < 2 || count > 60 {
		t.Fatalf("count = %d", count)
	}
	if !strings.HasPrefix(transcript, "cameron · 2026-11-25 04:00 UTC\nthe parent\n\n") {
		t.Fatalf("the parent is not first:\n%.200s", transcript)
	}
	if want := fmt.Sprintf("[… %d earlier replies left out]", 101-count); !strings.Contains(transcript, "the parent\n\n"+want+"\n\n") {
		t.Fatalf("transcript lacks %q after the parent:\n%.300s", want, transcript)
	}
	if !strings.HasSuffix(transcript, "reply 99 "+strings.Repeat("x", 300)) {
		t.Fatalf("the newest reply is not last")
	}
	if messages := result.Outputs["messages"].([]map[string]any); len(messages) != count || messages[0]["id"] != "p" || messages[1]["id"] != fmt.Sprintf("r%d", 101-count) {
		t.Fatalf("messages do not match the transcript: %d of them, first %v", len(messages), messages[0])
	}
}

func TestChatThreadReadCutsAHugeForwardAsTheEventTextDoes(t *testing.T) {
	thread := longThread(1, "the mention")
	thread[0].Forwarded, thread[0].ForwardedText = true, "> Forwarded message:\n> "+strings.Repeat("log ", 10000)
	bots, _ := threadBots(t, thread)
	result, err := NewChatThreadReadExecutor(bots).Start(context.Background(), chatThreadStep(map[string]any{"bot": "slack", "thread_id": "p"}))
	if err != nil {
		t.Fatal(err)
	}
	transcript := result.Outputs["transcript"].(string)
	if result.Outputs["count"] != 2 || !strings.Contains(transcript, cutForwardMarker+"\n\nbob · ") || !strings.HasSuffix(transcript, "reply 0 the mention") {
		t.Fatalf("count %v, transcript:\n…%s", result.Outputs["count"], transcript[len(transcript)-200:])
	}
}

func TestChatThreadReadKeepsTheParentAndTheNewestRepliesUpToTheLimit(t *testing.T) {
	bots, conn := threadBots(t, longThread(60, "hi"))
	result, err := NewChatThreadReadExecutor(bots).Start(context.Background(), chatThreadStep(map[string]any{"bot": "slack", "thread_id": "p", "limit": 10}))
	if err != nil {
		t.Fatal(err)
	}
	messages := result.Outputs["messages"].([]map[string]any)
	if result.Outputs["count"] != 10 || messages[0]["id"] != "p" || messages[1]["id"] != "r51" || messages[9]["id"] != "r59" {
		t.Fatalf("messages = %v", messages)
	}
	if !strings.Contains(result.Outputs["transcript"].(string), "[… 51 earlier replies left out]") {
		t.Fatalf("transcript does not say what the limit left out")
	}
	if conn.reads[0] != (fakeRead{Channel: "CDEF", ThreadID: "p", Max: ChatThreadReadMax}) {
		t.Fatalf("reads = %+v", conn.reads)
	}
}

func TestChatThreadReadLeavesOutBotsUnlessAskedButNeverTheParent(t *testing.T) {
	thread := longThread(2, "hi")
	thread[0].FromBot, thread[0].AuthorName = true, "alerts"
	thread[1].FromBot = true
	bots, _ := threadBots(t, thread)
	executor := NewChatThreadReadExecutor(bots)
	result, err := executor.Start(context.Background(), chatThreadStep(map[string]any{"bot": "slack", "thread_id": "p"}))
	if err != nil {
		t.Fatal(err)
	}
	messages := result.Outputs["messages"].([]map[string]any)
	if result.Outputs["count"] != 2 || messages[0]["id"] != "p" || messages[0]["from_bot"] != true || messages[1]["id"] != "r1" {
		t.Fatalf("without bots: %v", messages)
	}
	if strings.Contains(result.Outputs["transcript"].(string), "left out") {
		t.Fatalf("a bot's message left out by choice is not a cut: %s", result.Outputs["transcript"])
	}
	result, err = executor.Start(context.Background(), chatThreadStep(map[string]any{"bot": "slack", "thread_id": "p", "include_bots": true}))
	if err != nil || result.Outputs["count"] != 3 {
		t.Fatalf("with bots: %v, %v", result.Outputs, err)
	}
}

func TestChatThreadReadFailsClearly(t *testing.T) {
	secret := "xoxb-super-secret"
	failed := &fakeChatConnection{name: "slack", platform: "slack", runs: []func(context.Context, func(ChatEvent), func()) error{
		failing(errors.Join(ErrChatAuth, errors.New("invalid_auth for "+secret))),
	}}
	bots, _ := testChatBots(t, []ChatBotSpec{
		{Name: "slack", Platform: "slack", Enabled: true, Configured: true, Conn: failed, Secrets: []string{secret}},
		{Name: "discord", Platform: "discord", Enabled: false, Configured: true, Conn: &fakeChatConnection{name: "discord", platform: "discord"}},
	}, nil)
	bots.Start(context.Background())
	waitForChat(t, "auth failed", func() bool { return chatStatusOf(bots, "slack").State == ChatBotAuthFailed })
	executor := NewChatThreadReadExecutor(bots)
	for name, tc := range map[string]struct {
		with map[string]any
		want string
	}{
		"no bot":       {map[string]any{"thread_id": "1.1", "channel": "C1"}, "bot is required"},
		"no thread":    {map[string]any{"bot": "slack", "channel": "C1"}, "thread_id is required"},
		"limit 0":      {map[string]any{"bot": "slack", "channel": "C1", "thread_id": "1.1", "limit": 0}, "limit must be from 1 to 200"},
		"limit 201":    {map[string]any{"bot": "slack", "channel": "C1", "thread_id": "1.1", "limit": 201}, "limit must be from 1 to 200"},
		"unknown bot":  {map[string]any{"bot": "teams", "channel": "C1", "thread_id": "1.1"}, `no such chat bot named "teams"`},
		"switched off": {map[string]any{"bot": "discord", "channel": "c1", "thread_id": "m1"}, "switched off"},
		"auth failed":  {map[string]any{"bot": "slack", "channel": "C1", "thread_id": "1.1"}, "auth failed"},
	} {
		_, err := executor.Start(context.Background(), chatThreadStep(tc.with))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		if err != nil && strings.Contains(err.Error(), secret) {
			t.Errorf("%s: err holds the token: %v", name, err)
		}
	}
	if _, err := NewChatThreadReadExecutor(nil).Start(context.Background(), chatThreadStep(map[string]any{"bot": "slack", "thread_id": "1.1"})); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("nil bots: %v", err)
	}
}

func TestChatThreadReadErrorsAreScrubbedAndNameTheThread(t *testing.T) {
	client := fakeDiscordAPI(t, nil)
	secret := client.Token
	_, err := NewChatThreadReadExecutor(discordThreadBots(t, client)).Start(context.Background(),
		chatThreadStep(map[string]any{"bot": "discord", "channel": "c1", "thread_id": "gone"}))
	if err == nil || !strings.Contains(err.Error(), "bot discord: reading thread gone in c1") || strings.Contains(err.Error(), secret) {
		t.Fatalf("err = %v", err)
	}
}
