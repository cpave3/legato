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

// How many times in a row a native endpoint may refuse its event stream
// before the watcher stops asking. The next agent list starts it again if
// the agent is still running.
const contextWatchAttempts = 3

// agentContextReading is how full a native agent's context window is.
type agentContextReading struct {
	Used   int
	Window int
}

// contextWatcher follows how full the context window of each running native
// agent is, and tells every web client when it changes (agent_context). It
// holds one event stream per agent, read from the agent's native endpoint,
// and takes a first reading from the endpoint's /context.
type contextWatcher struct {
	broadcast func(WSMessage)

	// What the watcher asks of the world; replaced in tests.
	sessions        func(ctx context.Context, taskID string) (*store.AgentSession, error)
	read            func(ctx context.Context, endpoint, sessionID string) (*chimera.SessionContext, error)
	subscribeEvents chimeraSubscribeFunc
	retryDelay      time.Duration
	// external: the agents' events arrive through Observe, from a watcher
	// that already holds each agent's stream, so none is opened here.
	external bool

	mu       sync.Mutex
	watches  map[string]*contextWatch
	readings map[string]agentContextReading
}

type contextWatch struct {
	endpoint  string
	sessionID string
	cancel    context.CancelFunc
}

func newContextWatcher(broadcast func(WSMessage)) *contextWatcher {
	return &contextWatcher{
		broadcast: broadcast,
		read: func(ctx context.Context, endpoint, sessionID string) (*chimera.SessionContext, error) {
			return chimera.NewClient(endpoint).GetSessionContext(ctx, sessionID)
		},
		subscribeEvents: func(ctx context.Context, endpoint, sessionID, since string, onEvent func(chimera.EventEnvelope)) error {
			return chimera.NewClient(endpoint).Subscribe(ctx, sessionID, since, onEvent)
		},
		retryDelay: time.Second,
		watches:    make(map[string]*contextWatch),
		readings:   make(map[string]agentContextReading),
	}
}

// Reconcile watches exactly the given agents: the running agents that have a
// native endpoint. A watch whose agent is gone, or whose endpoint changed, is
// stopped.
func (w *contextWatcher) Reconcile(taskIDs []string) {
	if w == nil || w.sessions == nil {
		return
	}
	wanted := make(map[string]bool, len(taskIDs))
	for _, taskID := range taskIDs {
		wanted[taskID] = true
	}

	w.mu.Lock()
	for taskID, watch := range w.watches {
		if !wanted[taskID] {
			watch.cancel()
			delete(w.watches, taskID)
			delete(w.readings, taskID)
		}
	}
	w.mu.Unlock()

	for taskID := range wanted {
		session, err := w.sessions(context.Background(), taskID)
		if err != nil || session.ChimeraEndpoint == nil || session.ChimeraSessionID == nil {
			continue
		}
		w.ensure(taskID, *session.ChimeraEndpoint, *session.ChimeraSessionID)
	}
}

func (w *contextWatcher) ensure(taskID, endpoint, sessionID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if watch, ok := w.watches[taskID]; ok {
		if watch.endpoint == endpoint && watch.sessionID == sessionID {
			return
		}
		watch.cancel()
		delete(w.readings, taskID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	watch := &contextWatch{endpoint: endpoint, sessionID: sessionID, cancel: cancel}
	w.watches[taskID] = watch
	go w.follow(ctx, taskID, watch)
}

// UseExternalEvents stops the watcher opening event streams of its own: the
// events are passed to Observe instead.
func (w *contextWatcher) UseExternalEvents() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.external = true
}

// Observe takes one event of a watched agent's stream.
func (w *contextWatcher) Observe(taskID string, event chimera.EventEnvelope) {
	reading, ok := contextFromEvent(event)
	if !ok {
		return
	}
	w.mu.Lock()
	watch := w.watches[taskID]
	w.mu.Unlock()
	if watch != nil {
		w.record(taskID, watch, reading)
	}
}

func (w *contextWatcher) follow(ctx context.Context, taskID string, watch *contextWatch) {
	if reading, err := w.read(ctx, watch.endpoint, watch.sessionID); err == nil && reading.LastPromptTokens != nil && reading.ContextWindow > 0 {
		w.record(taskID, watch, agentContextReading{Used: *reading.LastPromptTokens, Window: reading.ContextWindow})
	}
	w.mu.Lock()
	external := w.external
	w.mu.Unlock()
	if external {
		return
	}

	failures := 0
	for ctx.Err() == nil {
		received := false
		err := w.subscribeEvents(ctx, watch.endpoint, watch.sessionID, "", func(event chimera.EventEnvelope) {
			received = true
			if reading, ok := contextFromEvent(event); ok {
				w.record(taskID, watch, reading)
			}
		})
		if ctx.Err() != nil {
			return
		}
		if received {
			failures = 0
		} else if failures++; failures >= contextWatchAttempts {
			if err != nil {
				log.Printf("agent context for %s: %v", taskID, err)
			}
			w.forget(taskID, watch)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.retryDelay):
		}
	}
}

// contextFromEvent reads a usage_updated event, which every native endpoint
// sends when the context fill changes.
func contextFromEvent(event chimera.EventEnvelope) (agentContextReading, bool) {
	if event.Type != "usage_updated" {
		return agentContextReading{}, false
	}
	var data struct {
		Used   *int `json:"usedContextTokens"`
		Window int  `json:"contextWindow"`
	}
	if err := json.Unmarshal(event.Data, &data); err != nil || data.Used == nil || data.Window <= 0 {
		return agentContextReading{}, false
	}
	return agentContextReading{Used: *data.Used, Window: data.Window}, true
}

// record keeps a reading of the watch that is still current, and tells the
// clients when it differs from the last.
func (w *contextWatcher) record(taskID string, watch *contextWatch, reading agentContextReading) {
	w.mu.Lock()
	if w.watches[taskID] != watch || w.readings[taskID] == reading {
		w.mu.Unlock()
		return
	}
	w.readings[taskID] = reading
	w.mu.Unlock()
	w.broadcast(WSMessage{Type: MsgAgentContext, AgentID: taskID, ContextUsed: reading.Used, ContextWindow: reading.Window})
}

func (w *contextWatcher) forget(taskID string, watch *contextWatch) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.watches[taskID] == watch {
		watch.cancel()
		delete(w.watches, taskID)
	}
}

// Context is the last reading of an agent's context fill.
func (w *contextWatcher) Context(taskID string) (agentContextReading, bool) {
	if w == nil {
		return agentContextReading{}, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	reading, ok := w.readings[taskID]
	return reading, ok
}

func (w *contextWatcher) watching(taskID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.watches[taskID]
	return ok
}

// Stop ends every watch.
func (w *contextWatcher) Stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for taskID, watch := range w.watches {
		watch.cancel()
		delete(w.watches, taskID)
	}
}
