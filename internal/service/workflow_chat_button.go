package service

import (
	"context"
	"errors"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// How a click on a button stands once the chat.button triggers saw it.
const (
	// ChatClickAccepted: a trigger made a request for it.
	ChatClickAccepted = "accepted"
	// ChatClickDuplicate: every trigger it matches had seen it already.
	ChatClickDuplicate = "duplicate"
	// ChatClickRefused: a trigger matches all but who clicked.
	ChatClickRefused = "refused"
	// ChatClickUnmatched: no trigger that is on matches it.
	ChatClickUnmatched = "unmatched"
)

// ChatClickOutcome is what became of a click.
type ChatClickOutcome struct {
	Status string
	// Started is an accepted click that started a run at once, rather than
	// waiting for an input, a confirmation or the loop stop.
	Started bool
	// Line is what the message says once an accepted click settles it,
	// such as "🛠 Fix it — chosen by <@U7>"; empty otherwise.
	Line string
}

// waitingSuffix ends the line of a click whose run has not started.
const waitingSuffix = " · waiting in Legato"

// ChatButton fires each workflow's chat.button trigger the click matches:
// one request per click and workflow (the event key is the platform, bot
// and interaction ID, so a redelivered click starts nothing), counted by
// the loop stop per message. A click by someone the trigger's user does
// not name is refused and starts nothing. The caller settles the message
// with Line when the click is accepted, and does nothing otherwise.
func (c *WorkflowTriggerCoordinator) ChatButton(ctx context.Context, e ChatEvent) (ChatClickOutcome, error) {
	if e.Click == nil {
		return ChatClickOutcome{Status: ChatClickUnmatched}, nil
	}
	fields := workflowengine.ChatClickFields{Bot: e.Bot, Channel: e.Channel, User: e.User, Value: e.Click.Value, MessageID: e.MessageID}
	outcome := ChatClickOutcome{Status: ChatClickUnmatched}
	var errs []error
	for _, definition := range c.definitions().Definitions {
		trigger := definition.On.ChatButton
		if trigger == nil {
			continue
		}
		switch trigger.Match(fields) {
		case workflowengine.ClickUnmatched:
			continue
		case workflowengine.ClickRefused:
			if outcome.Status == ChatClickUnmatched {
				outcome.Status = ChatClickRefused
			}
			continue
		}
		// Fire says nothing of a paused trigger and a click seen before
		// alike; only the second is a duplicate.
		if on, err := c.triggerEnabled(ctx, definition, workflowengine.TriggerChatButton); err != nil {
			errs = append(errs, err)
			continue
		} else if !on {
			continue
		}
		match := map[string]any{}
		for name, value := range trigger.Groups(fields) {
			match[name] = value
		}
		request, err := c.Fire(ctx, TriggerEvent{Definition: definition, Kind: workflowengine.TriggerChatButton,
			Key:  e.Platform + ":" + e.Bot + ":click:" + e.Click.InteractionID,
			Item: trigger.ItemFor(fields), At: e.At,
			Context: map[string]any{"event": map[string]any{
				"bot": e.Bot, "platform": e.Platform, "channel": e.Channel, "user": e.User,
				"value": e.Click.Value, "label": e.Click.Label, "match": match,
				"message_id": e.MessageID, "thread_id": e.ThreadID,
			}}})
		switch {
		case err != nil:
			errs = append(errs, err)
		case request == nil:
			if outcome.Status != ChatClickAccepted {
				outcome.Status = ChatClickDuplicate
			}
		default:
			outcome.Status = ChatClickAccepted
			outcome.Started = outcome.Started || request.Status == "started"
		}
	}
	if outcome.Status == ChatClickAccepted {
		outcome.Line = chatClickLine(e)
		if !outcome.Started {
			outcome.Line += waitingSuffix
		}
	}
	return outcome, errors.Join(errs...)
}

// chatClickLine says who chose what, mentioning the user as both
// platforms write a mention.
func chatClickLine(e ChatEvent) string {
	label := e.Click.Label
	if label == "" {
		label = e.Click.Value
	}
	return label + " — chosen by <@" + e.User + ">"
}
