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

type learningCandidateCounter interface {
	CandidateCount(context.Context) (int, error)
}

// learningCandidates is how many learnings wait for a decision; none without
// a learning service.
func (s *Server) learningCandidates(ctx context.Context) (int, error) {
	counter, ok := s.learning.(learningCandidateCounter)
	if !ok {
		return 0, nil
	}
	return counter.CandidateCount(ctx)
}

// pendingApprovals is how many approvals wait for the person; none without
// an approval service.
func (s *Server) pendingApprovals(ctx context.Context) (int, error) {
	if s.approvals == nil {
		return 0, nil
	}
	return s.approvals.CountPending(ctx)
}

type navigationSummary struct {
	Attention         int `json:"attention"`
	Review            int `json:"review"`
	Plans             int `json:"plans"`
	WorkflowAttention int `json:"workflow_attention"`
	Agents            int `json:"agents"`
	Learning          int `json:"learning"`
	Approvals         int `json:"approvals"`
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
		if summary.Learning, err = s.learningCandidates(r.Context()); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if summary.Approvals, err = s.pendingApprovals(r.Context()); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, summary)
	}
}
