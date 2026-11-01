package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/cpave3/legato/internal/service"
)

// OwnerLookup finds the tasks that own a repository's work.
type OwnerLookup interface {
	Lookup(ctx context.Context, q service.OwnerQuery) ([]service.OwnerMatch, error)
}

// SetOwnerLookup serves GET /api/integrations/owner.
func (s *Server) SetOwnerLookup(owners OwnerLookup) { s.owners = owners }

// Delegations receives integrations' briefs and follow-up messages.
type Delegations interface {
	Delegate(ctx context.Context, req service.DelegationRequest) (service.DelegationStatus, bool, error)
	Status(ctx context.Context, integration, id string) (service.DelegationStatus, error)
	Message(ctx context.Context, integration, id, text, key string) (service.DelegationMessageStatus, bool, error)
}

// SetDelegations serves the delegation routes. Without it they answer 503.
func (s *Server) SetDelegations(d Delegations) { s.delegations = d }

// CaseAttention keeps Argus's cases in Attention.
type CaseAttention interface {
	Upsert(ctx context.Context, integration, key string, push service.CasePush) (service.CaseAttentionResult, error)
	Resolve(ctx context.Context, integration, key, revision, reason string) error
	Sync(ctx context.Context, integration string, keys []string) (int, error)
}

// SetCaseAttention serves the attention routes. Without it they answer 503.
func (s *Server) SetCaseAttention(c CaseAttention) { s.caseAttention = c }

// IntegrationOverview is what the settings show of integrations.
type IntegrationOverview interface {
	Overview(ctx context.Context) (service.IntegrationsOverview, error)
}

// SetIntegrationOverview serves GET /api/settings/integrations, for the full
// token only: it is outside /api/integrations/, where scoped tokens reach.
func (s *Server) SetIntegrationOverview(o IntegrationOverview) { s.integrationOverview = o }

// IntegrationAuthenticator checks an integration's scoped token.
type IntegrationAuthenticator interface {
	Authenticate(ctx context.Context, token string) (service.IntegrationIdentity, error)
}

// SetIntegrationAuth lets integrations reach /api/integrations/* with
// scoped tokens. Without it, only the full auth token reaches them.
func (s *Server) SetIntegrationAuth(auth IntegrationAuthenticator) { s.integrationAuth = auth }

type integrationKey struct{}

// integrationFrom returns the integration that made a request, when it used
// a scoped token.
func integrationFrom(ctx context.Context) (service.IntegrationIdentity, bool) {
	id, ok := ctx.Value(integrationKey{}).(service.IntegrationIdentity)
	return id, ok
}

const integrationPrefix = "/api/integrations/"

func isIntegrationPath(path string) bool { return strings.HasPrefix(path, integrationPrefix) }

// integrationScope is the scope a route under /api/integrations/ needs.
func integrationScope(path string) string {
	if strings.HasPrefix(path, integrationPrefix+"owner") {
		return service.ScopeOwnerRead
	}
	if strings.HasPrefix(path, integrationPrefix+"attention") {
		return service.ScopeAttention
	}
	return service.ScopeDelegate
}

// writeIntegrationError answers an integration route's error as
// {"error": code, "message": text}.
func writeIntegrationError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": message})
}

// serveScopedToken lets a request with an integration token through when the
// token is valid, the route is an integration route, and the token holds its
// scope.
func (s *Server) serveScopedToken(w http.ResponseWriter, r *http.Request, next http.Handler, token string) {
	id, err := s.integrationAuth.Authenticate(r.Context(), token)
	if err != nil {
		writeIntegrationError(w, http.StatusUnauthorized, "unauthorized", "unknown or revoked integration token")
		return
	}
	if !isIntegrationPath(r.URL.Path) {
		writeIntegrationError(w, http.StatusForbidden, "forbidden", "an integration token reaches only /api/integrations/")
		return
	}
	if scope := integrationScope(r.URL.Path); !id.Has(scope) {
		writeIntegrationError(w, http.StatusForbidden, "missing_scope", "this route needs the scope "+scope)
		return
	}
	next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), integrationKey{}, id)))
}

func (s *Server) registerIntegrationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/integrations/owner", s.integrationOwnerHandler())
	mux.HandleFunc("POST /api/integrations/delegations", s.integrationDelegateHandler())
	mux.HandleFunc("GET /api/integrations/delegations/{id}", s.integrationDelegationStatusHandler())
	mux.HandleFunc("POST /api/integrations/delegations/{id}/messages", s.integrationDelegationMessageHandler())
	mux.HandleFunc("PUT /api/integrations/attention/{key}", s.integrationCaseUpsertHandler())
	mux.HandleFunc("POST /api/integrations/attention/{key}/resolve", s.integrationCaseResolveHandler())
	mux.HandleFunc("POST /api/integrations/attention/sync", s.integrationCaseSyncHandler())
	mux.HandleFunc("GET /api/settings/integrations", s.integrationOverviewHandler())
}

func (s *Server) integrationOverviewHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.integrationOverview == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "integrations are not available on this server")
			return
		}
		overview, err := s.integrationOverview.Overview(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSONValue(w, http.StatusOK, overview)
	}
}

func unavailable(w http.ResponseWriter, what string) {
	writeIntegrationError(w, http.StatusServiceUnavailable, "unavailable", what+" is not configured on this Legato")
}

