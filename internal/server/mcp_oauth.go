package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"mime"
	"net/http"
	"strings"

	"github.com/cpave3/legato/internal/engine/egress"
	"github.com/cpave3/legato/internal/engine/oauthclient"
	"github.com/cpave3/legato/internal/service"
)

// MCPOAuth authorizes the operator's MCP connections: the host's
// OAuthFlowService. Every call names the owner the server derives from the
// request's credential; a request body never says who it is for.
type MCPOAuth interface {
	Begin(ctx context.Context, r service.OAuthFlowRequest) (service.OAuthFlowBegin, error)
	Status(ctx context.Context, id, owner string) (service.OAuthFlowStatus, error)
	Deliver(ctx context.Context, id, owner, key string, response oauthclient.AuthorizationResponse) (service.OAuthFlowStatus, error)
	Confirm(ctx context.Context, id, owner string) error
	Cancel(ctx context.Context, id, owner string) error
	Disconnect(ctx context.Context, connection string) (service.OAuthDisconnect, error)
}

// SetMCPOAuth serves /api/mcp/: connecting, confirming and disconnecting
// the operator's MCP connections.
func (s *Server) SetMCPOAuth(flows MCPOAuth) { s.mcpOAuth = flows }

func (s *Server) registerMCPOAuthRoutes(mux *routeMux) {
	mux.HandleFunc("POST /api/mcp/connections/{name}/oauth/begin", s.mcpOAuthBeginHandler())
	mux.HandleFunc("POST /api/mcp/connections/{name}/oauth/disconnect", s.mcpOAuthDisconnectHandler())
	mux.HandleFunc("GET /api/mcp/oauth/attempts/{id}", s.mcpOAuthStatusHandler())
	mux.HandleFunc("POST /api/mcp/oauth/attempts/{id}/deliver", s.mcpOAuthDeliverHandler())
	mux.HandleFunc("POST /api/mcp/oauth/attempts/{id}/confirm", s.mcpOAuthConfirmHandler())
	mux.HandleFunc("POST /api/mcp/oauth/attempts/{id}/cancel", s.mcpOAuthCancelHandler())
}

// mcpOAuthOwner is who an attempt belongs to: the operator credential the
// request was authenticated with (only it reaches these routes), hashed so
// the token itself is never kept. A new operator token owns none of the old
// one's attempts, which expire on their own.
func (s *Server) mcpOAuthOwner(r *http.Request) string {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if s.authToken == "" {
		token = ""
	}
	sum := sha256.Sum256([]byte("legato-mcp-oauth-owner\x00" + token))
	return "operator:" + hex.EncodeToString(sum[:16])
}

// mcpOAuthReady answers for the routes when there is no flow service, and
// refuses a change not sent as JSON: a page of another origin can send a
// form or text post without a preflight, never a JSON one.
func (s *Server) mcpOAuthReady(w http.ResponseWriter, r *http.Request) bool {
	if s.mcpOAuth == nil {
		s.writeError(w, http.StatusServiceUnavailable, "MCP connections cannot be authorized on this server")
		return false
	}
	if r.Method != http.MethodGet {
		if ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || ct != "application/json" {
			s.writeError(w, http.StatusUnsupportedMediaType, "send application/json")
			return false
		}
	}
	return true
}

