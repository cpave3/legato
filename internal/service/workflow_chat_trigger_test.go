package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func chatTriggerFixture(t *testing.T, on string) (*store.Store, *WorkflowTriggerCoordinator, *fakeWorkflowExecutor) {
	t.Helper()
	return chatTriggerFixtureWithInputs(t, "", on)
}

// chatTriggerFixtureWithInputs is chatTriggerFixture with more inputs.
func chatTriggerFixtureWithInputs(t *testing.T, inputs, on string) (*store.Store, *WorkflowTriggerCoordinator, *fakeWorkflowExecutor) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	dir := t.TempDir()
	definition := `schema_version: 1
name: ops
title: Ops
inputs:
  text: {type: string}
  thread: {type: string}
` + inputs + `on:
` + on + `steps:
  - {id: say, title: Say, type: chat.message.send, with: {bot: slack, text: "${{ workflow.inputs.text }}", thread_id: "${{ workflow.inputs.thread }}"}}
`
	if err := os.WriteFile(filepath.Join(dir, "ops.yaml"), []byte(definition), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: ChatSendStepType, result: WorkflowExecutionResult{Status: "succeeded"}}
	coordinator := NewWorkflowTriggerCoordinator(db, NewWorkflowService(db, []WorkflowExecutor{executor}), func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) })
	return db, coordinator, executor
}

const chatInputs = "    inputs:\n      text: ${{ event.text }}\n      thread: ${{ event.thread_id }}\n"

func chatMessage(id, channel, text string) ChatEvent {
	return ChatEvent{Bot: "slack", Platform: "slack", Channel: channel, User: "U1", Text: text, MessageID: id, ThreadID: id}
}

func requestsOfKind(t *testing.T, db *store.Store, status, kind string) []store.WorkflowTriggerRequest {
	t.Helper()
	var out []store.WorkflowTriggerRequest
	for _, request := range requestsWith(t, db, status) {
		if request.TriggerKind == kind {
			out = append(out, request)
		}
	}
	return out
}

