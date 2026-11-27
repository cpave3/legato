package server

import (
	"sync"

	"github.com/cpave3/legato/internal/engine/events"
)

// broadcastInvalidations sends msgType to every client each time one of the
// event types is published (coalesced by the bus). The returned stop is
// idempotent and returns only once the goroutine has ended.
func (s *Server) broadcastInvalidations(bus *events.Bus, msgType string, types ...events.EventType) func() {
	changes, unsubscribe := bus.SubscribeInvalidations(types...)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-changes:
				s.hub.Broadcast(WSMessage{Type: msgType})
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop); <-done; unsubscribe() }) }
}
