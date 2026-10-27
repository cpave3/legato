package server

import (
	"encoding/json"
	"net/http"
	"runtime/debug"

	"github.com/cpave3/legato/internal/server/static"
)

func healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := HealthResponse{
			Status:  "ok",
			Version: buildVersion(),
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, "encode error", http.StatusInternalServerError)
		}
	}
}

func frontendMetaHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		_ = json.NewEncoder(w).Encode(map[string]string{"build_id": static.BuildID()})
	}
}

func (s *Server) mobileMetaHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		meta := MobileMetaResponse{Version: buildVersion(), MobileShellAPI: 1, AuthBootstrap: "local_storage_v1"}
		if s.pairing != nil {
			meta.PhrasePairing = 1
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(meta)
	}
}

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "dev"
	}
	return info.Main.Version
}
