package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// AgentUIService reads what agents asked Legato to show: artifacts for the
// side panel and cards for the conversation. Agents write these through
// `legato mcp serve`, never through this API.
type AgentUIService interface {
	List(ctx context.Context, taskID string) ([]service.AgentUIItem, error)
	Get(ctx context.Context, id string, revision int) (*service.AgentUIItem, error)
	Delete(ctx context.Context, id string) error
}

// SetAgentUIService enables the authenticated agent UI API.
func (s *Server) SetAgentUIService(ui AgentUIService) {
	s.agentUI = ui
}

// StartAgentUIEvents tells web clients when an agent shows, replaces or loses
// an item, so an open panel or conversation draws it without a reload.
func (s *Server) StartAgentUIEvents() {
	if s.bus == nil {
		return
	}
	ch := s.bus.Subscribe(events.EventAgentUIChanged)
	go func() {
		for ev := range ch {
			if p, ok := ev.Payload.(events.AgentUIChangedPayload); ok {
				s.hub.Broadcast(WSMessage{
					Type: MsgAgentUIChanged, TaskID: p.TaskID, ItemID: p.ItemID,
					Placement: p.Placement, Revision: p.Revision, Kind: p.Kind,
				})
			}
		}
	}()
}

func (s *Server) requireAgentUIService(w http.ResponseWriter) bool {
	if s.agentUI == nil {
		s.writeError(w, http.StatusServiceUnavailable, "agent UI is unavailable")
		return false
	}
	return true
}

// agentUIItemsHandler lists what one agent session has shown, without content.
func (s *Server) agentUIItemsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireAgentUIService(w) {
			return
		}
		items, err := s.agentUI.List(r.Context(), r.PathValue("task_id"))
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if items == nil {
			items = []service.AgentUIItem{}
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// agentUIItemHandler reads one item with its content, or removes it. Content
// an agent wrote always leaves here inside JSON: served as a page, its
// scripts would run with Legato's own origin and the person's token.
func (s *Server) agentUIItemHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requireAgentUIService(w) {
			return
		}
		id := r.PathValue("id")
		switch r.Method {
		case http.MethodGet:
			revision := 0
			if raw := r.URL.Query().Get("revision"); raw != "" {
				parsed, err := strconv.Atoi(raw)
				if err != nil || parsed < 1 {
					s.writeError(w, http.StatusBadRequest, "revision must be a positive number")
					return
				}
				revision = parsed
			}
			item, err := s.agentUI.Get(r.Context(), id, revision)
			if err != nil {
				s.writeAgentUIError(w, err)
				return
			}
			w.Header().Set("X-Content-Type-Options", "nosniff")
			s.writeJSON(w, http.StatusOK, item)
		case http.MethodDelete:
			if err := s.agentUI.Delete(r.Context(), id); err != nil {
				s.writeAgentUIError(w, err)
				return
			}
			s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

func (s *Server) writeAgentUIError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		s.writeError(w, http.StatusNotFound, "item not found")
		return
	}
	s.writeError(w, http.StatusInternalServerError, err.Error())
}
