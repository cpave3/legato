package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/cpave3/legato/config"
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
			s.writeJSON(w, http.StatusOK, service.SubscriptionUsage{Providers: []service.ProviderUsage{}, Catalog: service.UsageCatalog()})
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
	return s.broadcastInvalidations(bus, MsgUsageChanged, events.EventSubscriptionUsageChanged, events.EventSubscriptionUsageInvalidated)
}

// usagePinHandler changes policy only after its owner acknowledges the write.
func (s *Server) usagePinHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		control, ok := s.usage.(interface {
			Pin(context.Context, *service.UsagePinRequest) error
		})
		if !ok {
			s.writeError(w, http.StatusServiceUnavailable, "usage policy is unavailable")
			return
		}
		var pin *service.UsagePinRequest
		if r.Method == http.MethodPut {
			var input service.UsagePinRequest
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&input); err != nil {
				s.writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if err := decoder.Decode(&struct{}{}); err != io.EOF {
				s.writeError(w, http.StatusBadRequest, "expected one pin object")
				return
			}
			pin = &input
		} else if r.Method != http.MethodDelete {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := control.Pin(r.Context(), pin); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// usageDefaultsHandler serves GET /api/usage/defaults?repo=owner/name: the
// current defaults for NEW work in that repository (none: the global
// policy), with the repository default and the reason behind them. It does
// not describe an agent already running, which keeps the identity it
// started with.
func (s *Server) usageDefaultsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		source, ok := s.usage.(interface {
			DefaultsFor(ctx context.Context, repo string) (service.UsageSelection, error)
		})
		if !ok {
			s.writeError(w, http.StatusServiceUnavailable, "usage policy is unavailable")
			return
		}
		repo := strings.TrimSpace(r.URL.Query().Get("repo"))
		if repo != "" {
			if _, valid := config.NormalizeRepository(repo); !valid {
				s.writeError(w, http.StatusBadRequest, "repo must name a repository, such as owner/name")
				return
			}
		}
		sel, err := source.DefaultsFor(r.Context(), repo)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, sel)
	}
}
