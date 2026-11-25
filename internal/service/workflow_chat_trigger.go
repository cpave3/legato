package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
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
//
// A fallback trigger fires only when no other enabled chat trigger on the
// bot, of either kind, matched: the normal triggers are matched first, and
// a fallback whose own filters matched is otherwise recorded as skipped,
// naming those that did. What happens to a normal trigger's request after
// it matched (held, waiting, failed) does not matter.
func (c *WorkflowTriggerCoordinator) ChatEvent(ctx context.Context, e ChatEvent) error {
	if e.Click != nil {
		return nil // a click is no message: see ChatButton
	}
	fields := workflowengine.ChatEventFields{Bot: e.Bot, Channel: e.Channel, User: e.User, Text: chatText(e.Text, e.ForwardedText), ThreadID: e.ThreadID, FromBot: e.FromBot}
	event := map[string]any{
		"bot": e.Bot, "platform": e.Platform, "channel": e.Channel, "user": e.User, "text": keptChatText(e.Text, e.ForwardedText),
		"message_id": e.MessageID, "thread_id": e.ThreadID, "forwarded": e.Forwarded, "forwarded_url": e.ForwardedURL,
	}
	normal, fallbacks, err := c.matchingChatTriggers(ctx, e, fields)
	if err != nil || len(normal)+len(fallbacks) == 0 {
		return err
	}
	if event["commands"], err = c.chatCommands(ctx, e.Bot, e.Platform); err != nil {
		return err
	}
	key := e.Platform + ":" + e.Bot + ":" + e.Channel + ":" + e.MessageID
	var errs []error
	fire := func(match chatTriggerMatch, eventContext map[string]any, skip string) {
		if _, err := c.Fire(ctx, TriggerEvent{Definition: match.definition, Kind: match.kind,
			Key: key, Item: match.trigger.ItemFor(fields), Context: eventContext, At: e.At, Skip: skip}); err != nil {
			errs = append(errs, err)
		}
	}
	for _, match := range normal {
		fire(match, map[string]any{"event": event}, "")
	}
	if len(fallbacks) > 0 {
		if len(normal) == 0 {
			fired := map[string]any{"event": event, "fallback": "no other enabled chat trigger on " + e.Bot + " matched"}
			for _, match := range fallbacks {
				fire(match, fired, "")
			}
		} else {
			skip := "fallback: matched by " + chatTriggerNames(normal)
			for _, match := range fallbacks {
				fire(match, map[string]any{"event": event}, skip)
			}
		}
	}
	return errors.Join(errs...)
}

// chatTriggerMatch is a chat trigger that matched a message and is on.
type chatTriggerMatch struct {
	definition *workflowengine.Definition
	kind       string
	trigger    *workflowengine.ChatTrigger
}

// matchingChatTriggers are the enabled chat triggers whose filters a
// message passes, normal ones apart from fallbacks. A paused trigger never
// matches.
func (c *WorkflowTriggerCoordinator) matchingChatTriggers(ctx context.Context, e ChatEvent, fields workflowengine.ChatEventFields) (normal, fallbacks []chatTriggerMatch, err error) {
	for _, definition := range c.definitions().Definitions {
		for _, kind := range []string{workflowengine.TriggerChatMessage, workflowengine.TriggerChatMention} {
			trigger := definition.On.Chat(kind)
			if trigger == nil || (kind == workflowengine.TriggerChatMention && !e.Mention) || !trigger.Matches(fields) {
				continue
			}
			on, err := c.triggerEnabled(ctx, definition, kind)
			if err != nil {
				return nil, nil, err
			}
			if !on {
				continue
			}
			match := chatTriggerMatch{definition: definition, kind: kind, trigger: trigger}
			if trigger.Fallback {
				fallbacks = append(fallbacks, match)
			} else {
				normal = append(normal, match)
			}
		}
	}
	return normal, fallbacks, nil
}

// Bounds of ${{ event.commands }}: each description, in characters, and
// the whole list, in bytes, its "… and N more" included. On Discord, which
// refuses a message over 2000 characters, the list leaves room for the
// reply's own words.
const (
	maxCommandDescription   = 200
	maxCommandsBytes        = 4 << 10
	maxDiscordCommandsBytes = 1500
)

// chatCommands is ${{ event.commands }}: one line per workflow with
// chat_help: true and an enabled chat trigger on the bot that is not a
// fallback, sorted by title, "• Title — first line of its description".
// Plain text, which Slack and Discord show alike; "" when none.
func (c *WorkflowTriggerCoordinator) chatCommands(ctx context.Context, bot, platform string) (string, error) {
	type command struct{ title, line string }
	var commands []command
	for _, definition := range c.definitions().Definitions {
		if !definition.ChatHelp {
			continue
		}
		listed := false
		for _, kind := range []string{workflowengine.TriggerChatMessage, workflowengine.TriggerChatMention} {
			trigger := definition.On.Chat(kind)
			if trigger == nil || trigger.Bot != bot || trigger.Fallback {
				continue
			}
			on, err := c.triggerEnabled(ctx, definition, kind)
			if err != nil {
				return "", err
			}
			listed = listed || on
		}
		if !listed {
			continue
		}
		line := "• " + definition.Title
		if description := strings.TrimSpace(firstLine(strings.TrimSpace(definition.Description))); description != "" {
			line += " — " + cutRunes(description, maxCommandDescription)
		}
		commands = append(commands, command{definition.Title, line})
	}
	sort.SliceStable(commands, func(i, j int) bool { return commands[i].title < commands[j].title })
	lines := make([]string, 0, len(commands))
	for _, command := range commands {
		lines = append(lines, command.line)
	}
	limit := maxCommandsBytes
	if platform == "discord" {
		limit = maxDiscordCommandsBytes
	}
	return boundedLines(lines, limit), nil
}

// boundedLines joins lines in at most limit bytes: as many as fit, then
// "… and N more" for the rest.
func boundedLines(lines []string, limit int) string {
	if text := strings.Join(lines, "\n"); len(text) <= limit {
		return text
	}
	for n := len(lines) - 1; n > 0; n-- {
		text := strings.Join(append(lines[:n:n], fmt.Sprintf("… and %d more", len(lines)-n)), "\n")
		if len(text) <= limit {
			return text
		}
	}
	return fmt.Sprintf("… and %d more", len(lines))
}

// maxSkipNames is how many triggers a skipped fallback's reason names.
const maxSkipNames = 5

// chatTriggerNames names triggers as "workflow (kind)", sorted, the first
// few and how many more.
func chatTriggerNames(matches []chatTriggerMatch) string {
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, match.definition.Name+" ("+match.kind+")")
	}
	sort.Strings(names)
	if len(names) > maxSkipNames {
		return strings.Join(names[:maxSkipNames], ", ") + fmt.Sprintf(" and %d more", len(names)-maxSkipNames)
	}
	return strings.Join(names, ", ")
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
