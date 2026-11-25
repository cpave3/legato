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
// the thread with loop_item: thread. A message that forwards others is
// matched and kept as its own text with them quoted after it.
func (c *WorkflowTriggerCoordinator) ChatEvent(ctx context.Context, e ChatEvent) error {
	fields := workflowengine.ChatEventFields{Bot: e.Bot, Channel: e.Channel, User: e.User, Text: chatText(e.Text, e.ForwardedText), ThreadID: e.ThreadID, FromBot: e.FromBot}
	eventContext := map[string]any{"event": map[string]any{
		"bot": e.Bot, "platform": e.Platform, "channel": e.Channel, "user": e.User, "text": keptChatText(e.Text, e.ForwardedText),
		"message_id": e.MessageID, "thread_id": e.ThreadID, "forwarded": e.Forwarded, "forwarded_url": e.ForwardedURL,
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

// chatText is what triggers see of a message: the sender's own text, then
// what it forwards.
func chatText(own, forwarded string) string {
	if own == "" || forwarded == "" {
		return own + forwarded
	}
	return own + "\n\n" + forwarded
}

// cutForwardMarker ends a forwarded part cut short.
const cutForwardMarker = "\n> …"

// keptChatText is chatText cut to MaxChatEventText, the forwarded part
// first: the sender's own words matter more. When they alone are too long,
// the forwarded part goes and they are cut.
func keptChatText(own, forwarded string) string {
	text := chatText(own, forwarded)
	if len(text) <= MaxChatEventText {
		return text
	}
	room := MaxChatEventText - len(own) - len(cutForwardMarker)
	if own != "" {
		room -= len("\n\n")
	}
	if room <= 0 {
		return cutUTF8(own, MaxChatEventText)
	}
	return chatText(own, cutUTF8(forwarded, room)+cutForwardMarker)
}

// cutUTF8 is at most n bytes of s, cut where a character starts.
func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