// writeMCPOAuthError says what went wrong in Legato's words: provider and
// upstream text never reaches the page.
func (s *Server) writeMCPOAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrOAuthFlowInput):
		s.writeError(w, http.StatusBadRequest, "no MCP connection of that name needs authorizing, or the mode or redirect is not one Legato accepts")
	case errors.Is(err, service.ErrOAuthFlowRefused):
		s.writeError(w, http.StatusConflict, "this authorization attempt is over, has expired or is not yours; start again")
	case errors.Is(err, service.ErrOAuthRegistrationUnsupported):
		s.writeError(w, http.StatusUnprocessableEntity, service.ErrOAuthRegistrationUnsupported.Error())
	case errors.Is(err, service.ErrOAuthSecretStoreLocked):
		s.writeError(w, http.StatusConflict, service.ErrOAuthSecretStoreLocked.Error())
	case errors.Is(err, egress.ErrDenied):
		s.writeError(w, http.StatusBadGateway, "Legato's network policy does not allow this server's address")
	case errors.Is(err, oauthclient.ErrNoServerMetadata):
		s.writeError(w, http.StatusBadGateway, "the server publishes no OAuth metadata Legato can use")
	case egress.Classify(err) == "tls":
		log.Printf("mcp oauth: %v", err)
		s.writeError(w, http.StatusBadGateway, "the server's certificate was not trusted")
	default:
		log.Printf("mcp oauth: %v", err)
		s.writeError(w, http.StatusBadGateway, "the MCP server or its authorization server could not be reached, or did not answer as OAuth requires")
	}
}

func (s *Server) mcpOAuthBeginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.mcpOAuthReady(w, r) {
			return
		}
		var in struct {
			Mode        string `json:"mode"`
			RedirectURI string `json:"redirect_uri,omitempty"`
			Scope       string `json:"scope,omitempty"`
		}
		if err := decodeMCPBody(r, &in); err != nil {
			s.writeError(w, http.StatusBadRequest, `send {mode: "web"} or {mode: "app", redirect_uri}`)
			return
		}
		begun, err := s.mcpOAuth.Begin(r.Context(), service.OAuthFlowRequest{Connection: r.PathValue("name"), Scope: in.Scope, Owner: s.mcpOAuthOwner(r), Mode: in.Mode, RedirectURI: in.RedirectURI})
		if err != nil {
			s.writeMCPOAuthError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, begun)
	}
}

func (s *Server) mcpOAuthStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.mcpOAuthReady(w, r) {
			return
		}
		status, err := s.mcpOAuth.Status(r.Context(), r.PathValue("id"), s.mcpOAuthOwner(r))
		if err != nil {
			s.writeMCPOAuthError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, status)
	}
}

// mcpOAuthDeliverHandler takes the authorization response a native app's
// receiver caught; the return key it was given at begin is its capability.
func (s *Server) mcpOAuthDeliverHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.mcpOAuthReady(w, r) {
			return
		}
		var in struct {
			ReturnKey string `json:"return_key"`
			oauthclient.AuthorizationResponse
		}
		if err := decodeMCPBody(r, &in); err != nil {
			s.writeError(w, http.StatusBadRequest, "send {return_key, state, iss?, code or error}")
			return
		}
		status, err := s.mcpOAuth.Deliver(r.Context(), r.PathValue("id"), s.mcpOAuthOwner(r), in.ReturnKey, in.AuthorizationResponse)
		if err != nil {
			s.writeMCPOAuthError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, status)
	}
}

func (s *Server) mcpOAuthConfirmHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.mcpOAuthReady(w, r) {
			return
		}
		id, owner := r.PathValue("id"), s.mcpOAuthOwner(r)
		if err := s.mcpOAuth.Confirm(r.Context(), id, owner); err != nil {
			s.writeMCPOAuthError(w, err)
			return
		}
		status, err := s.mcpOAuth.Status(r.Context(), id, owner)
		if err != nil {
			s.writeMCPOAuthError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, status)
	}
}

func (s *Server) mcpOAuthCancelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.mcpOAuthReady(w, r) {
			return
		}
		if err := s.mcpOAuth.Cancel(r.Context(), r.PathValue("id"), s.mcpOAuthOwner(r)); err != nil {
			s.writeMCPOAuthError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) mcpOAuthDisconnectHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.mcpOAuthReady(w, r) {
			return
		}
		out, err := s.mcpOAuth.Disconnect(r.Context(), r.PathValue("name"))
		if err != nil {
			s.writeMCPOAuthError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, out)
	}
}
