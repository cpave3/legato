package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// MCPConnectionService is this instance's registry of upstream MCP servers.
// Its mutations are operator operations: the route table classes them
// never, so the Reeve cannot make them, and integration tokens (paired main
// instances included) reach only /api/integrations/.
type MCPConnectionService interface {
	List(ctx context.Context) ([]service.MCPConnectionState, error)
	Get(ctx context.Context, name string) (service.MCPConnectionState, error)
	Add(ctx context.Context, in service.MCPConnectionInput) (service.MCPConnectionState, error)
	Update(ctx context.Context, name string, ch service.MCPConnectionChange) (service.MCPConnectionState, error)
	Remove(ctx context.Context, name string) error
}

// SetMCPConnectionService serves /api/settings/mcp/connections.
func (s *Server) SetMCPConnectionService(svc MCPConnectionService) { s.mcpConnections = svc }

func (s *Server) registerMCPConnectionRoutes(mux *routeMux) {
	mux.HandleFunc("GET /api/settings/mcp/connections", s.mcpConnectionsListHandler())
	mux.HandleFunc("POST /api/settings/mcp/connections", s.mcpConnectionAddHandler())
	mux.HandleFunc("GET /api/settings/mcp/connections/{name}", s.mcpConnectionGetHandler())
	mux.HandleFunc("PATCH /api/settings/mcp/connections/{name}", s.mcpConnectionUpdateHandler())
	mux.HandleFunc("DELETE /api/settings/mcp/connections/{name}", s.mcpConnectionRemoveHandler())
}

func (s *Server) haveMCPConnections(w http.ResponseWriter) bool {
	if s.mcpConnections == nil {
		s.writeError(w, http.StatusServiceUnavailable, "MCP connections are not available on this server")
		return false
	}
	return true
}

func (s *Server) writeMCPConnectionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrMCPConnectionInvalid):
		s.writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrMCPConnectionExists), errors.Is(err, service.ErrMCPConnectionStale), errors.Is(err, service.ErrMCPConnectionForeign):
		s.writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "no MCP connection of that name")
	default:
		s.writeError(w, http.StatusInternalServerError, "MCP connections could not be read or saved")
	}
}

// decodeMCPBody refuses unknown fields, so no token or secret field can be
// slipped in alongside the connection's metadata.
func decodeMCPBody(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func (s *Server) mcpConnectionsListHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.haveMCPConnections(w) {
			return
		}
		all, err := s.mcpConnections.List(r.Context())
		if err != nil {
			s.writeMCPConnectionError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"connections": all})
	}
}

func (s *Server) mcpConnectionAddHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.haveMCPConnections(w) {
			return
		}
		var in service.MCPConnectionInput
		if err := decodeMCPBody(r, &in); err != nil {
			s.writeError(w, http.StatusBadRequest, "send {name, url, issuer?, auth?}")
			return
		}
		added, err := s.mcpConnections.Add(r.Context(), in)
		if err != nil {
			s.writeMCPConnectionError(w, err)
			return
		}
		s.writeJSON(w, http.StatusCreated, added)
	}
}

func (s *Server) mcpConnectionGetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.haveMCPConnections(w) {
			return
		}
		got, err := s.mcpConnections.Get(r.Context(), r.PathValue("name"))
		if err != nil {
			s.writeMCPConnectionError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, got)
	}
}

func (s *Server) mcpConnectionUpdateHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.haveMCPConnections(w) {
			return
		}
		var ch service.MCPConnectionChange
		if err := decodeMCPBody(r, &ch); err != nil || ch.ExpectedRevision == 0 {
			s.writeError(w, http.StatusBadRequest, "send {url?, issuer?, auth?, enabled?, expected_revision}")
			return
		}
		updated, err := s.mcpConnections.Update(r.Context(), r.PathValue("name"), ch)
		if err != nil {
			s.writeMCPConnectionError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, updated)
	}
}

func (s *Server) mcpConnectionRemoveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.haveMCPConnections(w) {
			return
		}
		if err := s.mcpConnections.Remove(r.Context(), r.PathValue("name")); err != nil {
			s.writeMCPConnectionError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