func (s *Server) integrationOwnerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.owners == nil {
			unavailable(w, "owner lookup")
			return
		}
		query := r.URL.Query()
		q := service.OwnerQuery{Repo: query.Get("repo"), Branch: query.Get("branch"), SHA: query.Get("sha")}
		if raw := query.Get("pr"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n <= 0 {
				writeIntegrationError(w, http.StatusBadRequest, "bad_request", "pr must be a pull request number")
				return
			}
			q.PR = n
		}
		matches, err := s.owners.Lookup(r.Context(), q)
		if errors.Is(err, service.ErrOwnerQuery) {
			writeIntegrationError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if err != nil {
			writeIntegrationError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		if len(matches) == 0 {
			writeIntegrationError(w, http.StatusNotFound, "not_found", "no task owns that work in "+q.Repo)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"matches": matches})
	}
}

// requestIntegration is the integration a request acts for: the scoped
// token's, or Argus for the full token.
func requestIntegration(r *http.Request) string {
	if id, ok := integrationFrom(r.Context()); ok {
		return id.Name
	}
	return "argus"
}

// writeDelegationError answers a delegation error with the status its code
// stands for.
func writeDelegationError(w http.ResponseWriter, err error) {
	var de *service.DelegationError
	if !errors.As(err, &de) {
		writeIntegrationError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	status := http.StatusInternalServerError
	switch de.Code {
	case "bad_request":
		status = http.StatusBadRequest
	case "forbidden":
		status = http.StatusForbidden
	case "not_found", "unknown_task":
		status = http.StatusNotFound
	case "unknown_repo", "no_checkout", "not_resumable", "invalid_link":
		status = http.StatusUnprocessableEntity
	case "idempotency_conflict", "not_delivered", "closed":
		status = http.StatusConflict
	case "unavailable":
		status = http.StatusServiceUnavailable
	}
	writeIntegrationError(w, status, de.Code, de.Message)
}

// decodeIntegrationBody reads a JSON body of at most 128 KiB.
func decodeIntegrationBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(v); err != nil {
		writeIntegrationError(w, http.StatusBadRequest, "bad_request", "the body must be JSON of at most 128 KiB: "+err.Error())
		return false
	}
	return true
}

func (s *Server) integrationDelegateHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.delegations == nil {
			unavailable(w, "the Argus integration")
			return
		}
		var body struct {
			CaseID         string                   `json:"caseId"`
			Mode           string                   `json:"mode"`
			Target         service.DelegationTarget `json:"target"`
			Brief          string                   `json:"brief"`
			Title          string                   `json:"title"`
			IdempotencyKey string                   `json:"idempotencyKey"`
		}
		if !decodeIntegrationBody(w, r, &body) {
			return
		}
		status, created, err := s.delegations.Delegate(r.Context(), service.DelegationRequest{
			Integration: requestIntegration(r), CaseID: body.CaseID, Mode: body.Mode, Target: body.Target,
			Brief: body.Brief, Title: body.Title, IdempotencyKey: body.IdempotencyKey,
		})
		if err != nil {
			writeDelegationError(w, err)
			return
		}
		code := http.StatusOK
		if created {
			code = http.StatusCreated
		}
		s.writeJSON(w, code, status)
	}
}

func (s *Server) integrationDelegationStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.delegations == nil {
			unavailable(w, "the Argus integration")
			return
		}
		status, err := s.delegations.Status(r.Context(), requestIntegration(r), r.PathValue("id"))
		if err != nil {
			writeDelegationError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, status)
	}
}

func (s *Server) integrationDelegationMessageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.delegations == nil {
			unavailable(w, "the Argus integration")
			return
		}
		var body struct {
			Text           string `json:"text"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if !decodeIntegrationBody(w, r, &body) {
			return
		}
		status, created, err := s.delegations.Message(r.Context(), requestIntegration(r), r.PathValue("id"), body.Text, body.IdempotencyKey)
		if err != nil {
			writeDelegationError(w, err)
			return
		}
		code := http.StatusOK
		if created {
			code = http.StatusCreated
		}
		s.writeJSON(w, code, status)
	}
}

// decodeCaseBody reads a JSON body of at most limit bytes.
func decodeCaseBody(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(v); err != nil {
		writeIntegrationError(w, http.StatusBadRequest, "bad_request", "the body must be JSON of at most "+strconv.FormatInt(limit>>10, 10)+" KiB: "+err.Error())
		return false
	}
	return true
}

func (s *Server) integrationCaseUpsertHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.caseAttention == nil {
			unavailable(w, "the Argus integration")
			return
		}
		var push service.CasePush
		if !decodeIntegrationBody(w, r, &push) {
			return
		}
		res, err := s.caseAttention.Upsert(r.Context(), requestIntegration(r), r.PathValue("key"), push)
		if err != nil {
			writeDelegationError(w, err)
			return
		}
		writeJSONValue(w, http.StatusOK, res)
	}
}

func (s *Server) integrationCaseResolveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.caseAttention == nil {
			unavailable(w, "the Argus integration")
			return
		}
		var body struct {
			Revision string `json:"revision"`
			Reason   string `json:"reason"`
		}
		if !decodeIntegrationBody(w, r, &body) {
			return
		}
		if err := s.caseAttention.Resolve(r.Context(), requestIntegration(r), r.PathValue("key"), body.Revision, body.Reason); err != nil {
			writeDelegationError(w, err)
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]string{"status": "resolved"})
	}
}

func (s *Server) integrationCaseSyncHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.caseAttention == nil {
			unavailable(w, "the Argus integration")
			return
		}
		var body struct {
			Keys []string `json:"keys"`
		}
		// 10,000 keys of about 45 bytes each.
		if !decodeCaseBody(w, r, 512<<10, &body) {
			return
		}
		n, err := s.caseAttention.Sync(r.Context(), requestIntegration(r), body.Keys)
		if err != nil {
			writeDelegationError(w, err)
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]int{"resolved": n})
	}
}
