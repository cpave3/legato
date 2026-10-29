package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// MsgApprovalsChanged tells web clients that an approval was created or closed.
const MsgApprovalsChanged = "approvals_changed"

func (s *Server) SetApprovalService(approvals *service.ApprovalService) {
	s.approvals = approvals
}

// UseAgentEvents takes native agents' events from watcher, which holds one
// stream per agent, so the server opens no stream of its own to read how
// full each agent's context is.
func (s *Server) UseAgentEvents(watcher interface {
	AddListener(func(taskID string, event chimera.EventEnvelope))
}) {
	s.agentContexts.UseExternalEvents()
	watcher.AddListener(s.agentContexts.Observe)
}

func (s *Server) requireApprovalService(w http.ResponseWriter) bool {
	if s.approvals != nil {
		return true
	}
	s.writeError(w, http.StatusServiceUnavailable, "approval service not available")
	return false
}

// approvalsHandler serves GET /api/approvals?status=&task=.
func (s *Server) approvalsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireApprovalService(w) {
			return
		}
		status := r.URL.Query().Get("status")
		if status == "" {
			status = store.ApprovalPending
		}
		if status == "all" {
			status = ""
		}
		approvals, err := s.approvals.List(r.Context(), store.ApprovalFilter{Status: status, TaskID: r.URL.Query().Get("task"), Limit: 200})
		if err != nil {
			s.writeApprovalError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, struct {
			Approvals []service.Approval `json:"approvals"`
		}{Approvals: approvals})
	}
}

// approvalHandler serves GET /api/approvals/{id}.
func (s *Server) approvalHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireApprovalService(w) {
			return
		}
		approval, err := s.approvals.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			s.writeApprovalError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, approval)
	}
}

// approvalDecisionHandler serves POST /api/approvals/{id}/decision.
func (s *Server) approvalDecisionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireApprovalService(w) {
			return
		}
		var decision service.ApprovalDecision
		if err := json.NewDecoder(r.Body).Decode(&decision); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid decision")
			return
		}
		if decision.By == "" {
			decision.By = "web"
		}
		approval, err := s.approvals.Decide(r.Context(), r.PathValue("id"), decision)
		if err != nil {
			s.writeApprovalError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, approval)
	}
}

// approvalDismissHandler serves POST /api/approvals/{id}/dismiss.
func (s *Server) approvalDismissHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireApprovalService(w) {
			return
		}
		var request struct {
			Note string `json:"note"`
			By   string `json:"by"`
		}
		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				s.writeError(w, http.StatusBadRequest, "invalid dismiss request")
				return
			}
		}
		if request.By == "" {
			request.By = "web"
		}
		approval, err := s.approvals.Dismiss(r.Context(), r.PathValue("id"), request.Note, request.By)
		if err != nil {
			s.writeApprovalError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, approval)
	}
}

func (s *Server) writeApprovalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "approval not found")
	case errors.Is(err, service.ErrApprovalDecided):
		s.writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrApprovalInvalid):
		s.writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrApprovalNeedsSignature), errors.Is(err, service.ErrApprovalBadSignature):
		s.writeError(w, http.StatusForbidden, err.Error())
	default:
		s.writeError(w, http.StatusBadGateway, err.Error())
	}
}

// StartApprovalEvents forwards EventApprovalChanged to web clients.
func (s *Server) StartApprovalEvents() {
	if s.bus == nil {
		return
	}
	ch := s.bus.Subscribe(events.EventApprovalChanged)
	go func() {
		for ev := range ch {
			if p, ok := ev.Payload.(events.ApprovalChangedPayload); ok {
				s.hub.Broadcast(WSMessage{Type: MsgApprovalsChanged, ItemID: p.ApprovalID, AgentID: p.TaskID, Kind: p.Kind})
			}
		}
	}()
}
