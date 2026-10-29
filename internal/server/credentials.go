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
		entries := []service.AuditRow{}
		if s.credentials != nil {
			query := r.URL.Query()
			limit, _ := strconv.Atoi(query.Get("limit"))
			if limit <= 0 || limit > 500 {
				limit = 100
			}
			before, _ := strconv.ParseInt(query.Get("before"), 10, 64)
			var err error
			entries, err = s.credentials.AuditRows(r.Context(), store.CredentialAuditFilter{
				Event: query.Get("event"), Credential: query.Get("credential"), TaskID: query.Get("task"),
				ExecutionID: query.Get("execution"), BeforeSeq: before, Limit: limit,
			})
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	}
}

// credentialAuditVerifyHandler serves GET /api/credentials/audit/verify: whether
// the audit log's hash chain holds, and where it breaks when it does not.
func (s *Server) credentialAuditVerifyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.credentials == nil {
			s.writeJSON(w, http.StatusOK, map[string]any{"intact": true})
			return
		}
		broken, err := s.credentials.VerifyAudit(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if broken != 0 {
			s.writeJSON(w, http.StatusOK, map[string]any{"intact": false, "broken_at": broken})
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"intact": true})
	}
}
