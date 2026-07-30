package server

import (
	"encoding/json"
	"net/http"
)

func (s *Server) workflowTriggerRequestHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.workflowTriggers == nil {
			s.writeError(w, http.StatusServiceUnavailable, "workflow triggers unavailable")
			return
		}
		requestID := r.PathValue("request_id")
		switch r.Method {
		case http.MethodGet:
			request, err := s.workflowTriggers.Request(r.Context(), requestID)
			if err != nil {
				s.writeError(w, http.StatusNotFound, err.Error())
				return
			}
			s.writeJSON(w, http.StatusOK, request)
		case http.MethodPost:
			var body struct {
				Inputs   map[string]string `json:"inputs"`
				Remember bool              `json:"remember"`
				Skip     bool              `json:"skip"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				s.writeError(w, http.StatusBadRequest, "invalid request")
				return
			}
			if body.Skip {
				if err := s.workflowTriggers.Skip(r.Context(), requestID); err != nil {
					s.writeError(w, http.StatusConflict, err.Error())
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			request, err := s.workflowTriggers.SupplyInputs(r.Context(), requestID, body.Inputs, body.Remember)
			if err != nil {
				s.writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			s.writeJSON(w, http.StatusOK, request)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}
