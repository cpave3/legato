package service

import (
	"context"

	"github.com/cpave3/legato/internal/engine/store"
)

// AgentInteraction is the generic interface for sending messages and
// controlling agents after they've been spawned. It decouples the
// delivery path (tmux send-keys vs. Chimera HTTP API) from the caller.
//
// The interface is dispatched by AgentService based on the agent's kind:
// Chimera agents with a discovered endpoint use the Chimera adapter;
// all others use the generic send-keys adapter.
// PermissionAction is a decision for a pending structured permission request.
type PermissionAction struct {
	Decision string `json:"decision"`
	Feedback string `json:"feedback,omitempty"`
}

type AgentInteraction interface {
	// SendMessage delivers a text message to the agent. For tmux agents,
	// this is equivalent to send-keys + Enter. For Chimera agents, this
	// POSTs to the session's messages endpoint.
	SendMessage(ctx context.Context, taskID, message string) error
	// QueueMessage delivers a message at the next supported step boundary.
	QueueMessage(ctx context.Context, taskID, message string) error
	// Interrupt sends an interrupt to the agent's active turn.
	Interrupt(ctx context.Context, taskID string) error
	// ResolvePermission resolves a pending structured permission request.
	ResolvePermission(ctx context.Context, taskID, requestID string, action PermissionAction) error
}

// SendKeysAdapter wraps the existing tmux send-keys behavior as the
// default AgentInteraction implementation. This is a thin adapter — it
// delegates to the agent service's TmuxManager, so all non-Chimera agents
// (Claude Code, Codex, shell) keep their current behavior unchanged.
type SendKeysAdapter struct {
	tmux  TmuxManager
	store *store.Store
}

// NewSendKeysAdapter creates the default send-keys interaction adapter.
func NewSendKeysAdapter(tmux TmuxManager, store *store.Store) *SendKeysAdapter {
	return &SendKeysAdapter{tmux: tmux, store: store}
}

func (a *SendKeysAdapter) SendMessage(ctx context.Context, taskID, message string) error {
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return err
	}
	return a.tmux.SendKeysMultiline(session.TmuxSession, message)
}

func (a *SendKeysAdapter) QueueMessage(ctx context.Context, taskID, message string) error {
	return a.SendMessage(ctx, taskID, message)
}

func (a *SendKeysAdapter) Interrupt(ctx context.Context, taskID string) error {
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return err
	}
	return a.tmux.SendKey(session.TmuxSession, "C-c")
}

func (a *SendKeysAdapter) ResolvePermission(ctx context.Context, taskID, requestID string, action PermissionAction) error {
	// Non-Chimera agents don't have structured permissions.
	return nil
}
