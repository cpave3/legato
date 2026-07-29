package server

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/engine/store"
)

const (
	chimeraReplayLimit = 1000
	chimeraStreamGrace = 30 * time.Second
	chimeraRetryMax    = 5 * time.Second
)

type chimeraSubscribeFunc func(context.Context, string, string, string, func(chimera.EventEnvelope)) error

type chimeraStreamManager struct {
	agents interface {
		HasChimeraEndpoint(ctx context.Context, taskID string) bool
		GetAgentSession(ctx context.Context, taskID string) (*store.AgentSession, error)
	}

	mu              sync.Mutex
	streams         map[string]*chimeraStream
	subscribeEvents chimeraSubscribeFunc
	gracePeriod     time.Duration
}

type chimeraStream struct {
	taskID    string
	endpoint  string
	sessionID string
	ctx       context.Context
	cancel    context.CancelFunc

	mu           sync.Mutex
	clients      map[*wsClient]*chimeraSubscriber
	events       []chimera.EventEnvelope
	lastEvent    string
	cleanupTimer *time.Timer
}

type chimeraSubscriber struct {
	messages chan WSMessage
}

func newChimeraStreamManager() *chimeraStreamManager {
	return &chimeraStreamManager{
		streams:     make(map[string]*chimeraStream),
		gracePeriod: chimeraStreamGrace,
		subscribeEvents: func(ctx context.Context, endpoint, sessionID, since string, onEvent func(chimera.EventEnvelope)) error {
			return chimera.NewClient(endpoint).Subscribe(ctx, sessionID, since, onEvent)
		},
	}
}

