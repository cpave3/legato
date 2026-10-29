package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// SetCredentialBroker gives the server the credential broker, whose
// credentials, grants and audit log the web UI shows.
func (s *Server) SetCredentialBroker(broker *service.CredentialBroker) {
	s.credentials = broker
}

// credentialsHandler serves GET /api/credentials: names, profiles, stores
// and rules, never a value. Empty without a broker.
func (s *Server) credentialsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		list := []service.CredentialSummary{}
		if s.credentials != nil {
			list = s.credentials.Credentials()
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"credentials": list})
	}
}

// credentialGrantsHandler serves GET /api/credentials/grants.
func (s *Server) credentialGrantsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		grants := []service.GrantInfo{}
		if s.credentials != nil {
			var err error
			if grants, err = s.credentials.Grants(r.Context()); err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"grants": grants})
	}
}

// credentialGrantHandler serves DELETE /api/credentials/grants/{id}.
func (s *Server) credentialGrantHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.credentials == nil {
			s.writeError(w, http.StatusNotFound, "grant not found")
			return
		}
		if err := s.credentials.EndGrant(r.Context(), r.PathValue("id"), "web"); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.writeError(w, http.StatusNotFound, "grant not found")
				return
			}
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// credentialAuditHandler serves GET /api/credentials/audit, newest first.
func (s *Server) credentialAuditHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		entries := []store.CredentialAuditEntry{}
		if s.credentials != nil {
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if limit <= 0 || limit > 500 {
				limit = 100
			}
			var err error
			entries, err = s.credentials.Audit(r.Context(), store.CredentialAuditFilter{
				Credential: r.URL.Query().Get("credential"), TaskID: r.URL.Query().Get("task"), ExecutionID: r.URL.Query().Get("execution"), Limit: limit,
			})
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	}
}
