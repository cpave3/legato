package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeChatConnection is a ChatConnection a test scripts: each Run takes the
// next session's script from runs.
type fakeChatConnection struct {
	name, platform string

	mu      sync.Mutex
	runs    []func(ctx context.Context, emit func(ChatEvent), connected func()) error
	calls   int
	sent    []fakeSent
	send    func(ctx context.Context, channel, text, threadID string) (SentMessage, error)
	reacted []fakeReaction
	react   func(ctx context.Context, channel, messageID, emoji string, remove bool) error
	// thread is what ReadThread answers, and reads what it was asked.
	thread []ChatThreadMessage
	reads  []fakeRead
}

type fakeRead struct {
	Channel, ThreadID string
	Max               int
}

func (f *fakeChatConnection) ReadThread(ctx context.Context, channel, threadID string, max int) ([]ChatThreadMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, fakeRead{channel, threadID, max})
	return append([]ChatThreadMessage(nil), f.thread...), nil
}

type fakeSent struct {
	Channel, Text, ThreadID string
	Buttons                 []ChatButton
}

type fakeReaction struct {
	Channel, MessageID, Emoji string
	Remove                    bool
}

func (f *fakeChatConnection) React(ctx context.Context, channel, messageID, emoji string, remove bool) error {
	f.mu.Lock()
	f.reacted = append(f.reacted, fakeReaction{channel, messageID, emoji, remove})
	react := f.react
	f.mu.Unlock()
	if react != nil {
		return react(ctx, channel, messageID, emoji, remove)
	}
	return nil
}

func (f *fakeChatConnection) Name() string     { return f.name }
func (f *fakeChatConnection) Platform() string { return f.platform }

func (f *fakeChatConnection) Run(ctx context.Context, emit func(ChatEvent), connected func()) error {
	f.mu.Lock()
	f.calls++
	var run func(context.Context, func(ChatEvent), func()) error
	if len(f.runs) > 0 {
		run, f.runs = f.runs[0], f.runs[1:]
	}
	f.mu.Unlock()
	if run == nil {
		// Out of script: stay connected until stopped.
		connected()
		<-ctx.Done()
		return ctx.Err()
	}
	return run(ctx, emit, connected)
}

func (f *fakeChatConnection) Send(ctx context.Context, channel, text, threadID string, buttons ...ChatButton) (SentMessage, error) {
	f.mu.Lock()
	f.sent = append(f.sent, fakeSent{channel, text, threadID, buttons})
	send := f.send
	f.mu.Unlock()
	if send != nil {
		return send(ctx, channel, text, threadID)
	}
	id := fmt.Sprintf("m%d", len(f.sent))
	if threadID == "" {
		threadID = id
	}
	return SentMessage{MessageID: id, Channel: channel, ThreadID: threadID}, nil
}

func (f *fakeChatConnection) runCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func failing(err error) func(context.Context, func(ChatEvent), func()) error {
	return func(context.Context, func(ChatEvent), func()) error { return err }
}

// testChatBots makes a supervisor whose waits are recorded and instant.
func testChatBots(t *testing.T, specs []ChatBotSpec, onEvent func(context.Context, ChatEvent)) (*ChatBots, *[]time.Duration) {
	t.Helper()
	bots := NewChatBots(specs, onEvent)
	var mu sync.Mutex
	delays := &[]time.Duration{}
	bots.rand = func() float64 { return 1 }
	bots.after = func(d time.Duration) <-chan time.Time {
		mu.Lock()
		*delays = append(*delays, d)
		mu.Unlock()
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}
	t.Cleanup(bots.Stop)
	return bots, delays
}

func waitForChat(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func chatStatusOf(bots *ChatBots, name string) ChatBotStatus {
	for _, status := range bots.Statuses() {
		if status.Name == name {
			return status
		}
	}
	return ChatBotStatus{}
}

func TestChatBotsConnectAndDeliverEvents(t *testing.T) {
	conn := &fakeChatConnection{name: "slack", platform: "slack"}
	conn.runs = append(conn.runs, func(ctx context.Context, emit func(ChatEvent), connected func()) error {
		connected()
		emit(ChatEvent{Bot: "slack", Channel: "C1", Text: "hi", MessageID: "1.0"})
		<-ctx.Done()
		return ctx.Err()
	})
	got := make(chan ChatEvent, 1)
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Conn: conn}}, func(_ context.Context, e ChatEvent) { got <- e })
	bots.Start(context.Background())
	select {
	case e := <-got:
		if e.Text != "hi" || e.Channel != "C1" {
			t.Fatalf("event = %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event")
	}
	waitForChat(t, "connected", func() bool { return chatStatusOf(bots, "slack").State == ChatBotConnected })
}

