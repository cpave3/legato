package server

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	"github.com/cpave3/legato/internal/engine/chimera"
)

// chimeraStreamManager manages SSE subscriptions to Chimera servers for
// WebSocket clients. Each WebSocket client that subscribes to a Chimera
// agent gets its own SSE subscription that forwards events to the client.
type chimeraStreamManager struct {
	agents chimeraAgentLookup
	mu     sync.Mutex
	subs   map[*wsClient]map[string]context.CancelFunc // client -> agentID -> cancel
}

// chimeraAgentLookup provides the Chimera endpoint and session ID for a task.
type chimeraAgentLookup interface {
	GetAgentSession(ctx context.Context, taskID string) (endpoint, sessionID string, err error)
}

type agentServiceChimeraLookup struct {
	agents interface {
		GetAgentSession(ctx context.Context, taskID string) (*chimeraSessionRow, error)
	}
}

type chimeraSessionRow struct {
	ChimeraEndpoint  *string
	ChimeraSessionID *string
}

func (s *Server) startChimeraEventForwarder(client *wsClient, taskID string) {
	if s.agents == nil {
		return
	}

	session, err := s.agents.GetAgentSession(context.Background(), taskID)
	if err != nil {
		client.send(WSMessage{Type: MsgError, Error: "agent session not found"})
		return
	}
	if session.ChimeraEndpoint == nil || session.ChimeraSessionID == nil {
		client.send(WSMessage{Type: MsgError, Error: "no chimera endpoint"})
		return
	}

	endpoint := *session.ChimeraEndpoint
	sessionID := *session.ChimeraSessionID
	client.ctx, client.cancel = context.WithCancel(context.Background())

	// Subscribe to Chimera SSE and forward events to the WebSocket client.
	go func() {
		client := client
		var lastEventID string

		for {
			select {
			case <-client.ctx.Done():
				return
			default:
			}

			c := chimera.NewClient(endpoint)
			err := c.Subscribe(client.ctx, sessionID, lastEventID, func(env chimera.EventEnvelope) {
				if env.EventID != "" {
					lastEventID = env.EventID
				}
				// Forward the event as a WebSocket message.
				data, err := json.Marshal(env)
				if err != nil {
					return
				}
				client.send(WSMessage{
					Type:    MsgChimeraEvent,
					AgentID: taskID,
					Content: string(data),
				})
			})
			if err != nil {
				if client.ctx.Err() != nil {
					return
				}
				log.Printf("chimera SSE forwarder for %s: %v", taskID, err)
				// Reconnect after a brief pause.
				select {
				case <-client.ctx.Done():
					return
				default:
				}
			} else {
				return
			}
		}
	}()
}
