package service

import (
	"context"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

// ChatSendStepType posts a message as a chat bot (LEG-624).
const ChatSendStepType = "chat.message.send"

// ChatSendExecutor is chat.message.send.
type ChatSendExecutor struct {
	bots ChatSender
}

// NewChatSendExecutor sends through bots; nil fails every step, as in a
// process with no chat bots.
func NewChatSendExecutor(bots ChatSender) *ChatSendExecutor { return &ChatSendExecutor{bots: bots} }

func (e *ChatSendExecutor) Type() string { return ChatSendStepType }

func (e *ChatSendExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

func (e *ChatSendExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	bot, err := workflowString(step.With, "bot", true)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	text, err := workflowString(step.With, "text", true)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	channel, err := workflowString(step.With, "channel", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	threadID, err := workflowString(step.With, "thread_id", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	buttons, err := chatSendButtons(step.With["buttons"])
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if e.bots == nil {
		return WorkflowExecutionResult{}, fmt.Errorf("bot %s: chat bots are not available in this process", bot)
	}
	sent, err := e.bots.Send(ctx, bot, channel, text, threadID, buttons...)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{
		"message_id": sent.MessageID, "channel": sent.Channel, "thread_id": sent.ThreadID,
	}}, nil
}

// chatSendButtons reads with.buttons once resolved: absent, or a list of
// {label, value, style}. The bots check the limits.
func chatSendButtons(value any) ([]ChatButton, error) {
	if value == nil {
		return nil, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("with buttons must be a list of buttons, each {label, value, style}")
	}
	buttons := make([]ChatButton, 0, len(list))
	for i, item := range list {
		fields, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("with buttons[%d] must be a map of label, value and style", i)
		}
		var button ChatButton
		for name, target := range map[string]*string{"label": &button.Label, "value": &button.Value, "style": &button.Style} {
			raw, present := fields[name]
			if !present {
				continue
			}
			text, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("with buttons[%d]: %s must be text", i, name)
			}
			*target = text
		}
		buttons = append(buttons, button)
	}
	return buttons, nil
}
