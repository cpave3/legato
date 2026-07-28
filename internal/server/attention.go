package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func (s *Server) SetAttentionService(attention *service.AttentionService) {
	s.attention = attention
}

func (s *Server) requireAttentionService(w http.ResponseWriter) bool {
	if s.attention != nil {
		return true
	}
	s.writeError(w, http.StatusServiceUnavailable, "attention service not available")
	return false
}

func (s *Server) attentionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireAttentionService(w) {
			return
		}
		filter := service.AttentionFilter{
			Producer: r.URL.Query().Get("producer"),
			Category: r.URL.Query().Get("category"),
			Urgency:  r.URL.Query().Get("urgency"),
			TaskID:   r.URL.Query().Get("task_id"),
		}
		items, err := s.attention.List(r.Context(), filter)
		if err != nil {
			s.writeAttentionError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, struct {
			Items []service.AttentionItem `json:"items"`
			Total int                     `json:"total"`
		}{Items: items, Total: len(items)})
	}
}

func (s *Server) attentionActionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireAttentionService(w) {
			return
		}
		var request service.AttentionActionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid action request")
			return
		}
		request.ActionID = r.PathValue("action_id")
		if err := s.attention.Execute(r.Context(), r.PathValue("id"), request); err != nil {
			s.writeAttentionError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) attentionDismissHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireAttentionService(w) {
			return
		}
		var request struct {
			ExpectedRevision string `json:"expected_revision"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid dismiss request")
			return
		}
		if err := s.attention.Dismiss(r.Context(), r.PathValue("id"), request.ExpectedRevision); err != nil {
			s.writeAttentionError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) writeAttentionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "attention item not found")
	case errors.Is(err, service.ErrAttentionConflict):
		s.writeError(w, http.StatusConflict, err.Error())
	default:
		s.writeError(w, http.StatusBadRequest, err.Error())
	}
}
