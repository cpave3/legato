package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

// Review actions on an away task's stuck review: a main's routes for its
// web board (POST /api/tasks/{id}/away/review/{another-pass|message}), the
// worker's review passes for the Hand off dialog, and the worker's own
// route the main calls. See docs/ai/integrations.md § Review passes.

// mainReviewActions are a main's review actions (TaskHandoffService).
type mainReviewActions interface {
	AnotherPass(ctx context.Context, taskID string) error
	MessageAuthor(ctx context.Context, taskID, text string) error
	WorkerReviewPasses(ctx context.Context, workerID string) (*service.ReviewPassesAdvert, error)
}

// workerReviewActions are a worker's (HandoffWorkerService).
type workerReviewActions interface {
	ReviewAction(ctx context.Context, mainInstanceID, handoffID, action, text string) error
}

func readReviewText(r *http.Request) string {
	var body struct {
		Text string `json:"text"`
	}
	r.Body = http.MaxBytesReader(nil, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
		return ""
	}
	return body.Text
}

func (s *Server) awayReviewActionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actions, ok := s.instances.mainHandoffs.(mainReviewActions)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "this Legato does not hand tasks off")
			return
		}
		id := r.PathValue("id")
		var err error
		switch r.PathValue("action") {
		case service.ReviewActionAnotherPass:
			err = actions.AnotherPass(r.Context(), id)
		case service.ReviewActionMessage:
			err = actions.MessageAuthor(r.Context(), id, readReviewText(r))
		default:
			writeJSONError(w, http.StatusNotFound, "no such review action")
			return
		}
		if err != nil {
			writeJSONError(w, handOffStatus(err), err.Error())
			return
		}
		s.notifyTaskChanged(id)
		writeJSONValue(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (s *Server) workerReviewPassesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actions, ok := s.instances.mainHandoffs.(mainReviewActions)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "this Legato does not hand tasks off")
			return
		}
		advert, err := actions.WorkerReviewPasses(r.Context(), r.PathValue("instance_id"))
		if err != nil {
			writeJSONError(w, handOffStatus(err), err.Error())
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"review_passes": advert})
	}
}

func (s *Server) taskHandoffReviewHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		main, ok := requireHandoffMain(w, r)
		if !ok {
			return
		}
		actions, ok := s.instances.handoffs.(workerReviewActions)
		if !ok {
			writeIntegrationError(w, http.StatusServiceUnavailable, "unavailable", "this worker does not take handoffs")
			return
		}
		if err := actions.ReviewAction(r.Context(), main.InstanceID, r.PathValue("handoff_id"), r.PathValue("action"), readReviewText(r)); err != nil {
			writeHandoffError(w, err)
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
