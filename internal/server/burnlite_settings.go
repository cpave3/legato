package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/cpave3/legato/internal/service"
)

// BurnliteSettings connects Legato to Burnlite from the web UI and keeps its
// projects (service.BurnliteSettings).
type BurnliteSettings interface {
	Overview(ctx context.Context) (service.BurnliteOverview, error)
	Begin(ctx context.Context, address, redirectURI string) (string, error)
	Callback(ctx context.Context, redirected string) error
	SaveProjects(ctx context.Context, projects []string) (service.BurnliteOverview, error)
	Disconnect(ctx context.Context) error
}

// SetBurnliteSettings serves Burnlite's settings and its connect flow.
func (s *Server) SetBurnliteSettings(b BurnliteSettings) { s.burnliteSettings = b }

func (s *Server) registerBurnliteRoutes(mux *routeMux) {
	mux.HandleFunc("POST /api/settings/integrations/burnlite/connect", s.burnliteConnectHandler())
	mux.HandleFunc("PUT /api/settings/integrations/burnlite", s.burnliteProjectsHandler())
	mux.HandleFunc("DELETE /api/settings/integrations/burnlite", s.burnliteDisconnectHandler())
	// The browser comes back here from Burnlite: it carries no token, and is
	// checked by the state of a connection this server began.
	mux.HandleFunc("GET /oauth/callback", s.burnliteCallbackHandler())
	mux.HandleFunc("GET /api/ticket-providers", s.ticketProvidersHandler())
}

// requestOrigin is the scheme and host the request came to.
func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func burnliteStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrBurnliteInput):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrSecretsNeedPassphrase), errors.Is(err, service.ErrProjectsFromConfig):
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

func (s *Server) burnliteConnectHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.burnliteSettings == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "Burnlite settings are not available on this server")
			return
		}
		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		authorize, err := s.burnliteSettings.Begin(r.Context(), req.URL, requestOrigin(r)+"/oauth/callback")
		if err != nil {
			writeJSONError(w, burnliteStatus(err), err.Error())
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]string{"authorize_url": authorize})
	}
}

func (s *Server) burnliteCallbackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		back := url.Values{"integration": {"burnlite"}, "result": {"connected"}}
		if s.burnliteSettings == nil {
			back.Set("result", "failed")
			back.Set("error", "Burnlite settings are not available on this server")
		} else if err := s.burnliteSettings.Callback(r.Context(), requestOrigin(r)+r.URL.RequestURI()); err != nil {
			back.Set("result", "failed")
			back.Set("error", err.Error())
		}
		http.Redirect(w, r, "/settings?"+encodeOrdered(back)+"#integrations", http.StatusFound)
	}
}

// encodeOrdered writes integration, result, then error, so the address reads
// in that order.
func encodeOrdered(v url.Values) string {
	out := "integration=" + url.QueryEscape(v.Get("integration")) + "&result=" + url.QueryEscape(v.Get("result"))
	if e := v.Get("error"); e != "" {
		out += "&error=" + url.QueryEscape(e)
	}
	return out
}

func (s *Server) burnliteProjectsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.burnliteSettings == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "Burnlite settings are not available on this server")
			return
		}
		var req struct {
			Projects []string `json:"projects"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		overview, err := s.burnliteSettings.SaveProjects(r.Context(), req.Projects)
		if err != nil {
			writeJSONError(w, burnliteStatus(err), err.Error())
			return
		}
		writeJSONValue(w, http.StatusOK, overview)
	}
}

func (s *Server) burnliteDisconnectHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.burnliteSettings == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "Burnlite settings are not available on this server")
			return
		}
		if err := s.burnliteSettings.Disconnect(r.Context()); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]string{"disconnected": "burnlite"})
	}
}

// ticketProvidersHandler lists the connected ticket providers with a site,
// for the browser extension: where it shows its import button.
func (s *Server) ticketProvidersHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providers := []service.TicketProviderInfo{}
		if lister, ok := s.sync.(service.TicketProviderLister); ok {
			if listed := lister.TicketProviders(r.Context()); listed != nil {
				providers = listed
			}
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"providers": providers})
	}
}