func TestChatBotsBackOffWithGrowingDelays(t *testing.T) {
	conn := &fakeChatConnection{name: "discord", platform: "discord"}
	for range 4 {
		conn.runs = append(conn.runs, failing(errors.New("connection refused")))
	}
	conn.runs = append(conn.runs, func(ctx context.Context, _ func(ChatEvent), _ func()) error {
		<-ctx.Done() // still connecting
		return ctx.Err()
	})
	bots, delays := testChatBots(t, []ChatBotSpec{{Name: "discord", Platform: "discord", Enabled: true, Conn: conn}}, nil)
	bots.BaseDelay, bots.MaxDelay = time.Second, 5*time.Second
	bots.Start(context.Background())
	waitForChat(t, "five runs", func() bool { return conn.runCalls() >= 5 })
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second}
	if len(*delays) < 4 || fmt.Sprint((*delays)[:4]) != fmt.Sprint(want) {
		t.Fatalf("delays = %v, want %v", *delays, want)
	}
	if status := chatStatusOf(bots, "discord"); !strings.Contains(status.LastError, "connection refused") {
		t.Fatalf("status = %+v", status)
	}
}

// A session that connects and soon drops keeps the backoff growing: only
// one that lasts StableAfter starts it over.
func TestChatBotsKeepBackingOffAfterShortSessions(t *testing.T) {
	conn := &fakeChatConnection{name: "slack", platform: "slack"}
	for range 3 {
		conn.runs = append(conn.runs, func(_ context.Context, _ func(ChatEvent), connected func()) error {
			connected()
			return errors.New("connection reset")
		})
	}
	bots, delays := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Conn: conn}}, nil)
	bots.BaseDelay, bots.MaxDelay, bots.StableAfter = time.Second, time.Minute, time.Hour
	bots.Start(context.Background())
	waitForChat(t, "four runs", func() bool { return conn.runCalls() >= 4 })
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	if len(*delays) < 3 || fmt.Sprint((*delays)[:3]) != fmt.Sprint(want) {
		t.Fatalf("delays = %v, want %v", *delays, want)
	}
}

func TestChatBotsJitterTheirBackoff(t *testing.T) {
	conn := &fakeChatConnection{name: "discord", platform: "discord", runs: []func(context.Context, func(ChatEvent), func()) error{failing(errors.New("down"))}}
	bots, delays := testChatBots(t, []ChatBotSpec{{Name: "discord", Platform: "discord", Enabled: true, Conn: conn}}, nil)
	bots.BaseDelay, bots.rand = 4*time.Second, func() float64 { return 0.25 }
	bots.Start(context.Background())
	waitForChat(t, "a second run", func() bool { return conn.runCalls() >= 2 })
	if (*delays)[0] != time.Second {
		t.Fatalf("delay = %v, want a quarter of 4s", (*delays)[0])
	}
}

func TestChatBotsReconnectAtOnceWhenAskedTo(t *testing.T) {
	conn := &fakeChatConnection{name: "slack", platform: "slack"}
	conn.runs = append(conn.runs, func(_ context.Context, _ func(ChatEvent), connected func()) error {
		connected()
		return fmt.Errorf("%w: refresh_requested", ErrChatReconnect)
	})
	bots, delays := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Conn: conn}}, nil)
	bots.Start(context.Background())
	waitForChat(t, "a second run", func() bool { return conn.runCalls() >= 2 })
	waitForChat(t, "connected", func() bool { return chatStatusOf(bots, "slack").State == ChatBotConnected })
	if len(*delays) != 0 {
		t.Fatalf("backed off %v on a reconnect request", *delays)
	}
}

func TestChatBotsStopTryingOnAuthFailureUntilReconnect(t *testing.T) {
	conn := &fakeChatConnection{name: "slack", platform: "slack", runs: []func(context.Context, func(ChatEvent), func()) error{
		failing(fmt.Errorf("%w: invalid_auth", ErrChatAuth)),
	}}
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Conn: conn}}, nil)
	bots.Start(context.Background())
	waitForChat(t, "auth failed", func() bool { return chatStatusOf(bots, "slack").State == ChatBotAuthFailed })
	time.Sleep(20 * time.Millisecond)
	if conn.runCalls() != 1 {
		t.Fatalf("runs = %d after an auth failure", conn.runCalls())
	}
	if _, err := bots.Send(context.Background(), "slack", "C1", "hi", ""); !errors.Is(err, ErrChatBotNotConnected) || !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("send err = %v", err)
	}
	if err := bots.Reconnect("slack"); err != nil {
		t.Fatal(err)
	}
	waitForChat(t, "connected again", func() bool { return chatStatusOf(bots, "slack").State == ChatBotConnected })
}

