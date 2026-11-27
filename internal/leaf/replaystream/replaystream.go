// Package replaystream is a stream of numbered events served as Server-Sent
// Events: it keeps the newest events for a subscriber that reconnects with a
// cursor, fans each event out to every subscriber, and drops a subscriber
// that falls too far behind rather than wait for it. A dropped subscriber
// reconnects and replays from its cursor. The stream knows nothing of what
// its events say.
//
// A Stream is not safe for concurrent use. Its owner serializes every call,
// normally under the lock that already guards what it publishes, so that
// what a new subscriber replays and what it then receives live never overlap
// or leave a gap. Subscriber.Serve touches no Stream state and runs outside
// that lock.
package replaystream

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Event is one published event: its ID, which is also its replay cursor, and
// its encoded data.
type Event struct {
	ID   string
	Data []byte
}

// Stream keeps and fans out one source's events.
type Stream struct {
	prefix      string
	limit       int
	buffer      int
	sequence    int64
	retained    []Event
	subscribers map[*Subscriber]struct{}
}

// New returns a stream whose event IDs are "<prefix>-<n>", counting from 1.
// A prefix unique to this stream makes a cursor from another stream unknown
// here. It keeps the newest limit events for replay and lets a subscriber
// fall at most buffer events behind before dropping it.
func New(prefix string, limit, buffer int) *Stream {
	return &Stream{prefix: prefix, limit: limit, buffer: buffer, subscribers: map[*Subscriber]struct{}{}}
}

// NextID spends the next event ID. Its owner stamps the ID into the event
// before encoding it and then publishes it; an event that cannot be encoded
// is not published, and its ID is not used again.
func (s *Stream) NextID() string {
	s.sequence++
	return fmt.Sprintf("%s-%d", s.prefix, s.sequence)
}

// Publish keeps the event for replay and hands it to every subscriber. A
// subscriber whose backlog is full is dropped and its channel closed; the
// publisher never waits.
func (s *Stream) Publish(id string, data []byte) {
	published := Event{ID: id, Data: data}
	s.retained = append(s.retained, published)
	if len(s.retained) > s.limit {
		// Copied, so the evicted events' backing array can be freed.
		s.retained = append([]Event(nil), s.retained[len(s.retained)-s.limit:]...)
	}
	for subscriber := range s.subscribers {
		select {
		case subscriber.events <- published:
		default:
			delete(s.subscribers, subscriber)
			close(subscriber.events)
		}
	}
}

// Subscribe adds a subscriber and returns it with its replay: the kept events
// that follow cursor, which its Serve writes before any live event. An
// unknown or empty cursor replays nothing: the subscriber starts live and
// hydrates elsewhere.
func (s *Stream) Subscribe(cursor string) (*Subscriber, []Event) {
	subscriber := &Subscriber{events: make(chan Event, s.buffer)}
	replay := s.after(cursor)
	subscriber.replay = replay
	s.subscribers[subscriber] = struct{}{}
	return subscriber, replay
}

// Unsubscribe removes a subscriber, closing its channel if publishing has not
// already dropped it, and returns how many subscribers are left.
func (s *Stream) Unsubscribe(subscriber *Subscriber) int {
	if _, subscribed := s.subscribers[subscriber]; subscribed {
		delete(s.subscribers, subscriber)
		close(subscriber.events)
	}
	return len(s.subscribers)
}

// Viewers is how many subscribers are attached. One dropped for falling
// behind no longer counts, though its connection may not have ended yet.
func (s *Stream) Viewers() int {
	return len(s.subscribers)
}

func (s *Stream) after(cursor string) []Event {
	if cursor == "" {
		return nil
	}
	for index := len(s.retained) - 1; index >= 0; index-- {
		if s.retained[index].ID == cursor {
			return append([]Event(nil), s.retained[index+1:]...)
		}
	}
	return nil
}

// Subscriber is one attached reader of a stream.
type Subscriber struct {
	events chan Event
	replay []Event
}

// Events delivers the subscriber's live events. It is closed when the
// subscriber is dropped or unsubscribed.
func (s *Subscriber) Events() <-chan Event {
	return s.events
}

// Serve writes the subscriber's replay and then its live events as a
// Server-Sent Events response, with a comment line every heartbeat while
// nothing else is written. It returns when ctx ends, when closed is closed,
// when the subscriber's channel is closed, or when a write fails. The caller
// has already checked that w can be flushed; it unsubscribes afterwards.
func (s *Subscriber) Serve(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, closed <-chan struct{}, heartbeat time.Duration) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	write := func(event Event) bool {
		if _, err := fmt.Fprintf(w, "id: %s\ndata: %s\n\n", event.ID, event.Data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	for _, event := range s.replay {
		if !write(event) {
			return
		}
	}
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-closed:
			return
		case event, open := <-s.events:
			if !open || !write(event) {
				return
			}
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