func TestAChatMessageStartsTheWorkflowWithTheEvent(t *testing.T) {
	db, coordinator, _ := chatTriggerFixture(t, "  chat.message:\n    bot: slack\n    text: {contains: deploy}\n"+chatInputs)
	ctx := context.Background()
	if err := coordinator.ChatEvent(ctx, chatMessage("1.1", "C1", "please deploy")); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.ChatEvent(ctx, chatMessage("1.2", "C1", "lunch?")); err != nil {
		t.Fatal(err)
	}
	started := requestsWith(t, db, "started")
	if len(started) != 1 || started[0].TriggerKind != workflowengine.TriggerChatMessage || started[0].Item != "slack:C1" {
		t.Fatalf("started = %+v", started)
	}
	run, err := db.FindWorkflowRunByTriggerRequest(ctx, started[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"text":"please deploy"`, `"thread":"1.1"`} {
		if !strings.Contains(run.Run.InputsJSON, want) {
			t.Fatalf("inputs %s lack %s", run.Run.InputsJSON, want)
		}
	}
	for _, want := range []string{`"platform":"slack"`, `"message_id":"1.1"`, `"channel":"C1"`, `"user":"U1"`} {
		if !strings.Contains(started[0].EventJSON, want) {
			t.Fatalf("event %s lacks %s", started[0].EventJSON, want)
		}
	}
}

func TestAMentionFiresBothKindsAndAMessageOnlyOne(t *testing.T) {
	db, coordinator, _ := chatTriggerFixture(t, "  chat.message:\n    bot: slack\n  chat.mention:\n    bot: slack\n")
	ctx := context.Background()
	plain := chatMessage("1.1", "C1", "hello")
	mention := chatMessage("1.2", "C1", "<@UBOT> hello")
	mention.Mention = true
	for _, e := range []ChatEvent{plain, mention} {
		if err := coordinator.ChatEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(requestsOfKind(t, db, "started", workflowengine.TriggerChatMessage)); n != 2 {
		t.Fatalf("chat.message requests = %d, want 2", n)
	}
	if n := len(requestsOfKind(t, db, "started", workflowengine.TriggerChatMention)); n != 1 {
		t.Fatalf("chat.mention requests = %d, want 1", n)
	}
}

func TestAChatMessageFiresOnce(t *testing.T) {
	db, coordinator, _ := chatTriggerFixture(t, "  chat.message:\n    bot: slack\n    max_runs_per_day: 10\n")
	ctx := context.Background()
	// Slack redelivers; the same ts in another channel is another message.
	for _, e := range []ChatEvent{chatMessage("1.1", "C1", "a"), chatMessage("1.1", "C1", "a"), chatMessage("1.1", "C2", "a")} {
		if err := coordinator.ChatEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(requestsWith(t, db, "started")); n != 2 {
		t.Fatalf("started = %d, want 2", n)
	}
}

func TestTheLoopStopCountsChatRunsPerChannelOrThread(t *testing.T) {
	db, coordinator, _ := chatTriggerFixture(t, "  chat.message:\n    bot: slack\n")
	ctx := context.Background()
	for _, id := range []string{"1", "2", "3", "4"} {
		if err := coordinator.ChatEvent(ctx, chatMessage(id, "C1", "x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := coordinator.ChatEvent(ctx, chatMessage("5", "C2", "x")); err != nil {
		t.Fatal(err)
	}
	if started, held := len(requestsWith(t, db, "started")), len(requestsWith(t, db, "held")); started != 4 || held != 1 {
		t.Fatalf("started %d, held %d; want 4 and 1", started, held)
	}

	db, coordinator, _ = chatTriggerFixture(t, "  chat.message:\n    bot: slack\n    loop_item: thread\n")
	for i, thread := range []string{"t1", "t1", "t1", "t1", "t2"} {
		e := chatMessage(string(rune('a'+i)), "C1", "x")
		e.ThreadID = thread
		if err := coordinator.ChatEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	held := requestsWith(t, db, "held")
	if started := len(requestsWith(t, db, "started")); started != 4 || len(held) != 1 || held[0].Item != "slack:C1/t1" {
		t.Fatalf("started %d, held %+v", started, held)
	}
}

func TestChatTriggersConfirmAndPause(t *testing.T) {
	db, coordinator, _ := chatTriggerFixture(t, "  chat.message:\n    bot: slack\n    confirm: true\n  chat.mention:\n    bot: slack\n    enabled: false\n")
	e := chatMessage("1", "C1", "<@UBOT>")
	e.Mention = true
	if err := coordinator.ChatEvent(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if waiting := requestsOfKind(t, db, "waiting_confirmation", workflowengine.TriggerChatMessage); len(waiting) != 1 {
		t.Fatalf("waiting = %+v", waiting)
	}
	if n := len(requestsOfKind(t, db, "started", workflowengine.TriggerChatMention)) + len(requestsOfKind(t, db, "waiting_confirmation", workflowengine.TriggerChatMention)); n != 0 {
		t.Fatalf("a paused trigger made %d requests", n)
	}
}

func TestChatTriggersSkipOtherBotsAndOtherChatBots(t *testing.T) {
	db, coordinator, _ := chatTriggerFixture(t, "  chat.message:\n    bot: slack\n")
	fromBot := chatMessage("1", "C1", "x")
	fromBot.FromBot = true
	discord := chatMessage("2", "C1", "x")
	discord.Bot, discord.Platform = "discord", "discord"
	for _, e := range []ChatEvent{fromBot, discord} {
		if err := coordinator.ChatEvent(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(requestsWith(t, db, "started")); n != 0 {
		t.Fatalf("started = %d", n)
	}
}

func TestLongChatTextIsCutBeforeItIsKept(t *testing.T) {
	db, coordinator, _ := chatTriggerFixture(t, "  chat.message:\n    bot: slack\n"+chatInputs)
	if err := coordinator.ChatEvent(context.Background(), chatMessage("1", "C1", strings.Repeat("é", MaxChatEventText))); err != nil {
		t.Fatal(err)
	}
	started := requestsWith(t, db, "started")
	if len(started) != 1 || len(started[0].EventJSON) > 2*MaxChatEventText {
		t.Fatalf("event json is %d bytes", len(started[0].EventJSON))
	}
}

func TestChatTriggerInputsReadFallbacksAndExpressionsInText(t *testing.T) {
	db, coordinator, _ := chatTriggerFixture(t, "  chat.message:\n    bot: slack\n    inputs:\n      text: ${{ event.subject ?? event.text }}\n      thread: \"re ${{ event.thread_id }}\"\n")
	if err := coordinator.ChatEvent(context.Background(), chatMessage("1.1", "C1", "deploy")); err != nil {
		t.Fatal(err)
	}
	started := requestsWith(t, db, "started")
	if len(started) != 1 {
		t.Fatalf("started = %+v", started)
	}
	for _, want := range []string{`"text":"deploy"`, `"thread":"re 1.1"`} {
		if !strings.Contains(started[0].InputsJSON, want) {
			t.Fatalf("inputs %s lack %s", started[0].InputsJSON, want)
		}
	}
}