func TestChatBotsWithAProblemNeverConnect(t *testing.T) {
	conn := &fakeChatConnection{name: "slack", platform: "slack"}
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Conn: conn, Problem: "the app-level token must start xapp-"}}, nil)
	bots.Start(context.Background())
	status := chatStatusOf(bots, "slack")
	if status.State != ChatBotAuthFailed || !strings.Contains(status.LastError, "xapp-") {
		t.Fatalf("status = %+v", status)
	}
	time.Sleep(10 * time.Millisecond)
	if conn.runCalls() != 0 {
		t.Fatal("connected with a bad token")
	}
}

func TestChatBotsLeaveDisabledBotsAlone(t *testing.T) {
	conn := &fakeChatConnection{name: "discord", platform: "discord"}
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "discord", Platform: "discord", Enabled: false, Configured: true, Conn: conn}}, nil)
	bots.Start(context.Background())
	time.Sleep(10 * time.Millisecond)
	if status := chatStatusOf(bots, "discord"); status.State != ChatBotDisabled || conn.runCalls() != 0 {
		t.Fatalf("status = %+v, runs %d", status, conn.runCalls())
	}
	if _, err := bots.Send(context.Background(), "discord", "C1", "hi", ""); !errors.Is(err, ErrChatBotNotConnected) {
		t.Fatalf("send err = %v", err)
	}
}

func TestChatBotsStopEndsEveryConnection(t *testing.T) {
	var conns []*fakeChatConnection
	var specs []ChatBotSpec
	for _, name := range []string{"slack", "discord"} {
		conn := &fakeChatConnection{name: name, platform: name}
		conns = append(conns, conn)
		specs = append(specs, ChatBotSpec{Name: name, Platform: name, Enabled: true, Conn: conn})
	}
	bots, _ := testChatBots(t, specs, nil)
	bots.Start(context.Background())
	waitForChat(t, "both connected", func() bool {
		return chatStatusOf(bots, "slack").State == ChatBotConnected && chatStatusOf(bots, "discord").State == ChatBotConnected
	})
	start := time.Now()
	bots.Stop()
	if time.Since(start) > time.Second {
		t.Fatalf("Stop took %v", time.Since(start))
	}
	bots.Stop() // twice is fine
}

func TestChatBotsScrubSecretsFromTheirErrors(t *testing.T) {
	conn := &fakeChatConnection{name: "slack", platform: "slack", runs: []func(context.Context, func(ChatEvent), func()) error{
		failing(errors.New(`dial wss://wss-primary.slack.com/link/?ticket=abc123&app_id=A1: Bearer xoxb-secret-bot refused`)),
	}}
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Conn: conn, Secrets: []string{"xoxb-secret-bot"}}}, nil)
	bots.after = func(time.Duration) <-chan time.Time { return make(chan time.Time) } // stay backing off
	bots.Start(context.Background())
	waitForChat(t, "backing off", func() bool { return chatStatusOf(bots, "slack").State == ChatBotBackingOff })
	status := chatStatusOf(bots, "slack")
	if strings.Contains(status.LastError, "abc123") || strings.Contains(status.LastError, "xoxb-secret-bot") || status.NextAttempt == nil {
		t.Fatalf("status = %+v", status)
	}
	if !strings.Contains(status.LastError, "wss://wss-primary.slack.com/link/") {
		t.Fatalf("scrubbed too much: %q", status.LastError)
	}
}

func TestChatBotsSend(t *testing.T) {
	conn := &fakeChatConnection{name: "slack", platform: "slack"}
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Conn: conn, DefaultChannel: "CDEF"}}, nil)
	if _, err := bots.Send(context.Background(), "teams", "C1", "hi", ""); !errors.Is(err, ErrChatBotUnknown) {
		t.Fatalf("unknown bot err = %v", err)
	}
	// Not started yet: the REST call needs no connection.
	sent, err := bots.Send(context.Background(), "slack", "", "hi", "1.5")
	if err != nil || sent.Channel != "CDEF" || sent.ThreadID != "1.5" {
		t.Fatalf("sent = %+v, %v", sent, err)
	}
	if _, err := bots.Send(context.Background(), "slack", "C1", "   ", ""); err == nil {
		t.Fatal("sent an empty message")
	}
	conn.send = func(ctx context.Context, _, _, _ string) (SentMessage, error) {
		<-ctx.Done()
		return SentMessage{}, ctx.Err()
	}
	bots.SendTimeout = 20 * time.Millisecond
	start := time.Now()
	if _, err := bots.Send(context.Background(), "slack", "C1", "hi", ""); err == nil || time.Since(start) > time.Second {
		t.Fatalf("blocked send: %v after %v", err, time.Since(start))
	}
}

