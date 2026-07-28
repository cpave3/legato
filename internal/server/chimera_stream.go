package server

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/engine/store"
)

// chimeraStreamManager manages a single SSE subscription per Chimera agent
// session and fans out events to all subscribed WebSocket clients. This
// ensures all connected clients (desktop, mobile, etc.) receive the same
// events in sync, even if a message is sent from one client — the Chimera
// server emits the event once, and the shared subscription broadcasts it
// to all clients.
type chimeraStreamManager struct {
	agents interface {
		HasChimeraEndpoint(ctx context.Context, taskID string) bool
		GetAgentSession(ctx context.Context, taskID string) (*store.AgentSession, error)
	}

	mu      sync.Mutex
	streams map[string]*chimeraStream // keyed by taskID
}

type chimeraStream struct {
	taskID    string
	endpoint  string
	sessionID string
	cancel    context.CancelFunc

	mu        sync.RWMutex
	clients   map[*wsClient]struct{}
	lastEvent string
}

func newChimeraStreamManager() *chimeraStreamManager {
	return &chimeraStreamManager{
		streams: make(map[string]*chimeraStream),
	}
}

// subscribe adds a WebSocket client to the fan-out for the given agent.
// If no SSE subscription exists yet, it starts one. The client will
// receive all future Chimera events for this agent.
func (m *chimeraStreamManager) subscribe(client *wsClient, taskID string) {
	m.mu.Lock()
	stream, ok := m.streams[taskID]
	if !ok {
		// Resolve the endpoint and start a new shared SSE subscription.
		session, err := m.agents.GetAgentSession(context.Background(), taskID)
		if err != nil || session.ChimeraEndpoint == nil || session.ChimeraSessionID == nil {
			m.mu.Unlock()
			client.send(WSMessage{Type: MsgError, Error: "no chimera endpoint"})
			return
		}
		stream = &chimeraStream{
			taskID:    taskID,
			endpoint:  *session.ChimeraEndpoint,
		sessionID: *session.ChimeraSessionID,
			clients:   make(map[*wsClient]struct{}),
		}
		m.streams[taskID] = stream
		ctx, cancel := context.WithCancel(context.Background())
		stream.cancel = cancel
		go m.runSSELoop(ctx, stream)
	}
	m.mu.Unlock()

	stream.mu.Lock()
	stream.clients[client] = struct{}{}
	stream.mu.Unlock()
}

// unsubscribe removes a WebSocket client from the fan-out. When the last
// client leaves, the SSE subscription is cancelled and the stream is
// removed.
func (m *chimeraStreamManager) unsubscribe(client *wsClient, taskID string) {
	m.mu.Lock()
	stream, ok := m.streams[taskID]
	if !ok {
		m.mu.Unlock()
		return
	}

	stream.mu.Lock()
	delete(stream.clients, client)
	empty := len(stream.clients) == 0
	stream.mu.Unlock()

	if empty {
		stream.cancel()
		delete(m.streams, taskID)
	}
	m.mu.Unlock()
}

// runSSELoop maintains a persistent SSE connection to the Chimera server,
// reconnecting on error. Every event is broadcast to all subscribed
// WebSocket clients.
func (m *chimeraStreamManager) runSSELoop(ctx context.Context, stream *chimeraStream) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c := chimera.NewClient(stream.endpoint)
		// On first connect, lastEvent is empty — subscribe without replay
		// so we only get live events. The frontend hydrates history from
		// the snapshot. On reconnect after error, lastEvent provides the
		// cursor for gap-free resume.
		since := stream.lastEvent
		err := c.Subscribe(ctx, stream.sessionID, since, func(env chimera.EventEnvelope) {
			if env.EventID != "" {
				stream.mu.Lock()
				stream.lastEvent = env.EventID
				stream.mu.Unlock()
			}

			data, err := json.Marshal(env)
			if err != nil {
				return
			}

			msg := WSMessage{
				Type:    MsgChimeraEvent,
				AgentID: stream.taskID,
				Content: string(data),
			}

			// Fan out to all connected clients.
			stream.mu.RLock()
			for client := range stream.clients {
				client.send(msg)
			}
			stream.mu.RUnlock()
		})

		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("chimera SSE stream for %s: %v", stream.taskID, err)
			select {
			case <-ctx.Done():
				return
			default:
			}
		} else {
			return
		}
	}
}
