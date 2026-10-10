package claudebridge

import (
	"encoding/json"
	"fmt"
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

// envelope is one published native event with its replay cursor.
type envelope struct {
	id   string
	data []byte
}

// publish stamps native events, retains them for replay and fans them out.
// The caller holds b.mu.
func (b *Bridge) publish(events []nativeEvent) {
	for _, event := range events {
		b.sequence++
		id := fmt.Sprintf("%s-%d", b.instance, b.sequence)
		event["eventId"] = id
		event["sessionId"] = b.taskID
		event["ts"] = b.now().UnixMilli()
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		published := envelope{id: id, data: data}
		b.retained = append(b.retained, published)
		if len(b.retained) > replayLimit {
			b.retained = append([]envelope(nil), b.retained[len(b.retained)-replayLimit:]...)
		}
		for subscriber := range b.subscribers {
			select {
			case subscriber <- published:
			default:
				delete(b.subscribers, subscriber)
				close(subscriber)
			}
		}
	}
}

// eventsAfter returns the retained events that follow cursor. An unknown or
// empty cursor replays nothing: the subscriber starts live and hydrates from a
// snapshot.
func (b *Bridge) eventsAfter(cursor string) []envelope {
	if cursor == "" {
		return nil
	}
	for index := len(b.retained) - 1; index >= 0; index-- {
		if b.retained[index].id == cursor {
			return append([]envelope(nil), b.retained[index+1:]...)
		}
	}
	return nil
}

func (b *Bridge) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	subscriber := make(chan envelope, subscriberBuffer)
	b.mu.Lock()
	replay := b.eventsAfter(r.URL.Query().Get("since"))
	b.subscribers[subscriber] = struct{}{}
	viewers := len(b.subscribers)
	b.mu.Unlock()
	b.notifyViewers(viewers)
	defer func() {
		b.mu.Lock()
		if _, subscribed := b.subscribers[subscriber]; subscribed {
			delete(b.subscribers, subscriber)
			close(subscriber)
		}
		viewers := len(b.subscribers)
		if viewers == 0 {
			// Nobody is left to answer in Legato: every open permission
			// prompt goes back to the terminal.
			b.publish(b.session.expirePermissions())
			b.releaseSettledWaiters()
		}
		b.mu.Unlock()
		b.notifyViewers(viewers)
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	write := func(event envelope) bool {
		if _, err := fmt.Fprintf(w, "id: %s\ndata: %s\n\n", event.id, event.data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	for _, event := range replay {
		if !write(event) {
			return
		}
	}
	heartbeat := time.NewTicker(heartbeatEvery)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-b.closed:
			return
		case event, open := <-subscriber:
			if !open || !write(event) {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