func TestChatBotsNoDefaultChannel(t *testing.T) {
	conn := &fakeChatConnection{name: "discord", platform: "discord"}
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "discord", Platform: "discord", Enabled: true, Conn: conn}}, nil)
	if _, err := bots.Send(context.Background(), "discord", "", "hi", ""); err == nil || !strings.Contains(err.Error(), "channel") {
		t.Fatalf("err = %v", err)
	}
}

func TestChatBotsHeldElsewhere(t *testing.T) {
	conn := &fakeChatConnection{name: "slack", platform: "slack"}
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Conn: conn}}, nil)
	bots.SetElsewhere()
	bots.Start(context.Background())
	time.Sleep(10 * time.Millisecond)
	if status := chatStatusOf(bots, "slack"); status.State != ChatBotElsewhere || conn.runCalls() != 0 {
		t.Fatalf("status = %+v, runs %d", status, conn.runCalls())
	}
	if _, err := bots.Send(context.Background(), "slack", "C1", "hi", ""); err != nil {
		t.Fatalf("send from another process: %v", err)
	}
}

func TestChatBotsKnow(t *testing.T) {
	bots, _ := testChatBots(t, []ChatBotSpec{
		{Name: "slack", Platform: "slack", Enabled: true, Configured: true, Conn: &fakeChatConnection{}},
		{Name: "discord", Platform: "discord", Conn: &fakeChatConnection{}},
	}, nil)
	if configured, enabled := bots.Known("slack"); !configured || !enabled {
		t.Fatalf("slack = %v, %v", configured, enabled)
	}
	if configured, enabled := bots.Known("discord"); configured || enabled {
		t.Fatalf("discord = %v, %v", configured, enabled)
	}
	if configured, _ := bots.Known("teams"); configured {
		t.Fatal("knows teams")
	}
}

func TestChatBotsReact(t *testing.T) {
	secret := "xoxb-react-secret"
	conn := &fakeChatConnection{name: "slack", platform: "slack"}
	off := &fakeChatConnection{name: "discord", platform: "discord"}
	bots, _ := testChatBots(t, []ChatBotSpec{
		{Name: "slack", Platform: "slack", Enabled: true, Conn: conn, DefaultChannel: "CDEF", Secrets: []string{secret}},
		{Name: "discord", Platform: "discord", Conn: off},
	}, nil)
	if err := bots.React(context.Background(), "slack", "C1", "1.5", "eyes", false); err != nil {
		t.Fatal(err)
	}
	if err := bots.React(context.Background(), "slack", "", "1.5", "white_check_mark", true); err != nil {
		t.Fatal(err)
	}
	want := []fakeReaction{{"C1", "1.5", "eyes", false}, {"CDEF", "1.5", "white_check_mark", true}}
	if len(conn.reacted) != 2 || conn.reacted[0] != want[0] || conn.reacted[1] != want[1] {
		t.Fatalf("reacted = %+v", conn.reacted)
	}
	for name, tc := range map[string]struct {
		bot, message, emoji, want string
	}{
		"unknown bot":  {"teams", "1.5", "eyes", "no such chat bot"},
		"switched off": {"discord", "1.5", "eyes", "switched off"},
		"no message":   {"slack", " ", "eyes", "message"},
		"no emoji":     {"slack", "1.5", "", "emoji"},
	} {
		err := bots.React(context.Background(), tc.bot, "C1", tc.message, tc.emoji, false)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	if len(conn.reacted) != 2 || len(off.reacted) != 0 {
		t.Fatalf("refused reactions reached a platform: %+v %+v", conn.reacted, off.reacted)
	}

	conn.react = func(context.Context, string, string, string, bool) error {
		return errors.New("refused for Bearer " + secret)
	}
	err := bots.React(context.Background(), "slack", "C1", "1.5", "eyes", false)
	if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "1.5") {
		t.Fatalf("err = %v", err)
	}

	conn.react = func(ctx context.Context, _, _, _ string, _ bool) error {
		<-ctx.Done()
		return ctx.Err()
	}
	bots.SendTimeout = 20 * time.Millisecond
	start := time.Now()
	if err := bots.React(context.Background(), "slack", "C1", "1.5", "eyes", false); err == nil || time.Since(start) > time.Second {
		t.Fatalf("blocked reaction: %v after %v", err, time.Since(start))
	}
}
