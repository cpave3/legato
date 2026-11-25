package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func chatSendStep(with map[string]any) ResolvedWorkflowStep {
	return ResolvedWorkflowStep{RunID: "wr-1", StepID: "say", Type: ChatSendStepType, With: with}
}

func TestChatSendPostsAndGivesTheMessage(t *testing.T) {
	conn := &fakeChatConnection{name: "slack", platform: "slack"}
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Configured: true, Conn: conn, DefaultChannel: "CDEF"}}, nil)
	executor := NewChatSendExecutor(bots)
	result, err := executor.Start(context.Background(), chatSendStep(map[string]any{"bot": "slack", "channel": "C1", "text": "shipped", "thread_id": "1.1"}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || result.Outputs["message_id"] != "m1" || result.Outputs["channel"] != "C1" || result.Outputs["thread_id"] != "1.1" {
		t.Fatalf("result = %+v", result)
	}
	result, err = executor.Start(context.Background(), chatSendStep(map[string]any{"bot": "slack", "text": "to the default"}))
	if err != nil || result.Outputs["channel"] != "CDEF" || result.Outputs["thread_id"] != "m2" {
		t.Fatalf("default channel: %+v, %v", result, err)
	}
	if sent := conn.sent[1]; sent.Channel != "CDEF" || sent.Text != "to the default" || sent.ThreadID != "" || sent.Buttons != nil {
		t.Fatalf("sent = %+v", conn.sent)
	}
}

func TestChatSendFailsClearly(t *testing.T) {
	secret := "xoxb-super-secret"
	failed := &fakeChatConnection{name: "slack", platform: "slack", runs: []func(context.Context, func(ChatEvent), func()) error{
		failing(errors.Join(ErrChatAuth, errors.New("invalid_auth for "+secret))),
	}}
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Configured: true, Conn: failed, Secrets: []string{secret}}}, nil)
	bots.Start(context.Background())
	waitForChat(t, "auth failed", func() bool { return chatStatusOf(bots, "slack").State == ChatBotAuthFailed })
	executor := NewChatSendExecutor(bots)
	for name, tc := range map[string]struct {
		with map[string]any
		want string
	}{
		"unknown bot": {map[string]any{"bot": "teams", "text": "x", "channel": "C1"}, `no such chat bot named "teams"`},
		"auth failed": {map[string]any{"bot": "slack", "text": "x", "channel": "C1"}, "auth failed"},
		"no text":     {map[string]any{"bot": "slack", "channel": "C1"}, "text is required"},
	} {
		_, err := executor.Start(context.Background(), chatSendStep(tc.with))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		if err != nil && strings.Contains(err.Error(), secret) {
			t.Errorf("%s: err holds the token: %v", name, err)
		}
	}
	if _, err := NewChatSendExecutor(nil).Start(context.Background(), chatSendStep(map[string]any{"bot": "slack", "text": "x"})); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("nil sender err = %v", err)
	}
}

func TestChatSendDoesNotHang(t *testing.T) {
	conn := &fakeChatConnection{name: "discord", platform: "discord", send: func(ctx context.Context, _, _, _ string) (SentMessage, error) {
		<-ctx.Done()
		return SentMessage{}, ctx.Err()
	}}
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "discord", Platform: "discord", Enabled: true, Configured: true, Conn: conn}}, nil)
	bots.SendTimeout = 20 * time.Millisecond
	start := time.Now()
	_, err := NewChatSendExecutor(bots).Start(context.Background(), chatSendStep(map[string]any{"bot": "discord", "channel": "c1", "text": "x"}))
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("err = %v after %v", err, time.Since(start))
	}
}