// subscribe registers an ordered writer before upstream delivery starts. A
// cursorless subscription to an existing stream requires a fresh snapshot,
// because there is no exact boundary between that snapshot and retained events.
func (m *chimeraStreamManager) subscribe(client *wsClient, taskID, cursor string) {
	m.mu.Lock()
	stream, exists := m.streams[taskID]
	created := false
	if !exists {
		session, err := m.agents.GetAgentSession(context.Background(), taskID)
		if err != nil || session.ChimeraEndpoint == nil || session.ChimeraSessionID == nil {
			m.mu.Unlock()
			client.send(WSMessage{Type: MsgError, Error: "no chimera endpoint"})
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		stream = &chimeraStream{
			taskID:    taskID,
			endpoint:  *session.ChimeraEndpoint,
			sessionID: *session.ChimeraSessionID,
			ctx:       ctx,
			cancel:    cancel,
			clients:   make(map[*wsClient]*chimeraSubscriber),
		}
		m.streams[taskID] = stream
		created = true
	}
	m.mu.Unlock()

	subscriber := &chimeraSubscriber{messages: make(chan WSMessage, chimeraReplayLimit+16)}
	stream.mu.Lock()
	if stream.cleanupTimer != nil {
		stream.cleanupTimer.Stop()
		stream.cleanupTimer = nil
	}
	stream.clients[client] = subscriber
	replay, cursorKnown := stream.eventsAfter(cursor)
	if !created && cursor == "" {
		subscriber.messages <- WSMessage{Type: MsgChimeraSyncRequired, AgentID: taskID}
	} else if cursor != "" && !cursorKnown {
		subscriber.messages <- WSMessage{Type: MsgChimeraSyncRequired, AgentID: taskID}
	} else {
		for _, event := range replay {
			subscriber.messages <- chimeraWSMessage(taskID, event)
		}
	}
	stream.mu.Unlock()
	go writeChimeraMessages(client, subscriber.messages)

	if created {
		go m.runSSELoop(stream)
	}
}

func writeChimeraMessages(client *wsClient, messages <-chan WSMessage) {
	for message := range messages {
		client.send(message)
	}
}

func (s *chimeraStream) eventsAfter(cursor string) ([]chimera.EventEnvelope, bool) {
	if cursor == "" {
		return nil, true
	}
	for index := len(s.events) - 1; index >= 0; index-- {
		if s.events[index].EventID == cursor {
			return append([]chimera.EventEnvelope(nil), s.events[index+1:]...), true
		}
	}
	return nil, false
}

func (m *chimeraStreamManager) unsubscribe(client *wsClient, taskID string) {
	m.mu.Lock()
	stream, ok := m.streams[taskID]
	m.mu.Unlock()
	if !ok {
		return
	}
	stream.mu.Lock()
	if subscriber, subscribed := stream.clients[client]; subscribed {
		delete(stream.clients, client)
		close(subscriber.messages)
	}
	if len(stream.clients) == 0 && stream.cleanupTimer == nil {
		stream.cleanupTimer = time.AfterFunc(m.gracePeriod, func() { m.removeIfIdle(taskID, stream) })
	}
	stream.mu.Unlock()
}

func (m *chimeraStreamManager) unsubscribeAll(client *wsClient) {
	m.mu.Lock()
	taskIDs := make([]string, 0)
	for taskID, stream := range m.streams {
		stream.mu.Lock()
		_, subscribed := stream.clients[client]
		stream.mu.Unlock()
		if subscribed {
			taskIDs = append(taskIDs, taskID)
		}
	}
	m.mu.Unlock()
	for _, taskID := range taskIDs {
		m.unsubscribe(client, taskID)
	}
}

func (m *chimeraStreamManager) removeIfIdle(taskID string, stream *chimeraStream) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.streams[taskID] != stream {
		return
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	stream.cleanupTimer = nil
	if len(stream.clients) != 0 {
		return
	}
	stream.cancel()
	delete(m.streams, taskID)
}

func (m *chimeraStreamManager) runSSELoop(stream *chimeraStream) {
	retryDelay := 100 * time.Millisecond
	failures := 0
	for stream.ctx.Err() == nil {
		stream.mu.Lock()
		since := stream.lastEvent
		stream.mu.Unlock()
		received := false
		err := m.subscribeEvents(stream.ctx, stream.endpoint, stream.sessionID, since, func(event chimera.EventEnvelope) {
			received = true
			m.publish(stream, event)
		})
		if stream.ctx.Err() != nil {
			return
		}
		if received {
			failures = 0
			retryDelay = 100 * time.Millisecond
		} else {
			failures++
		}
		if err != nil {
			log.Printf("chimera SSE stream for %s: %v", stream.taskID, err)
		}
		if failures >= 3 {
			m.requireSync(stream)
			failures = 0
		}
		select {
		case <-stream.ctx.Done():
			return
		case <-time.After(retryDelay):
		}
		retryDelay = min(retryDelay*2, chimeraRetryMax)
	}
}

func (m *chimeraStreamManager) publish(stream *chimeraStream, event chimera.EventEnvelope) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if event.EventID != "" {
		if event.EventID == stream.lastEvent {
			return
		}
		stream.lastEvent = event.EventID
	}
	stream.events = append(stream.events, event)
	if len(stream.events) > chimeraReplayLimit {
		stream.events = append([]chimera.EventEnvelope(nil), stream.events[len(stream.events)-chimeraReplayLimit:]...)
	}
	message := chimeraWSMessage(stream.taskID, event)
	for client, subscriber := range stream.clients {
		select {
		case subscriber.messages <- message:
		default:
			delete(stream.clients, client)
			close(subscriber.messages)
			client.cancel()
		}
	}
}

func (m *chimeraStreamManager) requireSync(stream *chimeraStream) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	stream.lastEvent = ""
	stream.events = nil
	message := WSMessage{Type: MsgChimeraSyncRequired, AgentID: stream.taskID}
	for _, subscriber := range stream.clients {
		select {
		case subscriber.messages <- message:
		default:
		}
	}
}

func chimeraWSMessage(taskID string, event chimera.EventEnvelope) WSMessage {
	data, err := json.Marshal(event)
	if err != nil {
		return WSMessage{Type: MsgError, Error: "marshal chimera event: " + err.Error()}
	}
	return WSMessage{Type: MsgChimeraEvent, AgentID: taskID, Content: string(data)}
}
