package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func chatReactionStep(stepType string, with map[string]any) ResolvedWorkflowStep {
	return ResolvedWorkflowStep{RunID: "wr-1", StepID: "react", Type: stepType, With: with}
}

func TestChatReactionStepsReactOnEachPlatform(t *testing.T) {
	slack := &fakeChatConnection{name: "slack", platform: "slack"}
	discord := &fakeChatConnection{name: "discord", platform: "discord"}
	bots, _ := testChatBots(t, []ChatBotSpec{
		{Name: "slack", Platform: "slack", Enabled: true, Configured: true, Conn: slack, DefaultChannel: "CDEF"},
		{Name: "discord", Platform: "discord", Enabled: true, Configured: true, Conn: discord},
	}, nil)
	add, remove := NewChatReactionAddExecutor(bots), NewChatReactionRemoveExecutor(bots)
	if add.Type() != "chat.reaction.add" || remove.Type() != "chat.reaction.remove" {
		t.Fatalf("types = %s, %s", add.Type(), remove.Type())
	}
	for _, run := range []struct {
		executor interface {
			Start(context.Context, ResolvedWorkflowStep) (WorkflowExecutionResult, error)
		}
		stepType string
		with     map[string]any
	}{
		{add, ChatReactionAddStepType, map[string]any{"bot": "slack", "channel": "C1", "message_id": "1.5", "emoji": "eyes"}},
		{remove, ChatReactionRemoveStepType, map[string]any{"bot": "slack", "message_id": "1.5", "emoji": "eyes"}},
		{add, ChatReactionAddStepType, map[string]any{"bot": "discord", "channel": "c1", "message_id": "m1", "emoji": "👀"}},
	} {
		result, err := run.executor.Start(context.Background(), chatReactionStep(run.stepType, run.with))
		if err != nil {
			t.Fatalf("%v: %v", run.with, err)
		}
		if result.Status != "succeeded" || result.Outputs["ok"] != true {
			t.Fatalf("%v: result = %+v", run.with, result)
		}
	}
	wantSlack := []fakeReaction{{"C1", "1.5", "eyes", false}, {"CDEF", "1.5", "eyes", true}}
	if len(slack.reacted) != 2 || slack.reacted[0] != wantSlack[0] || slack.reacted[1] != wantSlack[1] {
		t.Fatalf("slack reacted = %+v", slack.reacted)
	}
	if len(discord.reacted) != 1 || discord.reacted[0] != (fakeReaction{"c1", "m1", "👀", false}) {
		t.Fatalf("discord reacted = %+v", discord.reacted)
	}
}

func TestChatReactionStepsFailClearly(t *testing.T) {
	conn := &fakeChatConnection{name: "slack", platform: "slack", react: func(context.Context, string, string, string, bool) error {
		return errors.New("slack refused: missing_scope (needs reactions:write)")
	}}
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Configured: true, Conn: conn, DefaultChannel: "C1"}}, nil)
	executor := NewChatReactionAddExecutor(bots)
	for name, tc := range map[string]struct {
		with map[string]any
		want string
	}{
		"missing scope": {map[string]any{"bot": "slack", "message_id": "1.5", "emoji": "eyes"}, "reactions:write"},
		"unknown bot":   {map[string]any{"bot": "teams", "message_id": "1.5", "emoji": "eyes"}, `no such chat bot named "teams"`},
		"no message":    {map[string]any{"bot": "slack", "emoji": "eyes"}, "message_id is required"},
		"no emoji":      {map[string]any{"bot": "slack", "message_id": "1.5"}, "emoji is required"},
		"no bot":        {map[string]any{"message_id": "1.5", "emoji": "eyes"}, "bot is required"},
	} {
		_, err := executor.Start(context.Background(), chatReactionStep(ChatReactionAddStepType, tc.with))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	for _, e := range []interface {
		Start(context.Context, ResolvedWorkflowStep) (WorkflowExecutionResult, error)
	}{NewChatReactionAddExecutor(nil), NewChatReactionRemoveExecutor(nil)} {
		_, err := e.Start(context.Background(), chatReactionStep(ChatReactionAddStepType, map[string]any{"bot": "slack", "message_id": "1.5", "emoji": "eyes"}))
		if err == nil || !strings.Contains(err.Error(), "not available") {
			t.Fatalf("nil reactor err = %v", err)
		}
	}
}

func TestChatReactionDoesNotHang(t *testing.T) {
	conn := &fakeChatConnection{name: "discord", platform: "discord", react: func(ctx context.Context, _, _, _ string, _ bool) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "discord", Platform: "discord", Enabled: true, Configured: true, Conn: conn}}, nil)
	bots.SendTimeout = 20 * time.Millisecond
	start := time.Now()
	_, err := NewChatReactionAddExecutor(bots).Start(context.Background(), chatReactionStep(ChatReactionAddStepType, map[string]any{"bot": "discord", "channel": "c1", "message_id": "m1", "emoji": "eyes"}))
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("err = %v after %v", err, time.Since(start))
	}
}
