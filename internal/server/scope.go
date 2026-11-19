package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// scopeMaxBodyBytes bounds a batch: 20 pins with a 500 KB screenshot each,
// base64 encoded, and their context.
const scopeMaxBodyBytes = 16 << 20

// ScopeService is Legato Scope's pool, as the extension uses it. See
// docs/ai/scope.md.
type ScopeService interface {
	SendBatch(ctx context.Context, origin string, pins []service.ScopePinInput) (service.ScopeBatchResult, error)
	ListOrigin(ctx context.Context, origin string) ([]service.ScopePin, error)
	Reopen(ctx context.Context, pinID, comment string) (service.ScopePin, error)
	Withdraw(ctx context.Context, pinID string) error
	Screenshot(ctx context.Context, pinID string) ([]byte, string, error)
}

// SetScopeService enables the Scope API.
func (s *Server) SetScopeService(scope ScopeService) {
	s.scope = scope
}

// StartScopeEvents tells web clients (the extension's service worker) when
// an origin's pins change, so pages show their states without polling.
func (s *Server) StartScopeEvents() {
	if s.bus == nil {
		return
	}
	ch := s.bus.Subscribe(events.EventScopeChanged)
	go func() {
		for ev := range ch {
			if p, ok := ev.Payload.(events.ScopeChangedPayload); ok {
				s.hub.Broadcast(WSMessage{Type: MsgScopeChanged, Origin: p.Origin})
			}
		}
	}()
}

func (s *Server) requireScope(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return false
	}
	if s.scope == nil {
		s.writeError(w, http.StatusServiceUnavailable, "Scope is unavailable")
		return false
	}
	return true
}

// writeScopeError answers a ScopeService error with its status.
func (s *Server) writeScopeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, service.ErrScopeTooLarge):
		status = http.StatusRequestEntityTooLarge
	case errors.Is(err, service.ErrScopeInvalidOrigin), errors.Is(err, service.ErrScopeInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, service.ErrScopePinState):
		status = http.StatusConflict
	case errors.Is(err, store.ErrNotFound):
		status = http.StatusNotFound
	}
	s.writeError(w, status, err.Error())
}

// scopeBatchRequest is a batch as the extension posts it: each pin's
// screenshot is a data: URL.
type scopeBatchRequest struct {
	Origin string           `json:"origin"`
	Pins   []scopePinRequest `json:"pins"`
}

type scopePinRequest struct {
	service.ScopePinInput
	Screenshot string `json:"screenshot,omitempty"`
}

// decodeScopeScreenshot reads a base64 data: URL of an image.
func decodeScopeScreenshot(dataURL string) ([]byte, error) {
	header, payload, ok := strings.Cut(dataURL, ",")
	if !ok || !strings.HasPrefix(header, "data:image/") || !strings.HasSuffix(header, ";base64") {
		return nil, errors.New("a screenshot must be a base64 data: URL of an image")
	}
	if base64.StdEncoding.DecodedLen(len(payload)) > service.ScopeMaxScreenshotBytes+3 {
		return nil, service.ErrScopeTooLarge
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, errors.New("a screenshot's base64 does not decode")
	}
	return data, nil
}

// scopeBatchesHandler is POST /api/scope/batches: the extension's Send.
func (s *Server) scopeBatchesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requireScope(w, r, http.MethodPost) {
			return
		}
		var req scopeBatchRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, scopeMaxBodyBytes)).Decode(&req); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				s.writeError(w, http.StatusRequestEntityTooLarge, "the batch is larger than Legato takes (16 MB)")
				return
			}
			s.writeError(w, http.StatusBadRequest, "invalid batch: "+err.Error())
			return
		}
		pins := make([]service.ScopePinInput, len(req.Pins))
		for i, pin := range req.Pins {
			pins[i] = pin.ScopePinInput
			if pin.Screenshot == "" {
				continue
			}
			data, err := decodeScopeScreenshot(pin.Screenshot)
			if errors.Is(err, service.ErrScopeTooLarge) {
				s.writeError(w, http.StatusRequestEntityTooLarge, "a screenshot is larger than 500 KB")
				return
			}
			if err != nil {
				s.writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			pins[i].Screenshot = data
		}
		result, err := s.scope.SendBatch(r.Context(), req.Origin, pins)
		if err != nil {
			s.writeScopeError(w, err)
			return
		}
		s.writeJSON(w, http.StatusCreated, result)
	}
}

// scopePinsHandler is GET /api/scope/pins?origin=: an origin's pins.
func (s *Server) scopePinsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requireScope(w, r, http.MethodGet) {
			return
		}
		pins, err := s.scope.ListOrigin(r.Context(), r.URL.Query().Get("origin"))
		if err != nil {
			s.writeScopeError(w, err)
			return
		}
		if pins == nil {
			pins = []service.ScopePin{}
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"pins": pins})
	}
}

// scopeReopenHandler is POST /api/scope/pins/{id}/reopen with a new comment.
func (s *Server) scopeReopenHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requireScope(w, r, http.MethodPost) {
			return
		}
		var req struct {
			Comment string `json:"comment"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
			return
		}
		pin, err := s.scope.Reopen(r.Context(), r.PathValue("id"), req.Comment)
		if err != nil {
			s.writeScopeError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, pin)
	}
}

// scopeWithdrawHandler is POST /api/scope/pins/{id}/withdraw.
func (s *Server) scopeWithdrawHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requireScope(w, r, http.MethodPost) {
			return
		}
		if err := s.scope.Withdraw(r.Context(), r.PathValue("id")); err != nil {
			s.writeScopeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// scopeScreenshotHandler is GET /api/scope/pins/{id}/screenshot.
func (s *Server) scopeScreenshotHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requireScope(w, r, http.MethodGet) {
			return
		}
		data, mediaType, err := s.scope.Screenshot(r.Context(), r.PathValue("id"))
		if err != nil {
			s.writeScopeError(w, err)
			return
		}
		w.Header().Set("Content-Type", mediaType)
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, max-age=3600")
		_, _ = w.Write(data)
	}
}
