package service

import (
	"context"
	"errors"
	"unicode/utf8"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// MaxChatEventText is the most of a message's text, in bytes, a trigger
// request keeps (its event is stored with it).
const MaxChatEventText = 8 << 10

// ChatEvent fires each workflow's chat.message trigger for a message a bot
// received, and its chat.mention trigger when the message mentions the bot
// (or is sent to it directly). One request per message and kind: the event
// key is the platform, bot, channel and message ID (a Slack ts is unique
// only within its channel). The loop stop counts runs by the channel, or
// the thread with loop_item: thread.
func (c *WorkflowTriggerCoordinator) ChatEvent(ctx context.Context, e ChatEvent) error {
	text := e.Text
	if len(text) > MaxChatEventText {
		text = text[:MaxChatEventText]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	fields := workflowengine.ChatEventFields{Bot: e.Bot, Channel: e.Channel, User: e.User, Text: e.Text, ThreadID: e.ThreadID, FromBot: e.FromBot}
	eventContext := map[string]any{"event": map[string]any{
		"bot": e.Bot, "platform": e.Platform, "channel": e.Channel, "user": e.User, "text": text,
		"message_id": e.MessageID, "thread_id": e.ThreadID,
	}}
	var errs []error
	for _, definition := range c.definitions().Definitions {
		for _, kind := range []string{workflowengine.TriggerChatMessage, workflowengine.TriggerChatMention} {
			trigger := definition.On.Chat(kind)
			if trigger == nil || (kind == workflowengine.TriggerChatMention && !e.Mention) || !trigger.Matches(fields) {
				continue
			}
			if _, err := c.Fire(ctx, TriggerEvent{Definition: definition, Kind: kind,
				Key:  e.Platform + ":" + e.Bot + ":" + e.Channel + ":" + e.MessageID,
				Item: trigger.ItemFor(fields), Context: eventContext, At: e.At}); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
