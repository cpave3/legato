package service

import (
	"context"
	"fmt"

	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/engine/store"
)

// ChimeraInteractionAdapter implements AgentInteraction for Chimera agents
// that have a discovered endpoint. It uses the Go Chimera protocol client
// to send messages, interrupt, and resolve permissions through the HTTP API
// instead of tmux send-keys.
type ChimeraInteractionAdapter struct {
	clientFactory func(endpoint string) *chimera.Client
	store         *store.Store
}

// NewChimeraInteractionAdapter creates the Chimera-specific interaction adapter.
// clientFactory is injected so tests can provide a fake client.
func NewChimeraInteractionAdapter(store *store.Store, clientFactory func(string) *chimera.Client) *ChimeraInteractionAdapter {
	if clientFactory == nil {
		clientFactory = func(endpoint string) *chimera.Client {
			return chimera.NewClient(endpoint)
		}
	}
	return &ChimeraInteractionAdapter{
		clientFactory: clientFactory,
		store:         store,
	}
}

func (a *ChimeraInteractionAdapter) getEndpointAndSession(ctx context.Context, taskID string) (endpoint, sessionID string, err error) {
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return "", "", err
	}
	if session.ChimeraEndpoint == nil || session.ChimeraSessionID == nil {
		return "", "", fmt.Errorf("no chimera endpoint discovered for task %s", taskID)
	}
	return *session.ChimeraEndpoint, *session.ChimeraSessionID, nil
}

func (a *ChimeraInteractionAdapter) SendMessage(ctx context.Context, taskID, message string) error {
	endpoint, sessionID, err := a.getEndpointAndSession(ctx, taskID)
	if err != nil {
		return err
	}
	client := a.clientFactory(endpoint)
	return client.PostMessage(ctx, sessionID, message)
}

func (a *ChimeraInteractionAdapter) Interrupt(ctx context.Context, taskID string) error {
	endpoint, sessionID, err := a.getEndpointAndSession(ctx, taskID)
	if err != nil {
		return err
	}
	client := a.clientFactory(endpoint)
	return client.Interrupt(ctx, sessionID)
}

func (a *ChimeraInteractionAdapter) ResolvePermission(ctx context.Context, taskID, requestID, decision, rememberScope string) error {
	endpoint, sessionID, err := a.getEndpointAndSession(ctx, taskID)
	if err != nil {
		return err
	}
	client := a.clientFactory(endpoint)
	return client.ResolvePermission(ctx, sessionID, requestID, decision, rememberScope)
}
