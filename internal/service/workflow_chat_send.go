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
	if e.bots == nil {
		return WorkflowExecutionResult{}, fmt.Errorf("bot %s: chat bots are not available in this process", bot)
	}
	sent, err := e.bots.Send(ctx, bot, channel, text, threadID)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{
		"message_id": sent.MessageID, "channel": sent.Channel, "thread_id": sent.ThreadID,
	}}, nil
}
