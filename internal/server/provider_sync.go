package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

func (s *Server) boardSyncHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.providerSync == nil {
			s.writeError(w, http.StatusServiceUnavailable, "provider sync is not configured")
			return
		}
		result, err := s.providerSync.Sync(r.Context())
		if errors.Is(err, service.ErrProviderSyncInProgress) {
			s.writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}
}
