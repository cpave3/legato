package server

import (
	"context"
	"net/http"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/service"
)

// ReviewRuns are the review loops workflow runs started
// (service.ReviewRunCaller).
type ReviewRuns interface {
	// AnotherPass starts the next pass of a run's review step now;
	// refused unless the loop waits for its author.
	AnotherPass(ctx context.Context, runID, stepID string) error
}

// SetReviewRuns serves Run another pass on runs' review steps.
func (s *Server) SetReviewRuns(runs ReviewRuns) { s.reviewRuns = runs }

// reviewAnotherPassHandler is POST
// /api/workflow-runs/{run_id}/steps/{step_id}/another-pass.
func (s *Server) reviewAnotherPassHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.reviewRuns == nil || s.workflows == nil {
			http.Error(w, "review loops are not running on this Legato", http.StatusServiceUnavailable)
			return
		}
		runID, stepID := r.PathValue("run_id"), r.PathValue("step_id")
		if err := s.reviewRuns.AnotherPass(r.Context(), runID, stepID); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		s.NotifyWorkflowChanged(runID, stepID)
		view, err := s.workflows.Show(r.Context(), runID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.writeJSON(w, http.StatusOK, view)
	}
}

// ReviewLoopChanged is the kind of EventWorkflowChanged a run's review loop
// publishes when it moves on (service.ReviewRunCaller).
const ReviewLoopChanged = service.ReviewLoopChangedKind

// StartReviewRunEvents sends the web UI a run's review-loop changes, which
// the service publishes on the bus, so an open run page follows its loop.
// Other workflow events reach the web UI through NotifyWorkflowChanged.
func (s *Server) StartReviewRunEvents() {
	if s.bus == nil {
		return
	}
	go func() {
		ch := s.bus.Subscribe(events.EventWorkflowChanged)
		for event := range ch {
			payload, ok := event.Payload.(events.WorkflowChangedPayload)
			if !ok || payload.Kind != ReviewLoopChanged {
				continue
			}
			s.hub.Broadcast(WSMessage{Type: MsgWorkflowChanged, WorkflowRunID: payload.RunID, WorkflowStepID: payload.StepID})
		}
	}()
}
