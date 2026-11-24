package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

const chatWorkflow = `
schema_version: 1
name: ops
title: Ops
inputs:
  text: {type: string}
  thread: {type: string}
on:
TRIGGER
steps:
  - id: reply
    title: Reply
    type: chat.message.send
    with:
      bot: slack
      channel: C1
      text: seen
`

func parseChat(t *testing.T, trigger string) (*workflow.Definition, error) {
	t.Helper()
	return workflow.Parse("ops.yaml", []byte(strings.Replace(chatWorkflow, "TRIGGER\n", trigger, 1)))
}

func TestChatTriggersParseAndList(t *testing.T) {
	definition, err := parseChat(t, `  chat.message:
    bot: slack
    channel: C123
    user: U9
    text: {contains: deploy}
    enabled: false
    max_runs_per_day: 5
    inputs:
      text: ${{ event.text }}
      thread: ${{ event.thread_id }}
  chat.mention:
    bot: discord
    loop_item: thread
`)
	if err != nil {
		t.Fatal(err)
	}
	list := definition.On.List()
	if len(list) != 2 || list[0].Kind != workflow.TriggerChatMessage || list[1].Kind != workflow.TriggerChatMention {
		t.Fatalf("triggers = %+v", list)
	}
	if list[0].EnabledByDefault || list[0].MaxRunsPerDay != 5 || !list[1].EnabledByDefault {
		t.Fatalf("triggers = %+v", list)
	}
	message := definition.On.Chat(workflow.TriggerChatMessage)
	if message == nil || message.Bot != "slack" || message.Channel != "C123" || message.Text == nil || message.Text.Contains != "deploy" {
		t.Fatalf("chat.message = %+v", message)
	}
	if mention := definition.On.Chat(workflow.TriggerChatMention); mention == nil || mention.Bot != "discord" || mention.LoopItem != workflow.ChatLoopThread {
		t.Fatalf("chat.mention = %+v", mention)
	}
	if definition.On.Chat(workflow.TriggerSchedule) != nil {
		t.Fatal("Chat returned a trigger for a kind that is not a chat kind")
	}
}

func TestChatTriggerValidation(t *testing.T) {
	cases := map[string]string{
		"no bot":           "  chat.message:\n    channel: C1\n",
		"bad bot":          "  chat.message:\n    bot: Slack Bot\n",
		"templated bot":    "  chat.mention:\n    bot: ${{ event.bot }}\n",
		"bad regex":        "  chat.message:\n    bot: slack\n    text: {regex: \"(\"}\n",
		"long regex":       "  chat.message:\n    bot: slack\n    text: {regex: \"" + strings.Repeat("a", 513) + "\"}\n",
		"both matches":     "  chat.message:\n    bot: slack\n    text: {contains: a, regex: b}\n",
		"empty match":      "  chat.message:\n    bot: slack\n    text: {}\n",
		"undeclared input": "  chat.message:\n    bot: slack\n    inputs: {nope: x}\n",
		"negative max":     "  chat.message:\n    bot: slack\n    max_runs_per_day: -1\n",
		"bad loop item":    "  chat.message:\n    bot: slack\n    loop_item: user\n",
	}
	for name, trigger := range cases {
		if _, err := parseChat(t, trigger); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestChatTriggerMatches(t *testing.T) {
	event := workflow.ChatEventFields{Bot: "slack", Channel: "C1", User: "U1", Text: "Please DEPLOY to staging"}
	cases := []struct {
		name    string
		trigger workflow.ChatTrigger
		event   workflow.ChatEventFields
		want    bool
	}{
		{"bot only", workflow.ChatTrigger{Bot: "slack"}, event, true},
		{"other bot", workflow.ChatTrigger{Bot: "discord"}, event, false},
		{"channel", workflow.ChatTrigger{Bot: "slack", Channel: "C1"}, event, true},
		{"other channel", workflow.ChatTrigger{Bot: "slack", Channel: "C2"}, event, false},
		{"user", workflow.ChatTrigger{Bot: "slack", User: "U1"}, event, true},
		{"other user", workflow.ChatTrigger{Bot: "slack", User: "U2"}, event, false},
		{"contains ignores case", workflow.ChatTrigger{Bot: "slack", Text: &workflow.ChatTextMatch{Contains: "deploy"}}, event, true},
		{"contains misses", workflow.ChatTrigger{Bot: "slack", Text: &workflow.ChatTextMatch{Contains: "rollback"}}, event, false},
		{"regex", workflow.ChatTrigger{Bot: "slack", Text: &workflow.ChatTextMatch{Regex: `DEPLOY to \w+`}}, event, true},
		{"regex is case sensitive", workflow.ChatTrigger{Bot: "slack", Text: &workflow.ChatTextMatch{Regex: `deploy`}}, event, false},
		{"all together", workflow.ChatTrigger{Bot: "slack", Channel: "C1", User: "U1", Text: &workflow.ChatTextMatch{Regex: `(?i)staging$`}}, event, true},
		{"from a bot", workflow.ChatTrigger{Bot: "slack"}, workflow.ChatEventFields{Bot: "slack", Channel: "C1", FromBot: true}, false},
		{"from a bot, included", workflow.ChatTrigger{Bot: "slack", IncludeBots: true}, workflow.ChatEventFields{Bot: "slack", Channel: "C1", FromBot: true}, true},
	}
	for _, tc := range cases {
		if got := tc.trigger.Matches(tc.event); got != tc.want {
			t.Errorf("%s: Matches = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestChatTriggerLoopItem(t *testing.T) {
	event := workflow.ChatEventFields{Bot: "slack", Channel: "C1", ThreadID: "1.2"}
	if got := (workflow.ChatTrigger{Bot: "slack"}).ItemFor(event); got != "slack:C1" {
		t.Fatalf("channel item = %q", got)
	}
	if got := (workflow.ChatTrigger{Bot: "slack", LoopItem: workflow.ChatLoopThread}).ItemFor(event); got != "slack:C1/1.2" {
		t.Fatalf("thread item = %q", got)
	}
	if got := (workflow.ChatTrigger{Bot: "slack", LoopItem: workflow.ChatLoopThread}).ItemFor(workflow.ChatEventFields{Bot: "slack", Channel: "C1"}); got != "slack:C1" {
		t.Fatalf("thread item without a thread = %q", got)
	}
}
