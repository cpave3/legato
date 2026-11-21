package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// SetWorkStageService enables the agent stage routes: the operator's
// override and an agent's stage history.
func (s *Server) SetWorkStageService(stages *service.WorkStageService) {
	s.workStages = stages
}

// StartAgentStageEvents tells web clients when an agent's stage changes, as
// agent_stage_changed.
func (s *Server) StartAgentStageEvents() {
	if s.bus == nil {
		return
	}
	ch := s.bus.Subscribe(events.EventAgentStageChanged)
	go func() {
		for ev := range ch {
			if p, ok := ev.Payload.(events.AgentStageChangedPayload); ok {
				s.hub.Broadcast(WSMessage{
					Type: MsgAgentStageChanged, TaskID: service.AgentSlotKey(p.TaskID, p.Slot), ExecutionID: p.ExecutionID,
					Stage: p.Stage, StageReason: p.Reason, StageSource: p.Source,
				})
			}
		}
	}()
}

// AgentStageResponse is an agent's stage, as PUT /api/agents/{id}/stage
// answers it.
type AgentStageResponse struct {
	Stage       string     `json:"stage"`
	StageReason string     `json:"stage_reason,omitempty"`
	StageSource string     `json:"stage_source"`
	StageSince  *time.Time `json:"stage_since,omitempty"`
}

// AgentStageEvent is one change in an agent's stage history.
type AgentStageEvent struct {
	FromStage string     `json:"from_stage,omitempty"`
	Stage     string     `json:"stage"`
	Reason    string     `json:"reason,omitempty"`
	Source    string     `json:"source"`
	At        *time.Time `json:"at,omitempty"`
}

func stageTime(s string) *time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	return &t
}

func (s *Server) requireWorkStages(w http.ResponseWriter) bool {
	if s.workStages == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "agent stages are unavailable")
		return false
	}
	return true
}

// agentStagePlace is the place of the agent the path's task_id addresses,
// or false after writing why there is none.
func (s *Server) agentStagePlace(w http.ResponseWriter, r *http.Request) (service.WorkStagePlace, bool) {
	place, _, err := s.workStages.PlaceOfAgent(r.Context(), r.PathValue("task_id"))
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
		return place, false
	case err != nil:
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return place, false
	}
	return place, true
}

// agentStageHandler is PUT /api/agents/{task_id}/stage: the operator sets
// the agent's stage, {"stage": "...", "reason": "..."}.
func (s *Server) agentStageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireWorkStages(w) {
			return
		}
		var req struct {
			Stage  string `json:"stage"`
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		place, ok := s.agentStagePlace(w, r)
		if !ok {
			return
		}
		stage, err := s.workStages.SetByOperator(r.Context(), place, req.Stage, req.Reason)
		switch {
		case errors.Is(err, service.ErrInvalidWorkStage), errors.Is(err, service.ErrNoWorkStage):
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		case err != nil:
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSONValue(w, http.StatusOK, AgentStageResponse{
			Stage: stage.Stage, StageReason: stage.Reason, StageSource: stage.Source, StageSince: stageTime(stage.Since),
		})
	}
}

// agentStageHistoryHandler is GET /api/agents/{task_id}/stage-history: the
// agent's stage changes, newest first (?limit=, 100 unless asked).
func (s *Server) agentStageHistoryHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireWorkStages(w) {
			return
		}
		limit := 0
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 {
				writeJSONError(w, http.StatusBadRequest, "limit must be a positive number")
				return
			}
			limit = min(n, 500)
		}
		place, ok := s.agentStagePlace(w, r)
		if !ok {
			return
		}
		history, err := s.workStages.History(r.Context(), place, limit)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]AgentStageEvent, 0, len(history))
		for _, ev := range history {
			out = append(out, AgentStageEvent{FromStage: ev.FromStage, Stage: ev.Stage, Reason: ev.Reason, Source: ev.Source, At: stageTime(ev.CreatedAt)})
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"events": out})
	}
}
