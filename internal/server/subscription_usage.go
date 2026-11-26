package server

import (
	"context"
	"net/http"
	"sync"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/service"
)

// MsgUsageChanged tells web clients to read /api/usage again.
const MsgUsageChanged = "usage_changed"

// SubscriptionUsageSource is the subscription usage the server serves
// (service.SubscriptionUsageService).
type SubscriptionUsageSource interface {
	// Snapshot is the kept readings; it never reads a provider.
	Snapshot(ctx context.Context) (service.SubscriptionUsage, error)
	// RequestRefresh asks for a read in the background.
	RequestRefresh()
}

// SetSubscriptionUsage serves subscription usage at /api/usage.
func (s *Server) SetSubscriptionUsage(usage SubscriptionUsageSource) { s.usage = usage }

// usageHandler serves GET /api/usage: each provider's kept reading. Without
// a usage service it is an empty list, never an error.
func (s *Server) usageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.usage == nil {
			s.writeJSON(w, http.StatusOK, service.SubscriptionUsage{Providers: []service.ProviderUsage{}})
			return
		}
		usage, err := s.usage.Snapshot(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "could not read subscription usage")
			return
		}
		s.writeJSON(w, http.StatusOK, usage)
	}
}

// usageRefreshHandler serves POST /api/usage/refresh: asks for a read now
// (at most once a minute per provider) and answers at once; the new reading
// arrives as usage_changed.
func (s *Server) usageRefreshHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.usage == nil {
			s.writeError(w, http.StatusServiceUnavailable, "subscription usage is not read here")
			return
		}
		s.usage.RequestRefresh()
		s.writeJSON(w, http.StatusAccepted, map[string]string{"status": "refreshing"})
	}
}

// StartSubscriptionUsageEvents broadcasts usage_changed when this process
// or another keeps a reading.
func (s *Server) StartSubscriptionUsageEvents(bus *events.Bus) func() {
	changes, unsubscribe := bus.SubscribeInvalidations(events.EventSubscriptionUsageChanged, events.EventSubscriptionUsageInvalidated)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-changes:
				s.hub.Broadcast(WSMessage{Type: MsgUsageChanged})
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop); <-done; unsubscribe() }) }
}
