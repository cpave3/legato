package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// HandoffService is an optional review capability, separate from diff review.
type HandoffService interface {
	Handoff(context.Context, string) (*service.HandoffView, error)
	SaveHandoffNote(context.Context, string, string, service.HandoffNote) (*service.HandoffNote, error)
	ExportHandoff(context.Context, string, service.HandoffExportInput) (*service.HandoffExport, error)
}

func (s *Server) handoffHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireHandoffService(w) {
			return
		}
		view, err := s.handoffs.Handoff(r.Context(), r.PathValue("tour_id"))
		if err != nil {
			s.writeHandoffError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, view)
	}
}

func (s *Server) handoffNoteHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireHandoffService(w) {
			return
		}
		input, err := decodeHandoffRequest[service.HandoffNote](w, r)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		tourID := r.PathValue("tour_id")
		note, err := s.handoffs.SaveHandoffNote(r.Context(), tourID, r.PathValue("packet_id"), *input)
		if err != nil {
			s.writeHandoffError(w, err)
			return
		}
		s.hub.Broadcast(WSMessage{Type: MsgReviewChanged, TourID: tourID, Kind: "handoff_note"})
		s.writeJSON(w, http.StatusOK, note)
	}
}

func (s *Server) handoffExportHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireHandoffService(w) {
			return
		}
		input, err := decodeHandoffRequest[service.HandoffExportInput](w, r)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		export, err := s.handoffs.ExportHandoff(r.Context(), r.PathValue("tour_id"), *input)
		if err != nil {
			s.writeHandoffError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, export)
	}
}

func decodeHandoffRequest[T any](w http.ResponseWriter, r *http.Request) (*T, error) {
	r.Body = http.MaxBytesReader(w, r.Body, service.MaxHandoffBytes)
	var input *T
	if err := decodeJSON(r, &input); err != nil {
		return nil, err
	}
	if input == nil {
		return nil, errors.New("JSON object is required")
	}
	return input, nil
}

func (s *Server) writeHandoffError(w http.ResponseWriter, err error) {
	var validation *service.HandoffValidationError
	switch {
	case errors.As(err, &validation):
		s.writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "handoff or review not found")
	case errors.Is(err, service.ErrHandoffConflict), errors.Is(err, store.ErrConflict):
		s.writeError(w, http.StatusConflict, err.Error())
	default:
		s.writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func (s *Server) requireHandoffService(w http.ResponseWriter) bool {
	if s.handoffs != nil {
		return true
	}
	s.writeError(w, http.StatusServiceUnavailable, "handoff service not available")
	return false
}
