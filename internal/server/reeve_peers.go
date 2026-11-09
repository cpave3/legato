package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"slices"
	"sync"
	"time"

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
	OwnApprovals(ctx context.Context) ([]service.PeerApproval, error)
	PeerApprovals(ctx context.Context) (service.InstanceApprovals, error)
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
	mux.HandleFunc("GET "+service.ReeveApprovalsPath, s.reeveOwnApprovalsHandler())
	mux.HandleFunc("GET /api/reeve/peers/approvals", s.reevePeerApprovalsHandler())
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

// reeveOwnApprovalsHandler lists this instance's pending Reeve change sets
// for a paired instance's tray, with a reeve-scoped token.
func (s *Server) reeveOwnApprovalsHandler() http.HandlerFunc {
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
		own, err := s.reevePeers.OwnApprovals(r.Context())
		if err != nil {
			writeIntegrationError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"approvals": own})
	}
}

// reevePeerApprovalsHandler lists paired instances' pending Reeve change
// sets, for this instance's tray.
func (s *Server) reevePeerApprovalsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.reevePeers == nil {
			s.writeJSON(w, http.StatusOK, service.InstanceApprovals{Approvals: []service.InstanceApproval{}, Unreachable: []string{}})
			return
		}
		list, ok := s.peerApprovals.get()
		if !ok {
			s.refreshPeerApprovals(r.Context())
			list, _ = s.peerApprovals.get()
		}
		s.writeJSON(w, http.StatusOK, list)
	}
}

// peerApprovalCache is what paired instances last said of their pending
// Reeve change sets.
type peerApprovalCache struct {
	mu    sync.Mutex
	list  service.InstanceApprovals
	heard bool
}

func (c *peerApprovalCache) get() (service.InstanceApprovals, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.list, c.heard
}

// set keeps list and reports whether its change sets differ from the last.
func (c *peerApprovalCache) set(list service.InstanceApprovals) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := func(l service.InstanceApprovals) []string {
		var ids []string
		for _, a := range l.Approvals {
			ids = append(ids, a.InstanceID+"/"+a.ID)
		}
		return ids
	}
	changed := !slices.Equal(key(c.list), key(list))
	c.list, c.heard = list, true
	return changed
}

// peerApprovalCount is how many change sets wait on paired instances, as
// last heard; the navigation summary counts them.
func (s *Server) peerApprovalCount() int {
	list, _ := s.peerApprovals.get()
	return len(list.Approvals)
}

// refreshPeerApprovals asks the paired instances again, and tells web
// clients when their change sets changed.
func (s *Server) refreshPeerApprovals(ctx context.Context) {
	if s.reevePeers == nil {
		return
	}
	list, err := s.reevePeers.PeerApprovals(ctx)
	if err != nil {
		log.Printf("reeve: paired instances' approvals: %v", err)
		return
	}
	if s.peerApprovals.set(list) {
		s.hub.Broadcast(WSMessage{Type: MsgApprovalsChanged})
	}
}

// StartPeerApprovals asks paired instances for their pending change sets
// every interval until ctx ends.
func (s *Server) StartPeerApprovals(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			s.refreshPeerApprovals(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
