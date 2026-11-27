package claudebridge

import (
	"encoding/json"
	"net/http"
	"time"
)

const (
	// replayLimit bounds the events kept for a reconnecting subscriber.
	replayLimit = 2000
	// subscriberBuffer bounds one subscriber's backlog before it is dropped;
	// it then reconnects and replays from its cursor.
	subscriberBuffer = replayLimit + 64
	heartbeatEvery   = 15 * time.Second
)

// publish stamps native events with their envelope (eventId, sessionId, ts)
// and hands them to the stream, which keeps them for replay and fans them
// out. The caller holds b.mu.
func (b *Bridge) publish(events []nativeEvent) {
	for _, event := range events {
		id := b.stream.NextID()
		event["eventId"] = id
		event["sessionId"] = b.taskID
		event["ts"] = b.now().UnixMilli()
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		b.stream.Publish(id, data)
	}
}

// handleEvents serves the native event stream. Each viewer counts: the mod
// is told how many are attached, and when the last one leaves every open
// permission prompt goes back to the terminal.
func (b *Bridge) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	b.mu.Lock()
	subscriber, _ := b.stream.Subscribe(r.URL.Query().Get("since"))
	viewers := b.stream.Viewers()
	b.mu.Unlock()
	b.notifyViewers(viewers)
	defer func() {
		b.mu.Lock()
		viewers := b.stream.Unsubscribe(subscriber)
		if viewers == 0 {
			// Nobody is left to answer in Legato: every open permission
			// prompt goes back to the terminal.
			b.publish(b.session.expirePermissions())
			b.releaseSettledWaiters()
		}
		b.mu.Unlock()
		b.notifyViewers(viewers)
	}()
	subscriber.Serve(r.Context(), w, flusher, b.closed, heartbeatEvery)
}
