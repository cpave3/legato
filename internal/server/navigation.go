package server

import (
	"context"
	"net/http"
)

type queueCounter interface {
	QueueCount(context.Context) (int, error)
}

type workflowAttentionCounter interface {
	AttentionCount(context.Context) (int, error)
}

type attentionCounter interface {
	Count(context.Context) (int, error)
}

type navigationSummary struct {
	Attention         int `json:"attention"`
	Review            int `json:"review"`
	Plans             int `json:"plans"`
	WorkflowAttention int `json:"workflow_attention"`
	Agents            int `json:"agents"`
}

func (s *Server) navigationSummaryHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.reviews == nil || s.plans == nil || s.workflows == nil || s.attention == nil || s.agents == nil {
			s.writeError(w, http.StatusServiceUnavailable, "navigation summary services not available")
			return
		}

		reviews, ok := s.reviews.(queueCounter)
		if !ok {
			s.writeError(w, http.StatusServiceUnavailable, "review count not available")
			return
		}
		plans, ok := s.plans.(queueCounter)
		if !ok {
			s.writeError(w, http.StatusServiceUnavailable, "plan count not available")
			return
		}
		workflows, ok := s.workflows.(workflowAttentionCounter)
		if !ok {
			s.writeError(w, http.StatusServiceUnavailable, "workflow count not available")
			return
		}
		attention, ok := any(s.attention).(attentionCounter)
		if !ok {
			s.writeError(w, http.StatusServiceUnavailable, "attention count not available")
			return
		}

		summary := navigationSummary{}
		var err error
		if summary.Review, err = reviews.QueueCount(r.Context()); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if summary.Plans, err = plans.QueueCount(r.Context()); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if summary.WorkflowAttention, err = workflows.AttentionCount(r.Context()); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if summary.Attention, err = attention.Count(r.Context()); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		working, waiting, agentErrors, _, err := s.agents.GetAgentSummary(r.Context(), "")
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		summary.Agents = working + waiting + agentErrors
		s.writeJSON(w, http.StatusOK, summary)
	}
}
