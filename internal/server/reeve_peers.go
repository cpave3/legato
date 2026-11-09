package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// ReevePeers messages other instances' Reeves and takes their messages
// (service.ReevePeerService).
type ReevePeers interface {
	List(ctx context.Context) ([]service.Peer, error)
	Pair(ctx context.Context, address, phrase string) (service.Peer, error)
	Remove(ctx context.Context, instanceID string) error
	Receive(ctx context.Context, identity service.IntegrationIdentity, msg service.PeerMessage) (service.PeerAnswer, error)
}

// MainReeveScope turns a paired main's reeve scope on or off
// (service.IntegrationTokenService).
type MainReeveScope interface {
	SetMainReeve(ctx context.Context, instanceID string, on bool) error
}

// SetReevePeers serves Reeve to Reeve.
func (s *Server) SetReevePeers(peers ReevePeers, mains MainReeveScope) {
	s.reevePeers, s.mainReeve = peers, mains
}

func (s *Server) registerReevePeerRoutes(mux *routeMux) {
	mux.HandleFunc("POST "+service.ReeveMessagePath, s.reevePeerMessageHandler())
	mux.HandleFunc("GET /api/reeve/peers", s.reevePeersHandler())
	mux.HandleFunc("POST /api/reeve/peers", s.reevePeerPairHandler())
	mux.HandleFunc("DELETE /api/reeve/peers/{instance_id}", s.reevePeerRemoveHandler())
	mux.HandleFunc("PUT /api/settings/mains/{instance_id}/reeve", s.mainReeveHandler())
}

// reevePeerMessageHandler serves the message another instance's Reeve
// sends, with its reeve-scoped token: who sent it is the token's instance.
func (s *Server) reevePeerMessageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := integrationFrom(r.Context())
		if !ok || !identity.Has(service.ScopeReeve) {
			writeIntegrationError(w, http.StatusForbidden, "missing_scope", "this route needs the scope reeve")
			return
		}
		if s.reevePeers == nil {
			unavailable(w, "the Reeve")
			return
		}
		var msg service.PeerMessage
		if json.NewDecoder(r.Body).Decode(&msg) != nil {
			writeIntegrationError(w, http.StatusBadRequest, "bad_request", "the message does not read")
			return
		}
		answer, err := s.reevePeers.Receive(r.Context(), identity, msg)
		switch {
		case errors.Is(err, service.ErrPeerRefused):
			writeIntegrationError(w, http.StatusConflict, "refused", err.Error())
		case err != nil:
			writeIntegrationError(w, http.StatusInternalServerError, "internal", err.Error())
		default:
			writeJSONValue(w, http.StatusOK, answer)
		}
	}
}

func (s *Server) reevePeersHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.reevePeers == nil {
			s.writeJSON(w, http.StatusOK, map[string]any{"peers": []service.Peer{}})
			return
		}
		peers, err := s.reevePeers.List(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if peers == nil {
			peers = []service.Peer{}
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"peers": peers})
	}
}

func (s *Server) reevePeerPairHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.reevePeers == nil {
			s.writeError(w, http.StatusServiceUnavailable, "Reeve pairing is not available here")
			return
		}
		var in struct {
			Address string `json:"address"`
			Phrase  string `json:"phrase"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			s.writeError(w, http.StatusBadRequest, "want {address, phrase}")
			return
		}
		peer, err := s.reevePeers.Pair(r.Context(), in.Address, in.Phrase)
		if err != nil {
			s.writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		s.writeJSON(w, http.StatusCreated, peer)
	}
}

func (s *Server) reevePeerRemoveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.reevePeers == nil {
			s.writeError(w, http.StatusServiceUnavailable, "Reeve pairing is not available here")
			return
		}
		if err := s.reevePeers.Remove(r.Context(), r.PathValue("instance_id")); errors.Is(err, store.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "no such Reeve pairing")
			return
		} else if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
	}
}

// mainReeveHandler serves PUT /api/settings/mains/{instance_id}/reeve
// {enabled}: whether a paired main's Reeve may message this one's.
func (s *Server) mainReeveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.mainReeve == nil {
			s.writeError(w, http.StatusServiceUnavailable, "main instances are not available here")
			return
		}
		var in struct {
			Enabled bool `json:"enabled"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			s.writeError(w, http.StatusBadRequest, "want {enabled}")
			return
		}
		if err := s.mainReeve.SetMainReeve(r.Context(), r.PathValue("instance_id"), in.Enabled); errors.Is(err, service.ErrMainNotPaired) {
			s.writeError(w, http.StatusNotFound, err.Error())
			return
		} else if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]bool{"enabled": in.Enabled})
	}
}
