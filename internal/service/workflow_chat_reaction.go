package service

import (
	"context"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

// The chat reaction steps add a chat bot's reaction to a message, or remove
// it (LEG-633). They take the same bot and channel as chat.message.send, and
// any message in the channel: a chat trigger's event.message_id, an earlier
// send's message_id output, or an ID passed in.
const (
	ChatReactionAddStepType    = "chat.reaction.add"
	ChatReactionRemoveStepType = "chat.reaction.remove"
)

// ChatReactionAddExecutor is chat.reaction.add.
type ChatReactionAddExecutor struct{ bots ChatReactor }

// ChatReactionRemoveExecutor is chat.reaction.remove.
type ChatReactionRemoveExecutor struct{ bots ChatReactor }

// NewChatReactionAddExecutor reacts through bots; nil fails every step, as in
// a process with no chat bots.
func NewChatReactionAddExecutor(bots ChatReactor) *ChatReactionAddExecutor {
	return &ChatReactionAddExecutor{bots: bots}
}

// NewChatReactionRemoveExecutor removes reactions through bots; nil fails
// every step.
func NewChatReactionRemoveExecutor(bots ChatReactor) *ChatReactionRemoveExecutor {
	return &ChatReactionRemoveExecutor{bots: bots}
}

func (e *ChatReactionAddExecutor) Type() string    { return ChatReactionAddStepType }
func (e *ChatReactionRemoveExecutor) Type() string { return ChatReactionRemoveStepType }

func (e *ChatReactionAddExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error {
	return nil
}

func (e *ChatReactionRemoveExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error {
	return nil
}

func (e *ChatReactionAddExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	return startChatReaction(ctx, e.bots, step, false)
}

func (e *ChatReactionRemoveExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	return startChatReaction(ctx, e.bots, step, true)
}

func startChatReaction(ctx context.Context, bots ChatReactor, step ResolvedWorkflowStep, remove bool) (WorkflowExecutionResult, error) {
	values := map[string]string{}
	for _, field := range []struct {
		key      string
		required bool
	}{{"bot", true}, {"channel", false}, {"message_id", true}, {"emoji", true}} {
		value, err := workflowString(step.With, field.key, field.required)
		if err != nil {
			return WorkflowExecutionResult{}, err
		}
		values[field.key] = value
	}
	if bots == nil {
		return WorkflowExecutionResult{}, fmt.Errorf("bot %s: chat bots are not available in this process", values["bot"])
	}
	if err := bots.React(ctx, values["bot"], values["channel"], values["message_id"], values["emoji"], remove); err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"ok": true}}, nil
}
